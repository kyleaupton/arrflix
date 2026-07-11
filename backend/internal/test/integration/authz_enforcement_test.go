//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// roleSet is the set of role names that should clear a gate for a given op.
func roleSet(names ...string) map[string]bool {
	s := make(map[string]bool, len(names))
	for _, n := range names {
		s[n] = true
	}
	return s
}

// TestAuthzGate_CoarseRoleMatrix drives the fail-closed middleware across every
// role at one representative operation per operator permission. Roles that hold
// the permission clear the gate (any status other than 403 — the request may
// then 404/422 for lack of a real target, which still proves authorization
// passed); roles that lack it are denied 403 before the handler runs.
func TestAuthzGate_CoarseRoleMatrix(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	_, viewer := app.UserToken(t, ctx, "viewer1", "viewer")
	_, requester := app.UserToken(t, ctx, "requester1", "requester")
	_, coAdmin := app.UserToken(t, ctx, "coadmin1", "co_admin")

	roles := []struct{ name, token string }{
		{"viewer", viewer},
		{"requester", requester},
		{"co_admin", coAdmin},
		{"admin", app.Token},
	}

	const nilUUID = "00000000-0000-0000-0000-000000000000"
	cases := []struct {
		name   string // the permission dimension under test
		method string
		path   string
		body   any
		pass   map[string]bool // roles that clear the gate
	}{
		{"library.read", http.MethodGet, "/api/v1/libraries", nil, roleSet("viewer", "requester", "co_admin", "admin")},
		{"jobs.read", http.MethodGet, "/api/v1/download-jobs", nil, roleSet("co_admin", "admin")},
		{"admin.settings.read", http.MethodGet, "/api/v1/settings", nil, roleSet("co_admin", "admin")},
		{"admin.users.manage", http.MethodGet, "/api/v1/users", nil, roleSet("admin")},
		{"admin.indexers.manage", http.MethodGet, "/api/v1/indexers/configured", nil, roleSet("co_admin", "admin")},
		{"admin.downloaders.manage", http.MethodGet, "/api/v1/downloaders", nil, roleSet("co_admin", "admin")},
		{"library.write", http.MethodPost, "/api/v1/libraries", map[string]any{}, roleSet("co_admin", "admin")},
		{"jobs.manage", http.MethodPost, "/api/v1/tracking/" + nilUUID + "/retry", nil, roleSet("co_admin", "admin")},
		{"admin.settings.write", http.MethodPatch, "/api/v1/settings", map[string]any{}, roleSet("co_admin", "admin")},
	}

	for _, c := range cases {
		for _, r := range roles {
			got := app.Status(t, r.token, c.method, c.path, c.body)
			if c.pass[r.name] {
				if got == http.StatusForbidden {
					t.Errorf("%s: %s should clear the gate, got 403", c.name, r.name)
				}
			} else if got != http.StatusForbidden {
				t.Errorf("%s: %s should be denied 403, got %d", c.name, r.name, got)
			}
		}
	}
}

// TestAuthzGate_CoarseReadsSucceed pins the entitled-role happy path to a real
// 2xx (not merely "not 403") on read ops that need no fixture: an admin sees the
// full response, closing the gap that a "not 403" assertion alone would leave.
func TestAuthzGate_CoarseReadsSucceed(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	_, viewer := app.UserToken(t, ctx, "viewer1", "viewer")

	// viewer holds only library.read: 200 on libraries, 403 on the operator read.
	var libs []model.Library
	app.GETAs(t, viewer, "/api/v1/libraries", &libs, http.StatusOK)
	app.GETAs(t, viewer, "/api/v1/users", nil, http.StatusForbidden)

	// admin holds everything.
	var users []model.User
	app.GET(t, "/api/v1/users", &users, http.StatusOK)
	if len(users) == 0 {
		t.Fatal("admin users-list returned empty; expected at least the admin user")
	}
}

// TestAuthzFine_RequestsOwnAny proves the service-level own/any refinement for
// requests, past the coarse gate: a requester reads and lists only their own,
// an admin sees all, and the exact per-tier create gate blocks a tier the
// requester's grant doesn't cover.
func TestAuthzFine_RequestsOwnAny(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	_, reqA := app.UserToken(t, ctx, "req_a", "requester")
	_, reqB := app.UserToken(t, ctx, "req_b", "requester")

	// A requester is not auto-approved, so a create is persisted 'pending' with no
	// spawn — no TMDB or quality-profile fixture needed. requester holds
	// requests.create:{movie,series}:hd.
	var aReq model.Request
	app.POSTAs(t, reqA, "/api/v1/requests", map[string]any{
		"tmdbId": 603, "type": "movie", "tier": "HD",
	}, &aReq, http.StatusCreated)
	if aReq.Status != string(model.RequestPending) {
		t.Fatalf("requester create status = %q, want pending", aReq.Status)
	}

	var bReq model.Request
	app.POSTAs(t, reqB, "/api/v1/requests", map[string]any{
		"tmdbId": 1399, "type": "series", "tier": "HD",
	}, &bReq, http.StatusCreated)

	// Exact per-tier gate (service refinement): requester holds create:movie:hd
	// but not create:movie:4k, so a 4K create is 403 even though the coarse gate
	// (hold ANY create key) passed.
	app.POSTAs(t, reqA, "/api/v1/requests", map[string]any{
		"tmdbId": 603, "type": "movie", "tier": "4K",
	}, nil, http.StatusForbidden)

	// Get own/any: A reads own (200); B (a requester with only view.own) reading
	// A's request is refused (403); admin (view.any) reads it (200).
	app.GETAs(t, reqA, "/api/v1/requests/"+aReq.ID.String(), nil, http.StatusOK)
	app.GETAs(t, reqB, "/api/v1/requests/"+aReq.ID.String(), nil, http.StatusForbidden)
	app.GET(t, "/api/v1/requests/"+aReq.ID.String(), nil, http.StatusOK)

	// List own/any: each requester sees only their own; admin sees all.
	var aList, bList, adminList []model.Request
	app.GETAs(t, reqA, "/api/v1/requests", &aList, http.StatusOK)
	app.GETAs(t, reqB, "/api/v1/requests", &bList, http.StatusOK)
	app.GET(t, "/api/v1/requests", &adminList, http.StatusOK)

	if len(aList) != 1 || aList[0].ID != aReq.ID {
		t.Errorf("requester A list = %d requests, want just A's own", len(aList))
	}
	if len(bList) != 1 || bList[0].ID != bReq.ID {
		t.Errorf("requester B list = %d requests, want just B's own", len(bList))
	}
	if len(adminList) != 2 {
		t.Errorf("admin list = %d requests, want all 2", len(adminList))
	}
}

// TestAuthzFine_TrackingCancelOwnAny proves the own/any gate on stop-tracking: a
// requester holds no tracking.cancel grant at all, so cancel is refused; an
// admin (tracking.cancel.any) cancels the same tracking.
func TestAuthzFine_TrackingCancelOwnAny(t *testing.T) {
	t.Parallel()
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
	// Admin auto-approves (via its role grants) so its request spawns a real
	// tracking to cancel.
	_, requester := app.UserToken(t, ctx, "requester1", "requester")

	var created model.Request
	app.POST(t, "/api/v1/requests", map[string]any{
		"tmdbId": spawnTmdbID, "type": "movie", "tier": "HD",
	}, &created, http.StatusCreated)
	if created.SpawnedTrackingID == nil {
		t.Fatalf("admin request did not spawn a tracking")
	}
	trackingID := created.SpawnedTrackingID.String()

	// requester holds neither tracking.cancel.own nor .any → gate 403.
	app.POSTAs(t, requester, "/api/v1/tracking/"+trackingID+"/cancel", nil, nil, http.StatusForbidden)

	// admin holds tracking.cancel.any → 200.
	var canceled model.Tracking
	app.POST(t, "/api/v1/tracking/"+trackingID+"/cancel", nil, &canceled, http.StatusOK)
	if canceled.State != string(model.TrackingCanceled) {
		t.Errorf("canceled tracking state = %q, want canceled", canceled.State)
	}
}
