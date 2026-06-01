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

type App struct {
	Pool      *pgxpool.Pool
	Repo      *repo.Repository
	Services  *service.Services
	Server    *internalhttp.Server
	URL       string
	Token     string
	JWTSecret string
}

type options struct {
	tmdbClient *tmdb.Client
}

type Option func(*options)

// WithTMDB swaps the TMDB client used by services for one configured against
// a fake server — typically obtained from tmdbtest.New.
func WithTMDB(client *tmdb.Client) Option {
	return func(o *options) {
		o.tmdbClient = client
	}
}

// New builds an integration test app. Cleanup is automatic via t.Cleanup.
func New(t *testing.T, pool *pgxpool.Pool, opts ...Option) *App {
	t.Helper()

	o := &options{}
	for _, fn := range opts {
		fn(o)
	}

	ctx := t.Context()

	jwtSecret := randomHex(t, 32)

	// TmdbAPIKey is deliberately empty; tmdb.api_key is seeded into the DB
	// below so service.New picks it up out of settings without firing the
	// OnChange hook (which is only registered after that read).
	cfg := config.Config{
		Env:            "test",
		Port:           "0",
		JWTSecret:      jwtSecret,
		ProwlarrPort:   "0",
		ProwlarrAPIKey: "test-prowlarr-key",
	}

	logg := logger.New(false)
	broker := sse.NewBroker(ctx)
	r := repo.New(pool)

	registry := downloader.NewRegistry()
	qbittorrent.Register(registry)
	dm := downloader.NewManager(registry, r, logg)

	// When WithTmdbClient is set, service.New skips registering the OnChange
	// hook for "tmdb.api_key", so the Settings.Set call below stays a plain
	// row write and won't clobber the injected client.
	svcOpts := []service.Option{service.WithJWTSecret(jwtSecret)}
	if o.tmdbClient != nil {
		svcOpts = append(svcOpts, service.WithTmdbClient(o.tmdbClient))
	}
	services := service.New(ctx, r, logg, &cfg, broker, svcOpts...)

	const (
		adminEmail    = "admin@test.local"
		adminUsername = "admin"
		adminPassword = "test-password-123"
	)
	if err := services.Setup.Initialize(ctx, adminEmail, adminUsername, adminPassword); err != nil {
		t.Fatalf("testapp: initialize: %v", err)
	}

	// Persist a non-empty tmdb.api_key so IsInitialized returns true.
	if err := services.Settings.Set(ctx, "tmdb.api_key", "test-key"); err != nil {
		t.Fatalf("testapp: set tmdb.api_key: %v", err)
	}

	user, err := r.GetUserByLogin(ctx, adminUsername)
	if err != nil {
		t.Fatalf("testapp: lookup admin user: %v", err)
	}

	email := adminEmail
	token, err := services.Auth.IssueToken(user.ID, &email, adminUsername)
	if err != nil {
		t.Fatalf("testapp: issue token: %v", err)
	}

	srv := internalhttp.NewServer(cfg, logg, pool, services, r, dm, broker)

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

func (a *App) GET(t *testing.T, path string, out any, wantStatus int) {
	t.Helper()
	resp := a.Do(t, nethttp.MethodGet, path, nil, out, wantStatus)
	_ = resp
}

func (a *App) POST(t *testing.T, path string, body any, out any, wantStatus int) {
	t.Helper()
	_ = a.Do(t, nethttp.MethodPost, path, body, out, wantStatus)
}

func (a *App) PUT(t *testing.T, path string, body any, out any, wantStatus int) {
	t.Helper()
	_ = a.Do(t, nethttp.MethodPut, path, body, out, wantStatus)
}

func (a *App) DELETE(t *testing.T, path string, wantStatus int) {
	t.Helper()
	_ = a.Do(t, nethttp.MethodDelete, path, nil, nil, wantStatus)
}

// Do always sets the admin Bearer token, JSON-encodes body when non-nil,
// JSON-decodes into out when non-nil, and asserts the response status. The
// returned response body is already consumed.
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
	defer func() { _ = resp.Body.Close() }()

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

func randomHex(t *testing.T, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("testapp: random secret: %v", err)
	}
	return hex.EncodeToString(buf)
}
