//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// newNotifUser creates a distinct active user for a notification test. Email is
// parameterized so parallel tests don't collide on the unique constraint.
func newNotifUser(t *testing.T, ctx context.Context, r *repo.Repository, email string) model.User {
	t.Helper()
	u, err := r.CreateUser(ctx, repo.CreateUserParams{
		Email:    email,
		Username: email,
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return u
}

// TestNotification_OutboxLifecycle walks a user-audience in_app row through the
// full v1 delivery path — enqueue → due poll → delivering → delivered → bell
// inbox → unread count → mark-read — asserting the persistence floor and the
// 0024 migration hold together end to end.
func TestNotification_OutboxLifecycle(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	user := newNotifUser(t, ctx, r, "notif-lifecycle@test.local")
	payload := json.RawMessage(`{"title":"Sentinel","year":2024}`)

	row, err := r.EnqueueOutbox(ctx, repo.EnqueueOutboxParams{
		EventType:       "want.available",
		Audience:        string(model.AudienceUser),
		RecipientUserID: &user.ID,
		Channel:         string(model.ChannelInApp),
		Payload:         payload,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if row.Status != string(model.OutboxQueued) {
		t.Fatalf("status = %q, want queued", row.Status)
	}
	if row.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0", row.Attempts)
	}
	if row.RecipientUserID == nil || *row.RecipientUserID != user.ID {
		t.Fatalf("recipient = %v, want %s", row.RecipientUserID, user.ID)
	}
	// JSONB stores parsed and may reorder keys, so compare decoded values, not bytes.
	if !sameJSON(t, row.Payload, payload) {
		t.Fatalf("payload = %s, want equivalent to %s", row.Payload, payload)
	}
	if row.DedupKey != nil || row.DeliveredAt != nil || row.ReadAt != nil {
		t.Fatalf("dedup/delivered/read should be nil on enqueue, got %v/%v/%v", row.DedupKey, row.DeliveredAt, row.ReadAt)
	}

	// Worker poll picks it up (queued and due), then claims it.
	due, err := r.ListDueOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if !containsOutbox(due, row.ID) {
		t.Fatalf("due poll missing the queued row")
	}
	if _, err := r.MarkOutboxDelivering(ctx, row.ID); err != nil {
		t.Fatalf("mark delivering: %v", err)
	}
	delivered, err := r.MarkOutboxDelivered(ctx, row.ID)
	if err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if delivered.Status != string(model.OutboxDelivered) || delivered.DeliveredAt == nil {
		t.Fatalf("delivered row = %q / %v, want delivered + timestamp", delivered.Status, delivered.DeliveredAt)
	}

	// Bell inbox surfaces the delivered row and counts it unread.
	inbox, err := r.ListInbox(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].ID != row.ID {
		t.Fatalf("inbox = %d rows, want the delivered one", len(inbox))
	}
	if n, err := r.CountUnreadInbox(ctx, user.ID); err != nil || n != 1 {
		t.Fatalf("unread = %d (err %v), want 1", n, err)
	}

	// Mark-read drops the unread count but keeps the row as history.
	if err := r.MarkInboxRead(ctx, row.ID, user.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if n, err := r.CountUnreadInbox(ctx, user.ID); err != nil || n != 0 {
		t.Fatalf("unread after read = %d (err %v), want 0", n, err)
	}
	after, err := r.ListInbox(ctx, user.ID, 10)
	if err != nil || len(after) != 1 || after[0].ReadAt == nil {
		t.Fatalf("inbox after read = %d rows, read_at %v; want 1 row with read_at set", len(after), lastReadAt(after))
	}
}

// TestNotification_DedupIndex proves the partial unique index: a repeat enqueue
// under the same (recipient, channel, dedup_key) collides while the prior row is
// still pending, NULL keys never collide, and the key frees up once the prior row
// leaves the queued/delivering window.
func TestNotification_DedupIndex(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	user := newNotifUser(t, ctx, r, "notif-dedup@test.local")
	key := "hygiene.error_finding.lib-1"
	mk := func(dedup *string) (model.NotificationOutbox, error) {
		return r.EnqueueOutbox(ctx, repo.EnqueueOutboxParams{
			EventType:       "hygiene.error_finding",
			Audience:        string(model.AudienceUser),
			RecipientUserID: &user.ID,
			Channel:         string(model.ChannelInApp),
			Payload:         json.RawMessage(`{}`),
			DedupKey:        dedup,
		})
	}

	first, err := mk(&key)
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	// Second with the same key, while the first is still queued → unique violation,
	// surfaced as a Conflict by repo.FromPg.
	if _, err := mk(&key); !apperrors.IsConflict(err) {
		t.Fatalf("duplicate keyed enqueue: err = %v, want Conflict", err)
	}
	// NULL dedup keys never collide — two uncoalesced rows coexist.
	if _, err := mk(nil); err != nil {
		t.Fatalf("first null-key enqueue: %v", err)
	}
	if _, err := mk(nil); err != nil {
		t.Fatalf("second null-key enqueue should not collide: %v", err)
	}
	// Once the first row leaves the queued/delivering window, the key frees up.
	if _, err := r.MarkOutboxDelivered(ctx, first.ID); err != nil {
		t.Fatalf("mark first delivered: %v", err)
	}
	if _, err := mk(&key); err != nil {
		t.Fatalf("re-enqueue after prior settled should succeed: %v", err)
	}
}

// TestNotification_Preferences proves the columnar preference writes: one row per
// (user, bundle), each write touches only its own column (so email doesn't clobber
// push), untouched columns stay NULL (defer to defaults), and re-setting a column
// flips it in place rather than inserting a second row.
func TestNotification_Preferences(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	user := newNotifUser(t, ctx, r, "notif-prefs@test.local")

	// Two column writes to the same bundle must land in one row without clobbering.
	if err := r.SetBundlePreference(ctx, user.ID, "my_requests", string(model.ChannelPush), true); err != nil {
		t.Fatalf("set push: %v", err)
	}
	if err := r.SetBundlePreference(ctx, user.ID, "my_requests", string(model.ChannelEmail), true); err != nil {
		t.Fatalf("set email: %v", err)
	}

	prefs, err := r.ListPreferences(ctx, user.ID)
	if err != nil {
		t.Fatalf("list preferences: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("preferences = %d rows, want 1 (one per bundle)", len(prefs))
	}
	p := prefs[0]
	if p.Push == nil || !*p.Push {
		t.Fatalf("push should be set true, got %v", p.Push)
	}
	if p.Email == nil || !*p.Email {
		t.Fatalf("email should be set true (not clobbered by the push write), got %v", p.Email)
	}
	if p.Subscribed != nil {
		t.Fatalf("subscribed was never written; want NULL (defer to default), got %v", *p.Subscribed)
	}

	// Re-setting push flips it in place — still one row.
	if err := r.SetBundlePreference(ctx, user.ID, "my_requests", string(model.ChannelPush), false); err != nil {
		t.Fatalf("flip push: %v", err)
	}
	prefs, err = r.ListPreferences(ctx, user.ID)
	if err != nil {
		t.Fatalf("list preferences: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("preferences = %d rows after flip, want 1 (upsert, not insert)", len(prefs))
	}
	if prefs[0].Push == nil || *prefs[0].Push {
		t.Fatalf("push should now be false, got %v", prefs[0].Push)
	}
}

func containsOutbox(rows []model.NotificationOutbox, id uuid.UUID) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}

// TestNotification_Enqueue proves the service turns a typed user-audience event
// into queued outbox rows on the my_requests default channels (in_app + push;
// email defaults off), with the routing metadata and template payload intact on
// the in_app row.
func TestNotification_Enqueue(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	user := newNotifUser(t, ctx, r, "notif-enqueue@test.local")
	if err := svc.Enqueue(ctx, notifications.WantAvailable{
		Recipient: user.ID,
		Media:     notifications.MediaRef{Title: "Sentinel", Year: 2024},
		PlexLink:  "https://plex/watch/1",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	due, err := r.ListDueOutbox(ctx, 100)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	// in_app + push, one each; email off by default → no email row.
	if len(due) != 2 {
		t.Fatalf("enqueued rows = %d, want 2 (in_app + push)", len(due))
	}
	channels := map[string]model.NotificationOutbox{}
	for _, row := range due {
		if row.Channel == string(model.ChannelEmail) {
			t.Fatalf("unexpected email row — email defaults off for my_requests")
		}
		channels[row.Channel] = row
	}
	row, ok := channels[string(model.ChannelInApp)]
	if !ok {
		t.Fatalf("no in_app row among %v", channels)
	}
	if _, ok := channels[string(model.ChannelPush)]; !ok {
		t.Fatalf("no push row among %v", channels)
	}
	if row.EventType != "want.available" || row.Audience != string(model.AudienceUser) {
		t.Fatalf("row = %q/%s", row.EventType, row.Audience)
	}
	if row.RecipientUserID == nil || *row.RecipientUserID != user.ID {
		t.Fatalf("recipient = %v, want %s", row.RecipientUserID, user.ID)
	}
	var payload map[string]any
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if _, ok := payload["media"]; !ok {
		t.Fatalf("payload missing media: %s", row.Payload)
	}
}

// TestNotification_EnqueueRespectsDisabledPref proves the subscription master gates
// enqueue: unsubscribing the event's bundle suppresses every channel — in_app
// included — so nothing enqueues.
func TestNotification_EnqueueRespectsDisabledPref(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	user := newNotifUser(t, ctx, r, "notif-enqueue-off@test.local")
	if err := r.SetBundlePreference(ctx, user.ID, notifications.BundleMyRequests, "subscribed", false); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if err := svc.Enqueue(ctx, notifications.WantAvailable{
		Recipient: user.ID,
		Media:     notifications.MediaRef{Title: "Sentinel"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	due, err := r.ListDueOutbox(ctx, 100)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("enqueued rows = %d, want 0 (unsubscribed)", len(due))
	}
}

func sameJSON(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return reflect.DeepEqual(av, bv)
}

func lastReadAt(rows []model.NotificationOutbox) *time.Time {
	if len(rows) == 0 {
		return nil
	}
	return rows[len(rows)-1].ReadAt
}
