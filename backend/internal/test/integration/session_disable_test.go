//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// TestReqHard002_RefreshRefusesInactiveUser — REQ-HARD-002 — Disabling a user
// must end their access: refresh must refuse inactive users. The user is made
// inactive without touching their sessions, so only the refresh guard stands
// between them and a fresh token. The refused session is revoked too: a later
// refresh with the same token fails even once the user is active again.
func TestReqHard002_RefreshRefusesInactiveUser(t *testing.T) {
	t.Parallel()
	svc, r := newSessionSvc(t)
	ctx := context.Background()
	user := newNotifUser(t, ctx, r, "hard002-refresh@test.local")

	issued, err := svc.Create(ctx, user, service.SessionMeta{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	setUserActive(t, ctx, r, user, false)

	if _, err := svc.Refresh(ctx, issued.RefreshToken, nil); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("refresh as inactive user: err = %v, want Unauthenticated", err)
	}

	setUserActive(t, ctx, r, user, true)
	if _, err := svc.Refresh(ctx, issued.RefreshToken, nil); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("refresh after re-enable: err = %v, want Unauthenticated (session must stay revoked)", err)
	}
}

// TestReqHard002_DisablingUserRevokesSessions — REQ-HARD-002 — Disabling a user
// must end their access: disabling must revoke their sessions. Driven over the
// wire: the user signs in twice, an admin disables them, and both refresh
// cookies then get 401 with no active sessions left. Re-enabling does not bring
// the old sessions back; the user has to sign in again.
func TestReqHard002_DisablingUserRevokesSessions(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	const (
		username = "hard002-disable"
		email    = username + "@test.local"
		password = "disable-flow-password-123"
	)
	user, err := app.Services.Users.Create(ctx, email, username, password, "requester", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	login := func() *http.Cookie {
		t.Helper()
		resp := postJSON(t, app.URL+"/api/v1/auth/login", map[string]any{"login": username, "password": password})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login status = %d, want 200", resp.StatusCode)
		}
		c := findCookie(resp, refreshCookieName)
		if c == nil || c.Value == "" {
			t.Fatal("login did not set the refresh cookie")
		}
		return c
	}
	setActive := func(active bool) {
		t.Helper()
		var out model.User
		app.PUT(t, "/api/v1/users/"+user.ID.String(), map[string]any{
			"email": email, "username": username, "isActive": active,
		}, &out, http.StatusOK)
		if out.IsActive != active {
			t.Fatalf("isActive = %v, want %v", out.IsActive, active)
		}
	}
	activeSessions := func() int {
		t.Helper()
		list, err := app.Services.Sessions.ListForUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		return len(list)
	}

	cookies := []*http.Cookie{login(), login()}
	if n := activeSessions(); n != 2 {
		t.Fatalf("active sessions before disable = %d, want 2", n)
	}

	setActive(false)

	if n := activeSessions(); n != 0 {
		t.Fatalf("active sessions after disable = %d, want 0", n)
	}
	var audited int64
	if err := pool.QueryRow(ctx, `
		SELECT (detail->>'count')::bigint FROM auth_audit
		WHERE user_id = $1 AND event = 'session.revoked' AND detail->>'scope' = 'disabled'`,
		user.ID).Scan(&audited); err != nil {
		t.Fatalf("read disable audit row: %v", err)
	}
	if audited != 2 {
		t.Fatalf("disable audit count = %d, want 2", audited)
	}
	for i, c := range cookies {
		if resp := postWithCookie(t, app.URL+"/api/v1/auth/refresh", c); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("refresh %d after disable status = %d, want 401", i, resp.StatusCode)
		}
	}

	setActive(true)

	for i, c := range cookies {
		if resp := postWithCookie(t, app.URL+"/api/v1/auth/refresh", c); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("refresh %d after re-enable status = %d, want 401", i, resp.StatusCode)
		}
	}
	fresh := login()
	if resp := postWithCookie(t, app.URL+"/api/v1/auth/refresh", fresh); resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh of a new sign-in after re-enable status = %d, want 200", resp.StatusCode)
	}
}

// setUserActive flips is_active through the repo alone, leaving sessions intact.
func setUserActive(t *testing.T, ctx context.Context, r *repo.Repository, u model.User, active bool) {
	t.Helper()
	email := ""
	if u.Email != nil {
		email = *u.Email
	}
	if _, err := r.UpdateUser(ctx, repo.UpdateUserParams{ID: u.ID, Email: email, Username: u.Username, IsActive: active}); err != nil {
		t.Fatalf("set is_active=%v: %v", active, err)
	}
}
