//go:build integration

package importw

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/jobs/state"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/mediainfo"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/pathmapping"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	if err := dbtest.Start(ctx); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = dbtest.Stop(ctx)
	os.Exit(code)
}

// newTestWorker builds a Worker directly (not via the production New) so the
// test controls which collaborators are wired. dlm and broker stay nil — the
// processTask success path doesn't touch them.
func newTestWorker(r *repo.Repository) *Worker {
	log := logger.New(true)
	return &Worker{
		repo:         r,
		log:          log,
		pathMapper:   pathmapping.New(),
		sm:           state.NewImportTaskMachine(),
		mediaInfo:    mediainfo.NewAnalyzer(*log),
		pollInterval: 2 * time.Second,
		claimLimit:   10,
		maxAttempts:  5,
	}
}

// importFixture is the seeded persistence chain plus the on-disk paths a
// processTask run needs.
type importFixture struct {
	task        model.ImportTask
	want        model.Want // zero ID when seeded without a want
	libraryRoot string
	sourcePath  string
}

// seedImportTask seeds media_item → quality_profile → tracking → (optional)
// want → library → name_template → download_job → import_task, with a real
// source file on disk. When withWant is false the import_task's want_id is
// uuid.Nil, exercising the legacy/manual path the guards must leave untouched.
func seedImportTask(t *testing.T, ctx context.Context, r *repo.Repository, withWant bool) importFixture {
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
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	var want model.Want
	wantID := uuid.Nil
	if withWant {
		want, err = r.CreateWant(ctx, repo.CreateWantParams{
			TrackingID:       tracking.ID,
			MediaItemID:      media.ID,
			QualityProfileID: profile.ID,
			Status:           string(model.WantDownloading),
		})
		if err != nil {
			t.Fatalf("create want: %v", err)
		}
		wantID = want.ID
	}

	libraryRoot := t.TempDir()
	library, err := r.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name:     "Movies",
		Type:     "movie",
		RootPath: libraryRoot,
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

	job, err := r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
		Protocol:       "torrent",
		MediaType:      "movie",
		MediaItemID:    media.ID,
		WantID:         wantID,
		IndexerID:      1,
		Guid:           "guid",
		CandidateTitle: "The Matrix 1999 1080p BluRay x264",
		CandidateLink:  "http://localhost/canned.torrent",
		DownloaderID:   downloader.ID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create download job: %v", err)
	}

	// Real source file in a separate temp dir; the worker hardlinks it into the
	// library root.
	srcDir := t.TempDir()
	sourcePath := filepath.Join(srcDir, "the.matrix.1999.1080p.bluray.x264.mkv")
	if err := os.WriteFile(sourcePath, []byte("fake video bytes"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	task, err := r.CreateImportTask(ctx, repo.CreateImportTaskParams{
		DownloadJobID:  job.ID,
		SourcePath:     sourcePath,
		WantID:         wantID,
		MediaType:      "movie",
		MediaItemID:    media.ID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create import task: %v", err)
	}

	return importFixture{task: task, want: want, libraryRoot: libraryRoot, sourcePath: sourcePath}
}

// TestProcessTask_WantMirroredToAvailable proves the import-completion back-half:
// a pending import_task linked to a downloading want completes, the file lands on
// disk, and the want walks imported→available via the inline VerifyStep.
func TestProcessTask_WantMirroredToAvailable(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	fix := seedImportTask(t, ctx, r, true)
	w := newTestWorker(r)

	if err := w.processTask(ctx, fix.task); err != nil {
		t.Fatalf("processTask: %v", err)
	}

	gotTask, err := r.GetImportTask(ctx, fix.task.ID)
	if err != nil {
		t.Fatalf("get import task: %v", err)
	}
	if gotTask.Status != "completed" {
		t.Errorf("import task status = %q, want %q", gotTask.Status, "completed")
	}
	if gotTask.DestPath == nil {
		t.Fatalf("import task destPath is nil")
	}

	fullDest := filepath.Join(fix.libraryRoot, *gotTask.DestPath)
	if info, err := os.Stat(fullDest); err != nil || info.Size() == 0 {
		t.Fatalf("imported file missing/empty at %s: %v", fullDest, err)
	}

	gotWant, err := r.GetWant(ctx, fix.want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if gotWant.Status != string(model.WantAvailable) {
		t.Errorf("want status = %q, want %q", gotWant.Status, model.WantAvailable)
	}
}

// seriesImportFixture bundles a seeded series import fixture with the episode id
// the completed file is expected to link to.
type seriesImportFixture struct {
	importFixture
	episodeID uuid.UUID
}

// seedSeriesImportTask seeds the series persistence chain — series media_item →
// season → episode → quality_profile(series) → tracking → episode want →
// series library → series name_template → series download_job(season+episode) →
// series import_task(episode) — with a real source file on disk. It mirrors
// seedImportTask but on the series branch so the test can assert the imported
// file is born with episode_id set.
func seedSeriesImportTask(t *testing.T, ctx context.Context, r *repo.Repository) seriesImportFixture {
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
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	want, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		EpisodeID:        &episode.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantDownloading),
	})
	if err != nil {
		t.Fatalf("create want: %v", err)
	}

	libraryRoot := t.TempDir()
	library, err := r.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name:     "Series",
		Type:     "series",
		RootPath: libraryRoot,
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	showTpl := "{{.Media.Title}}"
	seasonTpl := "Season {{.Media.Season}}"
	nameTemplate, err := r.CreateNameTemplate(ctx, repo.CreateNameTemplateParams{
		Name:                 "default-series",
		Type:                 "series",
		Template:             "{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}}",
		SeriesShowTemplate:   &showTpl,
		SeriesSeasonTemplate: &seasonTpl,
	})
	if err != nil {
		t.Fatalf("create name template: %v", err)
	}
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

	job, err := r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
		Protocol:       "torrent",
		MediaType:      "series",
		MediaItemID:    media.ID,
		SeasonID:       season.ID,
		EpisodeID:      episode.ID,
		WantID:         want.ID,
		IndexerID:      1,
		Guid:           "guid-s03e05",
		CandidateTitle: "Game of Thrones S03E05 1080p BluRay x264",
		CandidateLink:  "http://localhost/s03e05.torrent",
		DownloaderID:   downloader.ID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create download job: %v", err)
	}

	srcDir := t.TempDir()
	sourcePath := filepath.Join(srcDir, "game.of.thrones.s03e05.1080p.bluray.x264.mkv")
	if err := os.WriteFile(sourcePath, []byte("fake video bytes"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	task, err := r.CreateImportTask(ctx, repo.CreateImportTaskParams{
		DownloadJobID:  job.ID,
		SourcePath:     sourcePath,
		WantID:         want.ID,
		MediaType:      "series",
		MediaItemID:    media.ID,
		EpisodeID:      episode.ID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create import task: %v", err)
	}

	return seriesImportFixture{
		importFixture: importFixture{task: task, want: want, libraryRoot: libraryRoot, sourcePath: sourcePath},
		episodeID:     episode.ID,
	}
}

// TestProcessTask_SeriesWantMirroredToAvailable proves the series import-fulfillment
// back-half: a downloading episode want completes, the file lands on disk born with
// episode_id set (the closed-world identity the series branch records), and the want
// walks imported→available via the inline VerifyStep.
func TestProcessTask_SeriesWantMirroredToAvailable(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	fix := seedSeriesImportTask(t, ctx, r)
	w := newTestWorker(r)

	if err := w.processTask(ctx, fix.task); err != nil {
		t.Fatalf("processTask: %v", err)
	}

	gotTask, err := r.GetImportTask(ctx, fix.task.ID)
	if err != nil {
		t.Fatalf("get import task: %v", err)
	}
	if gotTask.Status != "completed" {
		t.Errorf("import task status = %q, want %q", gotTask.Status, "completed")
	}
	if gotTask.DestPath == nil {
		t.Fatalf("import task destPath is nil")
	}
	if gotTask.FileID == uuid.Nil {
		t.Fatalf("completed import task has nil fileId")
	}

	fullDest := filepath.Join(fix.libraryRoot, *gotTask.DestPath)
	if info, err := os.Stat(fullDest); err != nil || info.Size() == 0 {
		t.Fatalf("imported file missing/empty at %s: %v", fullDest, err)
	}

	// The file is born with its episode identity — the linkage Phase 5 asserts.
	file, err := r.GetFile(ctx, gotTask.FileID)
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if file.EpisodeID == nil || *file.EpisodeID != fix.episodeID {
		t.Errorf("file episodeId = %v, want %s", file.EpisodeID, fix.episodeID)
	}

	gotWant, err := r.GetWant(ctx, fix.want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if gotWant.Status != string(model.WantAvailable) {
		t.Errorf("want status = %q, want %q", gotWant.Status, model.WantAvailable)
	}
}

// TestProcessTask_NoWant proves the legacy path: an import_task with no want
// completes normally and touches no want row (the uuid.Nil guards hold).
func TestProcessTask_NoWant(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	fix := seedImportTask(t, ctx, r, false)
	w := newTestWorker(r)

	if err := w.processTask(ctx, fix.task); err != nil {
		t.Fatalf("processTask: %v", err)
	}

	gotTask, err := r.GetImportTask(ctx, fix.task.ID)
	if err != nil {
		t.Fatalf("get import task: %v", err)
	}
	if gotTask.Status != "completed" {
		t.Errorf("import task status = %q, want %q", gotTask.Status, "completed")
	}
	if gotTask.WantID != uuid.Nil {
		t.Errorf("import task wantId = %s, want uuid.Nil", gotTask.WantID)
	}
}
