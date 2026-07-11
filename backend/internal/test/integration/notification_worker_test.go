//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

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
