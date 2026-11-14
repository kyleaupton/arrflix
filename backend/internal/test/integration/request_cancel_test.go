//go:build integration

package integration

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/indexer"
	importw "github.com/kyleaupton/arrflix/internal/jobs/import"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// cancelApp is lifecycleApp plus the default downloader, library and name
// template routing falls back to, so a spawned movie want can be grabbed. It
// returns the library, whose root is a per-test temp dir imports land in.
func cancelApp(t *testing.T) (*testapp.App, context.Context, model.Library) {
	t.Helper()
	app, ctx := lifecycleApp(t)
	_, library, _ := seedDefaults(t, app)
	return app, ctx, library
}

// grabTrackedMovie runs the acquisition front-half on the tracking's single want,
// offered only the release with the given guid, and returns the download job the
// grab created.
func grabTrackedMovie(t *testing.T, ctx context.Context, app *testapp.App, trackingID uuid.UUID, guid string) model.DownloadJob {
	t.Helper()
	wants, err := app.Repo.ListWantsByTracking(ctx, trackingID)
	if err != nil {
		t.Fatalf("list wants: %v", err)
	}
	if len(wants) != 1 {
		t.Fatalf("wants on tracking = %d, want 1", len(wants))
	}
	claimed := claimWant(t, ctx, app.Repo, wants[0].ID)

	r := app.Repo
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{movieResult(guid)}, nil
		},
	}
	quality := service.NewQualityProfileService(r)
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), quality,
		service.NewProposalService(r, quality, nil, nil, logger.New(true)))
	if _, outcome, err := svc.ProcessWant(ctx, claimed); err != nil {
		t.Fatalf("ProcessWant: %v", err)
	} else if outcome != service.OutcomeGrabbed {
		t.Fatalf("ProcessWant outcome = %v, want grabbed (release %q)", outcome, guid)
	}

	jobs, err := app.Repo.ListActiveDownloadJobsByWant(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list active download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("active download jobs = %d, want 1", len(jobs))
	}
	return jobs[0]
}

// REQ-CANCEL-001 — Withdrawing a request removes only that user's requester
// association. A tracking with other requesters, and its wants, continues
// unchanged.
//
// Two requesters share one tracking whose want is mid-acquisition (grabbed, with
// a live download job). One withdraws; the other's request, the tracking, the
// want and the download job must read back identical to before.
func TestReqCancel001_WithdrawalRemovesOnlyOwnAssociation(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	_, dana := app.UserToken(t, ctx, "dana", "requester")
	erin, erinToken := app.UserToken(t, ctx, "erin", "requester")

	danaReq := createMovieRequest(t, app, dana)
	erinReq := createMovieRequest(t, app, erinToken)
	app.POST(t, "/api/v1/requests/"+danaReq.ID.String()+"/approve", nil, &danaReq, http.StatusOK)
	app.POST(t, "/api/v1/requests/"+erinReq.ID.String()+"/approve", nil, &erinReq, http.StatusOK)
	if danaReq.SpawnedTrackingID == nil || erinReq.SpawnedTrackingID == nil ||
		*danaReq.SpawnedTrackingID != *erinReq.SpawnedTrackingID {
		t.Fatalf("setup: requests did not share one tracking (dana %v, erin %v)",
			danaReq.SpawnedTrackingID, erinReq.SpawnedTrackingID)
	}
	trackingID := *danaReq.SpawnedTrackingID
	trackingPath := "/api/v1/tracking/" + trackingID.String()

	job := grabTrackedMovie(t, ctx, app, trackingID, "guid-shared")

	var trackingBefore model.Tracking
	app.GET(t, trackingPath, &trackingBefore, http.StatusOK)
	var wantsBefore []model.Want
	app.GET(t, trackingPath+"/wants", &wantsBefore, http.StatusOK)
	var erinReqBefore model.Request
	app.GET(t, "/api/v1/requests/"+erinReq.ID.String(), &erinReqBefore, http.StatusOK)
	jobBefore, err := app.Repo.GetDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}

	var withdrawn model.Request
	app.POSTAs(t, dana, "/api/v1/requests/"+danaReq.ID.String()+"/cancel", nil, &withdrawn, http.StatusOK)
	if withdrawn.Status != string(model.RequestCanceled) {
		t.Fatalf("withdrawn request status = %q, want canceled", withdrawn.Status)
	}

	var requesters []model.TrackingRequester
	app.GET(t, trackingPath+"/requesters", &requesters, http.StatusOK)
	if len(requesters) != 1 || requesters[0].UserID != erin.ID {
		t.Errorf("requesters after withdrawal = %+v, want only erin (%s)", requesters, erin.ID)
	}

	var trackingAfter model.Tracking
	app.GET(t, trackingPath, &trackingAfter, http.StatusOK)
	if !reflect.DeepEqual(trackingAfter, trackingBefore) {
		t.Errorf("tracking changed by another requester's withdrawal:\nbefore %+v\nafter  %+v", trackingBefore, trackingAfter)
	}
	var wantsAfter []model.Want
	app.GET(t, trackingPath+"/wants", &wantsAfter, http.StatusOK)
	if !reflect.DeepEqual(wantsAfter, wantsBefore) {
		t.Errorf("wants changed by another requester's withdrawal:\nbefore %+v\nafter  %+v", wantsBefore, wantsAfter)
	}
	var erinReqAfter model.Request
	app.GET(t, "/api/v1/requests/"+erinReq.ID.String(), &erinReqAfter, http.StatusOK)
	if !reflect.DeepEqual(erinReqAfter, erinReqBefore) {
		t.Errorf("remaining request changed by another requester's withdrawal:\nbefore %+v\nafter  %+v", erinReqBefore, erinReqAfter)
	}
	jobAfter, err := app.Repo.GetDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	if !reflect.DeepEqual(jobAfter, jobBefore) {
		t.Errorf("download job changed by another requester's withdrawal:\nbefore %+v\nafter  %+v", jobBefore, jobAfter)
	}
}

// REQ-CANCEL-005 — Withdrawal never deletes files already acquired.
//
// The movie is acquired end to end (grab → download → import into the library),
// then its only requester withdraws. The imported file must still be on disk
// with its bytes intact, and still recorded against the title.
func TestReqCancel005_WithdrawalKeepsAcquiredFiles(t *testing.T) {
	t.Parallel()
	app, ctx, library := cancelApp(t)

	req := createMovieRequest(t, app, app.Token)
	if req.SpawnedTrackingID == nil {
		t.Fatalf("setup: request did not spawn a tracking")
	}
	job := grabTrackedMovie(t, ctx, app, *req.SpawnedTrackingID, "guid-acquired")

	content := []byte("acquired movie bytes")
	source := filepath.Join(t.TempDir(), "The.Matrix.1999.1080p.BluRay.x264.mkv")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatalf("write download payload: %v", err)
	}

	client := &fakeDownloaderClient{files: []downloader.File{{Path: source, Size: int64(len(content))}}}
	dlWorker := newDownloadWorkerWithClient(t, ctx, app.Repo, client)
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, app.Repo, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	impWorker := importw.New(app.Repo, dm, logger.New(false), nil, nil, nil)

	stopDownload := runWorker(t, ctx, dlWorker)
	stopImport := runWorker(t, ctx, impWorker)
	links, err := app.Repo.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil || len(links) != 1 {
		t.Fatalf("wants linked to job = %d (err %v), want 1", len(links), err)
	}
	waitForWantStatus(t, ctx, app.Repo, links[0].ID, model.WantAvailable, 30*time.Second)
	stopDownload()
	stopImport()

	filesBefore, err := app.Repo.ListFilesForItem(ctx, job.MediaItemID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(filesBefore) != 1 {
		t.Fatalf("setup: files for title = %d, want 1 imported file", len(filesBefore))
	}
	imported := filepath.Join(library.RootPath, filesBefore[0].Path)
	if got, err := os.ReadFile(imported); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("setup: imported file %s unreadable or wrong content (err %v)", imported, err)
	}

	var withdrawn model.Request
	app.POST(t, "/api/v1/requests/"+req.ID.String()+"/cancel", nil, &withdrawn, http.StatusOK)
	if withdrawn.Status != string(model.RequestCanceled) {
		t.Fatalf("withdrawn request status = %q, want canceled", withdrawn.Status)
	}

	got, err := os.ReadFile(imported)
	if err != nil {
		t.Fatalf("acquired file gone after withdrawal: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("acquired file content changed after withdrawal: %q, want %q", got, content)
	}
	filesAfter, err := app.Repo.ListFilesForItem(ctx, job.MediaItemID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(filesAfter) != 1 || filesAfter[0].ID != filesBefore[0].ID || filesAfter[0].Path != filesBefore[0].Path {
		t.Errorf("files for title after withdrawal = %+v, want the imported file %s unchanged", filesAfter, filesBefore[0].ID)
	}
}

// REQ-CANCEL-006 — Cancelling a want never excludes its release from future
// searches.
//
// The only requester withdraws while the want is grabbed on a release, which
// cancels the want and its download job. No exclusion may be recorded, and when
// the title is requested again the same release must still be grabbable.
func TestReqCancel006_CancelDoesNotExcludeRelease(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	const guid = "guid-withdrawn"
	req := createMovieRequest(t, app, app.Token)
	if req.SpawnedTrackingID == nil {
		t.Fatalf("setup: request did not spawn a tracking")
	}
	trackingID := *req.SpawnedTrackingID
	job := grabTrackedMovie(t, ctx, app, trackingID, guid)

	app.POST(t, "/api/v1/requests/"+req.ID.String()+"/cancel", nil, nil, http.StatusOK)

	wants, err := app.Repo.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil || len(wants) != 1 {
		t.Fatalf("wants linked to job = %d (err %v), want 1", len(wants), err)
	}
	want := wants[0]
	if want.Status != string(model.WantCanceled) {
		t.Fatalf("setup: want status after withdrawal = %q, want canceled", want.Status)
	}
	excl, err := app.Repo.ListWantReleaseExclusionsForWants(ctx, []uuid.UUID{want.ID})
	if err != nil {
		t.Fatalf("list exclusions: %v", err)
	}
	if len(excl) != 0 {
		t.Errorf("exclusions after cancel = %+v, want none", excl)
	}

	// Re-requesting re-arms the same want; the withdrawn release must win again.
	again := createMovieRequest(t, app, app.Token)
	if again.SpawnedTrackingID == nil || *again.SpawnedTrackingID != trackingID {
		t.Fatalf("re-request tracking = %v, want %s", again.SpawnedTrackingID, trackingID)
	}
	regrab := grabTrackedMovie(t, ctx, app, trackingID, guid)
	if regrab.Guid != guid {
		t.Errorf("re-grabbed release = %q, want %q", regrab.Guid, guid)
	}
}
