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
	"github.com/kyleaupton/arrflix/internal/repo"
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

// TestNotification_Preferences proves the preference upsert is a true upsert:
// re-setting the same (user, scope, value, channel) key flips the flag in place
// rather than inserting a second row.
func TestNotification_Preferences(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	user := newNotifUser(t, ctx, r, "notif-prefs@test.local")
	set := func(enabled bool) model.NotificationPreference {
		p, err := r.UpsertPreference(ctx, repo.UpsertPreferenceParams{
			UserID:  user.ID,
			Scope:   string(model.PreferenceScopeBundle),
			Value:   "my_requests",
			Channel: string(model.ChannelInApp),
			Enabled: enabled,
		})
		if err != nil {
			t.Fatalf("upsert preference: %v", err)
		}
		return p
	}

	if p := set(true); !p.Enabled {
		t.Fatalf("initial preference should be enabled")
	}
	if p := set(false); p.Enabled {
		t.Fatalf("re-upsert should flip enabled to false")
	}
	prefs, err := r.ListPreferences(ctx, user.ID)
	if err != nil {
		t.Fatalf("list preferences: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("preferences = %d rows, want 1 (upsert, not insert)", len(prefs))
	}
	if prefs[0].Enabled {
		t.Fatalf("stored preference should be the updated (disabled) value")
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
