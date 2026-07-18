package service

import (
	"context"
	"fmt"

	tmdb "github.com/cyruzin/golang-tmdb"

	"github.com/kyleaupton/arrflix/internal/config"
	prowlarradapter "github.com/kyleaupton/arrflix/internal/indexer/prowlarr"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/matcher/resolvers"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
)

type Services struct {
	Acquisition        *AcquisitionService
	Auth               *AuthService
	Authz              *AuthzService
	Downloaders        *DownloadersService
	DownloadCandidates *DownloadCandidatesService
	EmailProvider      *EmailProviderService
	DownloadJobs       *DownloadJobsService
	Enrichment         *EnrichmentService
	Invites            *InvitesService
	Feed               *FeedService
	Import             *ImportService
	ImportTasks        *ImportTasksService
	Indexer            *IndexerService
	Libraries          *LibrariesService
	Matcher            *MatcherService
	MatchDecisions     *MatchDecisionsService
	Media              *MediaService
	NameTemplates      *NameTemplatesService
	Notifications      *NotificationService
	Proposals          *ProposalService
	QualityProfiles    *QualityProfileService
	Reconcile          *ReconcileService
	Requests           *RequestService
	Scheduler          *SchedulerService
	Routing            *RoutingService
	Scanner            *ScannerService
	Sessions           *SessionService
	Tracking           *TrackingService
	Settings           *SettingsService
	Setup              *SetupService
	Tmdb               *TmdbService
	UnmatchedFiles     *UnmatchedFilesService
	Users              *UsersService
	Wants              *WantService
	Filesystem         *FilesystemService
	Version            *VersionService
}

func New(ctx context.Context, r *repo.Repository, l *logger.Logger, c *config.Config, broker *sse.Broker, opts ...Option) *Services {
	cfg := &cfg{}
	for _, o := range opts {
		o.apply(cfg)
	}

	settings := NewSettingsService(r)

	// Resolve TMDB API key: prefer DB setting, fall back to env var
	tmdbKey := c.TmdbAPIKey
	if raw, err := settings.GetRaw(ctx, "tmdb.api_key"); err == nil {
		if str, ok := raw.(string); ok && str != "" {
			tmdbKey = str
		}
	}

	var tmdb *TmdbService
	if cfg.tmdbClient != nil {
		// Test-only path: inject a pre-built TMDB client (e.g. one configured
		// against a fake httptest server). We deliberately skip the OnChange
		// hook below so a later `Settings.Set("tmdb.api_key", ...)` doesn't
		// clobber the injected client by calling InitClient with a real key.
		tmdb = NewTmdbServiceWithClient(r, l, cfg.tmdbClient)
	} else {
		tmdb = NewTmdbService(r, l, tmdbKey)
		// Register onChange hook so TMDB key changes hot-reload the client.
		settings.OnChange("tmdb.api_key", func(ctx context.Context, val any) error {
			return tmdb.InitClient(val.(string))
		})
	}

	prowlarrURL := fmt.Sprintf("http://localhost:%s", c.ProwlarrPort)
	indexer := NewIndexerService(r, l, prowlarrURL, c.ProwlarrAPIKey)
	indexerSource := prowlarradapter.New(indexer.Client(), l)
	media := NewMediaService(r, l, tmdb, settings)
	routingSvc := NewRoutingService(r)
	quality := NewQualityProfileService(r)
	authz := NewAuthzService(r)
	notifications := NewNotificationService(r)
	users := NewUsersService(r, authz)
	invites := NewInvitesService(r)
	reconcile := NewReconcileService(r, l)
	scheduler := NewSchedulerService(r, l)
	enrichment := NewEnrichmentService(r, l, tmdb, reconcile)
	downloadJobs := NewDownloadJobsService(r)
	wants := NewWantService(r, downloadJobs)
	// Proposals is constructed before Acquisition, which depends on it for the
	// propose branch. broker is in scope from New's params.
	proposals := NewProposalService(r, quality, broker, l)

	// Matcher: the v1 resolver catalog (path-embed + name-parse) wires
	// up via DefaultRegistry. ScannerService.MatchBatch is the only
	// caller today; manual re-match flows and drop-in flows (when
	// drop-in detection ships) consume the same surface.
	metadataProvider := metadata.NewTmdbProvider(tmdb)
	matcherSvc := NewMatcherService(
		l,
		r,
		metadataProvider,
		resolvers.DefaultRegistry(metadataProvider),
		matcher.DefaultConfig(),
	)
	// MatchDecisionsService is the user-driven half of the match_decision
	// write surface (re-match / un-match / detach / match-by-ID). It
	// writes through the same repo as MatcherService so the supersede
	// chain stays consistent across auto-match and manual flows.
	matchDecisionsSvc := NewMatchDecisionsService(r, l, tmdb, enrichment, metadataProvider, settings)

	return &Services{
		Acquisition:        NewAcquisitionService(r, l, indexerSource, routingSvc, quality, proposals),
		Auth:               NewAuthService(r, cfg, settings, invites),
		Authz:              authz,
		Downloaders:        NewDownloadersService(r),
		DownloadCandidates: NewDownloadCandidatesService(r, l, indexerSource, media, routingSvc),
		EmailProvider:      NewEmailProviderService(r),
		DownloadJobs:       downloadJobs,
		Enrichment:         enrichment,
		Invites:            invites,
		Filesystem:         NewFilesystemService(),
		Feed:               NewFeedService(r, l, tmdb),
		Import:             NewImportService(r, l),
		ImportTasks:        NewImportTasksService(r),
		Indexer:            indexer,
		Libraries:          NewLibrariesService(r, l),
		Matcher:            matcherSvc,
		MatchDecisions:     matchDecisionsSvc,
		Media:              media,
		NameTemplates:      NewNameTemplatesService(r),
		Notifications:      notifications,
		Proposals:          proposals,
		QualityProfiles:    quality,
		Reconcile:          reconcile,
		Requests:           NewRequestService(r, l, tmdb, quality, enrichment, reconcile, authz, wants),
		Routing:            routingSvc,
		Scanner:            NewScannerService(r, l, tmdb, broker, matcherSvc, enrichment),
		Scheduler:          scheduler,
		Sessions:           NewSessionService(r, cfg.jwtSecret, c.AccessTokenTTL, c.SessionTTL),
		Settings:           settings,
		Setup:              NewSetupService(r, users, settings, tmdb),
		Tmdb:               tmdb,
		Tracking:           NewTrackingService(r, wants, authz),
		UnmatchedFiles:     NewUnmatchedFilesService(r, l, tmdb),
		Users:              users,
		Wants:              wants,
		Version:            NewVersionService(r, l),
	}
}

type cfg struct {
	jwtSecret  string
	tmdbClient *tmdb.Client
}

type Option interface{ apply(*cfg) }

type withJWT string

func (w withJWT) apply(c *cfg) { c.jwtSecret = string(w) }

func WithJWTSecret(secret string) Option { return withJWT(secret) }

type withTmdbClient struct{ c *tmdb.Client }

func (w withTmdbClient) apply(c *cfg) { c.tmdbClient = w.c }

// WithTmdbClient injects a pre-built TMDB client into service construction.
// Intended for integration tests that point a real *tmdb.Client at a fake
// httptest server. When set, the OnChange hook for "tmdb.api_key" is NOT
// registered — so a subsequent settings write to that key won't replace the
// injected client with a real one.
func WithTmdbClient(c *tmdb.Client) Option { return withTmdbClient{c: c} }

func coalesce(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}
