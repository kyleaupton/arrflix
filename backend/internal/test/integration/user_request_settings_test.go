//go:build integration

package integration

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// setRequestSettings saves a user's request settings as the admin and returns
// the user the API reads back.
func setRequestSettings(t *testing.T, app *testapp.App, userID uuid.UUID, autoApprove, allow4k bool) model.User {
	t.Helper()
	var user model.User
	app.PUT(t, "/api/v1/users/"+userID.String()+"/request-settings", map[string]any{
		"autoApprove": autoApprove, "allow4k": allow4k,
	}, &user, http.StatusOK)
	if user.AutoApprove != autoApprove || user.Allow4k != allow4k {
		t.Fatalf("saved settings read back as (autoApprove=%v, allow4k=%v), want (%v, %v)",
			user.AutoApprove, user.Allow4k, autoApprove, allow4k)
	}
	return user
}

// requestMovieAt POSTs a movie request at tier as token, expecting status.
func requestMovieAt(t *testing.T, app *testapp.App, token, tier string, status int) model.Request {
	t.Helper()
	var req model.Request
	var out any = &req
	if status >= 400 {
		out = nil
	}
	app.POSTAs(t, token, "/api/v1/requests", map[string]any{
		"tmdbId": spawnTmdbID, "type": "movie", "tier": tier,
	}, out, status)
	return req
}

// offeredTiers is the request action's tier choice on the movie for token.
func offeredTiers(t *testing.T, app *testapp.App, token string) []model.TitleActionTier {
	t.Helper()
	st := titleStatusAs(t, app, token, "movie", spawnTmdbID)
	for _, a := range st.Actions {
		if a.Kind == "request" {
			return a.Tiers
		}
	}
	t.Fatalf("title status offers no request action: %+v", st.Actions)
	return nil
}

func hasKey(t *testing.T, ctx context.Context, app *testapp.App, userID uuid.UUID, key string) bool {
	t.Helper()
	keys, err := app.Services.Authz.EffectiveKeys(ctx, userID)
	if err != nil {
		t.Fatalf("effective keys: %v", err)
	}
	return slices.Contains(keys, key)
}

// TestReqRole004_AutoApproveSettingSkipsTheQueue — REQ-ROLE-004 — An admin sets,
// per user, whether their requests are auto-approved. One toggle covers every
// tier the user may request. When it's off, requests enter the approval queue.
func TestReqRole004_AutoApproveSettingSkipsTheQueue(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)

	t.Run("off: requests are queued", func(t *testing.T) {
		user, token := app.UserToken(t, ctx, "queued", "requester")
		setRequestSettings(t, app, user.ID, false, true)
		if got := requestMovieAt(t, app, token, "HD", http.StatusCreated); got.Status != string(model.RequestPending) {
			t.Errorf("HD request status = %q, want pending", got.Status)
		}
	})

	t.Run("on: HD skips the queue", func(t *testing.T) {
		user, token := app.UserToken(t, ctx, "trustedhd", "requester")
		setRequestSettings(t, app, user.ID, true, false)
		got := requestMovieAt(t, app, token, "HD", http.StatusCreated)
		if got.Status != string(model.RequestSpawned) || got.SpawnedTrackingID == nil {
			t.Errorf("HD request status = %q (tracking %v), want spawned", got.Status, got.SpawnedTrackingID)
		}
	})

	t.Run("on with 4K allowed: 4K skips the queue", func(t *testing.T) {
		user, token := app.UserToken(t, ctx, "trusted4k", "requester")
		setRequestSettings(t, app, user.ID, true, true)
		for _, tier := range offeredTiers(t, app, token) {
			if tier.RequiresApproval {
				t.Errorf("tier %s offered as requiring approval, want auto-approved", tier.Tier)
			}
		}
		got := requestMovieAt(t, app, token, "4K", http.StatusCreated)
		if got.Status != string(model.RequestSpawned) {
			t.Errorf("4K request status = %q, want spawned", got.Status)
		}
	})

	t.Run("turning 4K off removes 4K auto-approve; turning it on restores it", func(t *testing.T) {
		user, token := app.UserToken(t, ctx, "toggler", "requester")
		setRequestSettings(t, app, user.ID, true, true)
		setRequestSettings(t, app, user.ID, true, false)
		for _, typ := range []string{"movie", "series"} {
			if hasKey(t, ctx, app, user.ID, "requests.auto_approve:"+typ+":4k") {
				t.Errorf("4K off: user still holds requests.auto_approve:%s:4k", typ)
			}
			if !hasKey(t, ctx, app, user.ID, "requests.auto_approve:"+typ+":hd") {
				t.Errorf("4K off: user lost requests.auto_approve:%s:hd", typ)
			}
		}

		setRequestSettings(t, app, user.ID, true, true)
		tiers := offeredTiers(t, app, token)
		if len(tiers) != 2 {
			t.Fatalf("offered tiers = %+v, want HD and 4K", tiers)
		}
		for _, tier := range tiers {
			if tier.RequiresApproval {
				t.Errorf("4K back on: tier %s requires approval, want auto-approve to extend to it", tier.Tier)
			}
		}
	})

	t.Run("turning it on leaves queued requests queued", func(t *testing.T) {
		user, token := app.UserToken(t, ctx, "latecomer", "requester")
		queued := requestMovieAt(t, app, token, "HD", http.StatusCreated)
		setRequestSettings(t, app, user.ID, true, false)
		var after model.Request
		app.GET(t, "/api/v1/requests/"+queued.ID.String(), &after, http.StatusOK)
		if after.Status != string(model.RequestPending) {
			t.Errorf("queued request status after enabling auto-approve = %q, want pending", after.Status)
		}
	})
}

// TestReqRole005_Allow4kGovernsOfferedTiers — REQ-ROLE-005 — An admin sets, per
// user, whether they may request 4K. It governs which tiers are offered, and the
// API refuses a 4K request from a user without it.
func TestReqRole005_Allow4kGovernsOfferedTiers(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)
	user, token := app.UserToken(t, ctx, "member", "requester")

	tierNames := func() []string {
		var out []string
		for _, tier := range offeredTiers(t, app, token) {
			out = append(out, tier.Tier)
		}
		return out
	}

	if got := tierNames(); !slices.Equal(got, []string{"HD"}) {
		t.Errorf("new user: offered tiers = %v, want [HD]", got)
	}
	requestMovieAt(t, app, token, "4K", http.StatusForbidden)

	setRequestSettings(t, app, user.ID, false, true)
	if got := tierNames(); !slices.Equal(got, []string{"HD", "4K"}) {
		t.Errorf("4K allowed: offered tiers = %v, want [HD 4K]", got)
	}
	got := requestMovieAt(t, app, token, "4K", http.StatusCreated)
	if got.Tier != "4K" || got.Status != string(model.RequestPending) {
		t.Errorf("4K request = (tier %q, status %q), want a pending 4K request", got.Tier, got.Status)
	}
	// Withdrawn so the title offers a fresh request again.
	app.POSTAs(t, token, "/api/v1/requests/"+got.ID.String()+"/cancel", nil, nil, http.StatusOK)

	setRequestSettings(t, app, user.ID, false, false)
	if got := tierNames(); !slices.Equal(got, []string{"HD"}) {
		t.Errorf("4K turned off: offered tiers = %v, want [HD]", got)
	}
}

// The settings are user grants, so they outlast a role change: inert while the
// user is an admin, in force again once they're a requester.
func TestUserRequestSettings_SurviveRoleChange(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)
	user, token := app.UserToken(t, ctx, "promoted", "requester")
	setRequestSettings(t, app, user.ID, true, false)

	rolePath := "/api/v1/users/" + user.ID.String() + "/role"
	app.PUT(t, rolePath, map[string]any{"role": "admin"}, nil, http.StatusNoContent)
	app.PUT(t, rolePath, map[string]any{"role": "requester"}, nil, http.StatusNoContent)

	var got model.User
	app.GET(t, "/api/v1/users/"+user.ID.String(), &got, http.StatusOK)
	if !got.AutoApprove || got.Allow4k {
		t.Errorf("settings after admin round-trip = (autoApprove=%v, allow4k=%v), want (true, false)", got.AutoApprove, got.Allow4k)
	}
	if req := requestMovieAt(t, app, token, "HD", http.StatusCreated); req.Status != string(model.RequestSpawned) {
		t.Errorf("HD request status = %q, want spawned", req.Status)
	}
	requestMovieAt(t, app, token, "4K", http.StatusForbidden)
}

func TestUserRequestSettings_Validation(t *testing.T) {
	t.Parallel()
	app, ctx, _ := cancelApp(t)
	user, token := app.UserToken(t, ctx, "someone", "requester")
	path := "/api/v1/users/" + user.ID.String() + "/request-settings"

	app.PUT(t, path, map[string]any{"autoApprove": "yes", "allow4k": false}, nil, http.StatusUnprocessableEntity)
	app.PUT(t, path, map[string]any{"autoApprove": true}, nil, http.StatusUnprocessableEntity)
	app.PUT(t, "/api/v1/users/"+uuid.NewString()+"/request-settings",
		map[string]any{"autoApprove": true, "allow4k": true}, nil, http.StatusNotFound)
	app.DoAs(t, token, http.MethodPut, path, map[string]any{"autoApprove": true, "allow4k": true}, nil, http.StatusForbidden)

	var got model.User
	app.GET(t, "/api/v1/users/"+user.ID.String(), &got, http.StatusOK)
	if got.AutoApprove || got.Allow4k {
		t.Errorf("refused writes changed settings: (autoApprove=%v, allow4k=%v)", got.AutoApprove, got.Allow4k)
	}
}

// An invite carries request settings, applied to the user it creates.
func TestInvite_AcceptAppliesRequestSettings(t *testing.T) {
	t.Parallel()
	app, _, _ := cancelApp(t)

	accept := func(email string, body map[string]any) model.User {
		t.Helper()
		body["email"] = email
		var created struct {
			Invite model.Invite `json:"invite"`
			Token  string       `json:"token"`
		}
		app.POST(t, "/api/v1/invites", body, &created, http.StatusCreated)
		app.POST(t, "/api/v1/auth/accept-invite", map[string]any{
			"token": created.Token, "username": email[:6], "password": "test-password-123",
		}, nil, http.StatusCreated)
		var users []model.User
		app.GET(t, "/api/v1/users", &users, http.StatusOK)
		for _, u := range users {
			if u.Email != nil && *u.Email == email {
				return u
			}
		}
		t.Fatalf("no user for accepted invite %s", email)
		return model.User{}
	}

	trusted := accept("trusted@test.local", map[string]any{"role": "requester", "autoApprove": true, "allow4k": true})
	if !trusted.AutoApprove || !trusted.Allow4k {
		t.Errorf("invited with both on: user settings = (autoApprove=%v, allow4k=%v)", trusted.AutoApprove, trusted.Allow4k)
	}
	plain := accept("plainx@test.local", map[string]any{})
	if plain.AutoApprove || plain.Allow4k {
		t.Errorf("invited with defaults: user settings = (autoApprove=%v, allow4k=%v), want both off", plain.AutoApprove, plain.Allow4k)
	}
}
