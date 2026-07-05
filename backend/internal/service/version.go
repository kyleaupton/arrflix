package service

import (
	"context"
	"time"

	"github.com/kyleaupton/arrflix/internal/github"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/semver"
	"github.com/kyleaupton/arrflix/internal/versioninfo"
)

const (
	GitHubOwner = "kyleaupton"
	GitHubRepo  = "arrflix"
	UpdateTTL   = 15 * time.Minute
)

type VersionService struct {
	repo   *repo.Repository
	logger *logger.Logger
	gh     *github.Client
	build  versioninfo.BuildInfo
}

func NewVersionService(r *repo.Repository, l *logger.Logger) *VersionService {
	return &VersionService{
		repo:   r,
		logger: l,
		gh:     github.NewClient(GitHubOwner, GitHubRepo),
		build:  versioninfo.Get(),
	}
}

// GetBuildInfo returns current build information
func (s *VersionService) GetBuildInfo() versioninfo.BuildInfo {
	return s.build
}

// UpdateStatus represents update availability
type UpdateStatus string

const (
	StatusUpToDate        UpdateStatus = "up_to_date"
	StatusUpdateAvailable UpdateStatus = "update_available"
	StatusUnknown         UpdateStatus = "unknown"
)

// VersionInfo is the combined response for GET /v1/version.
type VersionInfo struct {
	Version    string            `json:"version"`
	Commit     string            `json:"commit,omitempty"`
	BuildDate  string            `json:"buildDate,omitempty"`
	Components map[string]string `json:"components,omitempty"`
	Update     UpdateDetails     `json:"update"`
}

// UpdateDetails contains the update-check portion of the response.
type UpdateDetails struct {
	Status UpdateStatus       `json:"status"`
	Reason string             `json:"reason,omitempty"`
	Latest *LatestVersionInfo `json:"latest,omitempty"`
}

type LatestVersionInfo struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	URL         string `json:"url"`
	PublishedAt string `json:"publishedAt,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Ref         string `json:"ref,omitempty"`
}

// GetVersionInfo returns build metadata and update status in one call.
func (s *VersionService) GetVersionInfo(ctx context.Context) (VersionInfo, error) {
	info := VersionInfo{
		Version:    s.build.Version,
		Commit:     s.build.Commit,
		BuildDate:  s.build.BuildDate,
		Components: s.build.Components,
	}

	// Dev builds: always unknown
	if s.build.IsDev() {
		info.Update = UpdateDetails{Status: StatusUnknown, Reason: "dev_build"}
		return info, nil
	}

	// Prerelease builds: always unknown
	if s.build.IsPrerelease() {
		info.Update = UpdateDetails{Status: StatusUnknown, Reason: "prerelease_build"}
		return info, nil
	}

	// Edge builds: compare commits
	if s.build.IsEdge() {
		details, err := s.checkEdgeUpdate(ctx)
		if err != nil {
			return info, err
		}
		info.Update = details
		return info, nil
	}

	// Stable releases: compare semver
	details, err := s.checkStableUpdate(ctx)
	if err != nil {
		return info, err
	}
	info.Update = details
	return info, nil
}

func (s *VersionService) checkEdgeUpdate(ctx context.Context) (UpdateDetails, error) {
	if s.build.Commit == "" {
		return UpdateDetails{Status: StatusUnknown, Reason: "missing_commit"}, nil
	}

	// Fetch latest commit on main with caching
	commit, err := s.getMainHeadCommit(ctx)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to fetch GitHub main HEAD")
		return UpdateDetails{Status: StatusUnknown, Reason: "github_error"}, nil
	}

	latestCommit := commit.SHA[:7] // Short SHA

	if s.build.Commit == latestCommit {
		return UpdateDetails{Status: StatusUpToDate}, nil
	}

	return UpdateDetails{
		Status: StatusUpdateAvailable,
		Latest: &LatestVersionInfo{
			Version:     "edge",
			Tag:         "edge",
			URL:         commit.HTMLURL,
			Commit:      latestCommit,
			Ref:         "main",
			PublishedAt: commit.Commit.Author.Date.Format(time.RFC3339),
		},
	}, nil
}

func (s *VersionService) checkStableUpdate(ctx context.Context) (UpdateDetails, error) {
	currentVer, err := semver.Parse(s.build.Version)
	if err != nil {
		s.logger.Warn().Err(err).Str("version", s.build.Version).Msg("Failed to parse current version")
		return UpdateDetails{Status: StatusUnknown, Reason: "invalid_version"}, nil
	}

	// Fetch latest release with caching
	release, err := s.getLatestRelease(ctx)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to fetch GitHub latest release")
		return UpdateDetails{Status: StatusUnknown, Reason: "github_error"}, nil
	}

	latestVer, err := semver.Parse(release.TagName)
	if err != nil {
		s.logger.Warn().Err(err).Str("tag", release.TagName).Msg("Failed to parse latest release version")
		return UpdateDetails{Status: StatusUnknown, Reason: "invalid_latest_version"}, nil
	}

	if currentVer.LessThan(latestVer) {
		return UpdateDetails{
			Status: StatusUpdateAvailable,
			Latest: &LatestVersionInfo{
				Version:     release.TagName,
				Tag:         release.TagName,
				URL:         release.HTMLURL,
				PublishedAt: release.PublishedAt.Format(time.RFC3339),
				Notes:       release.Body,
			},
		}, nil
	}

	return UpdateDetails{Status: StatusUpToDate}, nil
}

// Cached GitHub API calls
func (s *VersionService) getLatestRelease(ctx context.Context) (*github.Release, error) {
	cacheKey := "github_latest_release"
	release, err := getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*github.Release, error) {
		return s.gh.GetLatestRelease(ctx)
	}, UpdateTTL, false)
	if err != nil {
		return nil, err
	}
	return &release, nil
}

func (s *VersionService) getMainHeadCommit(ctx context.Context) (*github.Commit, error) {
	cacheKey := "github_main_head"
	commit, err := getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*github.Commit, error) {
		return s.gh.GetMainHeadCommit(ctx)
	}, UpdateTTL, false)
	if err != nil {
		return nil, err
	}
	return &commit, nil
}
