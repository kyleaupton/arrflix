//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// lifecycleApp boots an app with the HD movie profile seeded and TMDB primed
// for spawnTmdbID (The Matrix), the fixture every approval-lifecycle spawn uses.
func lifecycleApp(t *testing.T) (*testapp.App, context.Context) {
	t.Helper()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(spawnTmdbID, tmdb.MovieDetails{
		ID:          spawnTmdbID,
		Title:       "The Matrix",
		ReleaseDate: "1999-03-31",
	})
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()
	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	return app, ctx
}

// createMovieRequest POSTs a movie/HD request as the given token and returns the
// created request row.
func createMovieRequest(t *testing.T, app *testapp.App, token string) model.Request {
	t.Helper()
	var req model.Request
	app.POSTAs(t, token, "/api/v1/requests", map[string]any{
		"tmdbId": spawnTmdbID, "type": "movie", "tier": "HD",
	}, &req, http.StatusCreated)
	return req
}

// TestRequestLifecycle_AutoApprove proves auto-approve is now permission-driven:
// admin/co_admin (holding requests.auto_approve:movie:hd) spawn immediately with
// decision_auto=true; a requester (lacking it) lands pending with no tracking.
func TestRequestLifecycle_AutoApprove(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	_, coAdmin := app.UserToken(t, ctx, "coadmin1", "co_admin")
	_, requester := app.UserToken(t, ctx, "requester1", "requester")

	// Admin auto-spawns, stamped auto.
	adminReq := createMovieRequest(t, app, app.Token)
	if adminReq.Status != string(model.RequestSpawned) {
		t.Errorf("admin request status = %q, want spawned", adminReq.Status)
	}
	if adminReq.SpawnedTrackingID == nil {
		t.Errorf("admin request has nil spawnedTrackingId")
	}
	if adminReq.DecisionAuto == nil || !*adminReq.DecisionAuto {
		t.Errorf("admin request decisionAuto = %v, want true", adminReq.DecisionAuto)
	}

	// co_admin also auto-spawns (joins the admin's tracking via dedup).
	coReq := createMovieRequest(t, app, coAdmin)
	if coReq.Status != string(model.RequestSpawned) {
		t.Errorf("co_admin request status = %q, want spawned", coReq.Status)
	}

	// requester lands pending — no auto_approve grant.
	reqReq := createMovieRequest(t, app, requester)
	if reqReq.Status != string(model.RequestPending) {
		t.Errorf("requester request status = %q, want pending", reqReq.Status)
	}
	if reqReq.SpawnedTrackingID != nil {
		t.Errorf("pending request spawnedTrackingId = %v, want nil", reqReq.SpawnedTrackingID)
	}
	if reqReq.DecisionAuto != nil {
		t.Errorf("pending request decisionAuto = %v, want nil (undecided)", reqReq.DecisionAuto)
	}

	// The requester's pending request spawned nothing: still exactly one tracking
	// (the admin+co_admin dedup), and its single want lives.
	var trackings []model.Tracking
	app.GET(t, "/api/v1/tracking", &trackings, http.StatusOK)
	if len(trackings) != 1 {
		t.Fatalf("tracking count = %d, want 1 (pending request spawns nothing)", len(trackings))
	}
}

// TestRequestLifecycle_Approve proves the manual approve path: a requester's
// pending request is approved by an admin into a real tracking + want with
// decision_auto=false and decided_by=admin; a non-approver is refused, and
// approving a non-pending request conflicts.
func TestRequestLifecycle_Approve(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	admin := adminUser(t, app, ctx)
	_, requester := app.UserToken(t, ctx, "requester1", "requester")

	pending := createMovieRequest(t, app, requester)
	if pending.Status != string(model.RequestPending) {
		t.Fatalf("setup: requester request status = %q, want pending", pending.Status)
	}
	approvePath := "/api/v1/requests/" + pending.ID.String() + "/approve"

	// A requester holds no approve key → gate 403.
	app.POSTAs(t, requester, approvePath, nil, nil, http.StatusForbidden)

	// Admin approves → spawned, provenance stamped manual.
	var approved model.Request
	app.POST(t, approvePath, nil, &approved, http.StatusOK)
	if approved.Status != string(model.RequestSpawned) {
		t.Errorf("approved request status = %q, want spawned", approved.Status)
	}
	if approved.SpawnedTrackingID == nil {
		t.Fatalf("approved request has nil spawnedTrackingId")
	}
	if approved.DecidedBy == nil || *approved.DecidedBy != admin.ID {
		t.Errorf("approved request decidedBy = %v, want admin %s", approved.DecidedBy, admin.ID)
	}
	if approved.DecisionAuto == nil || *approved.DecisionAuto {
		t.Errorf("approved request decisionAuto = %v, want false (manual)", approved.DecisionAuto)
	}

	// The approve produced a real tracking + one pending want.
	trackingID := approved.SpawnedTrackingID.String()
	var wants []model.Want
	app.GET(t, "/api/v1/tracking/"+trackingID+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 {
		t.Fatalf("want count = %d, want 1", len(wants))
	}
	if wants[0].Status != string(model.WantPending) {
		t.Errorf("want status = %q, want pending", wants[0].Status)
	}

	// Re-approving a now-spawned request is a conflict.
	app.POST(t, approvePath, nil, nil, http.StatusConflict)
}

// TestRequestLifecycle_Deny proves an operator can deny a pending request with a
// reason, and the requester sees that reason on their own request.
func TestRequestLifecycle_Deny(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	admin := adminUser(t, app, ctx)
	_, requester := app.UserToken(t, ctx, "requester1", "requester")

	pending := createMovieRequest(t, app, requester)
	denyPath := "/api/v1/requests/" + pending.ID.String() + "/deny"

	const reason = "not available at HD yet"
	var denied model.Request
	app.POST(t, denyPath, map[string]any{"reason": reason}, &denied, http.StatusOK)
	if denied.Status != string(model.RequestDenied) {
		t.Errorf("denied request status = %q, want denied", denied.Status)
	}
	if denied.DeniedReason == nil || *denied.DeniedReason != reason {
		t.Errorf("denied reason = %v, want %q", denied.DeniedReason, reason)
	}
	if denied.DecidedBy == nil || *denied.DecidedBy != admin.ID {
		t.Errorf("denied request decidedBy = %v, want admin %s", denied.DecidedBy, admin.ID)
	}

	// The requester reads their own denial reason (view.own).
	var seen model.Request
	app.GETAs(t, requester, "/api/v1/requests/"+pending.ID.String(), &seen, http.StatusOK)
	if seen.DeniedReason == nil || *seen.DeniedReason != reason {
		t.Errorf("requester sees denied reason = %v, want %q", seen.DeniedReason, reason)
	}

	// Denying an already-denied request is a conflict.
	app.POST(t, denyPath, map[string]any{"reason": reason}, nil, http.StatusConflict)
}

// TestRequestLifecycle_Queue proves the pending-queue read: an admin
// (requests.view.any) sees every pending request; a requester sees only their
// own, both with and without the status filter.
func TestRequestLifecycle_Queue(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	_, reqA := app.UserToken(t, ctx, "req_a", "requester")
	_, reqB := app.UserToken(t, ctx, "req_b", "requester")

	aReq := createMovieRequest(t, app, reqA)
	_ = createMovieRequest(t, app, reqB)

	// Admin sees both pending requests.
	var adminQueue []model.Request
	app.GET(t, "/api/v1/requests?status=pending", &adminQueue, http.StatusOK)
	if len(adminQueue) != 2 {
		t.Errorf("admin pending queue = %d, want 2", len(adminQueue))
	}

	// Requester A sees only their own pending request — own/any scoping applies
	// before the status filter.
	var aQueue []model.Request
	app.GETAs(t, reqA, "/api/v1/requests?status=pending", &aQueue, http.StatusOK)
	if len(aQueue) != 1 || aQueue[0].ID != aReq.ID {
		t.Errorf("requester A pending queue = %d requests, want just A's own", len(aQueue))
	}

	// A no-match filter returns an empty (non-null) list for the admin.
	var spawnedQueue []model.Request
	app.GET(t, "/api/v1/requests?status=spawned", &spawnedQueue, http.StatusOK)
	if len(spawnedQueue) != 0 {
		t.Errorf("spawned queue = %d, want 0 (nothing spawned)", len(spawnedQueue))
	}
}

// TestRequestLifecycle_CancelPendingAndForbidden proves a requester can withdraw
// their own pending request (closing the P2 gap), but cannot cancel someone
// else's.
func TestRequestLifecycle_CancelPendingAndForbidden(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	_, reqA := app.UserToken(t, ctx, "req_a", "requester")
	_, reqB := app.UserToken(t, ctx, "req_b", "requester")

	aReq := createMovieRequest(t, app, reqA)
	cancelPath := "/api/v1/requests/" + aReq.ID.String() + "/cancel"

	// B (a requester holding only cancel.own) can't cancel A's request.
	app.POSTAs(t, reqB, cancelPath, nil, nil, http.StatusForbidden)

	// A withdraws their own pending request.
	var canceled model.Request
	app.POSTAs(t, reqA, cancelPath, nil, &canceled, http.StatusOK)
	if canceled.Status != string(model.RequestCanceled) {
		t.Errorf("canceled request status = %q, want canceled", canceled.Status)
	}
	if canceled.DecidedBy == nil {
		t.Errorf("canceled request decidedBy = nil, want the canceller")
	}
}

// TestRequestLifecycle_CancelSpawnedSingleRequester proves the spawned cascade:
// the sole requester's withdrawal cancels the tracking's want and parks the
// tracking 'paused'.
func TestRequestLifecycle_CancelSpawnedSingleRequester(t *testing.T) {
	t.Parallel()
	app, _ := lifecycleApp(t)

	// Admin auto-spawns, so there's a real tracking with one requester.
	req := createMovieRequest(t, app, app.Token)
	if req.SpawnedTrackingID == nil {
		t.Fatalf("admin request did not spawn a tracking")
	}
	trackingID := req.SpawnedTrackingID.String()

	app.POST(t, "/api/v1/requests/"+req.ID.String()+"/cancel", nil, nil, http.StatusOK)

	// Last requester withdrew: tracking parked 'paused', its want canceled.
	var tracking model.Tracking
	app.GET(t, "/api/v1/tracking/"+trackingID, &tracking, http.StatusOK)
	if tracking.State != string(model.TrackingPaused) {
		t.Errorf("tracking state = %q, want paused", tracking.State)
	}
	var wants []model.Want
	app.GET(t, "/api/v1/tracking/"+trackingID+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 || wants[0].Status != string(model.WantCanceled) {
		t.Errorf("want = %+v, want a single canceled want", wants)
	}
}

// TestRequestLifecycle_CancelSpawnedMultiRequester proves the dedup-aware
// cascade: with two requesters on one tracking, the first withdrawal only drops
// that requester (tracking stays active, want alive); the last withdrawal cancels
// the want and parks the tracking.
func TestRequestLifecycle_CancelSpawnedMultiRequester(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	_, coAdmin := app.UserToken(t, ctx, "coadmin1", "co_admin")

	// Admin and co_admin both auto-spawn the same movie → one shared tracking,
	// two requesters.
	adminReq := createMovieRequest(t, app, app.Token)
	if adminReq.SpawnedTrackingID == nil {
		t.Fatalf("admin request did not spawn a tracking")
	}
	trackingID := adminReq.SpawnedTrackingID.String()

	coReq := createMovieRequest(t, app, coAdmin)
	if coReq.SpawnedTrackingID == nil || coReq.SpawnedTrackingID.String() != trackingID {
		t.Fatalf("co_admin request did not join the shared tracking (got %v)", coReq.SpawnedTrackingID)
	}

	// First withdrawal (co_admin, not the last requester): tracking stays active,
	// want untouched.
	app.POSTAs(t, coAdmin, "/api/v1/requests/"+coReq.ID.String()+"/cancel", nil, nil, http.StatusOK)

	var tracking model.Tracking
	app.GET(t, "/api/v1/tracking/"+trackingID, &tracking, http.StatusOK)
	if tracking.State != string(model.TrackingActive) {
		t.Errorf("after first cancel, tracking state = %q, want active (shared)", tracking.State)
	}
	var wants []model.Want
	app.GET(t, "/api/v1/tracking/"+trackingID+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 || model.WantStatus(wants[0].Status).IsTerminal() {
		t.Errorf("after first cancel, want = %+v, want a single live want", wants)
	}
	var requesters []model.TrackingRequester
	app.GET(t, "/api/v1/tracking/"+trackingID+"/requesters", &requesters, http.StatusOK)
	if len(requesters) != 1 {
		t.Errorf("after first cancel, requester count = %d, want 1", len(requesters))
	}

	// Last withdrawal (admin): want canceled, tracking parked 'paused'.
	app.POST(t, "/api/v1/requests/"+adminReq.ID.String()+"/cancel", nil, nil, http.StatusOK)
	app.GET(t, "/api/v1/tracking/"+trackingID, &tracking, http.StatusOK)
	if tracking.State != string(model.TrackingPaused) {
		t.Errorf("after last cancel, tracking state = %q, want paused", tracking.State)
	}
	app.GET(t, "/api/v1/tracking/"+trackingID+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 || wants[0].Status != string(model.WantCanceled) {
		t.Errorf("after last cancel, want = %+v, want a single canceled want", wants)
	}
}
