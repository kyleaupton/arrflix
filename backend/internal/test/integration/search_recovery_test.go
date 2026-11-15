//go:build integration

package integration

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/indexer"
	acquisitionworker "github.com/kyleaupton/arrflix/internal/jobs/acquisition"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// newAcquisitionWorker wires an acquisition worker over the given indexer source on
// the hot retryTestConfig cadence.
func newAcquisitionWorker(r *repo.Repository, source indexer.IndexerSource) *acquisitionworker.Worker {
	quality := service.NewQualityProfileService(r)
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), quality, service.NewProposalService(r, quality, nil, nil, logger.New(true)))
	return acquisitionworker.NewWithConfig(r, svc, service.NewSchedulerService(r, logger.New(true)), logger.New(true), nil, nil, retryTestConfig())
}

// lastError renders a want's nullable last_error for failure messages.
func lastError(w model.Want) string {
	if w.LastError == nil {
		return ""
	}
	return *w.LastError
}

// TestReqSearch001_NoReleaseYetNeverFailsTheWant — REQ-SEARCH-001: a want must
// never reach a terminal failed state merely because no acceptable release exists
// yet. Failure is for things that actively went wrong, not for waiting.
//
// The real acquisition worker drives one want through repeated search cycles that
// each come back empty or with nothing but releases the profile hard-rejects (a cam
// rip, an out-of-profile 720p). After every cycle the want must be back in
// 'pending' with a future search scheduled — never 'failed'. Between cycles the
// tracking-level retry nudges the want due again so the loop runs several times
// within the test instead of waiting out the recheck cadence.
func TestReqSearch001_NoReleaseYetNeverFailsTheWant(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)

	junk := []indexer.SearchResult{
		{
			IndexerID:   7,
			IndexerName: "test-indexer",
			GUID:        "guid-cam",
			Title:       "The Matrix 1999 CAM x264",
			DownloadURL: "http://localhost/cam.torrent",
			Protocol:    "torrent",
			Size:        2 << 30,
			Categories:  []string{"Movies"},
		},
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
	}
	// Odd searches find nothing at all; even searches find only junk.
	var searches atomic.Int32
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			if searches.Add(1)%2 == 0 {
				return junk, nil
			}
			return nil, nil
		},
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go newAcquisitionWorker(r, source).Run(workerCtx)

	const cycles = 4
	for cycle := 1; cycle <= cycles; cycle++ {
		// Wait until this cycle's search has run and the worker has finished
		// handling it (the want has left 'searching').
		deadline := time.Now().Add(10 * time.Second)
		var got model.Want
		for {
			var err error
			got, err = r.GetWant(ctx, want.ID)
			if err != nil {
				t.Fatalf("get want: %v", err)
			}
			if got.Status == string(model.WantFailed) {
				t.Fatalf("cycle %d: want terminally failed with no release available (lastError=%q); waiting is not failure", cycle, lastError(got))
			}
			if int(searches.Load()) >= cycle && got.Status != string(model.WantSearching) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("cycle %d: searches=%d status=%q, want search %d handled within deadline", cycle, searches.Load(), got.Status, cycle)
			}
			time.Sleep(10 * time.Millisecond)
		}

		if got.Status != string(model.WantPending) {
			t.Fatalf("cycle %d: want status = %q, want %q (still waiting for a release)", cycle, got.Status, model.WantPending)
		}
		if !got.NextRunAt.After(time.Now()) {
			t.Fatalf("cycle %d: next_run_at = %v, want a future search scheduled", cycle, got.NextRunAt)
		}

		jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
		if err != nil {
			t.Fatalf("list download jobs: %v", err)
		}
		if len(jobs) != 0 {
			t.Fatalf("cycle %d: download jobs = %d, want 0 (nothing acceptable was offered)", cycle, len(jobs))
		}

		// Make the want due again. Only 'pending' is nudged here: the status was
		// asserted above, so a failed want can't be silently re-armed past the check.
		if cycle < cycles {
			if _, err := r.RetryTrackingWants(ctx, want.TrackingID); err != nil {
				t.Fatalf("retry tracking wants: %v", err)
			}
		}
	}
}

// TestReqSearch009_FailedReleaseNeverRepicked — REQ-SEARCH-009: a release that
// already failed to download must not be picked again for the same want.
//
// The real acquisition and download workers run together against an indexer that
// always offers the same two releases — A, which the profile ranks first (more
// seeders), and B — and a downloader that fails every download. A is grabbed and
// fails; the want must then grab B, not A again. When B fails too, the want must
// settle with no eligible release rather than re-grab either one. Exactly one
// download job per release proves neither was picked twice.
func TestReqSearch009_FailedReleaseNeverRepicked(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)

	many, few := 500, 5
	preferred := movieResult("guid-a")
	preferred.Seeders = &many
	fallback := movieResult("guid-b")
	fallback.Seeders = &few
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{preferred, fallback}, nil
		},
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go newAcquisitionWorker(r, source).Run(workerCtx)
	go newDownloadWorkerWithClient(t, ctx, r, &fakeDownloaderClient{status: downloader.StatusErrored}).Run(workerCtx)

	// Settled: both releases failed and excluded, and the follow-up search found
	// nothing eligible.
	deadline := time.Now().Add(20 * time.Second)
	for {
		got, err := r.GetWant(ctx, want.ID)
		if err != nil {
			t.Fatalf("get want: %v", err)
		}
		excl, err := r.ListWantReleaseExclusionsForWants(ctx, []uuid.UUID{want.ID})
		if err != nil {
			t.Fatalf("list exclusions: %v", err)
		}
		if len(excl) == 2 && got.Status == string(model.WantPending) &&
			got.LastError != nil && *got.LastError == "no eligible release" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("want did not settle: status=%q lastError=%q exclusions=%d", got.Status, lastError(got), len(excl))
		}
		time.Sleep(10 * time.Millisecond)
	}

	jobs, err := r.ListDownloadJobsByMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	perGUID := map[string]int{}
	for _, j := range jobs {
		perGUID[j.Guid]++
		if j.Status != "failed" {
			t.Errorf("job %s (guid %s) status = %q, want failed", j.ID, j.Guid, j.Status)
		}
	}
	if len(jobs) != 2 || perGUID["guid-a"] != 1 || perGUID["guid-b"] != 1 {
		t.Fatalf("download jobs per release = %v (total %d), want exactly one each for guid-a and guid-b", perGUID, len(jobs))
	}
}
