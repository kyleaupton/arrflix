package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	pw "github.com/kyleaupton/arrflix/internal/password"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type AuthService struct {
	repo      *repo.Repository
	jwtSecret string
	settings  *SettingsService
	invites   *InvitesService
}

func NewAuthService(r *repo.Repository, cfg *cfg, settings *SettingsService, invites *InvitesService) *AuthService {
	return &AuthService{repo: r, jwtSecret: cfg.jwtSecret, settings: settings, invites: invites}
}

// IssueToken generates an identity-only access token (no session binding). The
// session-backed login flow signs its access tokens through SessionService; this
// remains for the test harness, which mints a bearer without opening a session.
func (s *AuthService) IssueToken(userID uuid.UUID, email *string, username string) (string, error) {
	signed, _, err := signAccessToken(s.jwtSecret, userID, email, username, nil, 24*time.Hour)
	if err != nil {
		return "", apperrors.Internalf("sign token: %v", err).Op("AuthService.IssueToken")
	}
	return signed, nil
}

// Login verifies credentials and returns the authenticated user. The caller opens
// a session (SessionService.Create) to mint the access + refresh tokens.
func (s *AuthService) Login(ctx context.Context, login, password string) (model.User, error) {
	user, err := s.repo.GetUserByLogin(ctx, login)
	if err != nil {
		// Don't leak whether the username exists; surface a generic
		// invalid-credentials response. Original error stays in logs via slog.
		return model.User{}, apperrors.Unauthenticatedf("invalid credentials").
			Op("AuthService.Login")
	}

	ok, err := pw.Verify(password, deref(user.PasswordHash))
	if err != nil || !ok {
		return model.User{}, apperrors.Unauthenticatedf("invalid credentials").
			Op("AuthService.Login")
	}

	// Opportunistically upgrade hash format/cost
	if pw.NeedsRehash(deref(user.PasswordHash)) {
		if newHash, err := pw.Hash(password); err == nil {
			_ = s.repo.UpdateUserPassword(ctx, user.ID, newHash)
		}
	}

	return user, nil
}

// LoginWithPlex handles the Plex SSO login flow. It finds or creates a user based
// on the Plex identity, respecting the signup strategy for new users, and returns
// the authenticated user for the caller to open a session against.
func (s *AuthService) LoginWithPlex(ctx context.Context, plexSubject, email, username, plexToken string, raw json.RawMessage) (model.User, error) {
	// Check if this Plex identity already exists (returning user)
	identity, err := s.repo.GetIdentityByProviderSubject(ctx, model.AuthProviderPlex, plexSubject)
	if err == nil {
		// Returning user — get their account
		user, err := s.repo.GetUserByID(ctx, identity.UserID)
		if err != nil {
			return model.User{}, err
		}
		if !user.IsActive {
			return model.User{}, apperrors.Forbiddenf("account is disabled").
				Op("AuthService.LoginWithPlex")
		}

		// Update identity with fresh token
		s.upsertIdentity(ctx, user.ID, plexSubject, username, plexToken, raw)

		return user, nil
	}

	// Check if an existing local account matches the Plex email
	existingUser, err := s.repo.GetUserByEmail(ctx, email)
	if err == nil {
		// Existing active user found — link Plex identity to their account
		s.upsertIdentity(ctx, existingUser.ID, plexSubject, username, plexToken, raw)
		return existingUser, nil
	}

	// New user — check signup strategy
	all, err := s.settings.GetAll(ctx)
	if err != nil {
		return model.User{}, err
	}
	strategy, _ := all["auth.signup_strategy"].(string)
	if strategy == "" {
		strategy = "invite_only"
	}

	if strategy == "invite_only" {
		if err := s.invites.CheckAndClaim(ctx, email); err != nil {
			// CheckAndClaim already returns a typed error.
			return model.User{}, err
		}
	}

	// Create user without password. Repo translates unique violations to Conflict.
	user, err := s.repo.CreateUserNoPassword(ctx, email, username, true)
	if err != nil {
		return model.User{}, err
	}

	// Assign default role
	role, err := s.repo.GetRoleByName(ctx, "requester")
	if err == nil {
		_ = s.repo.AssignRole(ctx, user.ID, role.ID)
	}

	// Link Plex identity
	s.upsertIdentity(ctx, user.ID, plexSubject, username, plexToken, raw)

	return user, nil
}

func (s *AuthService) upsertIdentity(ctx context.Context, userID uuid.UUID, plexSubject, username, plexToken string, raw json.RawMessage) {
	_, _ = s.repo.UpsertIdentity(ctx, repo.UpsertIdentityParams{
		UserID:      userID,
		Provider:    model.AuthProviderPlex,
		Subject:     plexSubject,
		Username:    &username,
		AccessToken: &plexToken,
		Raw:         raw,
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
