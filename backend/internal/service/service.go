package service

import (
	"github.com/kyleaupton/arrflix/internal/config"
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

func New(r *repo.Repository, l *logger.Logger, c *config.Config, broker *sse.Broker, opts ...Option) *Services {
	cfg := &cfg{}
	for _, o := range opts {
		o.apply(cfg)
	}

	tmdb := NewTmdbService(r, l)
	indexer := NewIndexerService(r, l, c)
	indexerSource := prowlarradapter.New(indexer.Client(), l)
	settings := NewSettingsService(r)
	media := NewMediaService(r, l, tmdb, settings)
	policies := NewPoliciesService(r, l)
	policyEngine := policy.NewEngine(r, l)
	users := NewUsersService(r)
	invites := NewInvitesService(r)

	return &Services{
		Auth:               NewAuthService(r, cfg, settings, invites),
		Downloaders:        NewDownloadersService(r),
		DownloadCandidates: NewDownloadCandidatesService(r, l, indexerSource, media, policyEngine),
		DownloadJobs:       NewDownloadJobsService(r),
		Invites:            invites,
		Filesystem:         NewFilesystemService(),
		Feed:               NewFeedService(r, l, tmdb),
		Import:             NewImportService(r, l),
		ImportTasks:        NewImportTasksService(r),
		Indexer:            indexer,
		Libraries:          NewLibrariesService(r),
		Media:              media,
		NameTemplates:      NewNameTemplatesService(r),
		Policies:           policies,
		Scanner:            NewScannerService(r, l, tmdb, broker),
		Settings:           settings,
		Setup:              NewSetupService(r, users),
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
