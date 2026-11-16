//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/authz"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/indexer"
	acquisitionworker "github.com/kyleaupton/arrflix/internal/jobs/acquisition"
	downloadworker "github.com/kyleaupton/arrflix/internal/jobs/download"
	importw "github.com/kyleaupton/arrflix/internal/jobs/import"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/titlenotify"
)

// grantUser gives the user direct allow grants for the given permission keys.
// The API has no grant-management surface (roles are the only assignment path),
// so the story's "friend" — a requester who additionally auto-approves HD — is
// built by adding the user-subject grants directly and busting the authz cache.
func grantUser(t *testing.T, ctx context.Context, app *testapp.App, userID uuid.UUID, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := app.Pool.Exec(ctx,
			"INSERT INTO permission_grant (subject_type, subject_id, permission_key, effect) VALUES ('user', $1, $2, 'allow')",
			userID.String(), key,
		); err != nil {
			t.Fatalf("grant %s: %v", key, err)
		}
	}
	app.Services.Authz.Bust(userID)
}

// friendToken mints the story's Friend: a requester (HD create, no 4K) who also
// holds requests.auto_approve:movie:hd.
func friendToken(t *testing.T, ctx context.Context, app *testapp.App) (model.User, string) {
	t.Helper()
	friend, token := app.UserToken(t, ctx, "friend", "requester")
	grantUser(t, ctx, app, friend.ID, "requests.auto_approve:movie:hd")
	return friend, token
}

// REQ-FULFIL-001 — A request from a user who holds the matching create and
// auto-approve grants completes without any human action.
//
// Friend requests the movie at HD over the API. From then on only the
// background workers run — acquisition (fake indexer), download (fake
// downloader) and import — and no operator endpoint is called. The request must
// be auto-decided and spawned at creation, never queued, and the single want it
// spawns must reach 'available' with the file in the library.
func TestReqFulfil001_AutoApprovedRequestCompletesWithoutHumanAction(t *testing.T) {
	t.Parallel()
	app, ctx, library := cancelApp(t)
	friend, token := friendToken(t, ctx, app)

	req := createMovieRequest(t, app, token)
	if req.Status != string(model.RequestSpawned) {
		t.Fatalf("request status = %q, want %q (auto-approved at creation)", req.Status, model.RequestSpawned)
	}
	if req.DecisionAuto == nil || !*req.DecisionAuto {
		t.Errorf("request decisionAuto = %v, want true", req.DecisionAuto)
	}
	if req.DecidedBy != nil && *req.DecidedBy != friend.ID {
		t.Errorf("request decidedBy = %s, want no operator decision", *req.DecidedBy)
	}
	if req.SpawnedTrackingID == nil {
		t.Fatalf("auto-approved request spawned no tracking")
	}

	var pending []model.Request
	app.GET(t, "/api/v1/requests?status=pending", &pending, http.StatusOK)
	if len(pending) != 0 {
		t.Errorf("approval queue = %+v, want empty", pending)
	}

	var wants []model.Want
	app.GET(t, "/api/v1/tracking/"+req.SpawnedTrackingID.String()+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 {
		t.Fatalf("wants on spawned tracking = %d, want 1", len(wants))
	}
	want := wants[0]

	content := []byte("friend's movie bytes")
	payload := filepath.Join(t.TempDir(), "The.Matrix.1999.1080p.BluRay.x264.mkv")
	if err := os.WriteFile(payload, content, 0o644); err != nil {
		t.Fatalf("write download payload: %v", err)
	}
	client := &fakeDownloaderClient{files: []downloader.File{{Path: payload, Size: int64(len(content))}}}

	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{movieResult("guid-friend")}, nil
		},
	}
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, app.Repo, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}

	defer runWorker(t, ctx, newAcquisitionWorker(app.Repo, source))()
	defer runWorker(t, ctx, newDownloadWorkerWithClient(t, ctx, app.Repo, client))()
	defer runWorker(t, ctx, importw.New(app.Repo, dm, logger.New(false), nil, nil, nil))()

	waitForWantStatus(t, ctx, app.Repo, want.ID, model.WantAvailable, 30*time.Second)

	files, err := app.Repo.ListFilesForItem(ctx, want.MediaItemID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("library files for title = %d, want 1", len(files))
	}
	if got, err := os.ReadFile(filepath.Join(library.RootPath, files[0].Path)); err != nil || !bytes.Equal(got, content) {
		t.Errorf("library file %s unreadable or wrong content (err %v)", files[0].Path, err)
	}

	var after model.Request
	app.GETAs(t, token, "/api/v1/requests/"+req.ID.String(), &after, http.StatusOK)
	if after.Status != string(model.RequestSpawned) {
		t.Errorf("request status after fulfilment = %q, want %q", after.Status, model.RequestSpawned)
	}
}

// REQ-FULFIL-005 — A viewer is only offered tiers they hold a grant for.
//
// For several grant sets, the request action on the title-status read must
// offer exactly the tiers the viewer holds requests.create for — no more, no
// fewer — each labeled with whether it needs approval per the viewer's
// requests.auto_approve grant. A viewer with no create grant is offered no
// request action at all.
func TestReqFulfil005_OffersOnlyGrantedTiers(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	_, friend := friendToken(t, ctx, app)
	_, requester := app.UserToken(t, ctx, "requester", "requester")
	_, viewer := app.UserToken(t, ctx, "viewer", "viewer")
	fourK, fourKToken := app.UserToken(t, ctx, "fourk", "viewer")
	grantUser(t, ctx, app, fourK.ID, "requests.create:movie:4k")

	cases := []struct {
		name  string
		token string
		want  []model.TitleActionTier // nil: no request action offered
	}{
		{"friend (HD create + auto-approve)", friend, []model.TitleActionTier{{Tier: "HD", RequiresApproval: false}}},
		{"requester (HD create only)", requester, []model.TitleActionTier{{Tier: "HD", RequiresApproval: true}}},
		{"viewer (no create)", viewer, nil},
		{"4K create only", fourKToken, []model.TitleActionTier{{Tier: "4K", RequiresApproval: true}}},
		{"admin (every grant)", app.Token, []model.TitleActionTier{
			{Tier: "4K", RequiresApproval: false},
			{Tier: "HD", RequiresApproval: false},
		}},
	}
	for _, tc := range cases {
		var status model.TitleStatus
		app.GETAs(t, tc.token, "/api/v1/titles/movie/603/status", &status, http.StatusOK)

		var offered []model.TitleActionTier
		requestActions := 0
		for _, a := range status.Actions {
			if a.Kind == "request" {
				requestActions++
				offered = a.Tiers
			}
		}
		if tc.want == nil {
			if requestActions != 0 {
				t.Errorf("%s: offered request tiers %+v, want no request action", tc.name, offered)
			}
			continue
		}
		if requestActions != 1 {
			t.Errorf("%s: request actions = %d, want 1", tc.name, requestActions)
			continue
		}
		if !sameTiers(offered, tc.want) {
			t.Errorf("%s: offered tiers = %+v, want exactly %+v", tc.name, offered, tc.want)
		}
	}
}

// sameTiers compares tier offers as sets; the offer order is presentation.
func sameTiers(got, want []model.TitleActionTier) bool {
	index := func(ts []model.TitleActionTier) map[string]model.TitleActionTier {
		m := make(map[string]model.TitleActionTier, len(ts))
		for _, t := range ts {
			m[t.Tier] = t
		}
		return m
	}
	return len(got) == len(want) && reflect.DeepEqual(index(got), index(want))
}

// fulfilmentSource is an indexer that offers one acceptable HD release of the
// movie, so the acquisition worker grabs it on its first search.
func fulfilmentSource(guid string) stubIndexerSource {
	return stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{movieResult(guid)}, nil
		},
	}
}

// fulfilmentDownloader is a fake downloader that has already finished the
// movie's payload, and a manager resolving the default downloader to it.
func fulfilmentDownloader(t *testing.T, ctx context.Context, app *testapp.App) (*fakeDownloaderClient, *downloader.Manager) {
	t.Helper()
	content := []byte("the movie's bytes")
	payload := filepath.Join(t.TempDir(), "The.Matrix.1999.1080p.BluRay.x264.mkv")
	if err := os.WriteFile(payload, content, 0o644); err != nil {
		t.Fatalf("write download payload: %v", err)
	}
	client := &fakeDownloaderClient{files: []downloader.File{{Path: payload, Size: int64(len(content))}}}
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, app.Repo, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	return client, dm
}

// REQ-FULFIL-003 — The requester must be told when the film is ready, through a
// channel that works when the app is closed.
//
// Two users request the same movie, so both are requesters of one tracking.
// Neither has touched their notification preferences. The real workers take the
// want to 'available'; the import worker is wired with the production
// notification service. Each requester must then have a want.available
// notification queued on the push channel — in-app alone reaches nobody whose
// app is closed.
func TestReqFulfil003_AvailabilityPushesEveryRequesterByDefault(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)
	friend, friendTok := friendToken(t, ctx, app)
	admin, err := app.Repo.GetUserByLogin(ctx, "admin")
	if err != nil {
		t.Fatalf("lookup admin: %v", err)
	}

	req := createMovieRequest(t, app, friendTok)
	if req.SpawnedTrackingID == nil {
		t.Fatalf("auto-approved request spawned no tracking")
	}
	joined := createMovieRequest(t, app, app.Token)
	if joined.SpawnedTrackingID == nil || *joined.SpawnedTrackingID != *req.SpawnedTrackingID {
		t.Fatalf("second request tracking = %v, want it to join %s", joined.SpawnedTrackingID, *req.SpawnedTrackingID)
	}

	var requesters []model.TrackingRequester
	app.GET(t, "/api/v1/tracking/"+req.SpawnedTrackingID.String()+"/requesters", &requesters, http.StatusOK)
	if len(requesters) != 2 {
		t.Fatalf("requesters on tracking = %d, want 2", len(requesters))
	}

	var wants []model.Want
	app.GET(t, "/api/v1/tracking/"+req.SpawnedTrackingID.String()+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 {
		t.Fatalf("wants on tracking = %d, want 1", len(wants))
	}

	client, dm := fulfilmentDownloader(t, ctx, app)
	defer runWorker(t, ctx, newAcquisitionWorker(app.Repo, fulfilmentSource("guid-notify")))()
	defer runWorker(t, ctx, newDownloadWorkerWithClient(t, ctx, app.Repo, client))()
	defer runWorker(t, ctx, importw.New(app.Repo, dm, logger.New(false), nil, app.Services.Notifications, nil))()

	waitForWantStatus(t, ctx, app.Repo, wants[0].ID, model.WantAvailable, 30*time.Second)

	// The notification is enqueued just after the want flips, so wait for it
	// rather than reading the outbox at the instant of the transition.
	pushed := map[uuid.UUID]int{}
	deadline := time.Now().Add(5 * time.Second)
	for len(pushed) < 2 && time.Now().Before(deadline) {
		due, err := app.Repo.ListDueOutbox(ctx, 100)
		if err != nil {
			t.Fatalf("list outbox: %v", err)
		}
		pushed = map[uuid.UUID]int{}
		for _, row := range due {
			if row.EventType != "want.available" || row.Channel != string(model.ChannelPush) || row.RecipientUserID == nil {
				continue
			}
			pushed[*row.RecipientUserID]++
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, u := range []model.User{friend, admin} {
		if pushed[u.ID] != 1 {
			t.Errorf("want.available push notifications for %s = %d, want 1", u.Username, pushed[u.ID])
		}
	}
	if len(pushed) != 2 {
		t.Errorf("want.available pushed to %d users, want exactly the 2 requesters", len(pushed))
	}
}

// kickWatcher is a realtime session holding library.read — what a requester's
// browser holds — that records the title_status kicks it receives.
type kickWatcher struct {
	att sse.Attachment
}

func newKickWatcher(t *testing.T, broker *sse.Broker, userID uuid.UUID) *kickWatcher {
	t.Helper()
	att := broker.Attach(sse.AttachParams{UserID: userID, Capabilities: []string{authz.LibraryRead}})
	t.Cleanup(att.Cancel)
	return &kickWatcher{att: att}
}

// next returns the next title_status kick for the given title, or false if none
// arrives within timeout. Other events and other titles' kicks are skipped.
func (w *kickWatcher) next(mediaType string, tmdbID int64, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-w.att.Out:
			if ev.Type != realtime.NameTitleStatus {
				continue
			}
			var p realtime.TitleStatusPayload
			if err := json.Unmarshal(ev.Data, &p); err != nil {
				continue
			}
			if p.MediaType == mediaType && p.TmdbID == tmdbID {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// drain discards everything delivered until the session has been quiet for the
// given window.
func (w *kickWatcher) drain(quiet time.Duration) {
	for {
		select {
		case <-w.att.Out:
		case <-time.After(quiet):
			return
		}
	}
}

// REQ-FULFIL-004 — The status a requester sees must reflect where the work
// actually is, without a reload. This is the server half: a state change on a
// title emits a title-status kick for it.
//
// The notifier is wired as the API binary wires it, and the workers announce to
// it. The movie is first taken as far as a finished download with its import
// task queued, and the realtime session is drained until quiet, so any
// kick after that belongs to the next change. The import worker then makes the
// movie available, and a kick for that title must arrive within the flush
// window, well before the safety sweep could cover for a missing announcement.
// The requester's refetch on that kick must read 'available'.
func TestReqFulfil004_TitleStateChangeEmitsTitleStatusKick(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)
	friend, friendTok := friendToken(t, ctx, app)
	log := logger.New(false)

	broker := sse.NewBroker(ctx)
	titles := titlenotify.New(broker, func(ctx context.Context, mediaItemID uuid.UUID) (titlenotify.Ref, error) {
		item, err := app.Repo.GetMediaItem(ctx, mediaItemID)
		if err != nil {
			return titlenotify.Ref{}, err
		}
		if item.TmdbID == nil {
			return titlenotify.Ref{}, fmt.Errorf("media item %s has no tmdb id", mediaItemID)
		}
		return titlenotify.Ref{MediaType: item.Type, TmdbID: *item.TmdbID}, nil
	}, app.Repo.ListMediaItemsTouchedSince, log)
	defer runWorker(t, ctx, titles)()

	watcher := newKickWatcher(t, broker, friend.ID)

	req := createMovieRequest(t, app, friendTok)
	if req.SpawnedTrackingID == nil {
		t.Fatalf("auto-approved request spawned no tracking")
	}
	var wants []model.Want
	app.GET(t, "/api/v1/tracking/"+req.SpawnedTrackingID.String()+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 {
		t.Fatalf("wants on tracking = %d, want 1", len(wants))
	}

	_, dm := fulfilmentDownloader(t, ctx, app)
	quality := service.NewQualityProfileService(app.Repo)
	acqSvc := service.NewAcquisitionService(app.Repo, log, fulfilmentSource("guid-kick"), service.NewRoutingService(app.Repo), quality,
		service.NewProposalService(app.Repo, quality, broker, titles, log))
	defer runWorker(t, ctx, acquisitionworker.NewWithConfig(app.Repo, acqSvc, service.NewSchedulerService(app.Repo, log), log, broker, titles, retryTestConfig()))()
	defer runWorker(t, ctx, downloadworker.NewWithConfig(app.Repo, dm, log, broker, titles, downloadworker.Config{
		PollInterval: 10 * time.Millisecond,
		ClaimLimit:   20,
		MaxAttempts:  3,
	}))()

	deadline := time.Now().Add(30 * time.Second)
	for {
		tasks, err := app.Repo.ListImportTasksByMediaItem(ctx, wants[0].MediaItemID)
		if err != nil {
			t.Fatalf("list import tasks: %v", err)
		}
		if len(tasks) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no import task for the movie within deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}

	watcher.drain(time.Second)

	defer runWorker(t, ctx, importw.New(app.Repo, dm, log, broker, nil, titles))()

	// The flush interval is 250ms and the sweep 30s; five seconds is ample for
	// an announced change and far short of a swept one. Each kick is answered the
	// way the client answers it, with a refetch, until one shows the new state.
	statusPath := "/api/v1/titles/movie/603/status"
	deadline = time.Now().Add(5 * time.Second)
	for {
		if !watcher.next("movie", spawnTmdbID, time.Until(deadline)) {
			t.Fatalf("no title_status kick for movie/%d led to a refetch reading available", spawnTmdbID)
		}
		var status model.TitleStatus
		app.GETAs(t, friendTok, statusPath, &status, http.StatusOK)
		if status.State == "available" {
			return
		}
	}
}
