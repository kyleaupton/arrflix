// Package http is the HTTP layer entrypoint. As of phase 4 of the humachi
// migration, Echo is gone. This package stands up:
//
//  1. A chi router as the top-level mux.
//  2. A humachi API on top of chi for the typed OpenAPI surface (every
//     /api/v1/* handler that isn't a redirect or HTML UI).
//  3. Two plain chi handlers that don't fit the typed surface:
//     - /api/v1/auth/plex/start (302 redirect to Plex)
//     - /dev/* (HTML UI + opaque debug endpoints, dev env only)
//
// Cross-cutting middleware (auth, setup-mode) runs at the chi layer so it
// applies once regardless of whether the request lands on humachi or one of
// the plain chi handlers.
package http

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/http/handlers"
	// Side-effect import: installs apperrors.ToProblem as huma's NewError
	// so any error returned from a humachi handler renders as RFC 9457
	// problem-details with the typed-error fields preserved.
	_ "github.com/kyleaupton/arrflix/internal/http/humaerr"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// Server bundles the chi router (the top-level handler) and the huma API
// (the spec-bearing surface). The chi router is what main.go binds to the
// listener; the API is what cmd/genspec/main.go reads to produce
// openapi.json.
type Server struct {
	Router http.Handler
	API    huma.API
}

// NewServer wires up the HTTP stack:
//
//  1. Build a chi router with chi-shaped versions of the cross-cutting
//     middleware (setup-mode globally, JWT on protected paths).
//  2. Mount humachi onto chi and register every typed-API handler.
//  3. Register the two non-typed chi routes (Plex SSO 302 redirect, and the
//     dev-only /dev/* tree when cfg.Env == "dev").
//
// The single net/http listener serves Server.Router; that's the chi router
// with everything underneath.
func NewServer(cfg config.Config, log *logger.Logger, pool *pgxpool.Pool, services *service.Services, repo *repo.Repository, downloaderManager *downloader.Manager, broker *sse.Broker) *Server {
	// Chi router as the top-level mux.
	r := chi.NewRouter()

	// Setup-mode middleware runs globally (matches the pre-migration Echo
	// behavior of blocking everything outside /health, bootstrap, and
	// /api/v1/setup when the system isn't initialized).
	r.Use(middlewares.ChiSetupMode(services))

	// JWT runs globally, with an internal allowlist (ChiJWT.publicPathSet
	// + publicPathPrefixes) bypassing public routes (login, signup, plex
	// start/exchange, bootstrap, version, health, setup-*) and the dev
	// tree. Running it at chi level — before any handler dispatch — means
	// the auth check runs once per request regardless of whether the
	// route lands on humachi or a plain chi handler.
	r.Use(middlewares.ChiJWT(cfg.JWTSecret))

	// Build the humachi API. huma.NewError is wired in internal/http/humaerr
	// (imported above for its side effect) to use apperrors.ToProblem for
	// typed errors and a default RFC 9457 shape otherwise.
	api := humachi.New(r, huma.DefaultConfig("Arrflix API", "0.0.1"))

	// Typed-API handlers (humachi).
	handlers.NewLibraries(services).RegisterHumachi(api)
	handlers.NewDownloaders(services, downloaderManager).RegisterHumachi(api)
	handlers.NewNameTemplates(services).RegisterHumachi(api)
	handlers.NewPolicies(services).RegisterHumachi(api)
	handlers.NewSettings(services).RegisterHumachi(api)
	handlers.NewInvites(services).RegisterHumachi(api)
	handlers.NewUsers(services).RegisterHumachi(api)
	handlers.NewRoles(services).RegisterHumachi(api)

	authH := handlers.NewAuth(cfg, log, pool, services)
	authH.RegisterHumachi(api)
	handlers.NewSetup(services).RegisterHumachi(api)
	handlers.NewMedia(services).RegisterHumachi(api)
	handlers.NewEvents(services, broker).RegisterHumachi(api)
	handlers.NewDownloadJobs(services).RegisterHumachi(api)
	handlers.NewImportTasks(services).RegisterHumachi(api)
	handlers.NewBootstrap(cfg, services).RegisterHumachi(api)
	handlers.NewHealth().RegisterHumachi(api)
	handlers.NewVersion(services).RegisterHumachi(api)
	handlers.NewDownloadCandidates(services).RegisterHumachi(api)
	handlers.NewFilesystem(services).RegisterHumachi(api)
	handlers.NewFeed(services).RegisterHumachi(api)
	handlers.NewIndexers(services).RegisterHumachi(api)
	handlers.NewUnmatchedFiles(services).RegisterHumachi(api)

	// Plain chi: Plex SSO start (302 redirect — JSON-first humachi can't
	// model this cleanly). Public via the publicPathSet allowlist in
	// middlewares/chi.go.
	r.Get("/api/v1/auth/plex/start", authH.PlexStart)

	// Plain chi (dev only): downloader-test HTML UI + opaque debug API.
	// JWT-bypassed for the entire /dev/* tree via publicPathPrefixes.
	if cfg.Env == "dev" {
		handlers.NewDevDownloaderTest(downloaderManager, repo).RegisterDev(r)
	}

	return &Server{Router: r, API: api}
}
