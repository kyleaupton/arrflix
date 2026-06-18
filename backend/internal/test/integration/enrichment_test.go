//go:build integration

package integration

import (
	"context"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/scope"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

const selfHealSeriesTmdbID = int64(82856) // The Mandalorian

// TestEnrichSeries_SelfHealReconcile is the Phase 5 self-heal loop: a tracked
// series whose only episode is undated (air_date TBA) gets no want from the
// spawn-time reconcile — there's no anchor to schedule against; once TMDB
// announces the air date, the next enrichment re-syncs the tree and the post-sync
// reconcile trigger produces a pending want autonomously — closing the loop with
// no second request.
//
// The episode tree is seeded directly (rather than via spawn) so enrichment's
// season fetch — and the TMDB cache it populates — happens exactly once, with the
// announced air_date. Production reaches the same state when the 7-day metadata
// staleness window lets enrichment re-sync after TMDB fills the date in.
func TestEnrichSeries_SelfHealReconcile(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()

	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	profile, err := app.Services.QualityProfiles.ResolveByTier(ctx, qualityprofile.TierHD, parsing.DomainSeries)
	if err != nil {
		t.Fatalf("resolve HD series profile: %v", err)
	}

	// Tracked series with a single, undated episode (air_date TBA / NULL).
	tmdbID := selfHealSeriesTmdbID
	item, err := app.Repo.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
		Type:   string(parsing.DomainSeries),
		Title:  "Self Heal Show",
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("upsert media item: %v", err)
	}
	season, err := app.Repo.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: item.ID, SeasonNumber: 1})
	if err != nil {
		t.Fatalf("upsert season: %v", err)
	}
	episodeTmdbID := int64(900001)
	if _, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
		SeasonID:      season.ID,
		EpisodeNumber: 1,
		AirDate:       nil,
		TmdbID:        &episodeTmdbID,
	}); err != nil {
		t.Fatalf("upsert episode: %v", err)
	}

	tracking, err := app.Repo.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      item.ID,
		QualityProfileID: profile.ID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	admin := adminUser(t, app, ctx)
	if _, err := app.Repo.AddRequester(ctx, repo.AddRequesterParams{
		TrackingID: tracking.ID,
		UserID:     admin.ID,
		Tier:       "HD",
		ScopeRule:  string(scope.RuleAll),
	}); err != nil {
		t.Fatalf("add requester: %v", err)
	}

	// First reconcile: the episode is undated, so it gets no want yet.
	if err := app.Services.Reconcile.Reconcile(ctx, tracking.ID); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if wants, err := app.Services.Tracking.ListWants(ctx, tracking.ID); err != nil {
		t.Fatalf("list wants after first reconcile: %v", err)
	} else if len(wants) != 0 {
		t.Fatalf("want count after first reconcile = %d, want 0 (episode undated)", len(wants))
	}

	// TMDB announces the air date. Enrichment re-syncs the tree — the stub reports
	// the same episode (same TMDB id → updated in place) now carrying an air_date —
	// then the post-sync reconcile trigger fires.
	tmdbSrv.OnTVDetails(selfHealSeriesTmdbID, tmdb.TVDetails{
		ID:           selfHealSeriesTmdbID,
		Name:         "Self Heal Show",
		FirstAirDate: "2019-11-12",
		Seasons:      []tmdb.Season{{SeasonNumber: 1}},
	})
	tmdbSrv.OnTVSeasonDetails(selfHealSeriesTmdbID, 1,
		tmdbtest.SeasonEpisode{ID: episodeTmdbID, EpisodeNumber: 1, Name: "Chapter 1", AirDate: "2019-11-12"},
	)

	if err := app.Services.Enrichment.EnrichMediaItem(ctx, item); err != nil {
		t.Fatalf("re-enrich: %v", err)
	}

	// The post-sync reconcile produced the want the now-dated episode is owed.
	wants, err := app.Services.Tracking.ListWants(ctx, tracking.ID)
	if err != nil {
		t.Fatalf("list wants after enrich: %v", err)
	}
	if len(wants) != 1 {
		t.Fatalf("want count after enrich = %d, want 1 (episode now dated)", len(wants))
	}
	if wants[0].Status != string(model.WantPending) {
		t.Errorf("self-healed want status = %q, want pending", wants[0].Status)
	}
	if wants[0].EpisodeID == nil {
		t.Errorf("self-healed want has nil episodeId")
	}
}

const untrackedSeriesTmdbID = int64(1234567)

// TestEnrichSeries_UntrackedNoReconcile proves the FindTrackingByMediaItem guard:
// enriching an untracked series — even one whose episode has aired — completes
// without error and creates no tracking, so the reconcile trigger never fires.
// Without the NotFound guard, enrichSeries would surface the lookup miss as an
// error rather than returning cleanly.
func TestEnrichSeries_UntrackedNoReconcile(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)

	const episodeTmdbID = int64(900002)
	tmdbSrv.OnTVDetails(untrackedSeriesTmdbID, tmdb.TVDetails{
		ID:           untrackedSeriesTmdbID,
		Name:         "Untracked Show",
		FirstAirDate: "2010-01-01",
		Seasons:      []tmdb.Season{{SeasonNumber: 1}},
	})
	tmdbSrv.OnTVSeasonDetails(untrackedSeriesTmdbID, 1,
		tmdbtest.SeasonEpisode{ID: episodeTmdbID, EpisodeNumber: 1, Name: "Pilot", AirDate: "2010-01-01"},
	)

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()

	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}

	tmdbID := untrackedSeriesTmdbID
	item, err := app.Repo.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type:   "series",
		Title:  "Untracked Show",
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("create media item: %v", err)
	}

	if err := app.Services.Enrichment.EnrichMediaItem(ctx, item); err != nil {
		t.Fatalf("enrich untracked series: %v", err)
	}

	// No tracking was ever created, so the reconcile guard returned early and no
	// want exists (wants are reachable only through a tracking).
	if trackings, err := app.Services.Tracking.List(ctx); err != nil {
		t.Fatalf("list trackings: %v", err)
	} else if len(trackings) != 0 {
		t.Errorf("tracking count = %d, want 0 (enrich must not spawn a tracking)", len(trackings))
	}
}
