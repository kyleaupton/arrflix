//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	notificationworker "github.com/kyleaupton/arrflix/internal/jobs/notification"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// stubAdapter stands in for a real channel adapter under the in_app name so the
// worker routes to it. err is what its Deliver returns — nil for the happy path,
// a typed error to exercise the retry/kill branches.
type stubAdapter struct{ err error }

func (stubAdapter) Name() model.NotificationChannel                           { return model.ChannelInApp }
func (stubAdapter) IsConfigured(context.Context) bool                         { return true }
func (s stubAdapter) Deliver(context.Context, model.NotificationOutbox) error { return s.err }

// enqueueInApp writes one queued in_app row for a fresh user and returns both.
func enqueueInApp(t *testing.T, ctx context.Context, r *repo.Repository, email string) (model.User, model.NotificationOutbox) {
	t.Helper()
	user := newNotifUser(t, ctx, r, email)
	row, err := r.EnqueueOutbox(ctx, repo.EnqueueOutboxParams{
		EventType:       "want.available",
		Audience:        string(model.AudienceUser),
		RecipientUserID: &user.ID,
		Channel:         string(model.ChannelInApp),
		Payload:         json.RawMessage(`{"media":{"title":"Sentinel","year":2024}}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return user, row
}

// TestNotificationWorker_Delivers proves the real in_app adapter path: a Drain
// pass claims the queued row, "delivers" it (the no-op in_app adapter), marks it
// delivered, and the row then surfaces in the user's bell inbox.
func TestNotificationWorker_Delivers(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	user, row := enqueueInApp(t, ctx, r, "notif-worker-deliver@test.local")

	w, err := notificationworker.New(r, logger.New(false), notifications.InAppAdapter{})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	n, err := w.Drain(ctx)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 1 {
		t.Fatalf("delivered = %d, want 1", n)
	}

	inbox, err := r.ListInbox(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].ID != row.ID || inbox[0].Status != string(model.OutboxDelivered) {
		t.Fatalf("inbox = %+v, want the row delivered", inbox)
	}
	// Nothing due after delivery — a second drain is a no-op.
	if n, err := w.Drain(ctx); err != nil || n != 0 {
		t.Fatalf("second drain = %d (err %v), want 0", n, err)
	}
}

// TestNotificationWorker_RetriesTransient proves a retryable delivery failure
// requeues the row with backoff: status returns to queued, attempts increments,
// next_attempt_at moves into the future (so the same drain won't re-pick it), and
// the failure reason is recorded.
func TestNotificationWorker_RetriesTransient(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	_, row := enqueueInApp(t, ctx, r, "notif-worker-transient@test.local")

	// BadGateway is retryable by default in the typed-error model.
	w, err := notificationworker.New(r, logger.New(false), stubAdapter{err: apperrors.BadGatewayf("indexer blip")})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	if n, _ := w.Drain(ctx); n != 0 {
		t.Fatalf("delivered = %d, want 0 (transient failure)", n)
	}

	got := fetchOutbox(t, ctx, r, row.ID)
	if got.Status != string(model.OutboxQueued) {
		t.Fatalf("status = %q, want queued", got.Status)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.Attempts)
	}
	if !got.NextAttemptAt.After(row.NextAttemptAt) {
		t.Fatalf("next_attempt_at = %v, want after original %v", got.NextAttemptAt, row.NextAttemptAt)
	}
	if got.LastError == nil || *got.LastError == "" {
		t.Fatalf("last_error should record the failure")
	}
	// Backed off into the future, so it's no longer due.
	if due, _ := r.ListDueOutbox(ctx, 10); containsOutbox(due, row.ID) {
		t.Fatalf("row should not be due after backoff")
	}
}

// TestNotificationWorker_KillsPermanent proves a non-retryable delivery failure
// goes straight to dead — no retry, no lingering queued row.
func TestNotificationWorker_KillsPermanent(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	_, row := enqueueInApp(t, ctx, r, "notif-worker-permanent@test.local")

	w, err := notificationworker.New(r, logger.New(false), stubAdapter{err: apperrors.BadGatewayf("bad endpoint").NotRetryable()})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	if n, _ := w.Drain(ctx); n != 0 {
		t.Fatalf("delivered = %d, want 0 (permanent failure)", n)
	}

	got := fetchOutbox(t, ctx, r, row.ID)
	if got.Status != string(model.OutboxDead) {
		t.Fatalf("status = %q, want dead", got.Status)
	}
	if got.LastError == nil {
		t.Fatalf("last_error should record the failure")
	}
}

// TestNotificationWorker_ReclaimsStaleDelivering proves the crash-window reaper:
// a row claimed by a worker that died before settling it is stranded in
// 'delivering' (ListDueOutbox only selects 'queued'), and a later drain returns
// it to the queue and delivers it rather than losing the notification forever.
func TestNotificationWorker_ReclaimsStaleDelivering(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	user, row := enqueueInApp(t, ctx, r, "notif-worker-reap@test.local")

	// Claim the row the way the worker does, then abandon it — the state a crash
	// between the claim and a terminal transition leaves behind.
	if _, err := r.MarkOutboxDelivering(ctx, row.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if due, _ := r.ListDueOutbox(ctx, 10); containsOutbox(due, row.ID) {
		t.Fatalf("a claimed row must not be due — the reaper is what recovers it")
	}
	// Age the claim past the reap window so it reads as wedged rather than in
	// flight (the worker's window is 5 minutes).
	staleClaim(t, ctx, pool, row.ID, "10 minutes")

	w, err := notificationworker.New(r, logger.New(false), notifications.InAppAdapter{})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	// One pass reaps the row back to queued and delivers it in the same drain.
	if n, err := w.Drain(ctx); err != nil || n != 1 {
		t.Fatalf("drain = %d (err %v), want 1 delivered after reclaim", n, err)
	}

	got := fetchOutbox(t, ctx, r, row.ID)
	if got.Status != string(model.OutboxDelivered) {
		t.Fatalf("status = %q, want delivered", got.Status)
	}
	// The reclaim counts as an attempt: the row was handed to an adapter once
	// already and may even have been sent before the crash.
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (the reclaim)", got.Attempts)
	}
	inbox, err := r.ListInbox(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].ID != row.ID {
		t.Fatalf("inbox = %+v, want the reclaimed row", inbox)
	}
}

// TestNotificationWorker_LeavesFreshDelivering proves the reaper respects the
// lease: a row claimed moments ago is genuinely in flight, and reclaiming it
// would hand the same notification to a second adapter while the first is still
// sending it.
func TestNotificationWorker_LeavesFreshDelivering(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	_, row := enqueueInApp(t, ctx, r, "notif-worker-fresh-claim@test.local")

	if _, err := r.MarkOutboxDelivering(ctx, row.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}

	w, err := notificationworker.New(r, logger.New(false), notifications.InAppAdapter{})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	if n, err := w.Drain(ctx); err != nil || n != 0 {
		t.Fatalf("drain = %d (err %v), want 0 — a fresh claim is in flight", n, err)
	}

	got := fetchOutbox(t, ctx, r, row.ID)
	if got.Status != string(model.OutboxDelivering) {
		t.Fatalf("status = %q, want delivering (untouched)", got.Status)
	}
	if got.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0 — an in-flight row was not reclaimed", got.Attempts)
	}
}

// staleClaim backdates a claimed row's claimed_at by a Postgres interval, so a
// test can age a claim past the reap window without waiting or reaching into the
// worker's unexported lease.
func staleClaim(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, interval string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		"UPDATE notification_outbox SET claimed_at = now() - $2::interval WHERE id = $1",
		id, interval,
	); err != nil {
		t.Fatalf("backdate claimed_at: %v", err)
	}
}

// fetchOutbox reloads a row by id — the way to assert on a settled non-delivered
// row (queued-after-backoff, dead), which the inbox/due list filters exclude.
func fetchOutbox(t *testing.T, ctx context.Context, r *repo.Repository, id uuid.UUID) model.NotificationOutbox {
	t.Helper()
	row, err := r.GetOutbox(ctx, id)
	if err != nil {
		t.Fatalf("get outbox %s: %v", id, err)
	}
	return row
}
