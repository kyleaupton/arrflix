// Package http stands up the HTTP layer: a chi router as the top-level mux,
// a humachi API mounted on top for the typed OpenAPI surface, and a couple
// of plain chi handlers for routes that don't fit the typed surface (Plex
// SSO 302 redirect, dev-only debug UI).
//
// Cross-cutting middleware (auth, setup-mode) runs at the chi layer so it
// applies once regardless of which surface the request lands on.
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
	// Side-effect import: installs apperrors.ToProblem as huma's NewError so
	// errors returned from humachi handlers render as RFC 9457 problem-details.
	_ "github.com/kyleaupton/arrflix/internal/http/humaerr"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// Server bundles the chi router (bound to the net/http listener by main.go)
// and the huma API (read by cmd/genspec to produce openapi.json).
type Server struct {
	Router http.Handler
	API    huma.API
}

func NewServer(cfg config.Config, log *logger.Logger, pool *pgxpool.Pool, services *service.Services, repo *repo.Repository, downloaderManager *downloader.Manager, broker *sse.Broker) *Server {
	r := chi.NewRouter()

	r.Use(middlewares.ChiSetupMode(services))
	r.Use(middlewares.ChiJWT(cfg.JWTSecret))

	api := humachi.New(r, huma.DefaultConfig("Arrflix API", "0.0.1"))

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

	// Plex SSO start is a 302 redirect, which humachi (JSON-first) can't model
	// cleanly. Public via the publicPathSet allowlist in middlewares/chi.go.
	r.Get("/api/v1/auth/plex/start", authH.PlexStart)

	if cfg.Env == "dev" {
		handlers.NewDevDownloaderTest(downloaderManager, repo).RegisterDev(r)
	}

	return &Server{Router: r, API: api}
}
