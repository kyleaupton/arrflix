//go:build integration

package integration

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/downloader/qbittorrent"
	downloadworker "github.com/kyleaupton/arrflix/internal/jobs/download"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/qbittest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

const markingHash = "0123456789abcdef0123456789abcdef01234567"

// qbitApp is a request-lifecycle app whose default downloader is a fake
// qBittorrent, reached through the real qBittorrent client.
func qbitApp(t *testing.T) (*testapp.App, context.Context, *qbittest.Server) {
	t.Helper()
	app, ctx := lifecycleApp(t)
	qb := qbittest.New(t)
	seedDefaultsAt(t, app, qb.URL)
	return app, ctx, qb
}

// grabMagnet requests the movie and grabs a magnet release for it, returning
// the download job the grab created (status 'created', not yet sent).
func grabMagnet(t *testing.T, ctx context.Context, app *testapp.App) model.DownloadJob {
	t.Helper()
	req := createMovieRequest(t, app, app.Token)
	release := movieResult("guid-magnet")
	release.DownloadURL = "magnet:?xt=urn:btih:" + markingHash + "&dn=The.Matrix.1999.1080p.BluRay.x264"
	return grabTrackedMovieRelease(t, ctx, app, *req.SpawnedTrackingID, release)
}

// newQbitDownloadWorker builds a fast-cadence download worker over the real
// qBittorrent client, pointed at whatever downloaders the database holds.
func newQbitDownloadWorker(t *testing.T, ctx context.Context, r *repo.Repository) *downloadworker.Worker {
	t.Helper()
	reg := downloader.NewRegistry()
	qbittorrent.Register(reg)
	dm := downloader.NewManager(reg, r, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	return downloadworker.NewWithConfig(r, dm, logger.New(false), nil, nil, downloadworker.Config{
		PollInterval: 10 * time.Millisecond,
		ClaimLimit:   20,
		MaxAttempts:  3,
	})
}

// handOffThroughWorker runs the download worker until it has added the job's
// torrent and marked the job enqueued, then stops it.
func handOffThroughWorker(t *testing.T, ctx context.Context, r *repo.Repository, jobID uuid.UUID) model.DownloadJob {
	t.Helper()
	stop := runWorker(t, ctx, newQbitDownloadWorker(t, ctx, r))
	defer stop()
	var job model.DownloadJob
	waitFor(t, 30*time.Second, "download job handed to the downloader", func() bool {
		var err error
		job, err = r.GetDownloadJob(ctx, jobID)
		if err != nil {
			t.Fatalf("get download job: %v", err)
		}
		return job.DownloaderExternalID != nil
	})
	return job
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func cancelJob(t *testing.T, app *testapp.App, jobID uuid.UUID) {
	t.Helper()
	app.Do(t, http.MethodDelete, "/api/v1/download-jobs/"+jobID.String(), nil, nil, http.StatusNoContent)
}

// assertCanceledAndLeftInPlace checks the job is cancelled with no import tasks
// and its torrent is still in the downloader. The fake fails the test on any
// pause or delete call, so the torrent is also still transferring.
func assertCanceledAndLeftInPlace(t *testing.T, ctx context.Context, app *testapp.App, qb *qbittest.Server, jobID uuid.UUID) qbittest.Torrent {
	t.Helper()
	assertJobStatus(t, ctx, app.Repo, jobID, "cancelled")
	tasks, err := app.Repo.ListImportTasksByDownloadJob(ctx, jobID)
	if err != nil {
		t.Fatalf("list import tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("import tasks for cancelled job = %d, want 0", len(tasks))
	}
	torrent, ok := qb.Torrent(markingHash)
	if !ok {
		t.Fatalf("torrent %s gone from the downloader, want it left in place", markingHash)
	}
	return torrent
}

// REQ-HARD-005 — Every torrent Arrflix adds carries the `arrflix` category, so
// the user's downloader seeding limits can be scoped to Arrflix's torrents.
//
// A grab handed to qBittorrent lands in the category, which is created when
// qBittorrent doesn't have it yet and reused when it does.
func TestReqHard005_AddedTorrentCarriesArrflixCategory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		preseed bool
	}{
		{name: "category created when missing"},
		{name: "existing category reused", preseed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app, ctx, qb := qbitApp(t)
			if tc.preseed {
				qb.SeedCategory(downloader.Category)
			}
			job := grabMagnet(t, ctx, app)

			handed := handOffThroughWorker(t, ctx, app.Repo, job.ID)

			if got := *handed.DownloaderExternalID; got != markingHash {
				t.Errorf("job external id = %q, want %q", got, markingHash)
			}
			torrent, ok := qb.Torrent(markingHash)
			if !ok {
				t.Fatalf("torrent %s not added to the downloader", markingHash)
			}
			if torrent.Category != "arrflix" {
				t.Errorf("torrent category = %q, want %q", torrent.Category, "arrflix")
			}
			if !qb.HasCategory("arrflix") {
				t.Errorf("downloader has no %q category", "arrflix")
			}
		})
	}
}

// REQ-HARD-004 — A cancelled download job is never imported later, and stays
// identifiable in the downloader: its torrent is tagged `arrflix-canceled`.
//
// Cancelling a job already handed to qBittorrent tags its torrent and leaves it
// there, still downloading.
func TestReqHard004_CancelTagsHandedOffTorrentAndLeavesIt(t *testing.T) {
	t.Parallel()
	app, ctx, qb := qbitApp(t)
	job := grabMagnet(t, ctx, app)
	handOffThroughWorker(t, ctx, app.Repo, job.ID)

	cancelJob(t, app, job.ID)

	torrent := assertCanceledAndLeftInPlace(t, ctx, app, qb, job.ID)
	if !slices.Contains(torrent.Tags, "arrflix-canceled") {
		t.Errorf("torrent tags = %v, want arrflix-canceled", torrent.Tags)
	}
	if torrent.Category != "arrflix" {
		t.Errorf("torrent category = %q, want it kept as arrflix", torrent.Category)
	}

	// The worker never picks the cancelled job back up.
	stop := runWorker(t, ctx, newQbitDownloadWorker(t, ctx, app.Repo))
	time.Sleep(200 * time.Millisecond)
	stop()
	assertCanceledAndLeftInPlace(t, ctx, app, qb, job.ID)
}

// REQ-HARD-004 — A tagging failure doesn't fail the cancel: with the
// downloader unreachable, the job is still cancelled and never imported.
func TestReqHard004_CancelSucceedsWhenDownloaderUnreachable(t *testing.T) {
	t.Parallel()
	app, ctx, qb := qbitApp(t)
	job := grabMagnet(t, ctx, app)
	handOffThroughWorker(t, ctx, app.Repo, job.ID)
	qb.Close()

	cancelJob(t, app, job.ID)

	assertJobStatus(t, ctx, app.Repo, job.ID, "cancelled")
	tasks, err := app.Repo.ListImportTasksByDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list import tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("import tasks for cancelled job = %d, want 0", len(tasks))
	}
}

// REQ-HARD-004 — A job that never reached the downloader has nothing to tag:
// its cancel makes no downloader call.
func TestReqHard004_CancelOfUnsentJobTagsNothing(t *testing.T) {
	t.Parallel()
	app, ctx, qb := qbitApp(t)
	job := grabMagnet(t, ctx, app)

	cancelJob(t, app, job.ID)

	assertJobStatus(t, ctx, app.Repo, job.ID, "cancelled")
	if n := qb.TagCalls(); n != 0 {
		t.Errorf("tag calls = %d, want 0 for a job never handed to the downloader", n)
	}
}

// cancelDuringAdd runs the download worker with qBittorrent's add held open,
// cancels the job while it is, then lets the add finish. The cancel lands
// between the downloader's add and the switch to enqueued.
func cancelDuringAdd(t *testing.T, ctx context.Context, app *testapp.App, qb *qbittest.Server, jobID uuid.UUID) {
	t.Helper()
	adding := make(chan struct{})
	release := make(chan struct{})
	qb.OnAdd(func(string) {
		close(adding)
		<-release
	})
	stop := runWorker(t, ctx, newQbitDownloadWorker(t, ctx, app.Repo))
	defer stop()

	select {
	case <-adding:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the worker to add the torrent")
	}
	cancelJob(t, app, jobID)
	close(release)

	waitFor(t, 30*time.Second, "the worker to tag the torrent", func() bool { return qb.TagCalls() > 0 })
}

// REQ-HARD-004 — A cancel that lands while the worker is adding the torrent
// leaves the job cancelled, and the worker tags the torrent it just added
// `arrflix-canceled`, leaving it in place.
func TestReqHard004_CancelDuringAddTagsTorrent(t *testing.T) {
	t.Parallel()
	app, ctx, qb := qbitApp(t)
	job := grabMagnet(t, ctx, app)

	cancelDuringAdd(t, ctx, app, qb, job.ID)

	torrent := assertCanceledAndLeftInPlace(t, ctx, app, qb, job.ID)
	if !slices.Contains(torrent.Tags, "arrflix-canceled") {
		t.Errorf("torrent tags = %v, want arrflix-canceled", torrent.Tags)
	}
}

// REQ-HARD-004 — When the worker fails to tag a torrent whose job was cancelled
// mid-add, the job stays cancelled.
func TestReqHard004_TagFailureDuringAddKeepsJobCancelled(t *testing.T) {
	t.Parallel()
	app, ctx, qb := qbitApp(t)
	qb.FailTags()
	job := grabMagnet(t, ctx, app)

	cancelDuringAdd(t, ctx, app, qb, job.ID)

	assertCanceledAndLeftInPlace(t, ctx, app, qb, job.ID)
	if got, _ := app.Repo.GetDownloadJob(ctx, job.ID); got.DownloaderExternalID != nil {
		t.Errorf("cancelled job external id = %q, want none recorded", *got.DownloaderExternalID)
	}
}
