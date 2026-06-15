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

// TestRequests_HTTPSpine drives the request → tracking → want spine through the
// real HTTP surface: an authenticated, auto-approved movie request spawns a
// tracking and a pending want, all observable through the read endpoints.
func TestRequests_HTTPSpine(t *testing.T) {
	t.Parallel()
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
	admin := adminUser(t, app, ctx)
	if _, err := app.Repo.UpsertUserPolicy(ctx, admin.ID, true); err != nil {
		t.Fatalf("upsert admin policy: %v", err)
	}

	// Happy path: POST /requests spawns the tracking.
	var created model.Request
	app.POST(t, "/api/v1/requests", map[string]any{
		"tmdbId": spawnTmdbID,
		"type":   "movie",
		"tier":   "HD",
	}, &created, http.StatusCreated)

	if created.Status != string(model.RequestSpawned) {
		t.Errorf("request status = %q, want spawned", created.Status)
	}
	if created.SpawnedTrackingID == nil {
		t.Fatalf("spawned request has nil spawnedTrackingId")
	}
	trackingID := *created.SpawnedTrackingID
	if created.RequestedBy != admin.ID {
		t.Errorf("request requestedBy = %s, want admin %s", created.RequestedBy, admin.ID)
	}

	// One tracking, observable through GET /tracking.
	var trackings []model.Tracking
	app.GET(t, "/api/v1/tracking", &trackings, http.StatusOK)
	if len(trackings) != 1 {
		t.Fatalf("tracking count = %d, want 1", len(trackings))
	}
	if trackings[0].ID != trackingID {
		t.Errorf("tracking id = %s, want %s", trackings[0].ID, trackingID)
	}

	// One pending want under that tracking.
	var wants []model.Want
	app.GET(t, "/api/v1/tracking/"+trackingID.String()+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 {
		t.Fatalf("want count = %d, want 1", len(wants))
	}
	if wants[0].Status != string(model.WantPending) {
		t.Errorf("want status = %q, want pending", wants[0].Status)
	}

	// One requester under that tracking.
	var requesters []model.TrackingRequester
	app.GET(t, "/api/v1/tracking/"+trackingID.String()+"/requesters", &requesters, http.StatusOK)
	if len(requesters) != 1 {
		t.Fatalf("requester count = %d, want 1", len(requesters))
	}

	// The request shows up in GET /requests.
	var list []model.Request
	app.GET(t, "/api/v1/requests", &list, http.StatusOK)
	if len(list) != 1 {
		t.Fatalf("request count = %d, want 1", len(list))
	}
	if list[0].ID != created.ID {
		t.Errorf("listed request id = %s, want %s", list[0].ID, created.ID)
	}
}

// TestRequests_Validation proves huma binding rejects out-of-domain and missing
// fields with 422 before any service work.
func TestRequests_Validation(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	// An out-of-domain type is rejected by the enum (movie/series only).
	app.POST(t, "/api/v1/requests", map[string]any{
		"tmdbId": spawnTmdbID,
		"type":   "person",
		"tier":   "HD",
	}, nil, http.StatusUnprocessableEntity)

	// Missing required 'tier' fails binding.
	app.POST(t, "/api/v1/requests", map[string]any{
		"tmdbId": spawnTmdbID,
		"type":   "movie",
	}, nil, http.StatusUnprocessableEntity)
}

// TestRequests_Unauthenticated proves the global JWT middleware guards the spine
// endpoints: a POST with no Bearer token is rejected 401.
func TestRequests_Unauthenticated(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	resp, err := http.Post(app.URL+"/api/v1/requests", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/v1/requests: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
