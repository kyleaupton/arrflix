package service

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
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
// 1. Check initialization status
// 2. Create admin user
// 3. Assign admin role
// 4. Mark system as initialized
// All operations occur in a single transaction for atomicity.
func (s *SetupService) Initialize(ctx context.Context, email, username, password string) error {
	// Start transaction for atomicity
	tx, err := s.repo.Pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.Serializable, // Highest isolation to prevent concurrent setup
	})
	if err != nil {
		return apperrors.Internalf("begin tx: %v", err).Op("SetupService.Initialize")
	}
	defer tx.Rollback(ctx)

	// Check if already initialized (within transaction)
	txQueries := s.repo.Q.WithTx(tx)
	initialized, err := txQueries.GetSystemInitialized(ctx)
	if err != nil {
		return apperrors.Internalf("check initialized: %v", err).Op("SetupService.Initialize")
	}
	if initialized {
		return apperrors.Conflictf("system already initialized").
			Op("SetupService.Initialize")
	}

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

	// Create admin user within transaction
	user, err := txQueries.CreateUser(ctx, dbgen.CreateUserParams{
		Email:        &email,
		Username:     username,
		PasswordHash: &passwordHash,
		IsActive:     true,
	})
	if err != nil {
		// Direct SQLC call (in-tx) doesn't go through repo, so route the
		// pg error through FromPg here so the wire response stays consistent.
		return apperrors.FromPg(err, "create admin user %q", email)
	}

	// Assign admin role within transaction
	role, err := txQueries.GetRoleByName(ctx, "admin")
	if err != nil {
		return apperrors.FromPg(err, "admin role not found")
	}
	if err := txQueries.AssignRole(ctx, dbgen.AssignRoleParams{
		UserID: user.ID,
		RoleID: role.ID,
	}); err != nil {
		return apperrors.FromPg(err, "assign admin role to user %s", user.ID)
	}

	// Mark system as initialized (conditional update prevents double-init)
	if err := txQueries.SetSystemInitialized(ctx); err != nil {
		return apperrors.FromPg(err, "mark system initialized")
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return apperrors.Internalf("commit setup tx: %v", err).Op("SetupService.Initialize")
	}

	return nil
}
