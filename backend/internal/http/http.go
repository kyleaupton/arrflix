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
	"github.com/kyleaupton/arrflix/internal/http/authzgate"
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

	api := humachi.New(r, humaConfig(cfg))

	// Fail-closed authorization gate: maps each operation to its required
	// permission and 403s the unentitled (and any unclassified op). Registered
	// before the handlers so the middleware chain is in place; huma applies it
	// to every operation regardless of registration order.
	authzgate.Register(api, services.Authz)

	deps := handlers.Deps{
		Cfg:               cfg,
		Logger:            log,
		Pool:              pool,
		Services:          services,
		DownloaderManager: downloaderManager,
		Broker:            broker,
	}
	handlers.RegisterHumachiHandlers(api, deps)
	handlers.RegisterChiRoutes(r, deps)

	if cfg.Env == "dev" {
		handlers.NewDevDownloaderTest(downloaderManager, repo).RegisterDev(r)
	}

	return &Server{Router: r, API: api}
}

// humaConfig builds the huma config and decides where (or whether) to serve
// the OpenAPI spec, the docs UI, and the JSON Schema components. When
// EnableAPIDocs is true the three are mounted under /api/* so they share the
// existing reverse-proxy path in both dev (Vite/nginx) and prod (nginx); when
// false all three routes are disabled.
func humaConfig(cfg config.Config) huma.Config {
	c := huma.DefaultConfig("Arrflix API", "0.0.1")
	if cfg.EnableAPIDocs {
		c.OpenAPIPath = "/api/openapi"
		c.DocsPath = "/api/docs"
		c.SchemasPath = "/api/schemas"
	} else {
		c.OpenAPIPath = ""
		c.DocsPath = ""
		c.SchemasPath = ""
	}
	return c
}
