//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// twoSegmentTracking bundles a series tracking with one want in each autonomy
// segment (backfill and ongoing), seeded with explicit segments so the autonomy
// tests don't depend on air-date classification (covered by the model unit test).
type twoSegmentTracking struct {
	trackingID   uuid.UUID
	backfillWant model.Want
	ongoingWant  model.Want
}

// seedTwoSegmentTracking seeds a series tracking (autonomy auto/auto) with two
// pending episode wants — one 'backfill', one 'ongoing'. hold is set per want by
// the caller or left NULL.
func seedTwoSegmentTracking(t *testing.T, ctx context.Context, r *repo.Repository, backfillHold, ongoingHold *string) twoSegmentTracking {
	t.Helper()

	tmdbID := int64(1400)
	media, err := r.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type:   "series",
		Title:  "Autonomy Show",
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("create media item: %v", err)
	}
	season, err := r.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: media.ID, SeasonNumber: 1})
	if err != nil {
		t.Fatalf("upsert season: %v", err)
	}
	e1Tmdb, e2Tmdb := int64(9001), int64(9002)
	ep1, err := r.UpsertEpisode(ctx, repo.UpsertEpisodeParams{SeasonID: season.ID, EpisodeNumber: 1, TmdbID: &e1Tmdb})
	if err != nil {
		t.Fatalf("upsert episode 1: %v", err)
	}
	ep2, err := r.UpsertEpisode(ctx, repo.UpsertEpisodeParams{SeasonID: season.ID, EpisodeNumber: 2, TmdbID: &e2Tmdb})
	if err != nil {
		t.Fatalf("upsert episode 2: %v", err)
	}

	bluray1080 := parsing.BinKey{Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModNone}
	profile, err := r.CreateQualityProfile(ctx, repo.CreateQualityProfileParams{
		Name:       "HD-Series",
		Domain:     "series",
		Bins:       []parsing.BinKey{bluray1080},
		Cutoff:     bluray1080,
		MinSeeders: 0,
	})
	if err != nil {
		t.Fatalf("create quality profile: %v", err)
	}

	tracking, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		State:            string(model.TrackingActive),
		Scope:            "all",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
		AutonomyBackfill: string(model.AutonomyAuto),
		AutonomyOngoing:  string(model.AutonomyAuto),
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	backfillWant, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		EpisodeID:        &ep1.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
		Segment:          string(model.WantSegmentBackfill),
		Hold:             backfillHold,
	})
	if err != nil {
		t.Fatalf("create backfill want: %v", err)
	}
	ongoingWant, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		EpisodeID:        &ep2.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
		Segment:          string(model.WantSegmentOngoing),
		Hold:             ongoingHold,
	})
	if err != nil {
		t.Fatalf("create ongoing want: %v", err)
	}

	return twoSegmentTracking{trackingID: tracking.ID, backfillWant: backfillWant, ongoingWant: ongoingWant}
}

func newTrackingService(r *repo.Repository) *service.TrackingService {
	return service.NewTrackingService(r, service.NewWantService(r, service.NewDownloadJobsService(r)), service.NewAuthzService(r))
}

// TestTrackingAutonomy_SetManualHoldsPendingWants proves the segment-scoped hold:
// flipping backfill→manual (ongoing stays auto) holds only the backfill want —
// still 'pending', now hold='needs_pick' — and leaves the ongoing want untouched.
func TestTrackingAutonomy_SetManualHoldsPendingWants(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedTwoSegmentTracking(t, ctx, r, nil, nil)
	svc := newTrackingService(r)

	if _, err := svc.SetAutonomy(ctx, seeded.trackingID, string(model.AutonomyManual), string(model.AutonomyAuto)); err != nil {
		t.Fatalf("SetAutonomy: %v", err)
	}

	backfill, err := r.GetWant(ctx, seeded.backfillWant.ID)
	if err != nil {
		t.Fatalf("get backfill want: %v", err)
	}
	if backfill.Status != string(model.WantPending) {
		t.Errorf("backfill want status = %q, want pending (hold doesn't change status)", backfill.Status)
	}
	if backfill.Hold == nil || *backfill.Hold != model.WantHoldNeedsPick {
		t.Errorf("backfill want hold = %v, want %q", backfill.Hold, model.WantHoldNeedsPick)
	}

	ongoing, err := r.GetWant(ctx, seeded.ongoingWant.ID)
	if err != nil {
		t.Fatalf("get ongoing want: %v", err)
	}
	if ongoing.Hold != nil {
		t.Errorf("ongoing want hold = %q, want nil (its segment stayed auto)", *ongoing.Hold)
	}
}

// TestTrackingAutonomy_HeldWantNotClaimed proves the claim guard: a held, due want
// is skipped by ClaimRunnableWants while held, then picked up once released.
func TestTrackingAutonomy_HeldWantNotClaimed(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedTwoSegmentTracking(t, ctx, r, nil, nil)
	svc := newTrackingService(r)

	// Hold the backfill segment. Both wants are due (next_run_at default now()),
	// so only the hold — not scheduling — should keep it out of the claim set.
	if _, err := svc.SetAutonomy(ctx, seeded.trackingID, string(model.AutonomyManual), string(model.AutonomyAuto)); err != nil {
		t.Fatalf("SetAutonomy manual: %v", err)
	}

	claimed, err := r.ClaimRunnableWants(ctx, 20)
	if err != nil {
		t.Fatalf("claim runnable wants: %v", err)
	}
	if containsWant(claimed, seeded.backfillWant.ID) {
		t.Errorf("held backfill want was claimed, want skipped")
	}
	if !containsWant(claimed, seeded.ongoingWant.ID) {
		t.Errorf("ongoing (auto) want not claimed, want claimed")
	}

	// Release the backfill segment: the want re-arms and the next claim picks it up.
	if _, err := svc.SetAutonomy(ctx, seeded.trackingID, string(model.AutonomyAuto), string(model.AutonomyAuto)); err != nil {
		t.Fatalf("SetAutonomy auto: %v", err)
	}
	claimed2, err := r.ClaimRunnableWants(ctx, 20)
	if err != nil {
		t.Fatalf("claim runnable wants (2): %v", err)
	}
	if !containsWant(claimed2, seeded.backfillWant.ID) {
		t.Errorf("released backfill want not claimed, want claimed")
	}
}

// TestTrackingAutonomy_SetAutoReleasesHeld proves the release path: a want born
// held (manual segment) has its hold cleared and next_run_at re-armed to now when
// the segment flips back to auto.
func TestTrackingAutonomy_SetAutoReleasesHeld(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	hold := model.WantHoldNeedsPick
	seeded := seedTwoSegmentTracking(t, ctx, r, &hold, nil)
	// Reflect the seeded hold in the tracking's dial so SetAutonomy sees a change.
	if _, err := r.SetTrackingAutonomy(ctx, seeded.trackingID, string(model.AutonomyManual), string(model.AutonomyAuto)); err != nil {
		t.Fatalf("seed manual dial: %v", err)
	}
	svc := newTrackingService(r)

	if _, err := svc.SetAutonomy(ctx, seeded.trackingID, string(model.AutonomyAuto), string(model.AutonomyAuto)); err != nil {
		t.Fatalf("SetAutonomy auto: %v", err)
	}

	got, err := r.GetWant(ctx, seeded.backfillWant.ID)
	if err != nil {
		t.Fatalf("get backfill want: %v", err)
	}
	if got.Hold != nil {
		t.Errorf("backfill want hold = %q, want nil (released)", *got.Hold)
	}
	if got.NextRunAt.After(time.Now()) {
		t.Errorf("backfill want next_run_at = %v, want <= now (re-armed)", got.NextRunAt)
	}
}

// TestTrackingAutonomy_AcceptsPropose proves 'propose' is a settable dial and a
// pure dial change: setting the backfill segment to propose accepts the value and
// leaves its pending wants unheld (claimable), so the worker proposes each on its
// next tick rather than the dial pre-holding them the way manual does.
func TestTrackingAutonomy_AcceptsPropose(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedTwoSegmentTracking(t, ctx, r, nil, nil)
	svc := newTrackingService(r)

	updated, err := svc.SetAutonomy(ctx, seeded.trackingID, string(model.AutonomyPropose), string(model.AutonomyAuto))
	if err != nil {
		t.Fatalf("SetAutonomy(propose) err = %v, want nil", err)
	}
	if updated.AutonomyBackfill != string(model.AutonomyPropose) {
		t.Errorf("backfill dial = %q, want propose", updated.AutonomyBackfill)
	}

	// Pure-dial semantic: unlike manual, propose does not pre-hold. The backfill
	// want stays pending and unheld, so ClaimRunnableWants still picks it up (the
	// propose fork happens later, in ProcessWant).
	got, err := r.GetWant(ctx, seeded.backfillWant.ID)
	if err != nil {
		t.Fatalf("get backfill want: %v", err)
	}
	if got.Hold != nil {
		t.Errorf("backfill want hold = %q, want nil (propose does not pre-hold)", *got.Hold)
	}
}

// TestManualSeriesGrab_LinksAndAdvancesWant proves the series manual-grab join
// onto the want spine: a held episode want, when its release is hand-grabbed via
// EnqueueSeriesCandidate, is flipped to 'grabbed' (hold cleared) and linked to the
// download_job so the download worker advances it.
func TestManualSeriesGrab_LinksAndAdvancesWant(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingEpisodeWant(t, ctx, r)

	// Put the want under a manual hold, as flipping its segment to manual would.
	hold := model.WantHoldNeedsPick
	if _, err := r.SetWantHold(ctx, seeded.want.ID, &hold); err != nil {
		t.Fatalf("set want hold: %v", err)
	}

	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnTVDetails(1399, tmdb.TVDetails{ID: 1399, Name: "Game of Thrones", FirstAirDate: "2011-04-17"})
	tmdbSrv.OnTVEpisodeDetails(1399, 3, 5, tmdb.TVEpisodeDetails{ID: 63103, Name: "Mhysa"})

	log := logger.New(true)
	tmdbSvc := service.NewTmdbServiceWithClient(r, log, tmdbClient)
	media := service.NewMediaService(r, log, tmdbSvc, service.NewSettingsService(r))
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{seriesCannedResult()}, nil
		},
	}
	svc := service.NewDownloadCandidatesService(r, log, source, media, service.NewRoutingService(r))

	season, episode := 3, 5
	if _, err := svc.SearchSeriesDownloadCandidates(ctx, 1399, &season, &episode); err != nil {
		t.Fatalf("SearchSeriesDownloadCandidates: %v", err)
	}
	canned := seriesCannedResult()
	_, job, err := svc.EnqueueSeriesCandidate(ctx, 1399, canned.IndexerID, canned.GUID, &season, &episode)
	if err != nil {
		t.Fatalf("EnqueueSeriesCandidate: %v", err)
	}

	got, err := r.GetWant(ctx, seeded.want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if got.Status != string(model.WantGrabbed) {
		t.Errorf("want status = %q, want grabbed", got.Status)
	}
	if got.Hold != nil {
		t.Errorf("want hold = %q, want nil (grab clears hold)", *got.Hold)
	}

	linked, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != seeded.want.ID {
		t.Errorf("linked wants = %+v, want [%s]", linked, seeded.want.ID)
	}
}

// seriesCannedResult is a single-episode series release the stub source returns.
func seriesCannedResult() indexer.SearchResult {
	return indexer.SearchResult{
		IndexerID:   7,
		IndexerName: "test-indexer",
		GUID:        "series-canned-guid",
		Title:       "Game of Thrones S03E05 1080p BluRay x264",
		DownloadURL: "http://localhost/series-canned.torrent",
		Protocol:    "torrent",
		Size:        3 << 30,
		Categories:  []string{"TV"},
	}
}

func containsWant(wants []model.Want, id uuid.UUID) bool {
	for _, w := range wants {
		if w.ID == id {
			return true
		}
	}
	return false
}
