//go:build integration

package integration

import (
	"context"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

const spawnTmdbID = int64(603) // The Matrix

// adminUser resolves the admin user the testapp seeds, so a test can attach a
// policy or stand in as the requester.
func adminUser(t *testing.T, app *testapp.App, ctx context.Context) model.User {
	t.Helper()
	user, err := app.Repo.GetUserByLogin(ctx, "admin")
	if err != nil {
		t.Fatalf("lookup admin user: %v", err)
	}
	return user
}

// TestSpawn_HappyPathAndDedup drives the Story-1 flow through the service: an
// approved movie request spawns exactly one tracking + want + requester, and a
// second approved requester for the same movie joins the existing tracking
// without creating a second tracking or want.
func TestSpawn_HappyPathAndDedup(t *testing.T) {
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

	// The HD movie profile the spawn should resolve and snapshot.
	hdProfile, err := app.Services.QualityProfiles.ResolveByTier(ctx, qualityprofile.TierHD, parsing.DomainMovie)
	if err != nil {
		t.Fatalf("resolve HD movie profile: %v", err)
	}

	// Happy path: the admin requests The Matrix at HD.
	req, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: admin.ID,
		TmdbID:      spawnTmdbID,
		Type:        "movie",
		Tier:        "HD",
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if req.Status != string(model.RequestSpawned) {
		t.Errorf("request status = %q, want spawned", req.Status)
	}
	if req.SpawnedTrackingID == nil {
		t.Fatalf("spawned request has nil spawnedTrackingId")
	}
	trackingID := *req.SpawnedTrackingID

	trackings, err := app.Services.Tracking.List(ctx)
	if err != nil {
		t.Fatalf("list trackings: %v", err)
	}
	if len(trackings) != 1 {
		t.Fatalf("tracking count = %d, want 1", len(trackings))
	}
	tracking := trackings[0]
	if tracking.ID != trackingID {
		t.Errorf("tracking id = %s, want %s", tracking.ID, trackingID)
	}
	if tracking.State != string(model.TrackingActive) {
		t.Errorf("tracking state = %q, want active", tracking.State)
	}
	if tracking.QualityProfileID != hdProfile.ID {
		t.Errorf("tracking profile = %s, want resolved HD %s", tracking.QualityProfileID, hdProfile.ID)
	}

	wants, err := app.Services.Tracking.ListWants(ctx, trackingID)
	if err != nil {
		t.Fatalf("list wants: %v", err)
	}
	if len(wants) != 1 {
		t.Fatalf("want count = %d, want 1", len(wants))
	}
	if wants[0].Status != string(model.WantPending) {
		t.Errorf("want status = %q, want pending", wants[0].Status)
	}
	if wants[0].QualityProfileID != hdProfile.ID {
		t.Errorf("want profile snapshot = %s, want %s", wants[0].QualityProfileID, hdProfile.ID)
	}

	requesters, err := app.Services.Tracking.ListRequesters(ctx, trackingID)
	if err != nil {
		t.Fatalf("list requesters: %v", err)
	}
	if len(requesters) != 1 {
		t.Fatalf("requester count = %d, want 1", len(requesters))
	}

	// Dedup / join: a second approved user requests the same movie.
	second, err := app.Repo.CreateUser(ctx, repo.CreateUserParams{
		Email:        "second@example.com",
		Username:     "second",
		PasswordHash: "x",
		IsActive:     true,
	})
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if _, err := app.Repo.UpsertUserPolicy(ctx, second.ID, true); err != nil {
		t.Fatalf("upsert second policy: %v", err)
	}

	req2, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: second.ID,
		TmdbID:      spawnTmdbID,
		Type:        "movie",
		Tier:        "HD",
	})
	if err != nil {
		t.Fatalf("create second request: %v", err)
	}
	if req2.SpawnedTrackingID == nil || *req2.SpawnedTrackingID != trackingID {
		t.Errorf("second request spawnedTrackingId = %v, want join onto %s", req2.SpawnedTrackingID, trackingID)
	}

	// Still one tracking, still one want, now two requesters.
	if trackings, err := app.Services.Tracking.List(ctx); err != nil {
		t.Fatalf("list trackings (2): %v", err)
	} else if len(trackings) != 1 {
		t.Errorf("tracking count after join = %d, want 1", len(trackings))
	}
	if wants, err := app.Services.Tracking.ListWants(ctx, trackingID); err != nil {
		t.Fatalf("list wants (2): %v", err)
	} else if len(wants) != 1 {
		t.Errorf("want count after join = %d, want 1 (no duplicate)", len(wants))
	}
	if requesters, err := app.Services.Tracking.ListRequesters(ctx, trackingID); err != nil {
		t.Fatalf("list requesters (2): %v", err)
	} else if len(requesters) != 2 {
		t.Errorf("requester count after join = %d, want 2", len(requesters))
	}
}

// TestSpawn_NotAutoApproved proves the default-deny branch: a requester without
// auto-approval gets a pending request and no spawn.
func TestSpawn_NotAutoApproved(t *testing.T) {
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
	if _, err := app.Repo.UpsertUserPolicy(ctx, admin.ID, false); err != nil {
		t.Fatalf("upsert admin policy: %v", err)
	}

	req, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: admin.ID,
		TmdbID:      spawnTmdbID,
		Type:        "movie",
		Tier:        "HD",
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if req.Status != string(model.RequestPending) {
		t.Errorf("request status = %q, want pending", req.Status)
	}
	if req.SpawnedTrackingID != nil {
		t.Errorf("pending request spawnedTrackingId = %v, want nil", req.SpawnedTrackingID)
	}

	if trackings, err := app.Services.Tracking.List(ctx); err != nil {
		t.Fatalf("list trackings: %v", err)
	} else if len(trackings) != 0 {
		t.Errorf("tracking count = %d, want 0 (no spawn)", len(trackings))
	}
}

// TestSpawn_UnboundTier proves a tier with no profile binding surfaces as
// NotFound. Defaults are deliberately not seeded, so 'HD' is a valid tier with
// no binding.
func TestSpawn_UnboundTier(t *testing.T) {
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

	admin := adminUser(t, app, ctx)
	if _, err := app.Repo.UpsertUserPolicy(ctx, admin.ID, true); err != nil {
		t.Fatalf("upsert admin policy: %v", err)
	}

	_, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: admin.ID,
		TmdbID:      spawnTmdbID,
		Type:        "movie",
		Tier:        "HD",
	})
	if !apperrors.IsNotFound(err) {
		t.Errorf("create with unbound tier = %v, want NotFound", err)
	}
}

// TestSpawn_RejectsSeries proves the PoC validation rejects a series request
// before any TMDB or DB work.
func TestSpawn_RejectsSeries(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	admin := adminUser(t, app, ctx)

	_, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: admin.ID,
		TmdbID:      spawnTmdbID,
		Type:        "series",
		Tier:        "HD",
	})
	if !apperrors.IsValidation(err) {
		t.Errorf("create series request = %v, want Validation", err)
	}
}
