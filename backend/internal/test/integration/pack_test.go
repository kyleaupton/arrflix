//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/indexer"
	downloadworker "github.com/kyleaupton/arrflix/internal/jobs/download"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// seededSeasonWants bundles a multi-episode season tracking with one pending want
// per episode, the substrate a season-pack grab fans out over.
type seededSeasonWants struct {
	mediaItemID uuid.UUID
	trackingID  uuid.UUID
	seasonID    uuid.UUID
	wants       map[int]model.Want // episode number → want
	episodeIDs  map[int]uuid.UUID  // episode number → episode id
}

// seedPendingSeasonWants seeds a series tracking with one pending episode want per
// given episode number in one season, plus the series-type routing defaults. It
// mirrors seedPendingEpisodeWant but spans N episodes so a pack can cover several.
func seedPendingSeasonWants(t *testing.T, ctx context.Context, r *repo.Repository, seasonNum int, episodes ...int) seededSeasonWants {
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

	season, err := r.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: media.ID, SeasonNumber: int32(seasonNum)})
	if err != nil {
		t.Fatalf("upsert season: %v", err)
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

	out := seededSeasonWants{
		mediaItemID: media.ID,
		trackingID:  tracking.ID,
		seasonID:    season.ID,
		wants:       make(map[int]model.Want, len(episodes)),
		episodeIDs:  make(map[int]uuid.UUID, len(episodes)),
	}
	for _, epNum := range episodes {
		epTitle := fmt.Sprintf("Episode %d", epNum)
		epRuntime := int32(60)
		epTmdb := int64(60000 + epNum)
		episode, err := r.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
			SeasonID:      season.ID,
			EpisodeNumber: int32(epNum),
			Title:         &epTitle,
			Runtime:       &epRuntime,
			TmdbID:        &epTmdb,
		})
		if err != nil {
			t.Fatalf("upsert episode %d: %v", epNum, err)
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
			t.Fatalf("create want for episode %d: %v", epNum, err)
		}
		out.wants[epNum] = want
		out.episodeIDs[epNum] = episode.ID
	}

	seedSeriesRoutingDefaults(t, ctx, r)
	return out
}

// seedSeriesRoutingDefaults creates the series-type downloader/library/name template
// the routing fallback resolves.
func seedSeriesRoutingDefaults(t *testing.T, ctx context.Context, r *repo.Repository) {
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
}

// packResult builds a SearchResult for a release title (a pack or single).
func packResult(guid, title string, sizeGiB int64) indexer.SearchResult {
	return indexer.SearchResult{
		IndexerID:   7,
		IndexerName: "test-indexer",
		GUID:        guid,
		Title:       title,
		DownloadURL: "http://localhost/" + guid + ".torrent",
		Protocol:    "torrent",
		Size:        sizeGiB << 30,
		Categories:  []string{"TV"},
	}
}

// fakeDownloaderClient is a hand-rolled downloader.Client whose Get reports a
// scripted download status and whose ListFiles returns a scripted file list — the
// seam the download→import flow drives over without a real qBittorrent. The zero
// value reports an instantly-completed download (the happy path most tests want);
// status overrides that terminal status (e.g. StatusErrored for failure recovery),
// and getErr makes every Get fail (driving the retry→max-attempts path).
type fakeDownloaderClient struct {
	files  []downloader.File
	status downloader.JobStatus
	getErr error
}

func (f *fakeDownloaderClient) Type() downloader.Type             { return downloader.TypeQbittorrent }
func (f *fakeDownloaderClient) InstanceID() downloader.InstanceID { return "fake" }
func (f *fakeDownloaderClient) Test(ctx context.Context) (downloader.TestResult, error) {
	return downloader.TestResult{Success: true}, nil
}
func (f *fakeDownloaderClient) Add(ctx context.Context, req downloader.AddRequest) (downloader.AddResult, error) {
	return downloader.AddResult{ExternalID: "ext-1", Name: "pack"}, nil
}
func (f *fakeDownloaderClient) Get(ctx context.Context, externalID string) (downloader.Item, error) {
	if f.getErr != nil {
		return downloader.Item{}, f.getErr
	}
	status := f.status
	if status == "" {
		status = downloader.StatusCompleted
	}
	return downloader.Item{
		ExternalID:  externalID,
		Status:      status,
		Progress:    1,
		SavePath:    "/downloads",
		ContentPath: "/downloads/pack",
		TotalSize:   1 << 30,
	}, nil
}
func (f *fakeDownloaderClient) List(ctx context.Context) ([]downloader.Item, error) {
	return nil, downloader.ErrUnsupported
}
func (f *fakeDownloaderClient) ListFiles(ctx context.Context, externalID string) ([]downloader.File, error) {
	return f.files, nil
}
func (f *fakeDownloaderClient) Pause(ctx context.Context, externalID string) error  { return nil }
func (f *fakeDownloaderClient) Resume(ctx context.Context, externalID string) error { return nil }
func (f *fakeDownloaderClient) Remove(ctx context.Context, externalID string, deleteData bool) error {
	return nil
}

// newDownloadWorker wires a download worker around a fake client built for the
// seeded (enabled) downloader, on a fast cadence so created→enqueued→completed
// cycles in milliseconds.
func newDownloadWorker(t *testing.T, ctx context.Context, r *repo.Repository, files []downloader.File) *downloadworker.Worker {
	t.Helper()
	return newDownloadWorkerWithClient(t, ctx, r, &fakeDownloaderClient{files: files})
}

// newDownloadWorkerWithClient wires a download worker around a caller-built fake
// client, so a test can script a failure status or a Get error the happy-path
// newDownloadWorker doesn't expose.
func newDownloadWorkerWithClient(t *testing.T, ctx context.Context, r *repo.Repository, client *fakeDownloaderClient) *downloadworker.Worker {
	t.Helper()
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(rec downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, r, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	return downloadworker.NewWithConfig(r, dm, logger.New(false), nil, downloadworker.Config{
		PollInterval: 10 * time.Millisecond,
		ClaimLimit:   20,
		MaxAttempts:  3,
	})
}

// episodeFile builds a fake downloader file for one episode of the GoT season.
func episodeFile(season, episode int) downloader.File {
	return downloader.File{
		Path: fmt.Sprintf("Game of Thrones S%02dE%02d 1080p BluRay x264.mkv", season, episode),
		Size: 3 << 30,
	}
}

// grabPackForSeason runs the front-half on the claimed want for the given episode,
// returning the created pack download_job.
func grabPackForSeason(t *testing.T, ctx context.Context, r *repo.Repository, seeded seededSeasonWants, claimEpisode int, results []indexer.SearchResult) model.DownloadJob {
	t.Helper()
	want := claimWant(t, ctx, r, seeded.wants[claimEpisode].ID)
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return results, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))
	if _, outcome, err := svc.ProcessWant(ctx, want); err != nil {
		t.Fatalf("ProcessWant: %v", err)
	} else if outcome != service.OutcomeGrabbed {
		t.Fatalf("ProcessWant grabbed = false, want true (the pack covers the want)")
	}
	jobs, err := r.ListDownloadJobsByMediaItem(ctx, seeded.mediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("download jobs = %d, want 1", len(jobs))
	}
	return jobs[0]
}

// TestPack_FrontHalf_GrabsPackCoveringSiblings proves the front-half pack grab: a
// single S03 COMPLETE release covering three in-flight episode wants is grabbed
// once — one pack download_job (season set, episode null) linked to all three
// wants, every want flipped to 'grabbed'.
func TestPack_FrontHalf_GrabsPackCoveringSiblings(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingSeasonWants(t, ctx, r, 3, 1, 2, 3)
	job := grabPackForSeason(t, ctx, r, seeded, 1, []indexer.SearchResult{
		packResult("guid-s03-complete", "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
	})

	if job.MediaType != "series" {
		t.Errorf("job mediaType = %q, want series", job.MediaType)
	}
	if job.SeasonID != seeded.seasonID {
		t.Errorf("job seasonId = %s, want %s", job.SeasonID, seeded.seasonID)
	}
	if job.EpisodeID != uuid.Nil {
		t.Errorf("pack job episodeId = %s, want nil", job.EpisodeID)
	}

	linked, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linked) != 3 {
		t.Fatalf("linked wants = %d, want 3 (the whole covered season)", len(linked))
	}
	for epNum, want := range seeded.wants {
		got, err := r.GetWant(ctx, want.ID)
		if err != nil {
			t.Fatalf("get want for episode %d: %v", epNum, err)
		}
		if got.Status != string(model.WantGrabbed) {
			t.Errorf("episode %d want status = %q, want grabbed", epNum, got.Status)
		}
	}
}

// TestPack_CoverageGuard_PrefersSingle proves the ≥2 guard: with a single in-flight
// want, a covering single and a full-season pack both qualify, but the pack covers
// only one want, so the single is grabbed — one job carrying the episode id, linked
// to the one want.
func TestPack_CoverageGuard_PrefersSingle(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingSeasonWants(t, ctx, r, 3, 5)
	job := grabPackForSeason(t, ctx, r, seeded, 5, []indexer.SearchResult{
		packResult("guid-s03-complete", "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
		packResult("guid-s03e05", "Game of Thrones S03E05 1080p BluRay x264", 3),
	})

	if job.Guid != "guid-s03e05" {
		t.Errorf("job guid = %q, want guid-s03e05 (the single, not the lone-coverage pack)", job.Guid)
	}
	if job.EpisodeID != seeded.episodeIDs[5] {
		t.Errorf("job episodeId = %s, want %s (single carries the episode id)", job.EpisodeID, seeded.episodeIDs[5])
	}
	linked, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != seeded.wants[5].ID {
		t.Errorf("linked wants = %v, want [%s]", linked, seeded.wants[5].ID)
	}
}

// waitForImportTasks polls until the job has the expected number of import tasks or
// the deadline passes, returning them keyed by episode id.
func waitForImportTasks(t *testing.T, ctx context.Context, r *repo.Repository, jobID uuid.UUID, want int) []model.ImportTask {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		tasks, err := r.ListImportTasksByDownloadJob(ctx, jobID)
		if err != nil {
			t.Fatalf("list import tasks: %v", err)
		}
		if len(tasks) >= want {
			return tasks
		}
		if time.Now().After(deadline) {
			t.Fatalf("import tasks = %d, want %d within deadline", len(tasks), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPack_BackHalf_FanOut proves the back-half fan-out: a completed pack job whose
// files cover all three episodes spawns one import task per episode, each filed onto
// its own want with its own episode id — the per-episode want map, not the single
// linkedWantID.
func TestPack_BackHalf_FanOut(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingSeasonWants(t, ctx, r, 3, 1, 2, 3)
	job := grabPackForSeason(t, ctx, r, seeded, 1, []indexer.SearchResult{
		packResult("guid-s03-complete", "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
	})

	files := []downloader.File{episodeFile(3, 1), episodeFile(3, 2), episodeFile(3, 3)}
	w := newDownloadWorker(t, ctx, r, files)
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go w.Run(wctx)

	tasks := waitForImportTasks(t, ctx, r, job.ID, 3)
	if len(tasks) != 3 {
		t.Fatalf("import tasks = %d, want 3", len(tasks))
	}

	// Each task files onto the want whose episode it carries.
	taskByEpisode := make(map[uuid.UUID]model.ImportTask, len(tasks))
	for _, task := range tasks {
		taskByEpisode[task.EpisodeID] = task
	}
	for epNum, want := range seeded.wants {
		task, ok := taskByEpisode[seeded.episodeIDs[epNum]]
		if !ok {
			t.Errorf("no import task for episode %d", epNum)
			continue
		}
		if task.WantID != want.ID {
			t.Errorf("episode %d import task wantId = %s, want %s", epNum, task.WantID, want.ID)
		}
	}
}

// TestPack_BackHalf_UnderCoverage proves the under-coverage release: a pack whose
// files cover only E1 and E2 imports those two, then releases the uncovered E3 want
// back to 'pending' and detaches it from the pack job so it re-searches instead of
// wedging in 'grabbed'.
func TestPack_BackHalf_UnderCoverage(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingSeasonWants(t, ctx, r, 3, 1, 2, 3)
	job := grabPackForSeason(t, ctx, r, seeded, 1, []indexer.SearchResult{
		packResult("guid-s03-complete", "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
	})

	// The pack carried only E1 and E2.
	files := []downloader.File{episodeFile(3, 1), episodeFile(3, 2)}
	w := newDownloadWorker(t, ctx, r, files)
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go w.Run(wctx)

	tasks := waitForImportTasks(t, ctx, r, job.ID, 2)
	if len(tasks) != 2 {
		t.Fatalf("import tasks = %d, want 2 (only E1, E2 covered)", len(tasks))
	}

	// E3 is released back to 'pending' and unlinked from the pack job.
	e3 := seeded.wants[3]
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := r.GetWant(ctx, e3.ID)
		if err != nil {
			t.Fatalf("get E3 want: %v", err)
		}
		if got.Status == string(model.WantPending) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("E3 want status = %q, want pending (under-covered → released)", got.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}

	linked, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for download job: %v", err)
	}
	if len(linked) != 2 {
		t.Errorf("linked wants after release = %d, want 2 (E3 unlinked)", len(linked))
	}
	activeJobs, err := r.ListActiveDownloadJobsByWant(ctx, e3.ID)
	if err != nil {
		t.Fatalf("list active jobs for E3 want: %v", err)
	}
	if len(activeJobs) != 0 {
		t.Errorf("E3 active jobs = %d, want 0 (detached from the pack job)", len(activeJobs))
	}
}

// addExclusion records that a want has blocklisted a release, the substrate the
// front-half's exclusion-aware coverage shrink reads.
func addExclusion(t *testing.T, ctx context.Context, r *repo.Repository, wantID uuid.UUID, indexerID int64, guid string) {
	t.Helper()
	if err := r.AddWantReleaseExclusion(ctx, repo.AddWantReleaseExclusionParams{
		WantID:    wantID,
		IndexerID: indexerID,
		GUID:      guid,
		Reason:    model.ExclusionDownloadFailed,
	}); err != nil {
		t.Fatalf("add exclusion for want %s: %v", wantID, err)
	}
}

// TestPack_ExcludedSiblingShrinksCoverage proves the sibling half of the exclusion
// asymmetry: a sibling that has excluded the pack release is carved out of its
// coverage. When enough siblings exclude it, coverage drops below the ≥2 guard and
// the single wins; when one sibling still remains covered alongside the processed
// want, the pack wins linking exactly those two.
func TestPack_ExcludedSiblingShrinksCoverage(t *testing.T) {
	t.Parallel()

	const packGUID = "guid-s03-complete"

	// E2 and E3 both exclude the pack → only E1 remains covered → the ≥2 guard is
	// not met → the covering single wins.
	t.Run("shrinks below guard, single wins", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()

		seeded := seedPendingSeasonWants(t, ctx, r, 3, 1, 2, 3)
		addExclusion(t, ctx, r, seeded.wants[2].ID, 7, packGUID)
		addExclusion(t, ctx, r, seeded.wants[3].ID, 7, packGUID)

		job := grabPackForSeason(t, ctx, r, seeded, 1, []indexer.SearchResult{
			packResult(packGUID, "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
			packResult("guid-s03e01", "Game of Thrones S03E01 1080p BluRay x264", 3),
		})

		if job.Guid != "guid-s03e01" {
			t.Errorf("job guid = %q, want guid-s03e01 (pack coverage shrank below the guard)", job.Guid)
		}
		if job.EpisodeID != seeded.episodeIDs[1] {
			t.Errorf("job episodeId = %s, want %s (single carries the episode id)", job.EpisodeID, seeded.episodeIDs[1])
		}
		linked, err := r.ListWantsByDownloadJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("list wants for download job: %v", err)
		}
		if len(linked) != 1 || linked[0].ID != seeded.wants[1].ID {
			t.Errorf("linked wants = %v, want just [%s]", linked, seeded.wants[1].ID)
		}
	})

	// Only E3 excludes the pack → E1 (processed) + E2 remain covered → the pack
	// wins, linking exactly those two; E3 stays pending.
	t.Run("one sibling excluded, pack wins remaining two", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()

		seeded := seedPendingSeasonWants(t, ctx, r, 3, 1, 2, 3)
		addExclusion(t, ctx, r, seeded.wants[3].ID, 7, packGUID)

		job := grabPackForSeason(t, ctx, r, seeded, 1, []indexer.SearchResult{
			packResult(packGUID, "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
		})

		if job.Guid != packGUID {
			t.Errorf("job guid = %q, want %s (pack still covers ≥2)", job.Guid, packGUID)
		}
		if job.EpisodeID != uuid.Nil {
			t.Errorf("pack job episodeId = %s, want nil", job.EpisodeID)
		}
		linked, err := r.ListWantsByDownloadJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("list wants for download job: %v", err)
		}
		if len(linked) != 2 {
			t.Fatalf("linked wants = %d, want 2 (E1+E2; E3 excluded)", len(linked))
		}
		linkedIDs := map[uuid.UUID]struct{}{}
		for _, w := range linked {
			linkedIDs[w.ID] = struct{}{}
		}
		if _, ok := linkedIDs[seeded.wants[1].ID]; !ok {
			t.Errorf("E1 want not linked to pack job")
		}
		if _, ok := linkedIDs[seeded.wants[2].ID]; !ok {
			t.Errorf("E2 want not linked to pack job")
		}
		if _, ok := linkedIDs[seeded.wants[3].ID]; ok {
			t.Errorf("E3 want linked to pack job, want excluded from coverage")
		}
		// claimWant flipped all three siblings to 'searching'; the pack grabbed E1
		// and E2 but left E3 (excluded from coverage) untouched — so it's neither
		// grabbed nor linked, just still searching.
		e3, err := r.GetWant(ctx, seeded.wants[3].ID)
		if err != nil {
			t.Fatalf("get E3 want: %v", err)
		}
		if e3.Status == string(model.WantGrabbed) {
			t.Errorf("E3 want status = grabbed, want it left uncovered by the pack")
		}
	})
}
