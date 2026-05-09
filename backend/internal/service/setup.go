package service

import (
	"context"
	"strings"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	pw "github.com/kyleaupton/arrflix/internal/password"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type SetupSteps struct {
	AdminAccount bool `json:"admin_account"`
	TmdbApiKey   bool `json:"tmdb_api_key"`
}

type SetupService struct {
	repo     *repo.Repository
	users    *UsersService
	settings *SettingsService
	tmdb     *TmdbService
}

func NewSetupService(r *repo.Repository, users *UsersService, settings *SettingsService, tmdb *TmdbService) *SetupService {
	return &SetupService{repo: r, users: users, settings: settings, tmdb: tmdb}
}

// GetSetupSteps returns the completion status of each setup step.
func (s *SetupService) GetSetupSteps(ctx context.Context) (SetupSteps, error) {
	steps := SetupSteps{}

	// Admin account: check if system.initialized flag is true (set when admin is created)
	initialized, err := s.repo.GetSystemInitialized(ctx)
	if err != nil {
		return steps, err
	}
	steps.AdminAccount = initialized

	// TMDB API key: check if a non-empty key is stored
	raw, err := s.settings.GetRaw(ctx, "tmdb.api_key")
	if err != nil {
		return steps, err
	}
	if str, ok := raw.(string); ok && str != "" {
		steps.TmdbApiKey = true
	}

	return steps, nil
}

// IsInitialized returns true when all setup steps are complete.
func (s *SetupService) IsInitialized(ctx context.Context) (bool, error) {
	// Fast path: if system.initialized is already true, check TMDB key too
	flagSet, err := s.repo.GetSystemInitialized(ctx)
	if err != nil {
		return false, err
	}

	if !flagSet {
		return false, nil
	}

	// Admin is done; also require TMDB key
	raw, err := s.settings.GetRaw(ctx, "tmdb.api_key")
	if err != nil {
		return false, err
	}
	if str, ok := raw.(string); ok && str != "" {
		return true, nil
	}
	return false, nil
}

// SetTmdbKey validates, persists, and hot-reloads the TMDB API key.
func (s *SetupService) SetTmdbKey(ctx context.Context, apiKey string) error {
	if err := ValidateTmdbKey(apiKey); err != nil {
		return err
	}
	if err := s.settings.Set(ctx, "tmdb.api_key", apiKey); err != nil {
		return err
	}
	return nil
}

// Initialize performs the one-time setup operation atomically:
// validates input, hashes the password, then defers to repo.InitializeSystem
// for the transactional create-admin / assign-role / mark-initialized work.
func (s *SetupService) Initialize(ctx context.Context, email, username, password string) error {
	// Validate input — collect every field problem before returning.
	email = strings.TrimSpace(email)
	username = strings.TrimSpace(username)

	var fields []apperrors.FieldError
	if email == "" {
		fields = append(fields, apperrors.Field("body.email", "required"))
	}
	if username == "" {
		fields = append(fields, apperrors.Field("body.username", "required"))
	}
	if password == "" {
		fields = append(fields, apperrors.Field("body.password", "required"))
	} else if len(password) < 8 {
		fields = append(fields, apperrors.Field("body.password", "must be at least 8 characters"))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid setup input", fields...).
			Op("SetupService.Initialize")
	}

	// Hash password
	passwordHash, err := pw.Hash(password)
	if err != nil {
		return apperrors.Internalf("failed to hash password: %v", err).Op("SetupService.Initialize")
	}

	// Repo runs the transactional bootstrap. Conflict / NotFound are already
	// typed by the repo; pass through unchanged.
	return s.repo.InitializeSystem(ctx, email, username, passwordHash)
}
