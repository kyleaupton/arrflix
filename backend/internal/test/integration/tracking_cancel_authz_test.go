//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
)

// REQ-CANCEL-012 — Cancelling a tracking that other requesters still justify
// must require authority over others' requests, not merely over one's own.
// Removing one's own requester association requires only own-intent authority,
// because it cancels nothing.
//
// Dana and Erin share one tracking, and Dana holds tracking.cancel.own only.
// Dana's cancel is refused 403 and leaves the tracking and its want untouched.
// Once Erin withdraws, Dana is the only requester, and the same call succeeds.
func TestReqCancel012_CancellingSharedTrackingNeedsAnyAuthority(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	dana, danaToken := app.UserToken(t, ctx, "dana", "requester")
	_, erinToken := app.UserToken(t, ctx, "erin", "requester")
	grantUser(t, ctx, app, dana.ID, "tracking.cancel.own")

	danaReq := createMovieRequest(t, app, danaToken)
	erinReq := createMovieRequest(t, app, erinToken)
	app.POST(t, "/api/v1/requests/"+danaReq.ID.String()+"/approve", nil, &danaReq, http.StatusOK)
	app.POST(t, "/api/v1/requests/"+erinReq.ID.String()+"/approve", nil, &erinReq, http.StatusOK)
	if danaReq.SpawnedTrackingID == nil || erinReq.SpawnedTrackingID == nil ||
		*danaReq.SpawnedTrackingID != *erinReq.SpawnedTrackingID {
		t.Fatalf("setup: requests did not share one tracking (dana %v, erin %v)",
			danaReq.SpawnedTrackingID, erinReq.SpawnedTrackingID)
	}
	trackingPath := "/api/v1/tracking/" + danaReq.SpawnedTrackingID.String()

	app.POSTAs(t, danaToken, trackingPath+"/cancel", nil, nil, http.StatusForbidden)

	var tracking model.Tracking
	app.GET(t, trackingPath, &tracking, http.StatusOK)
	if tracking.State == string(model.TrackingCanceled) {
		t.Fatalf("shared tracking canceled by a caller holding only tracking.cancel.own")
	}
	var wants []model.Want
	app.GET(t, trackingPath+"/wants", &wants, http.StatusOK)
	for _, w := range wants {
		if w.Status == string(model.WantCanceled) {
			t.Errorf("want %s canceled by a refused tracking cancel", w.ID)
		}
	}

	app.POSTAs(t, erinToken, "/api/v1/requests/"+erinReq.ID.String()+"/cancel", nil, nil, http.StatusOK)

	var canceled model.Tracking
	app.POSTAs(t, danaToken, trackingPath+"/cancel", nil, &canceled, http.StatusOK)
	if canceled.State != string(model.TrackingCanceled) {
		t.Errorf("sole requester's cancel left tracking %q, want canceled", canceled.State)
	}
}
