//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// requestNotifPayload is the template payload the request.* events store.
type requestNotifPayload struct {
	RequestID uuid.UUID              `json:"requestId"`
	Media     notifications.MediaRef `json:"media"`
	Requester string                 `json:"requester"`
	Reason    string                 `json:"reason"`
}

// outboxFor returns the queued outbox rows of one event type, grouped by
// recipient.
func outboxFor(t *testing.T, ctx context.Context, app *testapp.App, eventType string) map[uuid.UUID][]model.NotificationOutbox {
	t.Helper()
	due, err := app.Repo.ListDueOutbox(ctx, 1000)
	if err != nil {
		t.Fatalf("list due outbox: %v", err)
	}
	out := map[uuid.UUID][]model.NotificationOutbox{}
	for _, row := range due {
		if row.EventType != eventType {
			continue
		}
		if row.RecipientUserID == nil {
			t.Fatalf("%s row %s has no recipient user", eventType, row.ID)
		}
		out[*row.RecipientUserID] = append(out[*row.RecipientUserID], row)
	}
	return out
}

func decodeRequestPayload(t *testing.T, row model.NotificationOutbox) requestNotifPayload {
	t.Helper()
	var p requestNotifPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		t.Fatalf("decode %s payload: %v", row.EventType, err)
	}
	return p
}

// REQ-APPROVE-001 — A request needing review must reach someone able to decide
// it without them having to check. Creating a pending request enqueues
// request.pending to every user who may decide it (admin and co_admin under the
// seeded roles), and to no requester — neither the one who asked nor another.
// An auto-approved request enqueues none.
func TestReqApprove001_PendingRequestNotifiesDeciders(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	admin := adminUser(t, app, ctx)
	coAdmin, _ := app.UserToken(t, ctx, "coadmin1", "co_admin")
	reqA, reqAToken := app.UserToken(t, ctx, "req_a", "requester")
	reqB, _ := app.UserToken(t, ctx, "req_b", "requester")

	// Auto-approved (admin holds requests.auto_approve:movie:hd): nothing for review.
	auto := createMovieRequest(t, app, app.Token)
	if auto.Status != string(model.RequestSpawned) {
		t.Fatalf("admin request status = %q, want spawned", auto.Status)
	}
	if got := outboxFor(t, ctx, app, notifications.EventRequestPending); len(got) != 0 {
		t.Fatalf("auto-approved request enqueued request.pending to %d users, want 0", len(got))
	}
	if got := outboxFor(t, ctx, app, notifications.EventRequestApproved); len(got) != 0 {
		t.Fatalf("auto-approved request enqueued request.approved to %d users, want 0", len(got))
	}

	pending := createMovieRequest(t, app, reqAToken)
	if pending.Status != string(model.RequestPending) {
		t.Fatalf("requester request status = %q, want pending", pending.Status)
	}

	rows := outboxFor(t, ctx, app, notifications.EventRequestPending)
	for _, decider := range []model.User{admin, coAdmin} {
		got := rows[decider.ID]
		if len(got) == 0 {
			t.Fatalf("%s got no request.pending", decider.Username)
		}
		var inApp bool
		for _, row := range got {
			if row.Channel == string(model.ChannelInApp) {
				inApp = true
			}
			if row.Audience != string(model.AudienceAdmin) {
				t.Errorf("row audience = %q, want admin", row.Audience)
			}
			wantKey := "request:" + pending.ID.String() + ":request.pending"
			if row.DedupKey == nil || *row.DedupKey != wantKey {
				t.Errorf("row dedup key = %v, want %q", row.DedupKey, wantKey)
			}
			p := decodeRequestPayload(t, row)
			if p.RequestID != pending.ID || p.Media.Title != "The Matrix" || p.Media.Year != 1999 || p.Requester != "req_a" {
				t.Errorf("payload = %+v, want request %s for The Matrix (1999) from req_a", p, pending.ID)
			}
		}
		if !inApp {
			t.Errorf("%s got request.pending but not in the bell", decider.Username)
		}
	}
	for _, other := range []model.User{reqA, reqB} {
		if n := len(rows[other.ID]); n != 0 {
			t.Errorf("%s got %d request.pending rows, want 0", other.Username, n)
		}
	}
	if len(rows) != 2 {
		t.Errorf("request.pending recipients = %d, want exactly admin and co_admin", len(rows))
	}
}

// REQ-APPROVE-007 — A requester must learn the outcome of their request without
// checking, for either outcome. Approving a pending request enqueues
// request.approved to its requester; denying one enqueues request.denied.
func TestReqApprove007_DecisionNotifiesRequester(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	reqA, reqAToken := app.UserToken(t, ctx, "req_a", "requester")
	reqB, reqBToken := app.UserToken(t, ctx, "req_b", "requester")

	toApprove := createMovieRequest(t, app, reqAToken)
	toDeny := createMovieRequest(t, app, reqBToken)

	app.POST(t, "/api/v1/requests/"+toApprove.ID.String()+"/approve", nil, nil, http.StatusOK)
	app.POST(t, "/api/v1/requests/"+toDeny.ID.String()+"/deny", map[string]any{"reason": "already in 4K"}, nil, http.StatusOK)

	approved := outboxFor(t, ctx, app, notifications.EventRequestApproved)
	if len(approved) != 1 || len(approved[reqA.ID]) == 0 {
		t.Fatalf("request.approved recipients = %v, want only req_a", approved)
	}
	for _, row := range approved[reqA.ID] {
		if p := decodeRequestPayload(t, row); p.RequestID != toApprove.ID || p.Media.Title != "The Matrix" {
			t.Errorf("approved payload = %+v, want request %s for The Matrix", p, toApprove.ID)
		}
	}

	denied := outboxFor(t, ctx, app, notifications.EventRequestDenied)
	if len(denied) != 1 || len(denied[reqB.ID]) == 0 {
		t.Fatalf("request.denied recipients = %v, want only req_b", denied)
	}
	for _, row := range denied[reqB.ID] {
		if p := decodeRequestPayload(t, row); p.RequestID != toDeny.ID {
			t.Errorf("denied payload request = %s, want %s", p.RequestID, toDeny.ID)
		}
	}
}

// REQ-APPROVE-008 — A denial must carry a reason, and the requester must see it.
// Denying with a missing, empty or whitespace-only reason is a 422 on
// body.reason that leaves the request pending; a denial with a reason delivers
// that reason in the request.denied payload.
func TestReqApprove008_DenialRequiresAndDeliversReason(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	reqA, reqAToken := app.UserToken(t, ctx, "req_a", "requester")
	pending := createMovieRequest(t, app, reqAToken)
	denyPath := "/api/v1/requests/" + pending.ID.String() + "/deny"

	// A missing reason is rejected by huma's binding (reported at "body"); a
	// blank one reaches the service, which names body.reason.
	app.POST(t, denyPath, map[string]any{}, nil, http.StatusUnprocessableEntity)
	for _, reason := range []string{"", "  \t\n "} {
		var pd apperrors.ProblemDetails
		app.POST(t, denyPath, map[string]any{"reason": reason}, &pd, http.StatusUnprocessableEntity)
		findFieldError(t, pd, "body.reason")
	}
	if n := len(outboxFor(t, ctx, app, notifications.EventRequestDenied)); n != 0 {
		t.Fatalf("a rejected denial enqueued request.denied to %d users", n)
	}

	const reason = "We already have this in 4K."
	var denied model.Request
	app.POST(t, denyPath, map[string]any{"reason": "  " + reason + " "}, &denied, http.StatusOK)
	if denied.DeniedReason == nil || *denied.DeniedReason != reason {
		t.Errorf("stored reason = %v, want %q", denied.DeniedReason, reason)
	}

	rows := outboxFor(t, ctx, app, notifications.EventRequestDenied)[reqA.ID]
	if len(rows) == 0 {
		t.Fatalf("requester got no request.denied")
	}
	for _, row := range rows {
		if p := decodeRequestPayload(t, row); p.Reason != reason {
			t.Errorf("request.denied payload reason = %q, want %q", p.Reason, reason)
		}
	}
}

// A requester can't decide requests, so the admin_alerts bundle is not theirs:
// it is absent from their preferences, they can't write to it, and they never
// receive an admin_alerts notification. An admin sees it, subscribed with push
// on and email off by default.
func TestRequestNotifications_AdminAlertsOnlyForDeciders(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	requester, reqToken := app.UserToken(t, ctx, "req_a", "requester")

	var mine prefsBody
	app.GETAs(t, reqToken, "/api/v1/notifications/preferences", &mine, http.StatusOK)
	for _, b := range mine.Bundles {
		if b.Bundle == notifications.BundleAdminAlerts {
			t.Fatalf("requester preferences include %s", notifications.BundleAdminAlerts)
		}
	}
	app.DoAs(t, reqToken, http.MethodPut, "/api/v1/notifications/preferences", map[string]any{
		"bundle": notifications.BundleAdminAlerts, "target": "subscribed", "enabled": true,
	}, nil, http.StatusUnprocessableEntity)

	var admins prefsBody
	app.GET(t, "/api/v1/notifications/preferences", &admins, http.StatusOK)
	alerts := bundlePref(t, admins.Bundles, notifications.BundleAdminAlerts)
	if !alerts.Subscribed {
		t.Errorf("admin_alerts subscribed = false, want true by default")
	}
	if !channelPref(t, admins.Bundles, notifications.BundleAdminAlerts, "push").Enabled {
		t.Errorf("admin_alerts push = off, want on by default")
	}
	if channelPref(t, admins.Bundles, notifications.BundleAdminAlerts, "email").Enabled {
		t.Errorf("admin_alerts email = on, want off by default")
	}

	createMovieRequest(t, app, reqToken)
	due, err := app.Repo.ListDueOutbox(ctx, 1000)
	if err != nil {
		t.Fatalf("list due outbox: %v", err)
	}
	for _, row := range due {
		if row.RecipientUserID != nil && *row.RecipientUserID == requester.ID && row.Audience == string(model.AudienceAdmin) {
			t.Errorf("requester received admin-audience %s", row.EventType)
		}
	}
}
