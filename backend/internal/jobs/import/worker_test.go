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
