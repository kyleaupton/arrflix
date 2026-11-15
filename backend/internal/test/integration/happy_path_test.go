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
	"github.com/kyleaupton/arrflix/internal/test/testapp"
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
