//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// TestRefresh_BornSeriesScheduledNotDue proves the Layer-2a scheduling floor: a
// series born via spawn lands with a non-NULL next_refresh_at (scheduled from
// its state), so the due sweep does not immediately re-pick it. Before this
// layer a born item had next_refresh_at = NULL → due on the very next tick.
func TestRefresh_BornSeriesScheduledNotDue(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)

	const bornSeriesID = int64(1396) // Breaking Bad, ended
	tmdbSrv.OnTVDetails(bornSeriesID, tmdb.TVDetails{
		ID:           bornSeriesID,
		Name:         "Breaking Bad",
		FirstAirDate: "2008-01-20",
		Status:       "Ended",
		Seasons:      []tmdb.Season{{SeasonNumber: 1}},
	})
	tmdbSrv.OnTVSeasonDetails(bornSeriesID, 1,
		tmdbtest.SeasonEpisode{ID: 62085, EpisodeNumber: 1, Name: "Pilot", AirDate: "2008-01-20"},
	)

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()

	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	// The admin auto-approves via its role grants (requests.auto_approve:*).
	admin := adminUser(t, app, ctx)

	if _, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: admin.ID,
		TmdbID:      bornSeriesID,
		Type:        "series",
		Tier:        "HD",
	}); err != nil {
		t.Fatalf("create series request: %v", err)
	}

	item, err := app.Repo.GetMediaItemByTmdbID(ctx, bornSeriesID)
	if err != nil {
		t.Fatalf("get born series: %v", err)
	}
	if item.NextRefreshAt == nil {
		t.Fatal("born series next_refresh_at = nil, want scheduled (non-NULL)")
	}
	now := time.Now()
	if !item.NextRefreshAt.After(now) {
		t.Errorf("born series next_refresh_at = %v, want in the future (not immediately due)", item.NextRefreshAt)
	}
	if item.MetadataAttemptCount != 0 {
		t.Errorf("born series metadata_attempt_count = %d, want 0", item.MetadataAttemptCount)
	}

	// The due sweep as of now returns nothing — the only item is scheduled ahead.
	due, err := app.Repo.ListDueMediaItems(ctx, now, 100)
	if err != nil {
		t.Fatalf("list due media items: %v", err)
	}
	for _, d := range due {
		if d.ID == item.ID {
			t.Errorf("born series appears in the due queue immediately after birth (next_refresh_at=%v, now=%v)", item.NextRefreshAt, now)
		}
	}
}

// TestRefresh_AiringVsEndedCadence proves cadence-by-state: an airing series
// (continuing, next episode imminent) schedules ~daily, while an ended series
// schedules ~monthly — near vs far.
func TestRefresh_AiringVsEndedCadence(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)

	const airingID = int64(7001)
	const endedID = int64(7002)

	soon := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	tmdbSrv.OnTVDetails(airingID, tmdb.TVDetails{
		ID:               airingID,
		Name:             "Airing Show",
		FirstAirDate:     "2020-01-01",
		Status:           "Returning Series",
		InProduction:     true,
		NextEpisodeToAir: tmdb.NextEpisodeToAir{ID: 90001, SeasonNumber: 1, AirDate: soon},
		Seasons:          []tmdb.Season{{SeasonNumber: 1}},
	})
	tmdbSrv.OnTVSeasonDetails(airingID, 1,
		tmdbtest.SeasonEpisode{ID: 90001, EpisodeNumber: 1, Name: "Ep1", AirDate: "2020-01-01"},
	)

	tmdbSrv.OnTVDetails(endedID, tmdb.TVDetails{
		ID:           endedID,
		Name:         "Ended Show",
		FirstAirDate: "2000-01-01",
		Status:       "Ended",
		InProduction: false,
		Seasons:      []tmdb.Season{{SeasonNumber: 1}},
	})
	tmdbSrv.OnTVSeasonDetails(endedID, 1,
		tmdbtest.SeasonEpisode{ID: 80001, EpisodeNumber: 1, Name: "Ep1", AirDate: "2000-01-01"},
	)

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()

	airing := createSeries(t, app, ctx, airingID, "Airing Show")
	ended := createSeries(t, app, ctx, endedID, "Ended Show")

	before := time.Now()
	if err := app.Services.Enrichment.EnrichMediaItem(ctx, airing); err != nil {
		t.Fatalf("enrich airing: %v", err)
	}
	if err := app.Services.Enrichment.EnrichMediaItem(ctx, ended); err != nil {
		t.Fatalf("enrich ended: %v", err)
	}

	airingRow, err := app.Repo.GetMediaItemByTmdbID(ctx, airingID)
	if err != nil {
		t.Fatalf("get airing: %v", err)
	}
	endedRow, err := app.Repo.GetMediaItemByTmdbID(ctx, endedID)
	if err != nil {
		t.Fatalf("get ended: %v", err)
	}
	if airingRow.NextRefreshAt == nil || endedRow.NextRefreshAt == nil {
		t.Fatalf("next_refresh_at nil (airing=%v ended=%v)", airingRow.NextRefreshAt, endedRow.NextRefreshAt)
	}

	airingDelta := airingRow.NextRefreshAt.Sub(before)
	endedDelta := endedRow.NextRefreshAt.Sub(before)

	// Airing → daily tier (well under a week); ended → monthly tier (well over
	// three weeks). The exact intervals are unit-tested; here we only assert the
	// state actually drives near-vs-far scheduling end-to-end through the DB.
	if airingDelta > 7*24*time.Hour {
		t.Errorf("airing next_refresh_at Δ = %v, want daily-ish (< 7d)", airingDelta)
	}
	if endedDelta < 20*24*time.Hour {
		t.Errorf("ended next_refresh_at Δ = %v, want monthly-ish (> 20d)", endedDelta)
	}
	if !airingRow.NextRefreshAt.Before(*endedRow.NextRefreshAt) {
		t.Errorf("airing (%v) should be scheduled before ended (%v)", airingRow.NextRefreshAt, endedRow.NextRefreshAt)
	}
}

// TestRefresh_CanonicalFetchBypassesCache proves the freshness invariant: a
// canonical enrichment read consults upstream directly rather than serving a
// warm response-cache body. TMDB updates the record between two enrichments; if
// the second read went through the cache it would re-materialize the stale
// (first) body, but the bypass surfaces the fresh one.
func TestRefresh_CanonicalFetchBypassesCache(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)

	const movieID = int64(603) // The Matrix
	tmdbSrv.OnMovieDetails(movieID, tmdb.MovieDetails{
		ID:          movieID,
		Title:       "The Matrix",
		ReleaseDate: "1999-03-31",
		Overview:    "V1 overview",
	})

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()

	item, err := app.Repo.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type: "movie", Title: "The Matrix", TmdbID: ptrInt64(movieID),
	})
	if err != nil {
		t.Fatalf("create media item: %v", err)
	}

	// First enrich warms the response cache with the V1 body.
	if err := app.Services.Enrichment.EnrichMediaItem(ctx, item); err != nil {
		t.Fatalf("first enrich: %v", err)
	}
	first, err := app.Repo.GetMediaItemByTmdbID(ctx, movieID)
	if err != nil {
		t.Fatalf("get after first enrich: %v", err)
	}
	if first.Overview == nil || *first.Overview != "V1 overview" {
		t.Fatalf("overview after first enrich = %v, want %q", first.Overview, "V1 overview")
	}

	// TMDB updates the record. Re-register the same route with a fresh body.
	tmdbSrv.OnMovieDetails(movieID, tmdb.MovieDetails{
		ID:          movieID,
		Title:       "The Matrix",
		ReleaseDate: "1999-03-31",
		Overview:    "V2 overview",
	})

	// Second enrich: a cache-served read would re-apply V1; the bypass applies V2.
	if err := app.Services.Enrichment.EnrichMediaItem(ctx, first); err != nil {
		t.Fatalf("second enrich: %v", err)
	}
	second, err := app.Repo.GetMediaItemByTmdbID(ctx, movieID)
	if err != nil {
		t.Fatalf("get after second enrich: %v", err)
	}
	if second.Overview == nil || *second.Overview != "V2 overview" {
		t.Errorf("overview after second enrich = %v, want %q (fresh fetch bypassed the warm cache)", second.Overview, "V2 overview")
	}
}

// TestRefresh_FailingEnrichBacksOff proves the failure path: a sync that fails
// upstream advances metadata_attempt_count and pushes next_refresh_at out
// exponentially rather than retrying every tick.
func TestRefresh_FailingEnrichBacksOff(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)

	const goneID = int64(999999)
	tmdbSrv.OnMovieDetailsNotFound(goneID) // upstream 404 → enrich fails

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()

	item, err := app.Repo.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type: "movie", Title: "Gone Upstream", TmdbID: ptrInt64(goneID),
	})
	if err != nil {
		t.Fatalf("create media item: %v", err)
	}
	// Born with next_refresh_at NULL → immediately due.

	t0 := time.Now()
	if _, err := app.Services.Enrichment.EnrichBatch(ctx, t0, 100); err != nil {
		t.Fatalf("first EnrichBatch: %v", err)
	}

	after1, err := app.Repo.GetMediaItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("get after first batch: %v", err)
	}
	if after1.MetadataAttemptCount != 1 {
		t.Errorf("metadata_attempt_count after 1 failure = %d, want 1", after1.MetadataAttemptCount)
	}
	if after1.MetadataLastError == nil || *after1.MetadataLastError == "" {
		t.Errorf("metadata_last_error = %v, want the upstream failure recorded", after1.MetadataLastError)
	}
	if after1.NextRefreshAt == nil || !after1.NextRefreshAt.After(t0) {
		t.Fatalf("next_refresh_at after failure = %v, want pushed out past %v", after1.NextRefreshAt, t0)
	}
	firstBackoff := *after1.NextRefreshAt

	// A second batch once the back-off window has passed advances the counter and
	// pushes the next attempt out further (exponential growth).
	t1 := t0.Add(20 * time.Minute)
	if _, err := app.Services.Enrichment.EnrichBatch(ctx, t1, 100); err != nil {
		t.Fatalf("second EnrichBatch: %v", err)
	}
	after2, err := app.Repo.GetMediaItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("get after second batch: %v", err)
	}
	if after2.MetadataAttemptCount != 2 {
		t.Errorf("metadata_attempt_count after 2 failures = %d, want 2", after2.MetadataAttemptCount)
	}
	if after2.NextRefreshAt == nil || !after2.NextRefreshAt.After(firstBackoff) {
		t.Errorf("next_refresh_at after second failure = %v, want past the first back-off %v", after2.NextRefreshAt, firstBackoff)
	}
}

// createSeries inserts a bare series media_item for enrichment tests. Enrichment
// has no public "create bare item" endpoint (items are born by the import/spawn
// pipelines), so these tests seed the row directly — the same pattern the
// existing enrichment integration tests use.
func createSeries(t *testing.T, app *testapp.App, ctx context.Context, tmdbID int64, title string) model.MediaItem {
	t.Helper()
	it, err := app.Repo.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type: "series", Title: title, TmdbID: ptrInt64(tmdbID),
	})
	if err != nil {
		t.Fatalf("create series %q: %v", title, err)
	}
	return it
}
