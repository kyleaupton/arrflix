//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// assertInterval checks that next - now lands within a small tolerance of the
// expected tier interval. The tolerance absorbs the few microseconds between
// the test's `now` and the value cadence stamps; the tiers are minutes/hours
// apart, so this never aliases between tiers.
func assertInterval(t *testing.T, label string, now, next time.Time, want time.Duration) {
	t.Helper()
	got := next.Sub(now)
	const tol = 2 * time.Second
	if got < want-tol || got > want+tol {
		t.Errorf("%s: next-run interval = %v, want ~%v", label, got, want)
	}
}

// TestScheduler_NextSearchAt drives the resolver against real rows: a series
// want anchors on its episode air_date, a movie want on its release_date, and
// an undated episode falls back to the want's creation time. Each lands on the
// smart curve's expected tier.
func TestScheduler_NextSearchAt(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	seriesProfile, err := app.Services.QualityProfiles.ResolveByTier(ctx, qualityprofile.TierHD, parsing.DomainSeries)
	if err != nil {
		t.Fatalf("resolve series profile: %v", err)
	}
	movieProfile, err := app.Services.QualityProfiles.ResolveByTier(ctx, qualityprofile.TierHD, parsing.DomainMovie)
	if err != nil {
		t.Fatalf("resolve movie profile: %v", err)
	}

	now := time.Now()

	// air_date and release_date are DATE columns — date-only, no time component —
	// so an episode/movie anchor is always midnight UTC of that date. The sub-day
	// tiers (peak 15m, low 1h) are therefore unreachable from a real row and are
	// covered in cadence_test.go against raw timestamps; here we use day-grained
	// ages whose tier is stable under that truncation.

	// --- Series want: anchors on episode air_date (3d ago → catch-up tier, 6h). ---
	seriesTmdb := int64(1399)
	series, err := app.Repo.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
		Type:   string(parsing.DomainSeries),
		Title:  "Test Show",
		TmdbID: &seriesTmdb,
	})
	if err != nil {
		t.Fatalf("upsert series: %v", err)
	}
	season, err := app.Repo.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: series.ID, SeasonNumber: 1})
	if err != nil {
		t.Fatalf("upsert season: %v", err)
	}
	airedThreeDaysAgo := now.Add(-3 * 24 * time.Hour)
	epTmdb := int64(101)
	episode, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
		SeasonID:      season.ID,
		EpisodeNumber: 1,
		AirDate:       &airedThreeDaysAgo,
		TmdbID:        &epTmdb,
	})
	if err != nil {
		t.Fatalf("upsert episode: %v", err)
	}
	undatedTmdb := int64(102)
	undatedEp, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
		SeasonID:      season.ID,
		EpisodeNumber: 2,
		AirDate:       nil,
		TmdbID:        &undatedTmdb,
	})
	if err != nil {
		t.Fatalf("upsert undated episode: %v", err)
	}

	seriesTracking, err := app.Repo.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      series.ID,
		QualityProfileID: seriesProfile.ID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
	})
	if err != nil {
		t.Fatalf("create series tracking: %v", err)
	}

	seriesWant, err := app.Repo.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       seriesTracking.ID,
		MediaItemID:      series.ID,
		EpisodeID:        &episode.ID,
		QualityProfileID: seriesProfile.ID,
		Status:           string(model.WantPending),
	})
	if err != nil {
		t.Fatalf("create series want: %v", err)
	}
	next, err := app.Services.Scheduler.NextSearchAt(ctx, seriesWant, now)
	if err != nil {
		t.Fatalf("NextSearchAt(series): %v", err)
	}
	assertInterval(t, "series aired 3d ago", now, next, 6*time.Hour)

	// --- Undated episode: no air_date → anchor falls back to want.CreatedAt
	// (Δ ≈ 0) → low tier, 1h. Evaluate the curve relative to that anchor. ---
	undatedWant, err := app.Repo.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       seriesTracking.ID,
		MediaItemID:      series.ID,
		EpisodeID:        &undatedEp.ID,
		QualityProfileID: seriesProfile.ID,
		Status:           string(model.WantPending),
	})
	if err != nil {
		t.Fatalf("create undated want: %v", err)
	}
	next, err = app.Services.Scheduler.NextSearchAt(ctx, undatedWant, undatedWant.CreatedAt)
	if err != nil {
		t.Fatalf("NextSearchAt(undated): %v", err)
	}
	assertInterval(t, "undated episode -> CreatedAt fallback", undatedWant.CreatedAt, next, 1*time.Hour)

	// --- Movie want: anchors on release_date (10d ago → cold tier, 24h).
	// release_date is set through the metadata update path, not the upsert. ---
	movieTmdb := int64(550)
	movie, err := app.Repo.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
		Type:   string(parsing.DomainMovie),
		Title:  "Test Movie",
		TmdbID: &movieTmdb,
	})
	if err != nil {
		t.Fatalf("upsert movie: %v", err)
	}
	releasedTenDaysAgo := now.Add(-10 * 24 * time.Hour)
	movie, err = app.Repo.UpdateMediaItemMetadata(ctx, repo.UpdateMediaItemMetadataParams{
		ID:          movie.ID,
		ReleaseDate: &releasedTenDaysAgo,
	})
	if err != nil {
		t.Fatalf("update movie metadata: %v", err)
	}
	movieTracking, err := app.Repo.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      movie.ID,
		QualityProfileID: movieProfile.ID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
	})
	if err != nil {
		t.Fatalf("create movie tracking: %v", err)
	}
	movieWant, err := app.Repo.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       movieTracking.ID,
		MediaItemID:      movie.ID,
		EpisodeID:        nil,
		QualityProfileID: movieProfile.ID,
		Status:           string(model.WantPending),
	})
	if err != nil {
		t.Fatalf("create movie want: %v", err)
	}
	next, err = app.Services.Scheduler.NextSearchAt(ctx, movieWant, now)
	if err != nil {
		t.Fatalf("NextSearchAt(movie): %v", err)
	}
	assertInterval(t, "movie released 10d ago", now, next, 24*time.Hour)
}
