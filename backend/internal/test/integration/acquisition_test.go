//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/indexer"
	acquisitionworker "github.com/kyleaupton/arrflix/internal/jobs/acquisition"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// stubIndexerSource is a hand-rolled indexer.IndexerSource whose Search is
// driven by a SearchFn field. ListIndexers/Test are unused by the acquisition
// flow and return zero values.
type stubIndexerSource struct {
	SearchFn func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error)
}

func (s stubIndexerSource) Search(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
	return s.SearchFn(ctx, q)
}

func (s stubIndexerSource) ListIndexers(ctx context.Context) ([]indexer.IndexerInfo, error) {
	return nil, nil
}

func (s stubIndexerSource) Test(ctx context.Context) error { return nil }

// seedPendingWant seeds the full chain a want needs (media_item → quality_profile
// → tracking → want) plus the default downloader/library/name_template that
// RoutingService.Dispatch falls back to, and returns the pending want.
func seedPendingWant(t *testing.T, ctx context.Context, r *repo.Repository) model.Want {
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

	want, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
		Segment:          string(model.WantSegmentOngoing),
	})
	if err != nil {
		t.Fatalf("create want: %v", err)
	}

	// Defaults the routing fallback resolves: a torrent downloader, a movie
	// library, and a movie name template.
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

	return want
}

// seededEpisodeWant bundles a seeded episode want with the season/episode rows
// its download_job is expected to link to.
type seededEpisodeWant struct {
	want      model.Want
	seasonID  uuid.UUID
	episodeID uuid.UUID
}

// seedPendingEpisodeWant seeds the series chain an episode want needs — series
// media_item → season → episode (with a stored runtime+title) → series quality
// profile → series tracking → episode want — plus the series-type
// downloader/library/name_template defaults RoutingService.Dispatch falls back
// to. It returns the pending want with its season/episode ids.
func seedPendingEpisodeWant(t *testing.T, ctx context.Context, r *repo.Repository) seededEpisodeWant {
	t.Helper()

	tmdbID := int64(1399)
	media, err := r.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type:   "series",
		Title:  "Game of Thrones",
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("create media item: %v", err)
	}

	season, err := r.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: media.ID, SeasonNumber: 3})
	if err != nil {
		t.Fatalf("upsert season: %v", err)
	}
	epTitle := "Mhysa"
	epRuntime := int32(60)
	epTmdb := int64(63103)
	episode, err := r.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
		SeasonID:      season.ID,
		EpisodeNumber: 5,
		Title:         &epTitle,
		Runtime:       &epRuntime,
		TmdbID:        &epTmdb,
	})
	if err != nil {
		t.Fatalf("upsert episode: %v", err)
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

	want, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		EpisodeID:        &episode.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
		Segment:          string(model.WantSegmentOngoing),
	})
	if err != nil {
		t.Fatalf("create want: %v", err)
	}

	// Series-type defaults the routing fallback resolves for a series subject.
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
		Name:      "Series",
		Type:      "series",
		RootPath:  "/series",
		IsDefault: true,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if _, err := r.CreateNameTemplate(ctx, repo.CreateNameTemplateParams{
		Name:      "default-series",
		Type:      "series",
		Template:  "{title}",
		IsDefault: true,
	}); err != nil {
		t.Fatalf("create name template: %v", err)
	}

	return seededEpisodeWant{want: want, seasonID: season.ID, episodeID: episode.ID}
}

// claimWant flips the seeded want to 'searching' via the production claim path,
// exactly as the AcquisitionWorker does before ProcessWant runs. ProcessWant's
// grab CAS only fires while the want is 'searching', so tests that reach the
// grab must claim first. Returns the claimed want (Status now 'searching').
func claimWant(t *testing.T, ctx context.Context, r *repo.Repository, id uuid.UUID) model.Want {
	t.Helper()
	claimed, err := r.ClaimRunnableWants(ctx, 20)
	if err != nil {
		t.Fatalf("claim runnable wants: %v", err)
	}
	for _, w := range claimed {
		if w.ID == id {
			return w
		}
	}
	t.Fatalf("want %s not claimed", id)
	return model.Want{}
}

// cannedResult is the single SearchResult the happy-path stub returns.
func cannedResult() indexer.SearchResult {
	return indexer.SearchResult{
		IndexerID:   7,
		IndexerName: "test-indexer",
		GUID:        "canned-guid",
		Title:       "The Matrix 1999 1080p BluRay x264",
		DownloadURL: "http://localhost/canned.torrent",
		Protocol:    "torrent",
		Size:        10 << 30,
		Categories:  []string{"Movies"},
	}
}

// TestAcquisition_ProcessWant_HappyPath proves the front-half: a pending want is
// searched, the stub release is picked and routed, a download_job is created
// linked to the want, and the want flips to 'grabbed'.
func TestAcquisition_ProcessWant_HappyPath(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{cannedResult()}, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))

	_, outcome, err := svc.ProcessWant(ctx, want)
	grabbed := outcome == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("ProcessWant: %v", err)
	}
	if !grabbed {
		t.Fatalf("ProcessWant grabbed = false, want true")
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("download jobs = %d, want 1", len(jobs))
	}
	job := jobs[0]
	linkedWants, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linkedWants) != 1 || linkedWants[0].ID != want.ID {
		t.Errorf("download job linked wants = %v, want [%s]", linkedWants, want.ID)
	}
	if job.Status != "created" {
		t.Errorf("download job status = %q, want %q", job.Status, "created")
	}

	got, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if got.Status != string(model.WantGrabbed) {
		t.Errorf("want status = %q, want %q", got.Status, model.WantGrabbed)
	}
}

// TestAcquisition_ProcessWant_PicksQualified proves the gate→score→pick brain:
// given a 720p and a 1080p BluRay release, the engine grabs the 1080p one (the
// only bin in the seeded profile) and gates out the 720p.
func TestAcquisition_ProcessWant_PicksQualified(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{
				{
					IndexerID:   7,
					IndexerName: "test-indexer",
					GUID:        "guid-720",
					Title:       "The Matrix 1999 720p BluRay x264",
					DownloadURL: "http://localhost/720.torrent",
					Protocol:    "torrent",
					Size:        4 << 30,
					Categories:  []string{"Movies"},
				},
				{
					IndexerID:   7,
					IndexerName: "test-indexer",
					GUID:        "guid-1080",
					Title:       "The Matrix 1999 1080p BluRay x264",
					DownloadURL: "http://localhost/1080.torrent",
					Protocol:    "torrent",
					Size:        10 << 30,
					Categories:  []string{"Movies"},
				},
			}, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))

	_, outcome, err := svc.ProcessWant(ctx, want)
	grabbed := outcome == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("ProcessWant: %v", err)
	}
	if !grabbed {
		t.Fatalf("ProcessWant grabbed = false, want true")
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("download jobs = %d, want 1", len(jobs))
	}
	job := jobs[0]
	linkedWants, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linkedWants) != 1 || linkedWants[0].ID != want.ID {
		t.Errorf("download job linked wants = %v, want [%s]", linkedWants, want.ID)
	}
	if job.Guid != "guid-1080" {
		t.Errorf("download job guid = %q, want %q (the 720p should be gated out)", job.Guid, "guid-1080")
	}
	if job.CandidateTitle != "The Matrix 1999 1080p BluRay x264" {
		t.Errorf("download job candidateTitle = %q, want the 1080p release", job.CandidateTitle)
	}

	got, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if got.Status != string(model.WantGrabbed) {
		t.Errorf("want status = %q, want %q", got.Status, model.WantGrabbed)
	}
}

// TestAcquisition_ProcessWant_AllGatedOut proves the back-off path: when the only
// release fails the profile's bin gate, ProcessWant returns grabbed=false/err=nil
// (so the worker reschedules), no download_job is created, and the want is left
// untouched at its seeded status.
func TestAcquisition_ProcessWant_AllGatedOut(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{
				{
					IndexerID:   7,
					IndexerName: "test-indexer",
					GUID:        "guid-720",
					Title:       "The Matrix 1999 720p BluRay x264",
					DownloadURL: "http://localhost/720.torrent",
					Protocol:    "torrent",
					Size:        4 << 30,
					Categories:  []string{"Movies"},
				},
			}, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))

	_, outcome, err := svc.ProcessWant(ctx, want)
	grabbed := outcome == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("ProcessWant: %v", err)
	}
	if grabbed {
		t.Fatalf("ProcessWant grabbed = true, want false (720p is not in the profile's bins)")
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("download jobs = %d, want 0", len(jobs))
	}

	got, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if got.Status != want.Status {
		t.Errorf("want status = %q, want untouched %q", got.Status, want.Status)
	}
}

// TestAcquisition_ProcessWant_RejectsWrongTitle proves the relevance gate: a
// release with passing quality (1080p BluRay, identical to the happy path) but a
// title for a different work is dropped before quality selection, so ProcessWant
// returns grabbed=false and no download_job is created. The release here mirrors
// the production incident — a "Michael"-style want grabbing an unrelated boxing
// release that merely shared a word — but with the seeded Matrix want.
func TestAcquisition_ProcessWant_RejectsWrongTitle(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{
				{
					IndexerID:   7,
					IndexerName: "test-indexer",
					GUID:        "guid-wrong",
					Title:       "Boxing 2026 Nikita Tszyu vs Matrix Zerafa 1080p BluRay x264",
					DownloadURL: "http://localhost/wrong.torrent",
					Protocol:    "torrent",
					Size:        10 << 30,
					Categories:  []string{"Movies"},
				},
			}, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))

	_, outcome, err := svc.ProcessWant(ctx, want)
	grabbed := outcome == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("ProcessWant: %v", err)
	}
	if grabbed {
		t.Fatalf("ProcessWant grabbed = true, want false (wrong-title release must fail the relevance gate)")
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("download jobs = %d, want 0", len(jobs))
	}
}

// TestAcquisition_ProcessWant_NoRelease proves an empty search is a no-op
// (grabbed=false, err=nil) and that a subsequent reschedule returns the want to
// 'pending' with an incremented attempt count — the worker's no-release path.
func TestAcquisition_ProcessWant_NoRelease(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return nil, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))

	_, outcome, err := svc.ProcessWant(ctx, want)
	grabbed := outcome == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("ProcessWant: %v", err)
	}
	if grabbed {
		t.Fatalf("ProcessWant grabbed = true, want false")
	}

	// No download_job was created.
	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("download jobs = %d, want 0", len(jobs))
	}

	// Drive the worker's reschedule path directly. The want is still 'searching'
	// (ProcessWant's no-release path doesn't touch it), so the CAS owns it.
	rescheduled, ok, err := r.ScheduleWantRetry(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: "no eligible release",
		NextRunAt: time.Now().Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("schedule want retry: %v", err)
	}
	if !ok {
		t.Fatalf("ScheduleWantRetry ok = false, want true (want was 'searching')")
	}
	if rescheduled.Status != string(model.WantPending) {
		t.Errorf("rescheduled want status = %q, want %q", rescheduled.Status, model.WantPending)
	}
	if rescheduled.AttemptCount != 1 {
		t.Errorf("rescheduled want attemptCount = %d, want 1", rescheduled.AttemptCount)
	}
}

// TestAcquisition_WantRescheduleAndFail is the repo round-trip for the two
// CAS-from-'searching' want methods: ScheduleWantRetry sets pending + last_error
// + attempt bump, and MarkWantFailed sets the terminal failed status. Both only
// fire while the want is 'searching' (the worker's ownership phase), so each is
// driven from a freshly-claimed want.
func TestAcquisition_WantRescheduleAndFail(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	retried, ok, err := r.ScheduleWantRetry(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: "transient blip",
		NextRunAt: time.Now().Add(4 * time.Second),
	})
	if err != nil {
		t.Fatalf("schedule want retry: %v", err)
	}
	if !ok {
		t.Fatalf("ScheduleWantRetry ok = false, want true (want was 'searching')")
	}
	if retried.Status != string(model.WantPending) {
		t.Errorf("retried status = %q, want %q", retried.Status, model.WantPending)
	}
	if retried.AttemptCount != 1 {
		t.Errorf("retried attemptCount = %d, want 1", retried.AttemptCount)
	}
	if retried.LastError == nil || *retried.LastError != "transient blip" {
		t.Errorf("retried lastError = %v, want %q", retried.LastError, "transient blip")
	}

	// The reschedule returned the want to 'pending'; re-flip to 'searching' for
	// the fail CAS (next_run_at is now in the future, so claim wouldn't pick it).
	if _, err := r.SetWantStatus(ctx, want.ID, string(model.WantSearching)); err != nil {
		t.Fatalf("set want searching: %v", err)
	}

	failed, ok, err := r.MarkWantFailed(ctx, want.ID, "gave up")
	if err != nil {
		t.Fatalf("mark want failed: %v", err)
	}
	if !ok {
		t.Fatalf("MarkWantFailed ok = false, want true (want was 'searching')")
	}
	if failed.Status != string(model.WantFailed) {
		t.Errorf("failed status = %q, want %q", failed.Status, model.WantFailed)
	}
	if failed.LastError == nil || *failed.LastError != "gave up" {
		t.Errorf("failed lastError = %v, want %q", failed.LastError, "gave up")
	}
}

// TestAcquisition_WantLinkage proves the Phase 1 linkage substrate: a
// download_job and import_task can record which want they belong to, the
// want_id round-trips through the repo, and the nullable column / nil-safe
// mapper hold when no want is set.
func TestAcquisition_WantLinkage(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	// Seed media_item → quality_profile → tracking → want.
	year := int32(2020)
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

	want, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
		Segment:          string(model.WantSegmentOngoing),
	})
	if err != nil {
		t.Fatalf("create want: %v", err)
	}

	// Seed the persistence prerequisites a download_job/import_task FK to.
	downloader, err := r.CreateDownloader(ctx, repo.CreateDownloaderParams{
		Name:           "qbit",
		DownloaderType: "qbittorrent",
		Protocol:       "torrent",
		URL:            "http://localhost:8080",
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("create downloader: %v", err)
	}
	library, err := r.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name:     "Movies",
		Type:     "movie",
		RootPath: "/movies",
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	nameTemplate, err := r.CreateNameTemplate(ctx, repo.CreateNameTemplateParams{
		Name:     "default",
		Type:     "movie",
		Template: "{title}",
	})
	if err != nil {
		t.Fatalf("create name template: %v", err)
	}

	// download_job links to the want through download_job_want (M:N).
	job, err := r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
		Protocol:       "torrent",
		MediaType:      "movie",
		MediaItemID:    media.ID,
		IndexerID:      1,
		Guid:           "guid-with-want",
		CandidateTitle: "The Matrix 1999 1080p BluRay",
		CandidateLink:  "http://localhost/torrent",
		DownloaderID:   downloader.ID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create download job: %v", err)
	}
	if err := r.LinkDownloadJobWant(ctx, job.ID, want.ID); err != nil {
		t.Fatalf("link download job to want: %v", err)
	}

	// The edge resolves both directions: wants-by-job and active-jobs-by-want.
	linkedWants, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linkedWants) != 1 || linkedWants[0].ID != want.ID {
		t.Fatalf("linked wants = %v, want [%s]", linkedWants, want.ID)
	}
	activeJobs, err := r.ListActiveDownloadJobsByWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("list active jobs for want: %v", err)
	}
	if len(activeJobs) != 1 || activeJobs[0].ID != job.ID {
		t.Fatalf("active jobs for want = %v, want [%s]", activeJobs, job.ID)
	}

	// The link is idempotent — re-linking a covered want is a no-op, not a
	// duplicate row (P2's converging grabs rely on this).
	if err := r.LinkDownloadJobWant(ctx, job.ID, want.ID); err != nil {
		t.Fatalf("re-link download job to want: %v", err)
	}
	linkedWants, err = r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job after re-link: %v", err)
	}
	if len(linkedWants) != 1 {
		t.Fatalf("linked wants after re-link = %d, want 1 (idempotent)", len(linkedWants))
	}

	// import_task keeps its single want FK — each file fulfils exactly one want.
	task, err := r.CreateImportTask(ctx, repo.CreateImportTaskParams{
		DownloadJobID:  job.ID,
		SourcePath:     "/downloads/the.matrix.mkv",
		WantID:         want.ID,
		MediaType:      "movie",
		MediaItemID:    media.ID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create import task: %v", err)
	}
	if task.WantID != want.ID {
		t.Errorf("import task wantId = %s, want %s", task.WantID, want.ID)
	}
	gotTask, err := r.GetImportTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get import task: %v", err)
	}
	if gotTask.WantID != want.ID {
		t.Errorf("round-tripped import task wantId = %s, want %s", gotTask.WantID, want.ID)
	}

	// No-link case: a job with no want link reports an empty want list rather
	// than an error (the interactive series-grab path has no want).
	unlinkedJob, err := r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
		Protocol:       "torrent",
		MediaType:      "movie",
		MediaItemID:    media.ID,
		IndexerID:      2,
		Guid:           "guid-no-want",
		CandidateTitle: "The Matrix 1999 720p BluRay",
		CandidateLink:  "http://localhost/torrent2",
		DownloaderID:   downloader.ID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create download job without want: %v", err)
	}
	unlinkedWants, err := r.ListWantsByDownloadJob(ctx, unlinkedJob.ID)
	if err != nil {
		t.Fatalf("list wants for unlinked download job: %v", err)
	}
	if len(unlinkedWants) != 0 {
		t.Errorf("unlinked download job wants = %d, want 0", len(unlinkedWants))
	}
}

// TestAcquisition_ProcessWant_SeriesHappyPath proves the series front-half: an
// episode want searches with an SxxExx query, the matching single-episode release
// is picked and routed, and a series download_job is created carrying the
// media_type/season_id/episode_id/want_id the import worker's series branch needs.
func TestAcquisition_ProcessWant_SeriesHappyPath(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingEpisodeWant(t, ctx, r)
	want := claimWant(t, ctx, r, seeded.want.ID)

	var gotQuery indexer.SearchQuery
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			gotQuery = q
			return []indexer.SearchResult{{
				IndexerID:   7,
				IndexerName: "test-indexer",
				GUID:        "guid-s03e05",
				Title:       "Game of Thrones S03E05 1080p BluRay x264",
				DownloadURL: "http://localhost/s03e05.torrent",
				Protocol:    "torrent",
				Size:        3 << 30,
				Categories:  []string{"TV"},
			}}, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))

	_, outcome, err := svc.ProcessWant(ctx, want)
	grabbed := outcome == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("ProcessWant: %v", err)
	}
	if !grabbed {
		t.Fatalf("ProcessWant grabbed = false, want true")
	}

	if gotQuery.Query != "Game of Thrones S03E05" {
		t.Errorf("search query = %q, want %q", gotQuery.Query, "Game of Thrones S03E05")
	}
	if gotQuery.MediaType != indexer.MediaTypeSeries {
		t.Errorf("search mediaType = %q, want %q", gotQuery.MediaType, indexer.MediaTypeSeries)
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("download jobs = %d, want 1", len(jobs))
	}
	job := jobs[0]
	if job.MediaType != "series" {
		t.Errorf("download job mediaType = %q, want %q", job.MediaType, "series")
	}
	if job.SeasonID != seeded.seasonID {
		t.Errorf("download job seasonId = %s, want %s", job.SeasonID, seeded.seasonID)
	}
	if job.EpisodeID != seeded.episodeID {
		t.Errorf("download job episodeId = %s, want %s", job.EpisodeID, seeded.episodeID)
	}
	linkedWants, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linkedWants) != 1 || linkedWants[0].ID != want.ID {
		t.Errorf("download job linked wants = %v, want [%s]", linkedWants, want.ID)
	}

	got, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if got.Status != string(model.WantGrabbed) {
		t.Errorf("want status = %q, want %q", got.Status, model.WantGrabbed)
	}
}

// TestAcquisition_ProcessWant_SeriesGatesNonEpisode proves the still-rejected
// shapes survive P2's pack acceptance: a multi-season pack and a wrong-episode
// single both fail the classifier (multi-season and partial-season packs are never
// accepted, and a single must land on the wanted episode), so with no full-season
// or covering-range release present, ProcessWant grabs nothing.
func TestAcquisition_ProcessWant_SeriesGatesNonEpisode(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingEpisodeWant(t, ctx, r)
	want := claimWant(t, ctx, r, seeded.want.ID)

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{
				{
					IndexerID:   7,
					IndexerName: "test-indexer",
					GUID:        "guid-multi-season",
					Title:       "Game of Thrones S01-S03 1080p BluRay x264",
					DownloadURL: "http://localhost/s01-s03.torrent",
					Protocol:    "torrent",
					Size:        90 << 30,
					Categories:  []string{"TV"},
				},
				{
					IndexerID:   7,
					IndexerName: "test-indexer",
					GUID:        "guid-wrong-episode",
					Title:       "Game of Thrones S03E06 1080p BluRay x264",
					DownloadURL: "http://localhost/s03e06.torrent",
					Protocol:    "torrent",
					Size:        3 << 30,
					Categories:  []string{"TV"},
				},
			}, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))

	_, outcome, err := svc.ProcessWant(ctx, want)
	grabbed := outcome == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("ProcessWant: %v", err)
	}
	if grabbed {
		t.Fatalf("ProcessWant grabbed = true, want false (multi-season pack + wrong episode must be gated out)")
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("download jobs = %d, want 0", len(jobs))
	}
}

// TestAcquisition_ReclaimStaleSearchingWants proves the crash-window reaper: a
// want wedged in 'searching' past the cutoff is reset to 'pending' (reset-only,
// attempt_count untouched), while one inside the window is left alone.
func TestAcquisition_ReclaimStaleSearchingWants(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	// Negative: a past cutoff (the production case for a freshly-claimed want)
	// finds nothing stale — the want is well inside the window.
	none, err := r.ReclaimStaleSearchingWants(ctx, time.Now().Add(-time.Hour), "stale")
	if err != nil {
		t.Fatalf("reclaim with past cutoff: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("reclaimed = %d, want 0 (want is inside the window)", len(none))
	}
	still, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if still.Status != string(model.WantSearching) {
		t.Errorf("want status = %q, want %q (untouched)", still.Status, model.WantSearching)
	}

	// Positive: a future cutoff treats every 'searching' want as stale, so the
	// reaper resets it to 'pending' without bumping attempt_count.
	reclaimed, err := r.ReclaimStaleSearchingWants(ctx, time.Now().Add(time.Hour), "reset from stale 'searching'")
	if err != nil {
		t.Fatalf("reclaim with future cutoff: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("reclaimed = %d, want 1", len(reclaimed))
	}
	if reclaimed[0].ID != want.ID {
		t.Errorf("reclaimed want = %s, want %s", reclaimed[0].ID, want.ID)
	}
	if reclaimed[0].Status != string(model.WantPending) {
		t.Errorf("reclaimed status = %q, want %q", reclaimed[0].Status, model.WantPending)
	}
	if reclaimed[0].AttemptCount != want.AttemptCount {
		t.Errorf("reclaimed attemptCount = %d, want %d (reset-only, untouched)", reclaimed[0].AttemptCount, want.AttemptCount)
	}

	got, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if got.Status != string(model.WantPending) {
		t.Errorf("persisted status = %q, want %q", got.Status, model.WantPending)
	}
}

// TestAcquisition_ProcessWant_DedupesSupersededGrab proves the grab CAS dedup:
// re-running ProcessWant on a now-stale want (one already grabbed by a prior run)
// is a benign no-op — grabbed=false, no second job, the want unchanged. This is
// the race the reaper would otherwise open (reset + re-claim while a live worker
// still holds the want).
func TestAcquisition_ProcessWant_DedupesSupersededGrab(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{cannedResult()}, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))

	// First run: the CAS owns the 'searching' want, grabs it, creates one job.
	_, outcome, err := svc.ProcessWant(ctx, want)
	grabbed := outcome == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("first ProcessWant: %v", err)
	}
	if !grabbed {
		t.Fatalf("first ProcessWant grabbed = false, want true")
	}

	// Second run with the same (now-stale) want value: the want is no longer
	// 'searching', so the grab CAS matches 0 rows and ProcessWant no-ops.
	_, outcomeAgain, err := svc.ProcessWant(ctx, want)
	grabbedAgain := outcomeAgain == service.OutcomeGrabbed
	if err != nil {
		t.Fatalf("second ProcessWant: %v", err)
	}
	if grabbedAgain {
		t.Fatalf("second ProcessWant grabbed = true, want false (superseded grab must dedup)")
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("download jobs = %d, want 1 (no duplicate from the superseded grab)", len(jobs))
	}

	got, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if got.Status != string(model.WantGrabbed) {
		t.Errorf("want status = %q, want %q (unchanged by the superseded grab)", got.Status, model.WantGrabbed)
	}
}

// retryTestConfig drives the worker on a hot cadence with the retry backoff
// pinned to zero, so a rescheduled want is immediately due and the loop cycles
// claim → error → reschedule in milliseconds. ReapAfter is parked far in the
// future so reclamation rides the normal claim path, not the crash reaper.
func retryTestConfig() acquisitionworker.Config {
	return acquisitionworker.Config{
		PollInterval:    10 * time.Millisecond,
		ClaimLimit:      20,
		MaxRetryBackoff: 0,
		ReapAfter:       time.Hour,
	}
}

// TestAcquisition_RetryableErrorNeverFails is the S3 guarantee (Issue B): a
// retryable front-half error (an indexer briefly unreachable, surfaced as
// BadGateway) never terminally fails an in-scope want. The worker backs off and
// retries indefinitely — the want stays pending/searching and its attempt_count
// climbs well past the old 3-attempt ceiling, never reaching 'failed'.
func TestAcquisition_RetryableErrorNeverFails(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return nil, errors.New("indexer unreachable")
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))
	w := acquisitionworker.NewWithConfig(r, svc, service.NewSchedulerService(r, logger.New(true)), logger.New(true), nil, retryTestConfig())

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go w.Run(workerCtx)

	// Wait for attempt_count to climb past the old 3-attempt ceiling, asserting
	// the want never trips to terminal 'failed' on the way.
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := r.GetWant(ctx, want.ID)
		if err != nil {
			t.Fatalf("get want: %v", err)
		}
		if got.Status == string(model.WantFailed) {
			t.Fatalf("want terminally failed on a retryable error (attemptCount=%d, lastError=%v); S3 must retry forever", got.AttemptCount, got.LastError)
		}
		if got.AttemptCount >= 6 {
			break // proven: retried past the old ceiling without failing
		}
		if time.Now().After(deadline) {
			t.Fatalf("attemptCount reached %d, want >= 6 within deadline (status %q)", got.AttemptCount, got.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAcquisition_NonRetryableErrorFails proves the other side of S3: a
// non-retryable error (genuine bad state — here, no default downloader for
// routing to resolve, a NotFound) still terminally fails the want. The release
// passes the relevance and quality gates, so the failure originates at routing,
// downstream of the search, exactly where a real misconfiguration would.
func TestAcquisition_NonRetryableErrorFails(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	// Seed media → profile → tracking → want, deliberately omitting the
	// downloader/library/name_template defaults so routing's GetDefaultDownloader
	// misses and surfaces a NotFound.
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
	want, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
		Segment:          string(model.WantSegmentOngoing),
	})
	if err != nil {
		t.Fatalf("create want: %v", err)
	}

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{cannedResult()}, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))
	w := acquisitionworker.NewWithConfig(r, svc, service.NewSchedulerService(r, logger.New(true)), logger.New(true), nil, retryTestConfig())

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go w.Run(workerCtx)

	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := r.GetWant(ctx, want.ID)
		if err != nil {
			t.Fatalf("get want: %v", err)
		}
		if got.Status == string(model.WantFailed) {
			break // the non-retryable routing error tripped the want terminal, as it should
		}
		if time.Now().After(deadline) {
			t.Fatalf("want status = %q, want %q (a non-retryable error must fail terminally)", got.Status, model.WantFailed)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAcquisition_RescheduleWantRecheckResetsAttempt proves the attempt_count
// reset: a want that has accumulated retryable-error attempts has its counter
// cleared by RescheduleWantRecheck (the successful-reach, no-release path), so a
// later infra blip backs off from scratch rather than inheriting the climb.
func TestAcquisition_RescheduleWantRecheckResetsAttempt(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	// Climb attempt_count via the error-retry path: each ScheduleWantRetry bumps
	// the counter and returns the want to 'pending', so re-flip to 'searching'
	// before the next bump (and before the recheck CAS, which guards on it).
	for i := 0; i < 3; i++ {
		retried, ok, err := r.ScheduleWantRetry(ctx, repo.ScheduleWantRetryParams{
			ID:        want.ID,
			LastError: "transient blip",
			NextRunAt: time.Now().Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("schedule want retry: %v", err)
		}
		if !ok {
			t.Fatalf("ScheduleWantRetry ok = false, want true (want was 'searching')")
		}
		if int(retried.AttemptCount) != i+1 {
			t.Fatalf("attemptCount = %d, want %d after bump %d", retried.AttemptCount, i+1, i+1)
		}
		if _, err := r.SetWantStatus(ctx, want.ID, string(model.WantSearching)); err != nil {
			t.Fatalf("set want searching: %v", err)
		}
	}

	rechecked, ok, err := r.RescheduleWantRecheck(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: "no eligible release",
		NextRunAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("reschedule want recheck: %v", err)
	}
	if !ok {
		t.Fatalf("RescheduleWantRecheck ok = false, want true (want was 'searching')")
	}
	if rechecked.AttemptCount != 0 {
		t.Errorf("attemptCount after recheck = %d, want 0 (successful reach clears the counter)", rechecked.AttemptCount)
	}
	if rechecked.Status != string(model.WantPending) {
		t.Errorf("status after recheck = %q, want %q", rechecked.Status, model.WantPending)
	}
}
