//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	notificationworker "github.com/kyleaupton/arrflix/internal/jobs/notification"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// pendingRows returns a request's queued request.pending rows on one channel,
// keyed by recipient.
func pendingRows(t *testing.T, ctx context.Context, app *testapp.App, requestID uuid.UUID, ch model.NotificationChannel) map[uuid.UUID]model.NotificationOutbox {
	t.Helper()
	out := map[uuid.UUID]model.NotificationOutbox{}
	for recipient, rows := range outboxFor(t, ctx, app, notifications.EventRequestPending) {
		for _, row := range rows {
			if row.Channel == string(ch) && decodeRequestPayload(t, row).RequestID == requestID {
				out[recipient] = row
			}
		}
	}
	return out
}

// deliverPendingInApp puts a request's request.pending into each recipient's
// bell, as the worker's in_app adapter would.
func deliverPendingInApp(t *testing.T, ctx context.Context, app *testapp.App, requestID uuid.UUID) {
	t.Helper()
	rows := pendingRows(t, ctx, app, requestID, model.ChannelInApp)
	if len(rows) == 0 {
		t.Fatalf("request %s has no in_app request.pending rows", requestID)
	}
	for _, row := range rows {
		if _, err := app.Repo.MarkOutboxDelivered(ctx, row.ID); err != nil {
			t.Fatalf("deliver in_app row %s: %v", row.ID, err)
		}
	}
}

// bellEntry returns the caller's bell entry for a request's request.pending.
func bellEntry(t *testing.T, app *testapp.App, token string, requestID uuid.UUID) model.InboxNotification {
	t.Helper()
	var inbox []model.InboxNotification
	app.GETAs(t, token, "/api/v1/notifications", &inbox, http.StatusOK)
	for _, n := range inbox {
		if n.EventType != notifications.EventRequestPending {
			continue
		}
		var p requestNotifPayload
		if err := json.Unmarshal(n.Payload, &p); err != nil {
			t.Fatalf("decode bell payload: %v", err)
		}
		if p.RequestID == requestID {
			return n
		}
	}
	t.Fatalf("no request.pending for %s in the bell", requestID)
	return model.InboxNotification{}
}

func unreadCount(t *testing.T, app *testapp.App, token string) int64 {
	t.Helper()
	var body struct {
		Count int64 `json:"count"`
	}
	app.GETAs(t, token, "/api/v1/notifications/unread-count", &body, http.StatusOK)
	return body.Count
}

// REQ-APPROVE-003 — Deciding a request must clear it from every reviewer's
// attention, including reviewers who didn't act on it. With two admins, approving,
// denying, or the requester withdrawing a pending request resolves both admins'
// in-app request.pending with the outcome, and neither counts as unread.
func TestReqApprove003_DecisionResolvesEveryReviewersBell(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		outcome model.NotificationResolution
		decide  func(t *testing.T, app *testapp.App, requesterToken string, id uuid.UUID)
	}{
		{"approved", model.ResolutionApproved, func(t *testing.T, app *testapp.App, _ string, id uuid.UUID) {
			app.POST(t, "/api/v1/requests/"+id.String()+"/approve", nil, nil, http.StatusOK)
		}},
		{"denied", model.ResolutionDenied, func(t *testing.T, app *testapp.App, _ string, id uuid.UUID) {
			app.POST(t, "/api/v1/requests/"+id.String()+"/deny", map[string]any{"reason": "not this one"}, nil, http.StatusOK)
		}},
		{"withdrawn", model.ResolutionWithdrawn, func(t *testing.T, app *testapp.App, requesterToken string, id uuid.UUID) {
			app.POSTAs(t, requesterToken, "/api/v1/requests/"+id.String()+"/cancel", nil, nil, http.StatusOK)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app, ctx := lifecycleApp(t)

			_, secondAdmin := app.UserToken(t, ctx, "admin2", "admin")
			_, requesterToken := app.UserToken(t, ctx, "req_a", "requester")

			pending := createMovieRequest(t, app, requesterToken)
			deliverPendingInApp(t, ctx, app, pending.ID)

			admins := map[string]string{"admin": app.Token, "admin2": secondAdmin}
			for name, token := range admins {
				if n := bellEntry(t, app, token, pending.ID); n.ResolvedAt != nil {
					t.Fatalf("%s: request.pending resolved before any decision", name)
				}
				if got := unreadCount(t, app, token); got != 1 {
					t.Fatalf("%s: unread before the decision = %d, want 1", name, got)
				}
			}

			tc.decide(t, app, requesterToken, pending.ID)

			for name, token := range admins {
				n := bellEntry(t, app, token, pending.ID)
				if n.ResolvedAt == nil {
					t.Errorf("%s: request.pending not resolved after the request was %s", name, tc.name)
				}
				if n.Resolution == nil || *n.Resolution != string(tc.outcome) {
					t.Errorf("%s: resolution = %v, want %q", name, n.Resolution, tc.outcome)
				}
				if got := unreadCount(t, app, token); got != 0 {
					t.Errorf("%s: unread after the decision = %d, want 0", name, got)
				}
			}
		})
	}
}

// recordingPush is a configured push adapter that records what it delivers.
type recordingPush struct {
	mu        sync.Mutex
	delivered []uuid.UUID
}

func (*recordingPush) Name() model.NotificationChannel   { return model.ChannelPush }
func (*recordingPush) IsConfigured(context.Context) bool { return true }
func (p *recordingPush) Deliver(_ context.Context, row model.NotificationOutbox) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.delivered = append(p.delivered, row.ID)
	return nil
}

// A push for request.pending that hasn't gone out when the request is decided —
// still queued, or parked awaiting config — is superseded and never delivered. A
// push already delivered is left as it was.
func TestRequestPendingResolution_DropsUndeliveredPush(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	admin := adminUser(t, app, ctx)
	parkedAdmin, _ := app.UserToken(t, ctx, "admin2", "admin")
	sentAdmin, _ := app.UserToken(t, ctx, "admin3", "admin")
	_, requesterToken := app.UserToken(t, ctx, "req_a", "requester")

	pending := createMovieRequest(t, app, requesterToken)
	push := pendingRows(t, ctx, app, pending.ID, model.ChannelPush)
	for _, u := range []model.User{admin, parkedAdmin, sentAdmin} {
		if _, ok := push[u.ID]; !ok {
			t.Fatalf("%s has no queued request.pending push", u.Username)
		}
	}
	queued := push[admin.ID]
	parked, err := app.Repo.MarkOutboxAwaitingConfig(ctx, push[parkedAdmin.ID].ID)
	if err != nil {
		t.Fatalf("park push row: %v", err)
	}
	sent, err := app.Repo.MarkOutboxDelivered(ctx, push[sentAdmin.ID].ID)
	if err != nil {
		t.Fatalf("deliver push row: %v", err)
	}

	app.POST(t, "/api/v1/requests/"+pending.ID.String()+"/deny", map[string]any{"reason": "not this one"}, nil, http.StatusOK)

	adapter := &recordingPush{}
	w, err := notificationworker.New(app.Repo, logger.New(false), notifications.InAppAdapter{}, adapter)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	if _, err := w.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	for _, id := range adapter.delivered {
		if id == queued.ID || id == parked.ID {
			t.Errorf("push row %s for a decided request was delivered", id)
		}
	}
	for _, row := range []model.NotificationOutbox{queued, parked} {
		if got := fetchOutbox(t, ctx, app.Repo, row.ID); got.Status != string(model.OutboxSuperseded) {
			t.Errorf("undelivered push row status = %q, want superseded", got.Status)
		}
	}
	if got := fetchOutbox(t, ctx, app.Repo, sent.ID); got.Status != string(model.OutboxDelivered) {
		t.Errorf("already-delivered push row status = %q, want delivered", got.Status)
	}
}
