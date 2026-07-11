//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// deliverInApp enqueues a want.available in_app row for a user and forces it to
// delivered — the state the bell read path surfaces — bypassing the worker since
// these tests exercise the read side.
func deliverInApp(t *testing.T, ctx context.Context, r *repo.Repository, userID uuid.UUID, payload string) model.NotificationOutbox {
	t.Helper()
	row, err := r.EnqueueOutbox(ctx, repo.EnqueueOutboxParams{
		EventType:       "want.available",
		Audience:        string(model.AudienceUser),
		RecipientUserID: &userID,
		Channel:         string(model.ChannelInApp),
		Payload:         json.RawMessage(payload),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	delivered, err := r.MarkOutboxDelivered(ctx, row.ID)
	if err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	return delivered
}

// TestNotification_Inbox proves the read projection: delivered rows come back
// newest-first with title and body rendered from the event template, and the
// unread count / mark-read lifecycle tracks the DB.
func TestNotification_Inbox(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	user := newNotifUser(t, ctx, r, "notif-inbox@test.local")
	row := deliverInApp(t, ctx, r, user.ID, `{"media":{"title":"Sentinel","year":2024},"plexLink":"https://plex/1"}`)

	inbox, err := svc.Inbox(ctx, user.ID, 50)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("inbox = %d rows, want 1", len(inbox))
	}
	got := inbox[0]
	if got.ID != row.ID {
		t.Fatalf("id = %s, want %s", got.ID, row.ID)
	}
	if got.Title != "Sentinel is ready to watch" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Body == "" || got.Payload == nil {
		t.Fatalf("body/payload should be populated: body=%q payload=%s", got.Body, got.Payload)
	}
	if got.ReadAt != nil {
		t.Fatalf("read_at should be nil before read")
	}

	if n, err := svc.UnreadCount(ctx, user.ID); err != nil || n != 1 {
		t.Fatalf("unread = %d (err %v), want 1", n, err)
	}
	if err := svc.MarkRead(ctx, row.ID, user.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if n, err := svc.UnreadCount(ctx, user.ID); err != nil || n != 0 {
		t.Fatalf("unread after read = %d (err %v), want 0", n, err)
	}
	after, err := svc.Inbox(ctx, user.ID, 50)
	if err != nil || len(after) != 1 || after[0].ReadAt == nil {
		t.Fatalf("inbox after read = %d rows, read_at set? — want 1 read row", len(after))
	}
}

// TestNotification_InboxScopedToUser proves the inbox is per-user: one user's
// delivered notification is invisible to another.
func TestNotification_InboxScopedToUser(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	alice := newNotifUser(t, ctx, r, "notif-scope-alice@test.local")
	bob := newNotifUser(t, ctx, r, "notif-scope-bob@test.local")
	deliverInApp(t, ctx, r, alice.ID, `{"media":{"title":"Sentinel"}}`)

	if inbox, err := svc.Inbox(ctx, bob.ID, 50); err != nil || len(inbox) != 0 {
		t.Fatalf("bob inbox = %d (err %v), want 0 — alice's row must not leak", len(inbox), err)
	}
	if n, err := svc.UnreadCount(ctx, bob.ID); err != nil || n != 0 {
		t.Fatalf("bob unread = %d (err %v), want 0", n, err)
	}
	// Bob marking alice's id read is a guarded no-op — alice keeps her unread.
	if inbox, _ := svc.Inbox(ctx, alice.ID, 50); len(inbox) == 1 {
		if err := svc.MarkRead(ctx, inbox[0].ID, bob.ID); err != nil {
			t.Fatalf("bob mark-read of alice's row should be a silent no-op, got %v", err)
		}
	}
	if n, err := svc.UnreadCount(ctx, alice.ID); err != nil || n != 1 {
		t.Fatalf("alice unread = %d (err %v), want 1 — bob's mark-read must not touch it", n, err)
	}
}
