//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
)

// REQ-UNION-011 — A requester must not be told who else wants a title.
//
// Two requesters, each auto-approved for HD movies, request the same movie, so
// both share one tracking. Neither may list the tracking's requesters (who else
// asked, their tiers and scopes) nor its proposals (operator acquisition
// state): both are 403 at the gate. An admin reads both, and the requester
// list names both users.
func TestReqUnion011_RequesterCannotListOtherRequesters(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)
	alice, aliceTok := app.UserToken(t, ctx, "alice", "requester")
	grantUser(t, ctx, app, alice.ID, "requests.auto_approve:movie:hd")
	bob, bobTok := app.UserToken(t, ctx, "bob", "requester")
	grantUser(t, ctx, app, bob.ID, "requests.auto_approve:movie:hd")

	aliceReq := createMovieRequest(t, app, aliceTok)
	if aliceReq.SpawnedTrackingID == nil {
		t.Fatalf("auto-approved request spawned no tracking")
	}
	bobReq := createMovieRequest(t, app, bobTok)
	if bobReq.SpawnedTrackingID == nil || *bobReq.SpawnedTrackingID != *aliceReq.SpawnedTrackingID {
		t.Fatalf("second request tracking = %v, want it to join %s", bobReq.SpawnedTrackingID, *aliceReq.SpawnedTrackingID)
	}
	trackingPath := "/api/v1/tracking/" + aliceReq.SpawnedTrackingID.String()

	for _, tok := range []string{aliceTok, bobTok} {
		app.GETAs(t, tok, trackingPath+"/requesters", nil, http.StatusForbidden)
		app.GETAs(t, tok, trackingPath+"/proposals", nil, http.StatusForbidden)
	}

	var requesters []model.TrackingRequester
	app.GET(t, trackingPath+"/requesters", &requesters, http.StatusOK)
	got := map[string]bool{}
	for _, r := range requesters {
		got[r.UserID.String()] = true
	}
	if len(requesters) != 2 || !got[alice.ID.String()] || !got[bob.ID.String()] {
		t.Errorf("admin requesters = %+v, want alice (%s) and bob (%s)", requesters, alice.ID, bob.ID)
	}

	var proposals []model.ProposalView
	app.GET(t, trackingPath+"/proposals", &proposals, http.StatusOK)
	if len(proposals) != 0 {
		t.Errorf("admin proposals = %d, want 0 (tracking is on auto)", len(proposals))
	}
}
