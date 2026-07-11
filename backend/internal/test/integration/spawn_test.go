//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/scope"
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

	// The admin auto-approves via its role grants (requests.auto_approve:*).
	admin := adminUser(t, app, ctx)

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
	// The second user must auto-approve to spawn-and-join; 'co_admin' holds both
	// requests.create:movie:hd and requests.auto_approve:movie:hd.
	if err := app.Services.Users.AssignRole(ctx, second.ID, "co_admin"); err != nil {
		t.Fatalf("assign second co_admin role: %v", err)
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

// TestSpawn_MovieBornWithMetadata proves Layer 1: a spawned movie lands
// metadata-complete synchronously — the spawn applies the TMDB payload it
// already fetched inside its tx, so the poster/overview/runtime/status/genres
// are present the instant the row exists, with metadata_updated_at set so the
// enrichment worker won't re-fetch it.
func TestSpawn_MovieBornWithMetadata(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(spawnTmdbID, tmdb.MovieDetails{
		ID:          spawnTmdbID,
		Title:       "The Matrix",
		ReleaseDate: "1999-03-31",
		PosterPath:  "/matrix.jpg",
		Overview:    "A hacker learns the truth about his reality.",
		Runtime:     136,
		VoteMetrics: tmdb.VoteMetrics{VoteAverage: 8.2},
		Status:      "Released",
		Genres:      []tmdb.Genre{{ID: 28, Name: "Action"}, {ID: 878, Name: "Science Fiction"}},
	})
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()

	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	admin := adminUser(t, app, ctx)

	if _, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: admin.ID,
		TmdbID:      spawnTmdbID,
		Type:        "movie",
		Tier:        "HD",
	}); err != nil {
		t.Fatalf("create request: %v", err)
	}

	// Read the row straight back: the spawn applied the full payload, so no
	// worker sweep is needed for the metadata to be present.
	item, err := app.Repo.GetMediaItemByTmdbID(ctx, spawnTmdbID)
	if err != nil {
		t.Fatalf("get media item by tmdb id: %v", err)
	}
	if item.PosterPath == nil || *item.PosterPath == "" {
		t.Errorf("posterPath = %v, want populated at birth", item.PosterPath)
	}
	if item.Overview == nil || *item.Overview == "" {
		t.Errorf("overview = %v, want populated at birth", item.Overview)
	}
	if item.Runtime == nil || *item.Runtime != 136 {
		t.Errorf("runtime = %v, want 136", item.Runtime)
	}
	if item.Status == nil || *item.Status == "" {
		t.Errorf("status = %v, want populated at birth", item.Status)
	}
	if item.VoteAverage == nil || *item.VoteAverage <= 0 {
		t.Errorf("voteAverage = %v, want the fixture's positive vote at birth", item.VoteAverage)
	}
	if len(item.Genres) == 0 {
		t.Errorf("genres empty, want populated at birth")
	}
	if item.MetadataUpdatedAt == nil {
		t.Errorf("metadataUpdatedAt = nil, want set at birth (keeps worker from re-fetching)")
	}
}

const spawnSeriesTmdbID = int64(1396) // Breaking Bad

// TestSpawn_Series drives the series spawn branch end-to-end through the service:
// an approved series request spawns a tracking + requester (scope 'all'), then
// the post-commit structure sync + reconcile produce a pending want for each
// dated episode — the aired pilot due immediately (next_run_at in the past), the
// future episode deferred to its air date (next_run_at in the future).
func TestSpawn_Series(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnTVDetails(spawnSeriesTmdbID, tmdb.TVDetails{
		ID:             spawnSeriesTmdbID,
		Name:           "Breaking Bad",
		FirstAirDate:   "2008-01-20",
		Seasons:        []tmdb.Season{{SeasonNumber: 1}},
		PosterPath:     "/breaking-bad.jpg",
		Overview:       "A chemistry teacher turns to making meth.",
		VoteMetrics:    tmdb.VoteMetrics{VoteAverage: 8.9},
		Status:         "Ended",
		Genres:         []tmdb.Genre{{ID: 18, Name: "Drama"}, {ID: 80, Name: "Crime"}},
		EpisodeRunTime: []int{47},
	})
	tmdbSrv.OnTVSeasonDetails(spawnSeriesTmdbID, 1,
		tmdbtest.SeasonEpisode{ID: 62085, EpisodeNumber: 1, Name: "Pilot", AirDate: "2008-01-20"},
		tmdbtest.SeasonEpisode{ID: 62086, EpisodeNumber: 2, Name: "Future", AirDate: "2099-01-20"},
	)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()

	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	admin := adminUser(t, app, ctx)

	req, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: admin.ID,
		TmdbID:      spawnSeriesTmdbID,
		Type:        "series",
		Tier:        "HD",
	})
	if err != nil {
		t.Fatalf("create series request: %v", err)
	}
	if req.Status != string(model.RequestSpawned) {
		t.Errorf("request status = %q, want spawned", req.Status)
	}
	if req.SpawnedTrackingID == nil {
		t.Fatalf("spawned series request has nil spawnedTrackingId")
	}
	trackingID := *req.SpawnedTrackingID

	tracking, err := app.Services.Tracking.Get(ctx, trackingID)
	if err != nil {
		t.Fatalf("get tracking: %v", err)
	}
	if tracking.State != string(model.TrackingActive) {
		t.Errorf("tracking state = %q, want active", tracking.State)
	}

	requesters, err := app.Services.Tracking.ListRequesters(ctx, trackingID)
	if err != nil {
		t.Fatalf("list requesters: %v", err)
	}
	if len(requesters) != 1 {
		t.Fatalf("requester count = %d, want 1", len(requesters))
	}
	if requesters[0].ScopeRule != string(scope.RuleAll) {
		t.Errorf("requester scopeRule = %q, want all", requesters[0].ScopeRule)
	}

	// The reconciler produced a want per dated episode: the aired pilot due now,
	// the future episode deferred to its air date.
	wants, err := app.Services.Tracking.ListWants(ctx, trackingID)
	if err != nil {
		t.Fatalf("list wants: %v", err)
	}
	if len(wants) != 2 {
		t.Fatalf("want count = %d, want 2 (pilot + future episode)", len(wants))
	}
	now := time.Now()
	var dueNow, deferred int
	for _, w := range wants {
		if w.Status != string(model.WantPending) {
			t.Errorf("want status = %q, want pending", w.Status)
		}
		if w.EpisodeID == nil {
			t.Errorf("series want has nil episodeId")
		}
		if w.NextRunAt.After(now) {
			deferred++
		} else {
			dueNow++
		}
	}
	if dueNow != 1 || deferred != 1 {
		t.Errorf("want scheduling = %d due-now / %d deferred, want 1 / 1 (pilot due, future deferred)", dueNow, deferred)
	}

	// The series media_item was born with the full TMDB payload applied inside
	// the spawn tx — poster/overview/status/genres present immediately, and
	// metadata_updated_at set so the enrichment worker won't re-fetch it.
	seriesItem, err := app.Repo.GetMediaItemByTmdbID(ctx, spawnSeriesTmdbID)
	if err != nil {
		t.Fatalf("get series media item: %v", err)
	}
	if seriesItem.PosterPath == nil || *seriesItem.PosterPath == "" {
		t.Errorf("series posterPath = %v, want populated at birth", seriesItem.PosterPath)
	}
	if seriesItem.Overview == nil || *seriesItem.Overview == "" {
		t.Errorf("series overview = %v, want populated at birth", seriesItem.Overview)
	}
	if seriesItem.Status == nil || *seriesItem.Status == "" {
		t.Errorf("series status = %v, want populated at birth", seriesItem.Status)
	}
	if seriesItem.Runtime == nil || *seriesItem.Runtime != 47 {
		t.Errorf("series runtime = %v, want 47 (first episode runtime)", seriesItem.Runtime)
	}
	if len(seriesItem.Genres) == 0 {
		t.Errorf("series genres empty, want populated at birth")
	}
	if seriesItem.MetadataUpdatedAt == nil {
		t.Errorf("series metadataUpdatedAt = nil, want set at birth")
	}
}

// TestSpawn_NotAutoApproved proves the default-deny branch: a requester (who
// holds requests.create:movie:hd but not requests.auto_approve:movie:hd) gets a
// pending request and no spawn.
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

	requester, _ := app.UserToken(t, ctx, "requester1", "requester")

	req, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: requester.ID,
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
