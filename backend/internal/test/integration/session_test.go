//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// newSessionSvc builds a SessionService over a fresh DB. Zero TTLs fall back to
// the service defaults (24h access, 90d session), which suit every case here —
// none of these tests depend on absolute expiry.
func newSessionSvc(t *testing.T) (*service.SessionService, *repo.Repository) {
	t.Helper()
	pool := dbtest.New(t)
	r := repo.New(pool)
	return service.NewSessionService(r, "test-session-secret", 0, 0), r
}

// TestSession_CreateAndRefreshRotates proves a created session issues an access +
// refresh token, and refreshing rotates to brand-new tokens.
func TestSession_CreateAndRefreshRotates(t *testing.T) {
	t.Parallel()
	svc, r := newSessionSvc(t)
	ctx := context.Background()
	user := newNotifUser(t, ctx, r, "session-rotate@test.local")

	first, err := svc.Create(ctx, user, service.SessionMeta{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if first.AccessToken == "" || first.RefreshToken == "" {
		t.Fatal("create returned empty tokens")
	}

	second, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh did not rotate the refresh token")
	}
	// Note: the access token can be byte-identical to the previous one when both
	// are minted in the same second — same sub/sid/iat/exp claims sign to the same
	// JWT. That's benign; only the refresh token must rotate.
	if second.AccessToken == "" {
		t.Fatal("refresh returned an empty access token")
	}
	if second.Session.ID != first.Session.ID {
		t.Fatal("rotation should keep the same session id")
	}
}

// TestSession_ReuseDetectionRevokesFamily proves that replaying a refresh token
// two generations stale (matching neither the current nor the grace-window
// previous secret) is treated as theft and revokes the whole session — so even
// the currently-valid token stops working.
func TestSession_ReuseDetectionRevokesFamily(t *testing.T) {
	t.Parallel()
	svc, r := newSessionSvc(t)
	ctx := context.Background()
	user := newNotifUser(t, ctx, r, "session-reuse@test.local")

	rt1, err := svc.Create(ctx, user, service.SessionMeta{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rt2, err := svc.Refresh(ctx, rt1.RefreshToken) // rt1 -> prev
	if err != nil {
		t.Fatalf("refresh 1: %v", err)
	}
	rt3, err := svc.Refresh(ctx, rt2.RefreshToken) // rt2 -> prev, rt1 now stale
	if err != nil {
		t.Fatalf("refresh 2: %v", err)
	}

	// Replaying rt1 matches neither current (rt3) nor prev (rt2) → reuse.
	if _, err := svc.Refresh(ctx, rt1.RefreshToken); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("stale token reuse: err = %v, want Unauthenticated", err)
	}

	// The family is burned: the latest valid token no longer works either.
	if _, err := svc.Refresh(ctx, rt3.RefreshToken); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("post-reuse refresh of live token: err = %v, want Unauthenticated", err)
	}
}

// TestSession_RefreshRejectsRevoked proves a revoked session's refresh token is
// rejected, and TestSession garbage inputs all 401 rather than erroring oddly.
func TestSession_RefreshRejectsRevokedAndGarbage(t *testing.T) {
	t.Parallel()
	svc, r := newSessionSvc(t)
	ctx := context.Background()
	user := newNotifUser(t, ctx, r, "session-revoke@test.local")

	issued, err := svc.Create(ctx, user, service.SessionMeta{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Revoke(ctx, issued.Session.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.Refresh(ctx, issued.RefreshToken); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("refresh after revoke: err = %v, want Unauthenticated", err)
	}

	for _, tok := range []string{
		"",                                  // empty
		"not-a-token",                       // no separator
		"not-a-uuid.secret",                 // bad session id
		issued.Session.ID.String() + ".xxx", // real session, wrong secret
	} {
		if _, err := svc.Refresh(ctx, tok); !apperrors.IsUnauthenticated(err) {
			t.Fatalf("refresh %q: err = %v, want Unauthenticated", tok, err)
		}
	}
}

// TestSession_LogoutRevokesOwnSessionOnly proves logout-by-cookie revokes the
// session the token belongs to, and that a wrong secret for a real session id is a
// no-op (a guessed id can't force-log-out someone else).
func TestSession_LogoutRevokesOwnSessionOnly(t *testing.T) {
	t.Parallel()
	svc, r := newSessionSvc(t)
	ctx := context.Background()
	user := newNotifUser(t, ctx, r, "session-logout@test.local")

	issued, err := svc.Create(ctx, user, service.SessionMeta{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Wrong secret for the real session id: no-op, session survives.
	if err := svc.RevokeByRefreshToken(ctx, issued.Session.ID.String()+".wrong"); err != nil {
		t.Fatalf("revoke-by-token with wrong secret should be a no-op: %v", err)
	}
	if _, err := svc.Refresh(ctx, issued.RefreshToken); err != nil {
		t.Fatalf("session should survive a bad-secret logout: %v", err)
	}

	// Re-read the current token (refresh above rotated it) and log out properly.
	current, err := svc.Refresh(ctx, issued.RefreshToken)
	if err != nil {
		t.Fatalf("refresh to get current token: %v", err)
	}
	if err := svc.RevokeByRefreshToken(ctx, current.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Refresh(ctx, current.RefreshToken); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("refresh after logout: err = %v, want Unauthenticated", err)
	}
}

// TestSession_RevokeAllForUser proves "log out everywhere" ends every active
// session and empties the active-sessions list.
func TestSession_RevokeAllForUser(t *testing.T) {
	t.Parallel()
	svc, r := newSessionSvc(t)
	ctx := context.Background()
	user := newNotifUser(t, ctx, r, "session-revoke-all@test.local")

	a, err := svc.Create(ctx, user, service.SessionMeta{})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := svc.Create(ctx, user, service.SessionMeta{})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	list, err := svc.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("active sessions = %d, want 2", len(list))
	}

	if err := svc.RevokeAllForUser(ctx, user.ID); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	for _, s := range []service.Issued{a, b} {
		if _, err := svc.Refresh(ctx, s.RefreshToken); !apperrors.IsUnauthenticated(err) {
			t.Fatalf("refresh after revoke-all: err = %v, want Unauthenticated", err)
		}
	}
	list, err = svc.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list after revoke-all: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("active sessions after revoke-all = %d, want 0", len(list))
	}
}

// TestSession_LoginSetsCookieAndRefreshWorks drives the wire contract end-to-end:
// POST /auth/login returns the access token in the body AND an HttpOnly refresh
// cookie, /auth/refresh rotates it (both cookie-authed, no bearer), and /auth/logout
// invalidates it.
func TestSession_LoginSetsCookieAndRefreshWorks(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	const (
		username = "session-http-user"
		password = "http-flow-password-123"
	)
	if _, err := app.Services.Users.Create(ctx, username+"@test.local", username, password, "requester", true); err != nil {
		t.Fatalf("create login user: %v", err)
	}

	// Login → token in body + refresh cookie.
	loginResp := postJSON(t, app.URL+"/api/v1/auth/login", map[string]any{
		"login":    username,
		"password": password,
	})
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.StatusCode)
	}
	if tok := decodeToken(t, loginResp); tok == "" {
		t.Fatal("login body missing token")
	}
	rt := findCookie(loginResp, refreshCookieName)
	if rt == nil || rt.Value == "" {
		t.Fatal("login did not set the refresh cookie")
	}
	if !rt.HttpOnly {
		t.Error("refresh cookie is not HttpOnly")
	}

	// Refresh with the cookie (no bearer) → new token + rotated cookie.
	refreshResp := postWithCookie(t, app.URL+"/api/v1/auth/refresh", rt)
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200", refreshResp.StatusCode)
	}
	if tok := decodeToken(t, refreshResp); tok == "" {
		t.Fatal("refresh body missing token")
	}
	rotated := findCookie(refreshResp, refreshCookieName)
	if rotated == nil || rotated.Value == "" || rotated.Value == rt.Value {
		t.Fatal("refresh did not rotate the cookie")
	}

	// Logout invalidates the rotated cookie.
	logoutResp := postWithCookie(t, app.URL+"/api/v1/auth/logout", rotated)
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logoutResp.StatusCode)
	}
	afterLogout := postWithCookie(t, app.URL+"/api/v1/auth/refresh", rotated)
	if afterLogout.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want 401", afterLogout.StatusCode)
	}
}

// refreshCookieName mirrors the handler's cookie name; kept local so the test
// reads the wire contract rather than importing a handler internal.
const refreshCookieName = "arrflix_rt"

func postJSON(t *testing.T, url string, body map[string]any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func postWithCookie(t *testing.T, url string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func decodeToken(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode token body: %v", err)
	}
	return out.Token
}
