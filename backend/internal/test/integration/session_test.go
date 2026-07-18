//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
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
	return service.NewSessionService(r, logger.New(false), "test-session-secret", 0, 0), r
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

	second, err := svc.Refresh(ctx, first.RefreshToken, nil)
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
	rt2, err := svc.Refresh(ctx, rt1.RefreshToken, nil) // rt1 -> prev
	if err != nil {
		t.Fatalf("refresh 1: %v", err)
	}
	rt3, err := svc.Refresh(ctx, rt2.RefreshToken, nil) // rt2 -> prev, rt1 now stale
	if err != nil {
		t.Fatalf("refresh 2: %v", err)
	}

	// Replaying rt1 matches neither current (rt3) nor prev (rt2) → reuse.
	if _, err := svc.Refresh(ctx, rt1.RefreshToken, nil); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("stale token reuse: err = %v, want Unauthenticated", err)
	}

	// The family is burned: the latest valid token no longer works either.
	if _, err := svc.Refresh(ctx, rt3.RefreshToken, nil); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("post-reuse refresh of live token: err = %v, want Unauthenticated", err)
	}
}

// TestSession_RotationRaceLeapfrogs proves the multi-tab refresh race is benign.
// Two tabs load holding the same refresh token; tab A refreshes first, and tab B —
// still carrying the pre-rotation token — refreshes within the grace window. B's
// token now matches the just-superseded (previous) secret, so it leapfrogs to a
// fresh token instead of tripping reuse detection. Both tabs walk away with a
// working token and the session survives.
//
// This is the benign twin of TestSession_ReuseDetectionRevokesFamily: the same
// replay of a superseded token, but here it's still the immediately-previous
// secret within refreshGrace, so it rotates rather than burning the family.
func TestSession_RotationRaceLeapfrogs(t *testing.T) {
	t.Parallel()
	svc, r := newSessionSvc(t)
	ctx := context.Background()
	user := newNotifUser(t, ctx, r, "session-race@test.local")

	rt1, err := svc.Create(ctx, user, service.SessionMeta{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Tab A refreshes first: rt1 -> rt2, so rt1 becomes the graced previous secret.
	tabA, err := svc.Refresh(ctx, rt1.RefreshToken, nil)
	if err != nil {
		t.Fatalf("tab A refresh: %v", err)
	}

	// Tab B still holds rt1 and refreshes within the grace window. rt1 matches the
	// previous secret (not the current), so it leapfrogs to its own fresh token.
	tabB, err := svc.Refresh(ctx, rt1.RefreshToken, nil)
	if err != nil {
		t.Fatalf("tab B refresh should leapfrog within grace, got: %v", err)
	}

	if tabA.RefreshToken == tabB.RefreshToken {
		t.Fatal("the two tabs got the same refresh token; each rotation must be distinct")
	}
	if tabA.Session.ID != tabB.Session.ID {
		t.Fatal("leapfrog must stay within the same session")
	}

	// The session is alive after the race: tab B's fresh token still refreshes.
	if _, err := svc.Refresh(ctx, tabB.RefreshToken, nil); err != nil {
		t.Fatalf("session should survive the race: %v", err)
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
	if _, err := svc.Refresh(ctx, issued.RefreshToken, nil); !apperrors.IsUnauthenticated(err) {
		t.Fatalf("refresh after revoke: err = %v, want Unauthenticated", err)
	}

	for _, tok := range []string{
		"",                                  // empty
		"not-a-token",                       // no separator
		"not-a-uuid.secret",                 // bad session id
		issued.Session.ID.String() + ".xxx", // real session, wrong secret
	} {
		if _, err := svc.Refresh(ctx, tok, nil); !apperrors.IsUnauthenticated(err) {
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
	if _, err := svc.Refresh(ctx, issued.RefreshToken, nil); err != nil {
		t.Fatalf("session should survive a bad-secret logout: %v", err)
	}

	// Re-read the current token (refresh above rotated it) and log out properly.
	current, err := svc.Refresh(ctx, issued.RefreshToken, nil)
	if err != nil {
		t.Fatalf("refresh to get current token: %v", err)
	}
	if err := svc.RevokeByRefreshToken(ctx, current.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Refresh(ctx, current.RefreshToken, nil); !apperrors.IsUnauthenticated(err) {
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
		if _, err := svc.Refresh(ctx, s.RefreshToken, nil); !apperrors.IsUnauthenticated(err) {
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

// TestSession_CookieSecurityAttributes pins the refresh cookie's security contract
// on the wire: HttpOnly (JS can't read it), SameSite=Lax (CSRF mitigation), and
// Path-scoped to /api/v1/auth (it rides only refresh/logout, not every API call).
// Login and refresh set a live cookie with these flags; logout clears it.
//
// Secure is env-gated (set only when cfg.Env == "prod"); the harness runs as
// Env="test", so the wire cookie is correctly non-Secure here. The prod path that
// flips Secure on isn't driven — the harness has no prod seam — so we assert the
// test-env negative rather than a positive we can't produce.
func TestSession_CookieSecurityAttributes(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	const (
		username = "cookie-flags-user"
		password = "cookie-flags-password-123"
	)
	if _, err := app.Services.Users.Create(ctx, username+"@test.local", username, password, "requester", true); err != nil {
		t.Fatalf("create login user: %v", err)
	}

	loginResp := postJSON(t, app.URL+"/api/v1/auth/login", map[string]any{
		"login":    username,
		"password": password,
	})
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.StatusCode)
	}
	rt := findCookie(loginResp, refreshCookieName)
	if rt == nil {
		t.Fatal("login did not set the refresh cookie")
	}
	assertLiveRefreshCookie(t, rt)

	// Refresh rotates to a new cookie that carries the same security flags.
	refreshResp := postWithCookie(t, app.URL+"/api/v1/auth/refresh", rt)
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200", refreshResp.StatusCode)
	}
	rotated := findCookie(refreshResp, refreshCookieName)
	if rotated == nil {
		t.Fatal("refresh did not set a cookie")
	}
	assertLiveRefreshCookie(t, rotated)

	// Logout clears the cookie: empty value, immediate expiry (Max-Age=0 → parsed
	// back as -1), and the same Path scope so the browser drops exactly the cookie
	// login set.
	logoutResp := postWithCookie(t, app.URL+"/api/v1/auth/logout", rotated)
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logoutResp.StatusCode)
	}
	cleared := findCookie(logoutResp, refreshCookieName)
	if cleared == nil {
		t.Fatal("logout did not set a clearing cookie")
	}
	if cleared.Value != "" {
		t.Errorf("cleared cookie value = %q, want empty", cleared.Value)
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("cleared cookie MaxAge = %d, want < 0 (immediate expiry)", cleared.MaxAge)
	}
	if cleared.Path != "/api/v1/auth" {
		t.Errorf("cleared cookie Path = %q, want /api/v1/auth", cleared.Path)
	}
}

// assertLiveRefreshCookie checks the security flags every live (login + rotation)
// refresh cookie must carry.
func assertLiveRefreshCookie(t *testing.T, c *http.Cookie) {
	t.Helper()
	if c.Value == "" {
		t.Error("live refresh cookie has an empty value")
	}
	if !c.HttpOnly {
		t.Error("refresh cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("refresh cookie SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/api/v1/auth" {
		t.Errorf("refresh cookie Path = %q, want /api/v1/auth", c.Path)
	}
	// Env="test" in the harness, so Secure is off; prod flips it on.
	if c.Secure {
		t.Error("refresh cookie is Secure under the test harness (Env=test); expected off")
	}
}

// TestSession_SweepCascadesPushSubscription proves the terminal-session sweeper
// hard-deletes a revoked session and that the delete cascades to its push
// subscription (push_subscription.session_id ... ON DELETE CASCADE). The push row
// is registered end-to-end (login → subscribe, which reads the session id from the
// access token's sid claim); the sweep and the child-row assertions use the service
// and pool directly, because the sweeper is a background job with no HTTP surface
// and a cascade can only be observed by reading the child table.
func TestSession_SweepCascadesPushSubscription(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	const (
		username = "cascade-user"
		password = "cascade-password-123"
	)
	user, err := app.Services.Users.Create(ctx, username+"@test.local", username, password, "requester", true)
	if err != nil {
		t.Fatalf("create login user: %v", err)
	}

	// Login for an access token carrying a sid claim — the push subscribe handler
	// keys the subscription off the session id, which the admin harness token lacks.
	loginResp := postJSON(t, app.URL+"/api/v1/auth/login", map[string]any{
		"login":    username,
		"password": password,
	})
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.StatusCode)
	}
	accessToken := decodeToken(t, loginResp)
	rt := findCookie(loginResp, refreshCookieName)
	if rt == nil {
		t.Fatal("login did not set the refresh cookie")
	}

	// Register a push subscription for this session via the public endpoint.
	app.POSTAs(t, accessToken, "/api/v1/notifications/push/subscriptions", map[string]any{
		"endpoint": "https://push.example.test/ep/cascade",
		"p256dh":   "BExamplePublicKeyBytesBase64UrlEncodedValueForTest",
		"auth":     "AuthSecretBase64Url",
	}, nil, http.StatusNoContent)

	sessions, err := app.Services.Sessions.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("active sessions = %d, want 1", len(sessions))
	}
	sid := sessions[0].ID
	if got := pushCountForSession(t, app, ctx, sid); got != 1 {
		t.Fatalf("push subscriptions for session before sweep = %d, want 1", got)
	}

	// Logout revokes the session (soft delete), making it eligible for the sweep.
	logoutResp := postWithCookie(t, app.URL+"/api/v1/auth/logout", rt)
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logoutResp.StatusCode)
	}

	// Sweep with the cutoff an hour in the future so the just-revoked session is
	// unambiguously terminal-past-cutoff, independent of any Go/DB clock skew.
	n, err := app.Services.Sessions.SweepTerminal(ctx, -time.Hour)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n < 1 {
		t.Fatalf("swept rows = %d, want >= 1", n)
	}

	// The session row is gone and its push subscription cascaded away with it.
	if got := sessionRowCount(t, app, ctx, sid); got != 0 {
		t.Fatalf("session rows after sweep = %d, want 0", got)
	}
	if got := pushCountForSession(t, app, ctx, sid); got != 0 {
		t.Fatalf("push subscriptions after sweep = %d, want 0 (cascade)", got)
	}
}

// pushCountForSession reads the push_subscription child-row count for a session.
// Direct SQL is the only way to observe a cascade — the sweep deletes the parent,
// so no API surface can report on the (now absent) child.
func pushCountForSession(t *testing.T, app *testapp.App, ctx context.Context, sid any) int {
	t.Helper()
	var n int
	if err := app.Pool.QueryRow(ctx,
		"SELECT count(*) FROM push_subscription WHERE session_id = $1", sid).Scan(&n); err != nil {
		t.Fatalf("count push subscriptions: %v", err)
	}
	return n
}

// sessionRowCount reads the raw user_session row count for an id — used to prove
// the sweeper hard-deletes (not just soft-revokes) a terminal session.
func sessionRowCount(t *testing.T, app *testapp.App, ctx context.Context, sid any) int {
	t.Helper()
	var n int
	if err := app.Pool.QueryRow(ctx,
		"SELECT count(*) FROM user_session WHERE id = $1", sid).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
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
