package handlers

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/email"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// Deps bundles every dependency the handler constructors need. Both the
// live HTTP server and the spec generator pass a Deps value; the spec
// generator passes a zero Deps because handler constructors only need to
// be type-checked, not invoked.
type Deps struct {
	Cfg               config.Config
	Logger            *logger.Logger
	Pool              *pgxpool.Pool
	Services          *service.Services
	DownloaderManager *downloader.Manager
	EmailManager      *email.Manager
	Broker            *sse.Broker
}

// RegisterHumachiHandlers wires every humachi-shaped handler onto api.
// This is the single source of truth for the typed-API surface; both the
// live server (internal/http) and the spec generator (cmd/genspec) call it.
func RegisterHumachiHandlers(api huma.API, deps Deps) {
	NewLibraries(deps.Services).RegisterHumachi(api)
	NewDownloaders(deps.Services, deps.DownloaderManager).RegisterHumachi(api)
	NewEmailProvider(deps.Services, deps.EmailManager).RegisterHumachi(api)
	NewNameTemplates(deps.Services).RegisterHumachi(api)
	NewRouting(deps.Services).RegisterHumachi(api)
	NewQualityProfiles(deps.Services).RegisterHumachi(api)
	NewSettings(deps.Services).RegisterHumachi(api)
	NewInvites(deps.Services).RegisterHumachi(api)
	NewUsers(deps.Services).RegisterHumachi(api)
	NewRoles(deps.Services).RegisterHumachi(api)
	NewAuth(deps.Cfg, deps.Logger, deps.Pool, deps.Services).RegisterHumachi(api)
	NewSetup(deps.Services).RegisterHumachi(api)
	NewMedia(deps.Services).RegisterHumachi(api)
	NewEvents(deps.Broker).RegisterHumachi(api)
	NewDownloadJobs(deps.Services).RegisterHumachi(api)
	NewImportTasks(deps.Services).RegisterHumachi(api)
	NewBootstrap(deps.Cfg, deps.Services).RegisterHumachi(api)
	NewHealth().RegisterHumachi(api)
	NewVersion(deps.Services).RegisterHumachi(api)
	NewDownloadCandidates(deps.Services).RegisterHumachi(api)
	NewFilesystem(deps.Services).RegisterHumachi(api)
	NewFeed(deps.Services).RegisterHumachi(api)
	NewIndexers(deps.Services).RegisterHumachi(api)
	NewUnmatchedFiles(deps.Services).RegisterHumachi(api)
	NewMatchDecisions(deps.Services).RegisterHumachi(api)
	NewRequests(deps.Services).RegisterHumachi(api)
	NewTracking(deps.Services).RegisterHumachi(api)
	NewProposals(deps.Services).RegisterHumachi(api)
	NewWants(deps.Services).RegisterHumachi(api)
	NewNotifications(deps.Services).RegisterHumachi(api)
}

// RegisterChiRoutes wires plain-chi routes that don't fit humachi's typed
// JSON model. Currently just the Plex SSO start (302 redirect).
func RegisterChiRoutes(r chi.Router, deps Deps) {
	auth := NewAuth(deps.Cfg, deps.Logger, deps.Pool, deps.Services)
	r.Get("/api/v1/auth/plex/start", auth.PlexStart)
}
