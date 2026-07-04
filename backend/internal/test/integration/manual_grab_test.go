//go:build integration

package integration

import (
	"context"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// seedMovieProfile seeds a media_item and an HD quality_profile, returning both.
// The decision-table tests build trackings/wants on top of these.
func seedMovieProfile(t *testing.T, ctx context.Context, r *repo.Repository) (model.MediaItem, model.QualityProfile) {
	t.Helper()

	year := int32(1999)
	tmdbID := int64(603)
	media, err := r.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type:   "movie",
		Title:  "The Matrix",
		Year:   &year,
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("create media item: %v", err)
	}

	bluray1080 := parsing.BinKey{Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModNone}
	profile, err := r.CreateQualityProfile(ctx, repo.CreateQualityProfileParams{
		Name:       "HD",
		Domain:     "movie",
		Bins:       []parsing.BinKey{bluray1080},
		Cutoff:     bluray1080,
		MinSeeders: 0,
	})
	if err != nil {
		t.Fatalf("create quality profile: %v", err)
	}
	return media, profile
}

// seedRoutingDefaults seeds the default downloader/library/name_template that
// RoutingService.Dispatch falls back to, so EnqueueCandidate can resolve every
// action slot without a rule-set.
func seedRoutingDefaults(t *testing.T, ctx context.Context, r *repo.Repository) {
	t.Helper()
	if _, err := r.CreateDownloader(ctx, repo.CreateDownloaderParams{
		Name:           "qbit",
		DownloaderType: "qbittorrent",
		Protocol:       "torrent",
		URL:            "http://localhost:8080",
		Enabled:        true,
		IsDefault:      true,
	}); err != nil {
		t.Fatalf("create downloader: %v", err)
	}
	if _, err := r.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name:      "Movies",
		Type:      "movie",
		RootPath:  "/movies",
		IsDefault: true,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if _, err := r.CreateNameTemplate(ctx, repo.CreateNameTemplateParams{
		Name:      "default",
		Type:      "movie",
		Template:  "{title}",
		IsDefault: true,
	}); err != nil {
		t.Fatalf("create name template: %v", err)
	}
}

// TestManualGrab_Untracked proves the "none" row of the decision table: with no
// prior want (created=true), GrabManualWant creates a want directly 'grabbed'
// with a NULL profile, and the fresh NULL-profile tracking round-trips.
func TestManualGrab_Untracked(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	media, _ := seedMovieProfile(t, ctx, r)

	// A manual grab on an untracked movie creates a NULL-profile tracking.
	tracking, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      media.ID,
		QualityProfileID: uuid.Nil,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
		AutonomyBackfill: string(model.AutonomyAuto),
		AutonomyOngoing:  string(model.AutonomyAuto),
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	if tracking.QualityProfileID != uuid.Nil {
		t.Errorf("tracking profile = %s, want NULL (uuid.Nil)", tracking.QualityProfileID)
	}

	want, err := service.GrabManualWant(ctx, r, tracking.ID, media.ID, true)
	if err != nil {
		t.Fatalf("GrabManualWant: %v", err)
	}
	if want.Status != string(model.WantGrabbed) {
		t.Errorf("want status = %q, want %q", want.Status, model.WantGrabbed)
	}
	if want.QualityProfileID != uuid.Nil {
		t.Errorf("want profile = %s, want NULL (uuid.Nil) for a manual grab", want.QualityProfileID)
	}
	if want.TrackingID != tracking.ID {
		t.Errorf("want tracking = %s, want %s", want.TrackingID, tracking.ID)
	}
}

// TestManualGrab_PreemptsPendingOrSearching proves the pre-empt row: a want in
// 'pending' or 'searching' (the autonomous flow's pre-grab states) is CAS-flipped
// to 'grabbed' in place — the same want id, so the worker's later grab CAS no-ops.
func TestManualGrab_PreemptsPendingOrSearching(t *testing.T) {
	t.Parallel()

	for _, prior := range []model.WantStatus{model.WantPending, model.WantSearching} {
		prior := prior
		t.Run(string(prior), func(t *testing.T) {
			t.Parallel()
			pool := dbtest.New(t)
			r := repo.New(pool)
			ctx := context.Background()

			media, profile := seedMovieProfile(t, ctx, r)
			tracking, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
				MediaItemID:      media.ID,
				QualityProfileID: profile.ID,
				State:            string(model.TrackingActive),
				Scope:            "self",
				UpgradeBehavior:  "none",
				ScheduleStrategy: "smart",
				AutonomyBackfill: string(model.AutonomyAuto),
				AutonomyOngoing:  string(model.AutonomyAuto),
			})
			if err != nil {
				t.Fatalf("create tracking: %v", err)
			}
			seed, err := r.CreateWant(ctx, repo.CreateWantParams{
				TrackingID:       tracking.ID,
				MediaItemID:      media.ID,
				QualityProfileID: profile.ID,
				Status:           string(prior),
				Segment:          string(model.WantSegmentOngoing),
			})
			if err != nil {
				t.Fatalf("create want: %v", err)
			}

			grabbed, err := service.GrabManualWant(ctx, r, tracking.ID, media.ID, false)
			if err != nil {
				t.Fatalf("GrabManualWant: %v", err)
			}
			if grabbed.ID != seed.ID {
				t.Errorf("grabbed want id = %s, want the pre-existing %s (flip in place)", grabbed.ID, seed.ID)
			}
			if grabbed.Status != string(model.WantGrabbed) {
				t.Errorf("want status = %q, want %q", grabbed.Status, model.WantGrabbed)
			}
		})
	}
}

// TestManualGrab_ReactivatesCanceled proves the reactivate row: a terminal
// 'canceled' want (whose tracking is also 'canceled') is flipped back to
// 'grabbed' and its tracking back to 'active'.
func TestManualGrab_ReactivatesCanceled(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	media, profile := seedMovieProfile(t, ctx, r)
	tracking, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		State:            string(model.TrackingCanceled),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
		AutonomyBackfill: string(model.AutonomyAuto),
		AutonomyOngoing:  string(model.AutonomyAuto),
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	seed, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantCanceled),
		Segment:          string(model.WantSegmentOngoing),
	})
	if err != nil {
		t.Fatalf("create want: %v", err)
	}

	grabbed, err := service.GrabManualWant(ctx, r, tracking.ID, media.ID, false)
	if err != nil {
		t.Fatalf("GrabManualWant: %v", err)
	}
	if grabbed.ID != seed.ID || grabbed.Status != string(model.WantGrabbed) {
		t.Errorf("grabbed want = %+v, want %s flipped to grabbed", grabbed, seed.ID)
	}

	got, err := r.GetTracking(ctx, tracking.ID)
	if err != nil {
		t.Fatalf("get tracking: %v", err)
	}
	if got.State != string(model.TrackingActive) {
		t.Errorf("tracking state = %q, want %q (reactivated)", got.State, model.TrackingActive)
	}
}

// TestManualGrab_ConflictWhenInFlight proves the reject rows: a want already
// 'grabbed', 'downloading', or 'available' is acquiring or in the library, so a
// manual grab is a Conflict ("cancel first").
func TestManualGrab_ConflictWhenInFlight(t *testing.T) {
	t.Parallel()

	for _, prior := range []model.WantStatus{model.WantGrabbed, model.WantDownloading, model.WantAvailable} {
		prior := prior
		t.Run(string(prior), func(t *testing.T) {
			t.Parallel()
			pool := dbtest.New(t)
			r := repo.New(pool)
			ctx := context.Background()

			media, profile := seedMovieProfile(t, ctx, r)
			tracking, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
				MediaItemID:      media.ID,
				QualityProfileID: profile.ID,
				State:            string(model.TrackingActive),
				Scope:            "self",
				UpgradeBehavior:  "none",
				ScheduleStrategy: "smart",
				AutonomyBackfill: string(model.AutonomyAuto),
				AutonomyOngoing:  string(model.AutonomyAuto),
			})
			if err != nil {
				t.Fatalf("create tracking: %v", err)
			}
			if _, err := r.CreateWant(ctx, repo.CreateWantParams{
				TrackingID:       tracking.ID,
				MediaItemID:      media.ID,
				QualityProfileID: profile.ID,
				Status:           string(prior),
				Segment:          string(model.WantSegmentOngoing),
			}); err != nil {
				t.Fatalf("create want: %v", err)
			}

			if _, err := service.GrabManualWant(ctx, r, tracking.ID, media.ID, false); !apperrors.IsConflict(err) {
				t.Errorf("GrabManualWant on %q want err = %v, want Conflict", prior, err)
			}
		})
	}
}

// TestWant_ManualGrabClearsHold proves a hand grab clears a needs_pick hold: a
// held pending want (its segment on a manual dial) is CAS-flipped to 'grabbed' in
// place with hold cleared, so an advancing want never carries a stale hold.
func TestWant_ManualGrabClearsHold(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	media, profile := seedMovieProfile(t, ctx, r)
	tracking, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
		AutonomyBackfill: string(model.AutonomyManual),
		AutonomyOngoing:  string(model.AutonomyManual),
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	hold := model.WantHoldNeedsPick
	seed, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
		Segment:          string(model.WantSegmentOngoing),
		Hold:             &hold,
	})
	if err != nil {
		t.Fatalf("create held want: %v", err)
	}

	grabbed, err := service.GrabManualWant(ctx, r, tracking.ID, media.ID, false)
	if err != nil {
		t.Fatalf("GrabManualWant: %v", err)
	}
	if grabbed.ID != seed.ID {
		t.Errorf("grabbed want id = %s, want the held %s (flip in place)", grabbed.ID, seed.ID)
	}
	if grabbed.Status != string(model.WantGrabbed) {
		t.Errorf("want status = %q, want %q", grabbed.Status, model.WantGrabbed)
	}
	if grabbed.Hold != nil {
		t.Errorf("want hold = %q, want nil (grab clears hold)", *grabbed.Hold)
	}
}

// TestEnqueueCandidate_UntrackedYieldsLinkedWant proves the full manual-grab
// spine end-to-end through the service: an untracked movie's hand-picked release
// creates a NULL-profile tracking + a 'grabbed' NULL-profile want, and the
// download_job is linked to that want.
func TestEnqueueCandidate_UntrackedYieldsLinkedWant(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(603, tmdb.MovieDetails{ID: 603, Title: "The Matrix", ReleaseDate: "1999-03-31"})

	seedRoutingDefaults(t, ctx, r)

	log := logger.New(true)
	tmdbSvc := service.NewTmdbServiceWithClient(r, log, tmdbClient)
	media := service.NewMediaService(r, log, tmdbSvc, service.NewSettingsService(r))
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{cannedResult()}, nil
		},
	}
	svc := service.NewDownloadCandidatesService(r, log, source, media, service.NewRoutingService(r))

	// Populate the candidate cache: EnqueueCandidate grabs by (indexerID, guid).
	if _, err := svc.SearchDownloadCandidates(ctx, 603); err != nil {
		t.Fatalf("SearchDownloadCandidates: %v", err)
	}

	canned := cannedResult()
	_, job, err := svc.EnqueueCandidate(ctx, 603, canned.IndexerID, canned.GUID)
	if err != nil {
		t.Fatalf("EnqueueCandidate: %v", err)
	}
	linkedWants, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linkedWants) != 1 {
		t.Fatalf("download job linked wants = %d, want 1", len(linkedWants))
	}

	want, err := r.GetWant(ctx, linkedWants[0].ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if want.Status != string(model.WantGrabbed) {
		t.Errorf("want status = %q, want %q", want.Status, model.WantGrabbed)
	}
	if want.QualityProfileID != uuid.Nil {
		t.Errorf("want profile = %s, want NULL (uuid.Nil) for a manual grab", want.QualityProfileID)
	}

	tracking, err := r.GetTracking(ctx, want.TrackingID)
	if err != nil {
		t.Fatalf("get tracking: %v", err)
	}
	if tracking.QualityProfileID != uuid.Nil {
		t.Errorf("tracking profile = %s, want NULL (uuid.Nil)", tracking.QualityProfileID)
	}
	if tracking.State != string(model.TrackingActive) {
		t.Errorf("tracking state = %q, want %q", tracking.State, model.TrackingActive)
	}
}
