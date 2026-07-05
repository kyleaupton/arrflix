//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// movieResult builds a relevance-passing Matrix release under the given guid — the
// substrate for offering/excluding specific releases in the recovery tests.
func movieResult(guid string) indexer.SearchResult {
	return indexer.SearchResult{
		IndexerID:   7,
		IndexerName: "test-indexer",
		GUID:        guid,
		Title:       "The Matrix 1999 1080p BluRay x264",
		DownloadURL: "http://localhost/" + guid + ".torrent",
		Protocol:    "torrent",
		Size:        10 << 30,
		Categories:  []string{"Movies"},
	}
}

// grabMovieWant claims the seeded movie want and runs the front-half over the
// given results, returning the created download_job.
func grabMovieWant(t *testing.T, ctx context.Context, r *repo.Repository, want model.Want, results []indexer.SearchResult) model.DownloadJob {
	t.Helper()
	claimed := claimWant(t, ctx, r, want.ID)
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return results, nil
		},
	}
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))
	if _, outcome, err := svc.ProcessWant(ctx, claimed); err != nil {
		t.Fatalf("ProcessWant: %v", err)
	} else if outcome != service.OutcomeGrabbed {
		t.Fatalf("ProcessWant grabbed = false, want true")
	}
	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("download jobs = %d, want 1", len(jobs))
	}
	return jobs[0]
}

// waitForWantStatus polls until the want reaches status or the deadline passes.
func waitForWantStatus(t *testing.T, ctx context.Context, r *repo.Repository, wantID uuid.UUID, status model.WantStatus, timeout time.Duration) model.Want {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got, err := r.GetWant(ctx, wantID)
		if err != nil {
			t.Fatalf("get want: %v", err)
		}
		if got.Status == string(status) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("want status = %q, want %q within deadline", got.Status, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// runWorker starts a download worker in the background, returning the cancel that
// stops it. Callers poll the DB for the terminal state and defer cancel.
func runWorker(t *testing.T, ctx context.Context, w interface{ Run(context.Context) }) context.CancelFunc {
	t.Helper()
	wctx, cancel := context.WithCancel(ctx)
	go w.Run(wctx)
	return cancel
}

// assertExcluded fails unless the want has exactly one exclusion, for the given
// release, with the download_failed reason.
func assertExcluded(t *testing.T, ctx context.Context, r *repo.Repository, wantID uuid.UUID, indexerID int64, guid string) {
	t.Helper()
	excl, err := r.ListWantReleaseExclusionsForWants(ctx, []uuid.UUID{wantID})
	if err != nil {
		t.Fatalf("list exclusions: %v", err)
	}
	if len(excl) != 1 {
		t.Fatalf("exclusions for want %s = %d, want 1", wantID, len(excl))
	}
	e := excl[0]
	if e.IndexerID != indexerID || e.GUID != guid {
		t.Errorf("exclusion release = (%d, %q), want (%d, %q)", e.IndexerID, e.GUID, indexerID, guid)
	}
	if e.Reason != string(model.ExclusionDownloadFailed) {
		t.Errorf("exclusion reason = %q, want %q", e.Reason, model.ExclusionDownloadFailed)
	}
}

// TestRecovery_FailedDownload_ReleasesWantWithExclusion proves the S4 recovery: a
// download the downloader reports as errored fails the job, excludes the release
// per want, and returns the want to 'pending' — the want stays linked to the
// failed job (history) but has no active job.
func TestRecovery_FailedDownload_ReleasesWantWithExclusion(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	job := grabMovieWant(t, ctx, r, want, []indexer.SearchResult{movieResult("guid-a")})

	w := newDownloadWorkerWithClient(t, ctx, r, &fakeDownloaderClient{status: downloader.StatusErrored})
	cancel := runWorker(t, ctx, w)
	defer cancel()

	waitForWantStatus(t, ctx, r, want.ID, model.WantPending, 10*time.Second)

	failed, err := r.GetDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	if failed.Status != "failed" {
		t.Errorf("job status = %q, want failed", failed.Status)
	}

	assertExcluded(t, ctx, r, want.ID, job.IndexerID, job.Guid)

	// The want stays linked to the failed job for history, but that job is no
	// longer active — the want is free to be re-grabbed.
	linked, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for job: %v", err)
	}
	if len(linked) != 1 {
		t.Errorf("linked wants = %d, want 1 (link retained on failed job)", len(linked))
	}
	active, err := r.ListActiveDownloadJobsByWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("list active jobs for want: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active jobs for want = %d, want 0", len(active))
	}
}

// TestRecovery_ExcludedReleaseNotRepicked proves the consumer side: once a release
// is excluded for a want, the front-half won't re-pick it — offered only the
// excluded release it grabs nothing; offered the excluded one plus an alternative
// it grabs the alternative.
func TestRecovery_ExcludedReleaseNotRepicked(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	if err := r.AddWantReleaseExclusion(ctx, repo.AddWantReleaseExclusionParams{
		WantID:    want.ID,
		IndexerID: 7,
		GUID:      "guid-a",
		Reason:    model.ExclusionDownloadFailed,
	}); err != nil {
		t.Fatalf("add exclusion: %v", err)
	}

	claimed := claimWant(t, ctx, r, want.ID)
	// A fresh service per call so the stub source can vary its results — the want
	// stays 'searching' between calls (the front-half found no winner the first
	// time), so it can't be re-claimed via the pending path.
	newSvc := func(results []indexer.SearchResult) *service.AcquisitionService {
		source := stubIndexerSource{
			SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
				return results, nil
			},
		}
		return service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), service.NewQualityProfileService(r), service.NewProposalService(r, service.NewQualityProfileService(r), nil, logger.New(true)))
	}

	// Offered only the excluded release → no grab.
	if _, outcome, err := newSvc([]indexer.SearchResult{movieResult("guid-a")}).ProcessWant(ctx, claimed); err != nil {
		t.Fatalf("ProcessWant (excluded only): %v", err)
	} else if outcome == service.OutcomeGrabbed {
		t.Fatalf("grabbed = true, want false (only candidate is excluded)")
	}

	// Offer the excluded release plus an alternative → the alternative is grabbed.
	if _, outcome, err := newSvc([]indexer.SearchResult{movieResult("guid-a"), movieResult("guid-b")}).ProcessWant(ctx, claimed); err != nil {
		t.Fatalf("ProcessWant (excluded + alt): %v", err)
	} else if outcome != service.OutcomeGrabbed {
		t.Fatalf("grabbed = false, want true (guid-b is not excluded)")
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("download jobs = %d, want 1", len(jobs))
	}
	if jobs[0].Guid != "guid-b" {
		t.Errorf("job guid = %q, want guid-b (the excluded guid-a must not be re-picked)", jobs[0].Guid)
	}
}

// TestRecovery_MaxAttempts_ExcludesToo proves the retryable-exhaustion site
// recovers too: a Get that keeps erroring burns the attempt ceiling, then the same
// exclude-and-release recovery runs.
func TestRecovery_MaxAttempts_ExcludesToo(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	job := grabMovieWant(t, ctx, r, want, []indexer.SearchResult{movieResult("guid-a")})

	w := newDownloadWorkerWithClient(t, ctx, r, &fakeDownloaderClient{getErr: errors.New("downloader unreachable")})
	cancel := runWorker(t, ctx, w)
	defer cancel()

	// MaxAttempts=3 with 2^attempt-second backoff → ~6s of retries before the
	// terminal fail; allow generous headroom.
	waitForWantStatus(t, ctx, r, want.ID, model.WantPending, 30*time.Second)

	failed, err := r.GetDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	if failed.Status != "failed" {
		t.Errorf("job status = %q, want failed", failed.Status)
	}
	assertExcluded(t, ctx, r, want.ID, job.IndexerID, job.Guid)
}

// TestRecovery_PackFailure_RecoversAllLinkedWants proves a failing pack job
// recovers every linked want: all three return to 'pending', each gains an
// exclusion for the pack release, and the links stay on the failed job.
func TestRecovery_PackFailure_RecoversAllLinkedWants(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingSeasonWants(t, ctx, r, 3, 1, 2, 3)
	job := grabPackForSeason(t, ctx, r, seeded, 1, []indexer.SearchResult{
		packResult("guid-s03-complete", "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
	})

	w := newDownloadWorkerWithClient(t, ctx, r, &fakeDownloaderClient{status: downloader.StatusErrored})
	cancel := runWorker(t, ctx, w)
	defer cancel()

	for epNum, want := range seeded.wants {
		got := waitForWantStatus(t, ctx, r, want.ID, model.WantPending, 10*time.Second)
		if got.Status != string(model.WantPending) {
			t.Errorf("episode %d want status = %q, want pending", epNum, got.Status)
		}
		assertExcluded(t, ctx, r, want.ID, job.IndexerID, job.Guid)
	}

	linked, err := r.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list wants for job: %v", err)
	}
	if len(linked) != 3 {
		t.Errorf("linked wants = %d, want 3 (links retained on failed job)", len(linked))
	}
}

// TestRecovery_ManualGrab_RestampsHold proves the A1 edge is closed: when the
// failed want's segment is manual, recovery re-stamps the needs_pick hold so it
// lands back under the manual gate rather than in the auto-search pool.
func TestRecovery_ManualGrab_RestampsHold(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	job := grabMovieWant(t, ctx, r, want, []indexer.SearchResult{movieResult("guid-a")})

	// Flip the tracking's ongoing segment to manual after the grab (the want's
	// segment is 'ongoing'). The grab already cleared any hold; recovery must
	// re-stamp it.
	if _, err := r.SetTrackingAutonomy(ctx, want.TrackingID, string(model.AutonomyAuto), string(model.AutonomyManual)); err != nil {
		t.Fatalf("set tracking autonomy: %v", err)
	}

	w := newDownloadWorkerWithClient(t, ctx, r, &fakeDownloaderClient{status: downloader.StatusErrored})
	cancel := runWorker(t, ctx, w)
	defer cancel()

	got := waitForWantStatus(t, ctx, r, want.ID, model.WantPending, 10*time.Second)
	if got.Hold == nil || *got.Hold != model.WantHoldNeedsPick {
		t.Errorf("want hold = %v, want %q (manual segment must re-stamp the hold)", got.Hold, model.WantHoldNeedsPick)
	}

	failed, err := r.GetDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	if failed.Status != "failed" {
		t.Errorf("job status = %q, want failed", failed.Status)
	}
}
