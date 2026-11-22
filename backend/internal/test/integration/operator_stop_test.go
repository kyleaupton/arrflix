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
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// Story 07, operator variant: Cancel acquisition (movie) and Stop tracking
// (series) stop Arrflix looking, the way the last requester's withdrawal does. A
// download already handed to the downloader finishes and is imported; only the
// download job's own Cancel stops a transfer.

// cancelAcquisition is the movie Cancel acquisition action: the operator cancels
// the tracking's want over the API.
func cancelAcquisition(t *testing.T, app *testapp.App, wantID uuid.UUID) model.Want {
	t.Helper()
	var out model.Want
	app.POST(t, "/api/v1/wants/"+wantID.String()+"/cancel", nil, &out, http.StatusOK)
	return out
}

// startDownloading moves a handed-off movie job and its want to 'downloading',
// as the download worker's first poll would.
func startDownloading(t *testing.T, ctx context.Context, r *repo.Repository, jobID, wantID uuid.UUID) {
	t.Helper()
	if _, err := r.SetDownloadJobDownloadSnapshot(ctx, repo.SetDownloadJobDownloadSnapshotParams{
		ID:     jobID,
		Status: "downloading",
	}); err != nil {
		t.Fatalf("set download job downloading: %v", err)
	}
	if _, ok, err := r.MirrorWantStatus(ctx, wantID, string(model.WantDownloading)); err != nil || !ok {
		t.Fatalf("mirror want downloading: ok=%v err=%v", ok, err)
	}
}

// runMovieImport runs the download and import workers against a fake downloader
// holding one finished movie file.
func runMovieImport(t *testing.T, ctx context.Context, r *repo.Repository) func() {
	t.Helper()
	content := []byte("movie the operator stopped looking for")
	source := filepath.Join(t.TempDir(), "The.Matrix.1999.1080p.BluRay.x264.mkv")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatalf("write download payload: %v", err)
	}
	return runImportPipeline(t, ctx, r, &fakeDownloaderClient{files: []downloader.File{{Path: source, Size: int64(len(content))}}})
}

// runImportPipeline runs the download and import workers (no acquisition, so
// nothing is searched) against the given fake downloader client.
func runImportPipeline(t *testing.T, ctx context.Context, r *repo.Repository, client *fakeDownloaderClient) func() {
	t.Helper()
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, r, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	stopDownload := runWorker(t, ctx, newDownloadWorkerWithClient(t, ctx, r, client))
	stopImport := runWorker(t, ctx, importw.New(r, dm, logger.New(false), nil, nil, nil))
	return func() {
		stopDownload()
		stopImport()
	}
}

// Cancel acquisition on a movie whose download was already handed to the
// downloader leaves the job running. The download then imports, the want ends
// 'available', and the tracking stays 'canceled'.
func TestOperatorStop_CancelAcquisitionLetsHandedOffDownloadFinish(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		downloading bool
		jobStatus   string
		wantStatus  model.WantStatus
	}{
		{"enqueued", false, "enqueued", model.WantGrabbed},
		{"downloading", true, "downloading", model.WantDownloading},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app, ctx, _ := cancelApp(t)

			req := createMovieRequest(t, app, app.Token)
			trackingID := *req.SpawnedTrackingID
			job := grabTrackedMovie(t, ctx, app, trackingID, "guid-operator-"+tc.name)
			handOff(t, ctx, app.Repo, job.ID)
			want := movieWant(t, ctx, app.Repo, trackingID)
			if tc.downloading {
				startDownloading(t, ctx, app.Repo, job.ID, want.ID)
			}

			got := cancelAcquisition(t, app, want.ID)
			if got.Status != string(tc.wantStatus) {
				t.Errorf("returned want status = %q, want %q (left to finish)", got.Status, tc.wantStatus)
			}
			if got := movieWant(t, ctx, app.Repo, trackingID); got.Status != string(tc.wantStatus) {
				t.Errorf("want status after cancel = %q, want %q (left to finish)", got.Status, tc.wantStatus)
			}
			assertJobStatus(t, ctx, app.Repo, job.ID, tc.jobStatus)
			assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingCanceled)

			defer runMovieImport(t, ctx, app.Repo)()
			waitForWantStatus(t, ctx, app.Repo, want.ID, model.WantAvailable, 30*time.Second)

			files, err := app.Repo.ListFilesForItem(ctx, job.MediaItemID)
			if err != nil {
				t.Fatalf("list files: %v", err)
			}
			if len(files) != 1 {
				t.Errorf("files for title = %d, want the finished download imported", len(files))
			}
			assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingCanceled)
		})
	}
}

// Cancel acquisition on a movie that is still searching, or whose job was never
// sent to the downloader, cancels the want and the job.
func TestOperatorStop_CancelAcquisitionCancelsUnsentWork(t *testing.T) {
	t.Parallel()

	t.Run("searching", func(t *testing.T) {
		t.Parallel()
		app, ctx, _ := cancelApp(t)

		req := createMovieRequest(t, app, app.Token)
		trackingID := *req.SpawnedTrackingID
		want := claimWant(t, ctx, app.Repo, movieWant(t, ctx, app.Repo, trackingID).ID)

		if got := cancelAcquisition(t, app, want.ID); got.Status != string(model.WantCanceled) {
			t.Errorf("want status after cancel = %q, want canceled", got.Status)
		}
		assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingCanceled)
	})

	t.Run("job created", func(t *testing.T) {
		t.Parallel()
		app, ctx, _ := cancelApp(t)

		req := createMovieRequest(t, app, app.Token)
		trackingID := *req.SpawnedTrackingID
		job := grabTrackedMovie(t, ctx, app, trackingID, "guid-operator-unsent")
		want := movieWant(t, ctx, app.Repo, trackingID)

		if got := cancelAcquisition(t, app, want.ID); got.Status != string(model.WantCanceled) {
			t.Errorf("want status after cancel = %q, want canceled", got.Status)
		}
		assertJobStatus(t, ctx, app.Repo, job.ID, "cancelled")
		assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingCanceled)
	})
}

// A download left running by Cancel acquisition that then fails is not searched
// for again: the operator stopped acquisition, so the want ends 'failed'.
func TestOperatorStop_FailedDownloadAfterCancelAcquisitionIsNotResearched(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	req := createMovieRequest(t, app, app.Token)
	trackingID := *req.SpawnedTrackingID
	job := grabTrackedMovie(t, ctx, app, trackingID, "guid-operator-fails")
	handOff(t, ctx, app.Repo, job.ID)
	want := movieWant(t, ctx, app.Repo, trackingID)
	cancelAcquisition(t, app, want.ID)

	stop := runWorker(t, ctx, newDownloadWorkerWithClient(t, ctx, app.Repo, &fakeDownloaderClient{status: downloader.StatusErrored}))
	defer stop()

	waitForWantStatus(t, ctx, app.Repo, want.ID, model.WantFailed, 30*time.Second)
	assertTrackingState(t, ctx, app.Repo, trackingID, model.TrackingCanceled)
}

// Stop tracking on a series with one episode downloading and one searching: the
// downloading episode finishes and is 'available', the searching one is
// cancelled, and no new episode wants are created.
func TestOperatorStop_StopTrackingLetsDownloadingEpisodeFinish(t *testing.T) {
	t.Parallel()
	s := newSeriesReqApp(t)
	ctx := s.ctx
	r := s.app.Repo

	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {
			{ID: 9101, EpisodeNumber: 1, Name: "One", AirDate: "2024-01-05"},
			{ID: 9102, EpisodeNumber: 2, Name: "Two", AirDate: "2024-01-12"},
		},
	})
	trackingID := s.requestSeries(t)
	wants := s.episodeWants(t, trackingID)
	if len(wants) != 2 {
		t.Fatalf("wants after request = %v, want S01E01 and S01E02", keys(wants))
	}

	// Both episodes are claimed (searching); E01 is grabbed alone and handed off.
	e1 := claimWant(t, ctx, r, wants["S01E01"].ID)
	if got, err := r.GetWant(ctx, wants["S01E02"].ID); err != nil || got.Status != string(model.WantSearching) {
		t.Fatalf("S01E02 want = %q (err %v), want searching", got.Status, err)
	}
	single := packResult("guid-s01e01", seriesReqTitle+" S01E01 1080p BluRay x264", 1)
	source := stubIndexerSource{
		SearchFn: func(context.Context, indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{single}, nil
		},
	}
	quality := service.NewQualityProfileService(r)
	acq := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), quality,
		service.NewProposalService(r, quality, nil, nil, logger.New(true)))
	if _, outcome, err := acq.ProcessWant(ctx, e1); err != nil {
		t.Fatalf("ProcessWant: %v", err)
	} else if outcome != service.OutcomeGrabbed {
		t.Fatalf("ProcessWant outcome = %v, want grabbed", outcome)
	}
	jobs, err := r.ListActiveDownloadJobsByWant(ctx, e1.ID)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("S01E01 active download jobs = %d (err %v), want 1", len(jobs), err)
	}
	job := jobs[0]
	handOff(t, ctx, r, job.ID)
	startDownloading(t, ctx, r, job.ID, e1.ID)

	var stopped model.Tracking
	s.app.POST(t, "/api/v1/tracking/"+trackingID.String()+"/cancel", nil, &stopped, http.StatusOK)
	if stopped.State != string(model.TrackingCanceled) {
		t.Errorf("returned tracking state = %q, want canceled", stopped.State)
	}

	after := s.episodeWants(t, trackingID)
	if after["S01E01"].Status != string(model.WantDownloading) {
		t.Errorf("S01E01 want status = %q, want downloading (left to finish)", after["S01E01"].Status)
	}
	if after["S01E02"].Status != string(model.WantCanceled) {
		t.Errorf("S01E02 want status = %q, want canceled", after["S01E02"].Status)
	}
	assertJobStatus(t, ctx, r, job.ID, "downloading")

	files, _ := writeEpisodePayloads(t, 1)
	defer runImportPipeline(t, ctx, r, &fakeDownloaderClient{files: files})()
	waitForWantStatus(t, ctx, r, e1.ID, model.WantAvailable, 30*time.Second)

	final := s.episodeWants(t, trackingID)
	if len(final) != 2 {
		t.Errorf("wants after the download finished = %v, want only S01E01 and S01E02", keys(final))
	}
	if final["S01E02"].Status != string(model.WantCanceled) {
		t.Errorf("S01E02 want status = %q, want still canceled", final["S01E02"].Status)
	}
	if got := s.trackingState(t, trackingID); got != string(model.TrackingCanceled) {
		t.Errorf("tracking state = %q, want canceled", got)
	}
}
