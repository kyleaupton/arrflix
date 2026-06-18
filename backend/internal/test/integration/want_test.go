//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// TestWant_Cancel proves the user-initiated cancel cascade: a non-terminal want
// with a live download_job and a pending import task is flipped 'canceled'
// (want + tracking), and its job + pending task are cancelled via the delegated
// DownloadJobsService.Cancel.
func TestWant_Cancel(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)

	// A live download_job for the want. CreateDownloadJob is born 'created'; the
	// snapshot update flips it to 'downloading' (an active, cancellable state).
	downloader, err := r.CreateDownloader(ctx, repo.CreateDownloaderParams{
		Name:           "qbit-cancel",
		DownloaderType: "qbittorrent",
		Protocol:       "torrent",
		URL:            "http://localhost:8080",
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("create downloader: %v", err)
	}
	library, err := r.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name:     "Movies-cancel",
		Type:     "movie",
		RootPath: "/movies-cancel",
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	nameTemplate, err := r.CreateNameTemplate(ctx, repo.CreateNameTemplateParams{
		Name:     "default-cancel",
		Type:     "movie",
		Template: "{title}",
	})
	if err != nil {
		t.Fatalf("create name template: %v", err)
	}

	job, err := r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
		Protocol:       "torrent",
		MediaType:      "movie",
		MediaItemID:    want.MediaItemID,
		IndexerID:      1,
		Guid:           "guid-cancel",
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
	if _, err := r.SetDownloadJobDownloadSnapshot(ctx, repo.SetDownloadJobDownloadSnapshotParams{
		ID:     job.ID,
		Status: "downloading",
	}); err != nil {
		t.Fatalf("set download job downloading: %v", err)
	}

	task, err := r.CreateImportTask(ctx, repo.CreateImportTaskParams{
		DownloadJobID:  job.ID,
		SourcePath:     "/downloads/the.matrix.mkv",
		WantID:         want.ID,
		MediaType:      "movie",
		MediaItemID:    want.MediaItemID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create import task: %v", err)
	}

	svc := service.NewWantService(r, service.NewDownloadJobsService(r))
	canceled, err := svc.CancelWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("CancelWant: %v", err)
	}
	if canceled.Status != string(model.WantCanceled) {
		t.Errorf("canceled want status = %q, want %q", canceled.Status, model.WantCanceled)
	}

	tracking, err := r.GetTracking(ctx, want.TrackingID)
	if err != nil {
		t.Fatalf("get tracking: %v", err)
	}
	if tracking.State != "canceled" {
		t.Errorf("tracking state = %q, want %q", tracking.State, "canceled")
	}

	gotJob, err := r.GetDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	if gotJob.Status != "cancelled" {
		t.Errorf("download job status = %q, want %q", gotJob.Status, "cancelled")
	}

	gotTask, err := r.GetImportTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get import task: %v", err)
	}
	if gotTask.Status != "cancelled" {
		t.Errorf("import task status = %q, want %q", gotTask.Status, "cancelled")
	}
}

// TestWant_Cancel_Terminal proves the CAS guard at the service boundary: an
// 'available' want can't be canceled (Conflict), while an already-'canceled'
// want returns success idempotently. The two wants live on separate trackings —
// the idx_want_tracking_movie partial unique index caps a movie tracking at one
// want (single-atom), so the second want is seeded on its own tracking.
func TestWant_Cancel_Terminal(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	svc := service.NewWantService(r, service.NewDownloadJobsService(r))

	// Available is terminal-for-another-reason: cancel must conflict.
	if _, err := r.SetWantStatus(ctx, want.ID, string(model.WantAvailable)); err != nil {
		t.Fatalf("set want available: %v", err)
	}
	if _, err := svc.CancelWant(ctx, want.ID); !apperrors.IsConflict(err) {
		t.Fatalf("CancelWant on available want err = %v, want Conflict", err)
	}

	// A second want, on its own tracking, already canceled: cancel is idempotent.
	year := int32(2010)
	tmdbID := int64(27205)
	media2, err := r.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type: "movie", Title: "Inception", Year: &year, TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("create second media item: %v", err)
	}
	tracking2, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      media2.ID,
		QualityProfileID: want.QualityProfileID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
	})
	if err != nil {
		t.Fatalf("create second tracking: %v", err)
	}
	want2, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking2.ID,
		MediaItemID:      media2.ID,
		QualityProfileID: want.QualityProfileID,
		Status:           string(model.WantCanceled),
	})
	if err != nil {
		t.Fatalf("create canceled want: %v", err)
	}
	got, err := svc.CancelWant(ctx, want2.ID)
	if err != nil {
		t.Fatalf("CancelWant on canceled want (idempotent): %v", err)
	}
	if got.Status != string(model.WantCanceled) {
		t.Errorf("idempotent cancel status = %q, want %q", got.Status, model.WantCanceled)
	}
}

// TestWant_RescheduleRecheck_NoIncrement guards the attempt_count decoupling:
// RescheduleWantRecheck (the "no eligible release yet" path) returns a searching
// want to 'pending' WITHOUT bumping attempt_count, while ScheduleWantRetry (the
// error-retry path) still increments. Both fire from the 'searching' CAS state.
func TestWant_RescheduleRecheck_NoIncrement(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	want = claimWant(t, ctx, r, want.ID)

	// Recheck: pending, attempt_count untouched.
	rechecked, ok, err := r.RescheduleWantRecheck(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: "no eligible release",
		NextRunAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("RescheduleWantRecheck: %v", err)
	}
	if !ok {
		t.Fatalf("RescheduleWantRecheck ok = false, want true (want was 'searching')")
	}
	if rechecked.Status != string(model.WantPending) {
		t.Errorf("rechecked status = %q, want %q", rechecked.Status, model.WantPending)
	}
	if rechecked.AttemptCount != want.AttemptCount {
		t.Errorf("rechecked attemptCount = %d, want %d (recheck must not increment)", rechecked.AttemptCount, want.AttemptCount)
	}
	if rechecked.LastError == nil || *rechecked.LastError != "no eligible release" {
		t.Errorf("rechecked lastError = %v, want %q", rechecked.LastError, "no eligible release")
	}

	// Re-flip to 'searching' so the error-retry CAS owns it (next_run_at is now
	// in the future, so a claim wouldn't pick it).
	if _, err := r.SetWantStatus(ctx, want.ID, string(model.WantSearching)); err != nil {
		t.Fatalf("set want searching: %v", err)
	}

	// Error retry: still increments — the counter belongs to this path alone.
	retried, ok, err := r.ScheduleWantRetry(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: "transient blip",
		NextRunAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ScheduleWantRetry: %v", err)
	}
	if !ok {
		t.Fatalf("ScheduleWantRetry ok = false, want true (want was 'searching')")
	}
	if retried.AttemptCount != want.AttemptCount+1 {
		t.Errorf("retried attemptCount = %d, want %d (error retry must increment)", retried.AttemptCount, want.AttemptCount+1)
	}
}

// TestWant_MirrorStatus_TerminalSticky proves the resurrection guard: once a
// want is terminal ('canceled'), MirrorWantStatus — the worker mirror path —
// refuses to advance it. This is what stops a late in-flight import from
// flipping a canceled want back to 'available'.
func TestWant_MirrorStatus_TerminalSticky(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	if _, err := r.SetWantStatus(ctx, want.ID, string(model.WantCanceled)); err != nil {
		t.Fatalf("set want canceled: %v", err)
	}

	mirrored, ok, err := r.MirrorWantStatus(ctx, want.ID, string(model.WantAvailable))
	if err != nil {
		t.Fatalf("MirrorWantStatus: %v", err)
	}
	if ok {
		t.Fatalf("MirrorWantStatus ok = true, want false (terminal want must be sticky)")
	}
	_ = mirrored // zero value on a blocked mirror

	got, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if got.Status != string(model.WantCanceled) {
		t.Errorf("want status = %q, want %q (unchanged by blocked mirror)", got.Status, model.WantCanceled)
	}
}
