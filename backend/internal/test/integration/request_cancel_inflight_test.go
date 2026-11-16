//go:build integration

package integration

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/indexer"
	importw "github.com/kyleaupton/arrflix/internal/jobs/import"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// handOff records that the download worker gave the job to the downloader, the
// moment a download counts as transferring (REQ-CANCEL-004).
func handOff(t *testing.T, ctx context.Context, r *repo.Repository, jobID uuid.UUID) {
	t.Helper()
	if _, ok, err := r.SetDownloadJobEnqueued(ctx, jobID, "ext-1"); err != nil || !ok {
		t.Fatalf("hand off download job %s: ok=%v err=%v", jobID, ok, err)
	}
}

// withdrawRequest withdraws a request over HTTP as the given token.
func withdrawRequest(t *testing.T, app *testapp.App, token string, requestID uuid.UUID) {
	t.Helper()
	var withdrawn model.Request
	app.POSTAs(t, token, "/api/v1/requests/"+requestID.String()+"/cancel", nil, &withdrawn, http.StatusOK)
	if withdrawn.Status != string(model.RequestCanceled) {
		t.Fatalf("withdrawn request status = %q, want canceled", withdrawn.Status)
	}
}

// movieWant returns a movie tracking's single want.
func movieWant(t *testing.T, ctx context.Context, r *repo.Repository, trackingID uuid.UUID) model.Want {
	t.Helper()
	wants, err := r.ListWantsByTracking(ctx, trackingID)
	if err != nil {
		t.Fatalf("list wants: %v", err)
	}
	if len(wants) != 1 {
		t.Fatalf("wants on tracking = %d, want 1", len(wants))
	}
	return wants[0]
}

func assertJobStatus(t *testing.T, ctx context.Context, r *repo.Repository, jobID uuid.UUID, want string) {
	t.Helper()
	job, err := r.GetDownloadJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	if job.Status != want {
		t.Errorf("download job status = %q, want %q", job.Status, want)
	}
}

func assertTrackingState(t *testing.T, ctx context.Context, r *repo.Repository, trackingID uuid.UUID, want model.TrackingState) {
	t.Helper()
	tracking, err := r.GetTracking(ctx, trackingID)
	if err != nil {
		t.Fatalf("get tracking: %v", err)
	}
	if tracking.State != string(want) {
		t.Errorf("tracking state = %q, want %q", tracking.State, want)
	}
}

// REQ-CANCEL-004 — A download already handed to the downloader finishes after
// the last requester withdraws.
//
// The job is handed off but the worker hasn't polled it yet, so the want is
// still 'grabbed' rather than 'downloading'. Withdrawal must leave both alone
// and park the tracking; the download then completes, imports, and the want
// ends 'available'.
func TestReqCancel004_HandedOffDownloadFinishesAfterWithdrawal(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	req := createMovieRequest(t, app, app.Token)
	trackingID := *req.SpawnedTrackingID
	job := grabTrackedMovie(t, ctx, app, trackingID, "guid-handed-off")
	handOff(t, ctx, app.Repo, job.ID)

	withdrawRequest(t, app, app.Token, req.ID)

	want := movieWant(t, ctx, app.Repo, trackingID)
	if want.Status != string(model.WantGrabbed) {
		t.Fatalf("want status after withdrawal = %q, want grabbed (left to finish)", want.Status)
	}
	assertJobStatus(t, ctx, app.Repo, job.ID, "enqueued")
	assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingPaused)

	content := []byte("movie nobody asked for any more")
	source := filepath.Join(t.TempDir(), "The.Matrix.1999.1080p.BluRay.x264.mkv")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatalf("write download payload: %v", err)
	}
	client := &fakeDownloaderClient{files: []downloader.File{{Path: source, Size: int64(len(content))}}}
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, app.Repo, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	stopDownload := runWorker(t, ctx, newDownloadWorkerWithClient(t, ctx, app.Repo, client))
	defer stopDownload()
	stopImport := runWorker(t, ctx, importw.New(app.Repo, dm, logger.New(false), nil, nil, nil))
	defer stopImport()

	waitForWantStatus(t, ctx, app.Repo, want.ID, model.WantAvailable, 30*time.Second)
	files, err := app.Repo.ListFilesForItem(ctx, job.MediaItemID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("files for title = %d, want the finished download imported", len(files))
	}
}

// REQ-CANCEL-004 — Withdrawal cancels a download that was never handed to the
// downloader, and the cancel holds against the worker sending it afterwards.
func TestReqCancel004_WithdrawalCancelsUnsentDownload(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	req := createMovieRequest(t, app, app.Token)
	trackingID := *req.SpawnedTrackingID
	job := grabTrackedMovie(t, ctx, app, trackingID, "guid-unsent")

	withdrawRequest(t, app, app.Token, req.ID)

	if got := movieWant(t, ctx, app.Repo, trackingID); got.Status != string(model.WantCanceled) {
		t.Errorf("want status after withdrawal = %q, want canceled", got.Status)
	}
	assertJobStatus(t, ctx, app.Repo, job.ID, "cancelled")
	assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingPaused)

	// A worker that was mid-Add when the cancel landed can't revive the job.
	if _, ok, err := app.Repo.SetDownloadJobEnqueued(ctx, job.ID, "ext-late"); err != nil || ok {
		t.Errorf("enqueue after cancel: ok=%v err=%v, want a no-op", ok, err)
	}
	assertJobStatus(t, ctx, app.Repo, job.ID, "cancelled")
}

// REQ-CANCEL-004 — A want whose import is under way is never cancelled.
func TestReqCancel004_ImportingWantIsNotCancelled(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	req := createMovieRequest(t, app, app.Token)
	trackingID := *req.SpawnedTrackingID
	job := grabTrackedMovie(t, ctx, app, trackingID, "guid-importing")
	handOff(t, ctx, app.Repo, job.ID)
	if _, err := app.Repo.SetDownloadJobCompleted(ctx, job.ID, "/downloads", "/downloads/movie"); err != nil {
		t.Fatalf("complete download job: %v", err)
	}
	want := movieWant(t, ctx, app.Repo, trackingID)
	if _, ok, err := app.Repo.MirrorWantStatus(ctx, want.ID, string(model.WantImported)); err != nil || !ok {
		t.Fatalf("mirror want imported: ok=%v err=%v", ok, err)
	}

	withdrawRequest(t, app, app.Token, req.ID)

	if got := movieWant(t, ctx, app.Repo, trackingID); got.Status != string(model.WantImported) {
		t.Errorf("want status after withdrawal = %q, want imported (left to finish)", got.Status)
	}
	assertJobStatus(t, ctx, app.Repo, job.ID, "completed")
}

// A download that survives withdrawal and then fails must not send its want back
// to search: nobody wants the title, so the want ends 'failed'.
func TestReqCancel004_FailedDownloadAfterWithdrawalIsNotResearched(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	req := createMovieRequest(t, app, app.Token)
	trackingID := *req.SpawnedTrackingID
	job := grabTrackedMovie(t, ctx, app, trackingID, "guid-fails-later")
	handOff(t, ctx, app.Repo, job.ID)
	withdrawRequest(t, app, app.Token, req.ID)

	client := &fakeDownloaderClient{status: downloader.StatusErrored}
	stop := runWorker(t, ctx, newDownloadWorkerWithClient(t, ctx, app.Repo, client))
	defer stop()

	want := movieWant(t, ctx, app.Repo, trackingID)
	waitForWantStatus(t, ctx, app.Repo, want.ID, model.WantFailed, 30*time.Second)
	assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingPaused)
}

// Requesting a withdrawn title again resumes its tracking, so a download that
// survived the withdrawal recovers normally: when it fails, the want goes back
// to search.
func TestReqCancel004_ReRequestResumesWithdrawnTracking(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	req := createMovieRequest(t, app, app.Token)
	trackingID := *req.SpawnedTrackingID
	job := grabTrackedMovie(t, ctx, app, trackingID, "guid-rerequested")
	handOff(t, ctx, app.Repo, job.ID)
	withdrawRequest(t, app, app.Token, req.ID)

	again := createMovieRequest(t, app, app.Token)
	if again.SpawnedTrackingID == nil || *again.SpawnedTrackingID != trackingID {
		t.Fatalf("re-request tracking = %v, want %s", again.SpawnedTrackingID, trackingID)
	}
	assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingActive)
	want := movieWant(t, ctx, app.Repo, trackingID)
	if want.Status != string(model.WantGrabbed) {
		t.Fatalf("want status after re-request = %q, want grabbed (joined the live download)", want.Status)
	}

	client := &fakeDownloaderClient{status: downloader.StatusErrored}
	stop := runWorker(t, ctx, newDownloadWorkerWithClient(t, ctx, app.Repo, client))
	defer stop()
	waitForWantStatus(t, ctx, app.Repo, want.ID, model.WantPending, 30*time.Second)
}

// A grab racing the withdrawal leaves no orphan: the withdrawal waits for the
// grab's transaction, then sees the unsent job and cancels it with the want.
func TestReqCancel004_WithdrawalWaitsOutConcurrentGrab(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)
	dl, library, template := seedDefaults(t, app)
	admin := adminUser(t, app, ctx)

	req := createMovieRequest(t, app, app.Token)
	trackingID := *req.SpawnedTrackingID
	want := claimWant(t, ctx, app.Repo, movieWant(t, ctx, app.Repo, trackingID).ID)
	item, err := app.Repo.GetMediaItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("get media item: %v", err)
	}

	grabbed := make(chan struct{})
	release := make(chan struct{})
	grabErr := make(chan error, 1)
	go func() {
		grabErr <- app.Repo.InTx(ctx, func(r *repo.Repository) error {
			if _, ok, err := r.GrabWant(ctx, want.ID); err != nil || !ok {
				close(grabbed)
				return err
			}
			job, err := r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
				Protocol:       "torrent",
				MediaType:      "movie",
				MediaItemID:    item.ID,
				IndexerID:      1,
				Guid:           "guid-racing",
				CandidateTitle: "The Matrix 1999 1080p BluRay x264",
				CandidateLink:  "http://localhost/torrent",
				DownloaderID:   dl.ID,
				LibraryID:      library.ID,
				NameTemplateID: template.ID,
			})
			if err != nil {
				close(grabbed)
				return err
			}
			if err := r.LinkDownloadJobWant(ctx, job.ID, want.ID); err != nil {
				close(grabbed)
				return err
			}
			close(grabbed)
			<-release
			return nil
		})
	}()
	<-grabbed

	withdrawErr := make(chan error, 1)
	go func() {
		_, err := app.Services.Requests.Cancel(ctx, admin.ID, req.ID)
		withdrawErr <- err
	}()

	select {
	case err := <-withdrawErr:
		t.Fatalf("withdrawal finished while the grab held the want (err %v); it must wait", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if err := <-grabErr; err != nil {
		t.Fatalf("grab tx: %v", err)
	}
	if err := <-withdrawErr; err != nil {
		t.Fatalf("withdrawal: %v", err)
	}

	if got := movieWant(t, ctx, app.Repo, trackingID); got.Status != string(model.WantCanceled) {
		t.Errorf("want status = %q, want canceled", got.Status)
	}
	jobs, err := app.Repo.ListDownloadJobsByMediaItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != "cancelled" {
		t.Errorf("download jobs = %+v, want the raced job cancelled", jobs)
	}
}

// REQ-CANCEL-002 — Cancelling a want doesn't cancel a download job that still
// serves other live wants.
//
// One season pack covers three episode wants. Cancelling one episode leaves the
// pack running for the other two; cancelling the last of them cancels it.
func TestReqCancel002_SharedPackOutlivesOneCancelledEpisode(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	seeded := seedPendingSeasonWants(t, ctx, r, 3, 1, 2, 3)
	job := grabPackForSeason(t, ctx, r, seeded, 1, []indexer.SearchResult{
		packResult("guid-s03-shared", "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
	})
	wants := service.NewWantService(r)

	if _, err := wants.CancelWant(ctx, seeded.wants[1].ID); err != nil {
		t.Fatalf("cancel episode 1: %v", err)
	}
	assertJobStatus(t, ctx, r, job.ID, "created")
	for _, ep := range []int{2, 3} {
		got, err := r.GetWant(ctx, seeded.wants[ep].ID)
		if err != nil {
			t.Fatalf("get want: %v", err)
		}
		if got.Status != string(model.WantGrabbed) {
			t.Errorf("episode %d want status = %q, want grabbed", ep, got.Status)
		}
	}

	for _, ep := range []int{2, 3} {
		if _, err := wants.CancelWant(ctx, seeded.wants[ep].ID); err != nil {
			t.Fatalf("cancel episode %d: %v", ep, err)
		}
	}
	assertJobStatus(t, ctx, r, job.ID, "cancelled")
}

// REQ-CANCEL-004 — Reconciling a series whose requesters are gone leaves its
// committed downloads running, and cancels only what was never sent.
//
// The seeded tracking has no requesters, so every episode is out of scope, the
// state a series is in after its last requester withdraws.
func TestReqCancel004_ReconcileStandsDownOnlyUnsentEpisodes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		handedOff  bool
		wantStatus model.WantStatus
		jobStatus  string
	}{
		{"handed-off pack finishes", true, model.WantGrabbed, "enqueued"},
		{"unsent pack is cancelled", false, model.WantCanceled, "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pool := dbtest.New(t)
			r := repo.New(pool)
			ctx := context.Background()

			seeded := seedPendingSeasonWants(t, ctx, r, 3, 1, 2, 3)
			job := grabPackForSeason(t, ctx, r, seeded, 1, []indexer.SearchResult{
				packResult("guid-s03-withdrawn", "Game of Thrones S03 COMPLETE 1080p BluRay x264", 3),
			})
			if tc.handedOff {
				handOff(t, ctx, r, job.ID)
			}

			if err := service.NewReconcileService(r, logger.New(false)).Reconcile(ctx, seeded.trackingID); err != nil {
				t.Fatalf("reconcile: %v", err)
			}

			for ep, w := range seeded.wants {
				got, err := r.GetWant(ctx, w.ID)
				if err != nil {
					t.Fatalf("get want: %v", err)
				}
				if got.Status != string(tc.wantStatus) {
					t.Errorf("episode %d want status = %q, want %q", ep, got.Status, tc.wantStatus)
				}
			}
			assertJobStatus(t, ctx, r, job.ID, tc.jobStatus)
		})
	}
}
