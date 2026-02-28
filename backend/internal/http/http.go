package http

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/http/handlers"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"

	_ "github.com/kyleaupton/arrflix/internal/http/docs"

	"github.com/labstack/echo/v4"
)

// @title		Arrflix API
// @version		0.0.1
// @BasePath	/api
func NewServer(cfg config.Config, log *logger.Logger, pool *pgxpool.Pool, services *service.Services, repo *repo.Repository, downloaderManager *downloader.Manager, broker *sse.Broker) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Handlers
	auth := handlers.NewAuth(cfg, log, pool, services)
	downloadCandidates := handlers.NewDownloadCandidates(services)
	downloadJobs := handlers.NewDownloadJobs(services)
	importTasks := handlers.NewImportTasks(services)
	events := handlers.NewEvents(services, broker)
	downloaders := handlers.NewDownloaders(services, downloaderManager)
	filesystem := handlers.NewFilesystem(services)
	feed := handlers.NewFeed(services)
	health := handlers.NewHealth()
	invites := handlers.NewInvites(services)
	indexers := handlers.NewIndexers(services)
	libraries := handlers.NewLibraries(services)
	media := handlers.NewMedia(services)
	nameTemplates := handlers.NewNameTemplates(services)
	policies := handlers.NewPolicies(services)
	settings := handlers.NewSettings(services)
	bootstrap := handlers.NewBootstrap(cfg, services)
	setup := handlers.NewSetup(services)
	unmatchedFiles := handlers.NewUnmatchedFiles(services)
	users := handlers.NewUsers(services)
	version := handlers.NewVersion(services)

	// Apply setup mode middleware globally
	e.Use(middlewares.SetupMode(services))

	api := e.Group("/api")
	v1 := api.Group("/v1")
	protected := v1.Group("", middlewares.JWT(cfg.JWTSecret))

	// Public routes
	bootstrap.RegisterPublic(v1)
	auth.RegisterPublic(v1)
	health.RegisterPublic(e)
	setup.RegisterPublic(v1)
	version.RegisterPublic(v1)

	// Protected routes
	auth.RegisterProtected(protected)
	downloadCandidates.RegisterProtected(protected)
	downloadJobs.RegisterProtected(protected)
	importTasks.RegisterProtected(protected)
	events.RegisterProtected(protected)
	downloaders.RegisterProtected(protected)
	filesystem.RegisterProtected(protected)
	feed.RegisterProtected(protected)
	invites.RegisterProtected(protected)
	indexers.RegisterProtected(protected)
	libraries.RegisterProtected(protected)
	media.RegisterProtected(protected)
	nameTemplates.RegisterProtected(protected)
	policies.RegisterProtected(protected)
	settings.RegisterProtected(protected)
	unmatchedFiles.RegisterProtected(protected)
	users.RegisterProtected(protected)

	// Dev-only routes
	if cfg.Env == "dev" {
		devDownloaderTest := handlers.NewDevDownloaderTest(downloaderManager, repo)
		devDownloaderTest.RegisterDev(e)
	}

	return e
}
