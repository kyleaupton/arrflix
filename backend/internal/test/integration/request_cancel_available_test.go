//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/indexer"
	importw "github.com/kyleaupton/arrflix/internal/jobs/import"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// acquireMovie takes the tracking's single movie want end to end: grabbed on the
// given release guid, downloaded, and imported into the library. It returns once
// the want is available and the file is recorded against the title.
func acquireMovie(t *testing.T, ctx context.Context, app *testapp.App, trackingID uuid.UUID, guid string) {
	t.Helper()
	job := grabTrackedMovie(t, ctx, app, trackingID, guid)

	content := []byte("acquired movie bytes")
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

	waitForWantStatus(t, ctx, app.Repo, movieWant(t, ctx, app.Repo, trackingID).ID, model.WantAvailable, 30*time.Second)
	files, err := app.Repo.ListFilesForItem(ctx, job.MediaItemID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("setup: files for title = %d, want 1 imported file", len(files))
	}
}

// requestWholeSeries requests the series at HD as the auto-approving admin and
// returns the spawned request.
func requestWholeSeries(t *testing.T, s seriesReqApp) model.Request {
	t.Helper()
	var req model.Request
	s.app.POST(t, "/api/v1/requests", map[string]any{
		"tmdbId": seriesReqTmdbID, "type": "series", "tier": "HD",
	}, &req, http.StatusCreated)
	if req.Status != string(model.RequestSpawned) || req.SpawnedTrackingID == nil {
		t.Fatalf("series request status = %q, spawned tracking = %v; want spawned with a tracking", req.Status, req.SpawnedTrackingID)
	}
	return req
}

// titleStatusAs reads the title-status projection as the given token.
func titleStatusAs(t *testing.T, app *testapp.App, token, mediaType string, tmdbID int64) model.TitleStatus {
	t.Helper()
	var st model.TitleStatus
	app.GETAs(t, token, fmt.Sprintf("/api/v1/titles/%s/%d/status", mediaType, tmdbID), &st, http.StatusOK)
	return st
}

func hasAction(st model.TitleStatus, kind string) bool {
	for _, a := range st.Actions {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

// assertWithdrawalRefused asserts the request can't be withdrawn: the cancel is
// a 409, and the request reads back still spawned.
func assertWithdrawalRefused(t *testing.T, app *testapp.App, token string, requestID uuid.UUID) {
	t.Helper()
	var problem apperrors.ProblemDetails
	app.POSTAs(t, token, "/api/v1/requests/"+requestID.String()+"/cancel", nil, &problem, http.StatusConflict)
	if problem.Status != http.StatusConflict {
		t.Errorf("problem status = %d, want 409", problem.Status)
	}
	var after model.Request
	app.GET(t, "/api/v1/requests/"+requestID.String(), &after, http.StatusOK)
	if after.Status != string(model.RequestSpawned) {
		t.Errorf("request status after refused withdrawal = %q, want spawned", after.Status)
	}
}

// REQ-CANCEL-008 — Withdrawal must not be offered once the title is available to
// that user. Availability ends the request's life; removal from the library is a
// separate, operator-authorized action.
//
// A requester's movie is acquired end to end. The focus page's projection must
// no longer offer them a cancel, and the API must refuse the withdrawal with a
// 409, leaving the request spawned and the requester on the tracking.
func TestReqCancel008_AvailableMovieIsNotWithdrawable(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	dana, danaToken := app.UserToken(t, ctx, "dana", "requester")
	req := createMovieRequest(t, app, danaToken)
	app.POST(t, "/api/v1/requests/"+req.ID.String()+"/approve", nil, &req, http.StatusOK)
	if req.SpawnedTrackingID == nil {
		t.Fatalf("setup: approved request did not spawn a tracking")
	}
	trackingID := *req.SpawnedTrackingID

	if st := titleStatusAs(t, app, danaToken, "movie", spawnTmdbID); !hasAction(st, "cancel") {
		t.Fatalf("setup: a live request for a title not yet available must offer cancel, got %+v", st.Actions)
	}

	acquireMovie(t, ctx, app, trackingID, "guid-available")

	st := titleStatusAs(t, app, danaToken, "movie", spawnTmdbID)
	if st.State != "available" {
		t.Fatalf("setup: title state = %q, want available", st.State)
	}
	if hasAction(st, "cancel") {
		t.Errorf("an available title must not offer withdrawal, got actions %+v", st.Actions)
	}

	assertWithdrawalRefused(t, app, danaToken, req.ID)

	var requesters []model.TrackingRequester
	app.GET(t, "/api/v1/tracking/"+trackingID.String()+"/requesters", &requesters, http.StatusOK)
	if len(requesters) != 1 || requesters[0].UserID != dana.ID {
		t.Errorf("requesters after refused withdrawal = %+v, want dana (%s) still on the tracking", requesters, dana.ID)
	}
}

// REQ-CANCEL-008 — For a series, available means every in-scope episode has a
// file. Once the season pack delivers every episode, the series is no longer
// withdrawable.
func TestReqCancel008_FullyAvailableSeriesIsNotWithdrawable(t *testing.T) {
	t.Parallel()
	s := newSeriesReqApp(t)

	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {
			{ID: 8101, EpisodeNumber: 1, Name: "One", AirDate: "2024-01-05"},
			{ID: 8102, EpisodeNumber: 2, Name: "Two", AirDate: "2024-01-12"},
		},
	})
	req := requestWholeSeries(t, s)
	wants := s.episodeWants(t, *req.SpawnedTrackingID)
	if len(wants) != 2 {
		t.Fatalf("wants after request = %v, want S01E01..S01E02", keys(wants))
	}

	files, _ := writeEpisodePayloads(t, 1, 2)
	idx := &recordingIndexer{respond: func(n int, _ indexer.SearchQuery) []indexer.SearchResult {
		return []indexer.SearchResult{seasonOnePack("guid-s01-full")}
	}}
	stop := s.runSeriesPipeline(t, idx, &fakeDownloaderClient{files: files})
	waitForWantStatus(t, s.ctx, s.app.Repo, wants["S01E01"].ID, model.WantAvailable, 30*time.Second)
	waitForWantStatus(t, s.ctx, s.app.Repo, wants["S01E02"].ID, model.WantAvailable, 30*time.Second)
	stop()

	st := titleStatusAs(t, s.app, s.app.Token, "series", seriesReqTmdbID)
	if st.State != "available" {
		t.Fatalf("setup: title state = %q, want available", st.State)
	}
	if hasAction(st, "cancel") {
		t.Errorf("a fully available series must not offer withdrawal, got actions %+v", st.Actions)
	}

	assertWithdrawalRefused(t, s.app, s.app.Token, req.ID)
}

// REQ-CANCEL-008 — A series with in-scope episodes still to acquire is only
// partly available, so its request remains withdrawable.
func TestReqCancel008_PartlyAvailableSeriesIsWithdrawable(t *testing.T) {
	t.Parallel()
	s := newSeriesReqApp(t)

	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {
			{ID: 8201, EpisodeNumber: 1, Name: "One", AirDate: "2024-01-05"},
			{ID: 8202, EpisodeNumber: 2, Name: "Two", AirDate: "2024-01-12"},
		},
	})
	req := requestWholeSeries(t, s)
	wants := s.episodeWants(t, *req.SpawnedTrackingID)
	if len(wants) != 2 {
		t.Fatalf("wants after request = %v, want S01E01..S01E02", keys(wants))
	}

	// The pack carries only E01; E02 is never found again.
	files, _ := writeEpisodePayloads(t, 1)
	idx := &recordingIndexer{respond: func(n int, _ indexer.SearchQuery) []indexer.SearchResult {
		if n == 1 {
			return []indexer.SearchResult{seasonOnePack("guid-s01-partial")}
		}
		return nil
	}}
	stop := s.runSeriesPipeline(t, idx, &fakeDownloaderClient{files: files})
	waitForWantStatus(t, s.ctx, s.app.Repo, wants["S01E01"].ID, model.WantAvailable, 30*time.Second)
	stop()

	st := titleStatusAs(t, s.app, s.app.Token, "series", seriesReqTmdbID)
	if st.State != "partially_available" {
		t.Fatalf("setup: title state = %q, want partially_available", st.State)
	}
	if !hasAction(st, "cancel") {
		t.Errorf("a partly available series must still offer withdrawal, got actions %+v", st.Actions)
	}

	withdrawRequest(t, s.app, s.app.Token, req.ID)
}
