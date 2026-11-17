//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
)

// REQ-ROLE-003 — Perspective is presentation only. Every action must still be
// enforced by permission key on the backend, so a UI perspective never grants a
// capability.
//
// Tracking autonomy is offered only to operators in the UI, so the API must
// refuse it to everyone else: a requester holding an auto-approve grant who
// sends an autonomy field is denied 403 and nothing is created, while an
// operator (jobs.manage) sending the same fields spawns a tracking carrying them.
func TestReqRole003_OnlyOperatorsMaySetTrackingAutonomy(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)
	_, friend := friendToken(t, ctx, app)

	for _, body := range []map[string]any{
		{"tmdbId": spawnTmdbID, "type": "movie", "tier": "HD", "backfillAutonomy": "manual"},
		{"tmdbId": spawnTmdbID, "type": "movie", "tier": "HD", "ongoingAutonomy": "propose"},
	} {
		app.POSTAs(t, friend, "/api/v1/requests", body, nil, http.StatusForbidden)
	}

	var requests []model.Request
	app.GET(t, "/api/v1/requests", &requests, http.StatusOK)
	if len(requests) != 0 {
		t.Errorf("requests after denied create = %d, want 0", len(requests))
	}
	var trackings []model.Tracking
	app.GET(t, "/api/v1/tracking", &trackings, http.StatusOK)
	if len(trackings) != 0 {
		t.Errorf("trackings after denied create = %d, want 0", len(trackings))
	}

	var req model.Request
	app.POST(t, "/api/v1/requests", map[string]any{
		"tmdbId": spawnTmdbID, "type": "movie", "tier": "HD",
		"backfillAutonomy": "manual", "ongoingAutonomy": "propose",
	}, &req, http.StatusCreated)
	if req.SpawnedTrackingID == nil {
		t.Fatalf("operator request spawned no tracking (status %q)", req.Status)
	}
	var tracking model.Tracking
	app.GET(t, "/api/v1/tracking/"+req.SpawnedTrackingID.String(), &tracking, http.StatusOK)
	if tracking.AutonomyBackfill != string(model.AutonomyManual) || tracking.AutonomyOngoing != string(model.AutonomyPropose) {
		t.Errorf("tracking autonomy = (%q, %q), want (manual, propose)", tracking.AutonomyBackfill, tracking.AutonomyOngoing)
	}
}
