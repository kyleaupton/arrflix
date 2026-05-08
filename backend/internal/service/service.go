package service

import (
	"context"

	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/guessit"
	prowlarradapter "github.com/kyleaupton/arrflix/internal/indexer/prowlarr"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/policy"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
)

type Services struct {
	Auth               *AuthService
	Downloaders        *DownloadersService
	DownloadCandidates *DownloadCandidatesService
	DownloadJobs       *DownloadJobsService
	Enrichment         *EnrichmentService
	Invites            *InvitesService
	Feed               *FeedService
	Import             *ImportService
	ImportTasks        *ImportTasksService
	Indexer            *IndexerService
	Libraries          *LibrariesService
	Media              *MediaService
	NameTemplates      *NameTemplatesService
	Policies           *PoliciesService
	Scanner            *ScannerService
	Settings           *SettingsService
	Setup              *SetupService
	Tmdb               *TmdbService
	UnmatchedFiles     *UnmatchedFilesService
	Users              *UsersService
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

	tmdb := NewTmdbService(r, l, tmdbKey)
	indexer := NewIndexerService(r, l, c)
	indexerSource := prowlarradapter.New(indexer.Client(), l)
	media := NewMediaService(r, l, tmdb, settings)
	policies := NewPoliciesService(r, l)
	policyEngine := policy.NewEngine(r, l)
	users := NewUsersService(r)
	invites := NewInvitesService(r)
	enrichment := NewEnrichmentService(r, l, tmdb)

	// Register onChange hook so TMDB key changes hot-reload the client
	settings.OnChange("tmdb.api_key", func(ctx context.Context, val any) error {
		return tmdb.InitClient(val.(string))
	})

	return &Services{
		Auth:               NewAuthService(r, cfg, settings, invites),
		Downloaders:        NewDownloadersService(r),
		DownloadCandidates: NewDownloadCandidatesService(r, l, indexerSource, media, policyEngine),
		DownloadJobs:       NewDownloadJobsService(r),
		Enrichment:         enrichment,
		Invites:            invites,
		Filesystem:         NewFilesystemService(),
		Feed:               NewFeedService(r, l, tmdb),
		Import:             NewImportService(r, l),
		ImportTasks:        NewImportTasksService(r),
		Indexer:            indexer,
		Libraries:          NewLibrariesService(r, l),
		Media:              media,
		NameTemplates:      NewNameTemplatesService(r),
		Policies:           policies,
		Scanner:            NewScannerService(r, l, tmdb, broker, guessit.NewClient(""), enrichment),
		Settings:           settings,
		Setup:              NewSetupService(r, users, settings, tmdb),
		Tmdb:               tmdb,
		UnmatchedFiles:     NewUnmatchedFilesService(r, l, tmdb),
		Users:              users,
		Version:            NewVersionService(r, l),
	}
}

type cfg struct {
	jwtSecret string
}

type Option interface{ apply(*cfg) }

type withJWT string

func (w withJWT) apply(c *cfg) { c.jwtSecret = string(w) }

func WithJWTSecret(secret string) Option { return withJWT(secret) }

func coalesce(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}
