//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kyleaupton/arrflix/internal/authz"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// REQ-APPROVE-005 — A reviewer must only see requests they are permitted to act
// on. Under the seeded roles, every role that can read the review queue (GET
// /requests?status=pending with requests.view.any) holds approve and deny for
// every media type, so each queued request is one its viewer can decide. A user
// without review authority sees no one else's request: a requester's read is
// scoped to their own requests, and a viewer cannot read requests at all.
func TestReqApprove005_QueueShowsOnlyDecidableRequests(t *testing.T) {
	t.Parallel()
	app, ctx := lifecycleApp(t)

	_, reqA := app.UserToken(t, ctx, "req_a", "requester")
	_, reqB := app.UserToken(t, ctx, "req_b", "requester")
	aReq := createMovieRequest(t, app, reqA)
	bReq := createMovieRequest(t, app, reqB)

	_, coAdmin := app.UserToken(t, ctx, "coadmin1", "co_admin")
	_, outsider := app.UserToken(t, ctx, "req_c", "requester")
	_, viewer := app.UserToken(t, ctx, "viewer1", "viewer")

	reviewers := map[string]string{"admin": app.Token, "co_admin": coAdmin}
	for role, token := range reviewers {
		var me struct {
			Permissions []string `json:"permissions"`
		}
		app.GETAs(t, token, "/api/v1/auth/me", &me, http.StatusOK)
		if !slices.Contains(me.Permissions, authz.RequestViewAny) {
			t.Fatalf("%s lacks %s; it is expected to review", role, authz.RequestViewAny)
		}
		// Queue authority implies decision authority for every media type, so no
		// queued request can be one this reviewer is unable to act on.
		for _, mt := range []model.MediaType{model.MediaTypeMovie, model.MediaTypeSeries} {
			for _, key := range []string{authz.RequestApprove(mt), authz.RequestDeny(mt)} {
				if !slices.Contains(me.Permissions, key) {
					t.Errorf("%s reads the review queue but lacks %s", role, key)
				}
			}
		}

		var queue []model.Request
		app.GETAs(t, token, "/api/v1/requests?status=pending", &queue, http.StatusOK)
		ids := make([]uuid.UUID, 0, len(queue))
		for _, r := range queue {
			ids = append(ids, r.ID)
			if !slices.Contains(me.Permissions, authz.RequestApprove(model.MediaType(r.Type))) ||
				!slices.Contains(me.Permissions, authz.RequestDeny(model.MediaType(r.Type))) {
				t.Errorf("%s queue shows %s request %s it cannot decide", role, r.Type, r.ID)
			}
		}
		if len(ids) != 2 || !slices.Contains(ids, aReq.ID) || !slices.Contains(ids, bReq.ID) {
			t.Errorf("%s queue = %v, want both pending requests %s and %s", role, ids, aReq.ID, bReq.ID)
		}
	}

	// A requester holds no review authority: the pending read shows none of the
	// other requesters' requests.
	var outsiderQueue []model.Request
	app.GETAs(t, outsider, "/api/v1/requests?status=pending", &outsiderQueue, http.StatusOK)
	if len(outsiderQueue) != 0 {
		t.Errorf("requester without review authority sees %d pending requests, want 0", len(outsiderQueue))
	}
	app.GETAs(t, viewer, "/api/v1/requests?status=pending", nil, http.StatusForbidden)
}

// REQ-APPROVE-006 — Two reviewers acting on the same request must not both
// succeed. Two decisions are raced on one pending request, approve against deny
// and approve against approve; exactly one succeeds, the other is refused as a
// conflict, and only the winner's outcome takes effect.
//
// The race is made deterministic by holding the request row locked while both
// decisions are sent: each passes its pending check and then blocks on its
// decision write. Once both are waiting the lock is released, so the two writes
// contend for a row that both have already seen as pending.
func TestReqApprove006_ConcurrentDecisionsOnlyOneSucceeds(t *testing.T) {
	t.Parallel()

	t.Run("approve vs deny", func(t *testing.T) {
		t.Parallel()
		app, pool, ctx := reviewRaceApp(t)
		_, coAdmin := app.UserToken(t, ctx, "coadmin1", "co_admin")
		_, requester := app.UserToken(t, ctx, "requester1", "requester")
		pending := createMovieRequest(t, app, requester)
		base := "/api/v1/requests/" + pending.ID.String()

		results := raceDecisions(t, app, pool, pending.ID, [2]decision{
			{token: app.Token, path: base + "/approve"},
			{token: coAdmin, path: base + "/deny", body: map[string]any{"reason": "no space"}},
		})
		winner := oneWinner(t, results)

		var final model.Request
		app.GET(t, base, &final, http.StatusOK)
		var trackings []model.Tracking
		app.GET(t, "/api/v1/tracking", &trackings, http.StatusOK)

		if winner == 0 {
			if final.Status != string(model.RequestSpawned) || final.DeniedReason != nil {
				t.Errorf("approve won, but request is %q with denied reason %v", final.Status, final.DeniedReason)
			}
			if len(trackings) != 1 {
				t.Errorf("approve won, tracking count = %d, want 1", len(trackings))
			}
		} else {
			if final.Status != string(model.RequestDenied) || final.SpawnedTrackingID != nil {
				t.Errorf("deny won, but request is %q with tracking %v", final.Status, final.SpawnedTrackingID)
			}
			if len(trackings) != 0 {
				t.Errorf("deny won, tracking count = %d, want 0 (the losing approve spawned)", len(trackings))
			}
		}
		if final.DecidedBy == nil || results[winner].req.DecidedBy == nil || *final.DecidedBy != *results[winner].req.DecidedBy {
			t.Errorf("decidedBy = %v, want the winning reviewer", final.DecidedBy)
		}
	})

	t.Run("approve vs approve", func(t *testing.T) {
		t.Parallel()
		app, pool, ctx := reviewRaceApp(t)
		_, coAdmin := app.UserToken(t, ctx, "coadmin1", "co_admin")
		_, requester := app.UserToken(t, ctx, "requester1", "requester")
		pending := createMovieRequest(t, app, requester)
		base := "/api/v1/requests/" + pending.ID.String()

		results := raceDecisions(t, app, pool, pending.ID, [2]decision{
			{token: app.Token, path: base + "/approve"},
			{token: coAdmin, path: base + "/approve"},
		})
		winner := oneWinner(t, results)

		var final model.Request
		app.GET(t, base, &final, http.StatusOK)
		if final.Status != string(model.RequestSpawned) || final.SpawnedTrackingID == nil {
			t.Fatalf("request is %q with tracking %v, want spawned", final.Status, final.SpawnedTrackingID)
		}
		if final.DecidedBy == nil || results[winner].req.DecidedBy == nil || *final.DecidedBy != *results[winner].req.DecidedBy {
			t.Errorf("decidedBy = %v, want the winning reviewer", final.DecidedBy)
		}
		var trackings []model.Tracking
		app.GET(t, "/api/v1/tracking", &trackings, http.StatusOK)
		if len(trackings) != 1 {
			t.Errorf("tracking count = %d, want 1", len(trackings))
		}
		var wants []model.Want
		app.GET(t, "/api/v1/tracking/"+final.SpawnedTrackingID.String()+"/wants", &wants, http.StatusOK)
		if len(wants) != 1 {
			t.Errorf("want count = %d, want 1", len(wants))
		}
	})
}

// reviewRaceApp is lifecycleApp that also hands back the pool, which the race
// test needs to hold the request row lock.
func reviewRaceApp(t *testing.T) (*testapp.App, *pgxpool.Pool, context.Context) {
	t.Helper()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(spawnTmdbID, tmdb.MovieDetails{
		ID:          spawnTmdbID,
		Title:       "The Matrix",
		ReleaseDate: "1999-03-31",
	})
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()
	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	return app, pool, ctx
}

type decision struct {
	token string
	path  string
	body  any
}

type decisionResult struct {
	status int
	req    model.Request // decoded only from a 200
	err    error
}

// raceDecisions sends both decisions while the request row is locked, waits
// until both are blocked on that lock, then releases it and collects results.
// Requests are issued directly rather than through app.DoAs because they run
// off the test goroutine, where t.Fatal is not allowed.
func raceDecisions(t *testing.T, app *testapp.App, pool *pgxpool.Pool, requestID uuid.UUID, ds [2]decision) [2]decisionResult {
	t.Helper()
	ctx := context.Background()

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	defer func() { _ = lockTx.Rollback(ctx) }()
	if _, err := lockTx.Exec(ctx, `SELECT 1 FROM request WHERE id = $1 FOR UPDATE`, requestID); err != nil {
		t.Fatalf("lock request row: %v", err)
	}

	var results [2]decisionResult
	var wg sync.WaitGroup
	for i, d := range ds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = sendDecision(app, d)
		}()
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`,
		).Scan(&waiting); err != nil {
			t.Fatalf("count lock waiters: %v", err)
		}
		if waiting >= 2 {
			break
		}
		if time.Now().After(deadline) {
			_ = lockTx.Rollback(ctx)
			wg.Wait()
			t.Fatalf("only %d decisions reached the decision write while the row was locked; results %+v", waiting, results)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("release request row lock: %v", err)
	}
	wg.Wait()
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("decision %d (%s): %v", i, ds[i].path, r.err)
		}
	}
	return results
}

func sendDecision(app *testapp.App, d decision) decisionResult {
	var buf bytes.Buffer
	if d.body != nil {
		if err := json.NewEncoder(&buf).Encode(d.body); err != nil {
			return decisionResult{err: err}
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, app.URL+d.path, &buf)
	if err != nil {
		return decisionResult{err: err}
	}
	if d.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return decisionResult{err: err}
	}
	defer resp.Body.Close()
	res := decisionResult{status: resp.StatusCode}
	if resp.StatusCode == http.StatusOK {
		res.err = json.NewDecoder(resp.Body).Decode(&res.req)
	}
	return res
}

// oneWinner asserts exactly one decision succeeded and the other was refused as
// a conflict, returning the winner's index.
func oneWinner(t *testing.T, results [2]decisionResult) int {
	t.Helper()
	a, b := results[0].status, results[1].status
	switch {
	case a == http.StatusOK && b == http.StatusConflict:
		return 0
	case a == http.StatusConflict && b == http.StatusOK:
		return 1
	default:
		t.Fatalf("decision statuses = %d and %d, want one 200 and one 409", a, b)
		return -1
	}
}
