// Package testapp boots an integration-test-shaped Arrflix HTTP app behind an
// httptest server, with injectable fakes for external clients, an initialized
// system, an admin user, and a pre-issued admin JWT.
//
// Typical usage from an //go:build integration test:
//
//	pool := dbtest.New(t)
//	tmdbSrv, tmdbClient := tmdbtest.New(t)
//	_ = tmdbSrv
//	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
//
//	var out []model.Library
//	app.GET(t, "/api/v1/libraries", &out, http.StatusOK)
//
// The harness wires the production HTTP stack (chi + humachi) on top of the
// supplied pool, with injected service-level fakes where seams exist
// (currently TMDB). Cleanup is automatic via t.Cleanup.
package testapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/downloader/qbittorrent"
	internalhttp "github.com/kyleaupton/arrflix/internal/http"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// App bundles every handle a test might need to drive an integration scenario.
type App struct {
	Pool      *pgxpool.Pool
	Repo      *repo.Repository
	Services  *service.Services
	Server    *internalhttp.Server
	URL       string
	Token     string
	JWTSecret string
}

// options carries the optional knobs collected by Option functions.
type options struct {
	tmdbClient *tmdb.Client
}

// Option configures App construction.
type Option func(*options)

// WithTMDB swaps the TMDB client used by services for one configured against
// a fake server — typically obtained from tmdbtest.New.
func WithTMDB(client *tmdb.Client) Option {
	return func(o *options) {
		o.tmdbClient = client
	}
}

// New builds an integration test app: full huma router behind an httptest
// server, services wired with injected fakes, system marked as initialized,
// admin user created, and an auth token issued.
//
// Pool is required (caller obtains via dbtest.New(t)). Cleanup is automatic
// via t.Cleanup.
func New(t *testing.T, pool *pgxpool.Pool, opts ...Option) *App {
	t.Helper()

	o := &options{}
	for _, fn := range opts {
		fn(o)
	}

	ctx := t.Context()

	// 1. Random JWT secret per app instance. Avoids cross-binary collisions.
	jwtSecret := randomHex(t, 32)

	// 2. Minimal config. We deliberately leave TmdbAPIKey empty here; the
	//    tmdb.api_key setting is seeded directly into the DB below so
	//    service.New picks it up out of settings, without firing the
	//    OnChange hook (which is only registered after that read).
	cfg := config.Config{
		Env:            "test",
		Port:           "0",
		JWTSecret:      jwtSecret,
		ProwlarrPort:   "0",
		ProwlarrAPIKey: "test-prowlarr-key",
	}

	// 3. Broker.
	logg := logger.New(false)
	broker := sse.NewBroker()

	// 4. Repository.
	r := repo.New(pool)

	// 5. Downloader registry + manager. Register qBittorrent so the registry
	//    matches production wiring.
	registry := downloader.NewRegistry()
	qbittorrent.Register(registry)
	dm := downloader.NewManager(registry, r, logg)

	// 6. Build services. The TMDB client (if provided) is injected via
	//    service.WithTmdbClient so every downstream consumer — Media,
	//    Scanner, Feed, Setup, Enrichment, UnmatchedFiles, plus Services.Tmdb
	//    — sees the fake. When the option is set, service.New also skips
	//    registering the OnChange hook for "tmdb.api_key", which means the
	//    Settings.Set call below is a plain row write and won't clobber the
	//    injected client by rebuilding it against a real key.
	svcOpts := []service.Option{service.WithJWTSecret(jwtSecret)}
	if o.tmdbClient != nil {
		svcOpts = append(svcOpts, service.WithTmdbClient(o.tmdbClient))
	}
	services := service.New(ctx, r, logg, &cfg, broker, svcOpts...)

	// 7. Mark system initialized + create admin user (transactional).
	const (
		adminEmail    = "admin@test.local"
		adminUsername = "admin"
		adminPassword = "test-password-123"
	)
	if err := services.Setup.Initialize(ctx, adminEmail, adminUsername, adminPassword); err != nil {
		t.Fatalf("testapp: initialize: %v", err)
	}

	// 8. Persist a non-empty tmdb.api_key so IsInitialized returns true.
	//    Safe to do unconditionally: with WithTmdbClient set the OnChange
	//    hook isn't registered, so this is just a row write.
	if err := services.Settings.Set(ctx, "tmdb.api_key", "test-key"); err != nil {
		t.Fatalf("testapp: set tmdb.api_key: %v", err)
	}

	// 9. Look up the admin user to get their UUID. GetUserByLogin accepts
	//    either email or username.
	user, err := r.GetUserByLogin(ctx, adminUsername)
	if err != nil {
		t.Fatalf("testapp: lookup admin user: %v", err)
	}

	// 10. Issue an admin token.
	email := adminEmail
	token, err := services.Auth.IssueToken(user.ID, &email, adminUsername)
	if err != nil {
		t.Fatalf("testapp: issue token: %v", err)
	}

	// 11. Build the HTTP server.
	srv := internalhttp.NewServer(cfg, logg, pool, services, r, dm, broker)

	// 12. Wrap in httptest. Closed automatically.
	httpSrv := httptest.NewServer(srv.Router)
	t.Cleanup(httpSrv.Close)

	return &App{
		Pool:      pool,
		Repo:      r,
		Services:  services,
		Server:    srv,
		URL:       httpSrv.URL,
		Token:     token,
		JWTSecret: jwtSecret,
	}
}

// GET issues an authenticated GET, decodes the JSON response body into out
// (if non-nil), and fails the test on status mismatch.
func (a *App) GET(t *testing.T, path string, out any, wantStatus int) {
	t.Helper()
	resp := a.Do(t, nethttp.MethodGet, path, nil, out, wantStatus)
	_ = resp
}

// POST issues an authenticated POST. body is JSON-encoded (skip when nil),
// out is JSON-decoded (skip when nil), and the call fails the test on
// status mismatch.
func (a *App) POST(t *testing.T, path string, body any, out any, wantStatus int) {
	t.Helper()
	_ = a.Do(t, nethttp.MethodPost, path, body, out, wantStatus)
}

// PUT issues an authenticated PUT. See POST for body/out semantics.
func (a *App) PUT(t *testing.T, path string, body any, out any, wantStatus int) {
	t.Helper()
	_ = a.Do(t, nethttp.MethodPut, path, body, out, wantStatus)
}

// DELETE issues an authenticated DELETE and fails the test on status mismatch.
func (a *App) DELETE(t *testing.T, path string, wantStatus int) {
	t.Helper()
	_ = a.Do(t, nethttp.MethodDelete, path, nil, nil, wantStatus)
}

// Do is the underlying HTTP helper. It always sets the admin Bearer token,
// JSON-encodes body when non-nil, JSON-decodes into out when non-nil, and
// asserts the response status. The response is returned (body already
// consumed) for callers that want to inspect headers.
func (a *App) Do(t *testing.T, method, path string, body any, out any, wantStatus int) *nethttp.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("testapp: marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := nethttp.NewRequestWithContext(context.Background(), method, a.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("testapp: build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)

	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("testapp: %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != wantStatus {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("testapp: %s %s: unexpected status: got %d want %d; body: %s",
			method, path, resp.StatusCode, wantStatus, string(respBody))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("testapp: %s %s: decode response body: %v", method, path, err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	return resp
}

// randomHex returns a hex-encoded random string with n bytes of entropy.
func randomHex(t *testing.T, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("testapp: random secret: %v", err)
	}
	return hex.EncodeToString(buf)
}
