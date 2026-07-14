//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// seedPushSub registers a device for a user through the repo so the test holds
// the row's id (the service create path returns no model). endpoint is the
// natural key, so each call passes a distinct one.
func seedPushSub(t *testing.T, ctx context.Context, r *repo.Repository, userID uuid.UUID, endpoint string) model.PushSubscription {
	t.Helper()
	sub, err := r.UpsertPushSubscription(ctx, repo.UpsertPushSubscriptionParams{
		UserID:   userID,
		Endpoint: endpoint,
		P256dh:   "test-p256dh",
		Auth:     "test-auth",
	})
	if err != nil {
		t.Fatalf("seed push sub %q: %v", endpoint, err)
	}
	return sub
}

// TestPush_ListReturnsOnlyOwnDevices proves the list is owner-scoped: each user
// sees exactly their own registered devices, and one user's endpoints never leak
// into another's list.
func TestPush_ListReturnsOnlyOwnDevices(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	alice := newNotifUser(t, ctx, r, "push-list-alice@test.local")
	bob := newNotifUser(t, ctx, r, "push-list-bob@test.local")

	seedPushSub(t, ctx, r, alice.ID, "https://push.test/alice-phone")
	seedPushSub(t, ctx, r, alice.ID, "https://push.test/alice-laptop")
	seedPushSub(t, ctx, r, bob.ID, "https://push.test/bob-phone")

	aliceSubs, err := svc.ListPushSubscriptions(ctx, alice.ID)
	if err != nil {
		t.Fatalf("list alice: %v", err)
	}
	if len(aliceSubs) != 2 {
		t.Fatalf("alice devices = %d, want 2", len(aliceSubs))
	}
	got := map[string]bool{}
	for _, s := range aliceSubs {
		got[s.Endpoint] = true
	}
	if !got["https://push.test/alice-phone"] || !got["https://push.test/alice-laptop"] {
		t.Fatalf("alice endpoints = %v, want both alice devices", got)
	}
	if got["https://push.test/bob-phone"] {
		t.Fatal("bob's device leaked into alice's list")
	}

	bobSubs, err := svc.ListPushSubscriptions(ctx, bob.ID)
	if err != nil {
		t.Fatalf("list bob: %v", err)
	}
	if len(bobSubs) != 1 || bobSubs[0].Endpoint != "https://push.test/bob-phone" {
		t.Fatalf("bob devices = %+v, want just bob-phone", bobSubs)
	}
}

// TestPush_GetIsOwnerScoped proves a user can load their own device by id but
// gets a NotFound for another user's id — the owner scope lives in the query, so
// the lookup can never reveal (or act on) a foreign subscription.
func TestPush_GetIsOwnerScoped(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	alice := newNotifUser(t, ctx, r, "push-get-alice@test.local")
	bob := newNotifUser(t, ctx, r, "push-get-bob@test.local")
	aliceSub := seedPushSub(t, ctx, r, alice.ID, "https://push.test/get-alice")
	bobSub := seedPushSub(t, ctx, r, bob.ID, "https://push.test/get-bob")

	own, err := svc.GetPushSubscription(ctx, alice.ID, aliceSub.ID)
	if err != nil {
		t.Fatalf("get own: %v", err)
	}
	if own.Endpoint != "https://push.test/get-alice" {
		t.Fatalf("got endpoint %q, want alice's", own.Endpoint)
	}

	if _, err := svc.GetPushSubscription(ctx, alice.ID, bobSub.ID); !apperrors.IsNotFound(err) {
		t.Fatalf("get foreign id: err = %v, want NotFound", err)
	}
}

// TestPush_RemoveIsOwnerScopedAndIdempotent proves removal only touches the
// caller's own devices and never errors: removing another user's id is a no-op
// that leaves their device intact, and removing an already-gone id succeeds.
func TestPush_RemoveIsOwnerScopedAndIdempotent(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	alice := newNotifUser(t, ctx, r, "push-rm-alice@test.local")
	bob := newNotifUser(t, ctx, r, "push-rm-bob@test.local")
	aliceSub := seedPushSub(t, ctx, r, alice.ID, "https://push.test/rm-alice")
	bobSub := seedPushSub(t, ctx, r, bob.ID, "https://push.test/rm-bob")

	// Alice cannot remove Bob's device: no error, and Bob's device survives.
	if err := svc.RemovePushSubscription(ctx, alice.ID, bobSub.ID); err != nil {
		t.Fatalf("remove foreign id should be a no-op, got: %v", err)
	}
	bobSubs, err := svc.ListPushSubscriptions(ctx, bob.ID)
	if err != nil {
		t.Fatalf("list bob: %v", err)
	}
	if len(bobSubs) != 1 {
		t.Fatalf("bob devices after alice's cross-remove = %d, want 1 (untouched)", len(bobSubs))
	}

	// Alice removes her own device; a second removal is idempotent.
	if err := svc.RemovePushSubscription(ctx, alice.ID, aliceSub.ID); err != nil {
		t.Fatalf("remove own: %v", err)
	}
	aliceSubs, err := svc.ListPushSubscriptions(ctx, alice.ID)
	if err != nil {
		t.Fatalf("list alice: %v", err)
	}
	if len(aliceSubs) != 0 {
		t.Fatalf("alice devices after self-remove = %d, want 0", len(aliceSubs))
	}
	if err := svc.RemovePushSubscription(ctx, alice.ID, aliceSub.ID); err != nil {
		t.Fatalf("second remove should be idempotent, got: %v", err)
	}
}
