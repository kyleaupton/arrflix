package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/authz"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	pw "github.com/kyleaupton/arrflix/internal/password"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type UsersService struct {
	repo     *repo.Repository
	authz    *AuthzService
	sessions *SessionService
}

func NewUsersService(r *repo.Repository, authz *AuthzService, sessions *SessionService) *UsersService {
	return &UsersService{repo: r, authz: authz, sessions: sessions}
}

// List returns all users with their roles
func (s *UsersService) List(ctx context.Context) ([]model.User, error) {
	return s.repo.ListUsers(ctx)
}

// Get returns a single user with roles
func (s *UsersService) Get(ctx context.Context, id uuid.UUID) (model.User, error) {
	return s.repo.GetUser(ctx, id)
}

// Create creates a new user with password and role assignment. The user starts
// with both request settings off.
func (s *UsersService) Create(ctx context.Context, email, username, password string, roleName string, isActive bool) (model.User, error) {
	return s.create(ctx, email, username, password, roleName, isActive, model.RequestSettings{}, "UsersService.Create")
}

// CreateFromInvite creates the active user an invite provisions: the invite's
// email, role and request settings, all written in one transaction.
func (s *UsersService) CreateFromInvite(ctx context.Context, invite model.Invite, username, password string) (model.User, error) {
	settings := model.RequestSettings{AutoApprove: invite.AutoApprove, Allow4k: invite.Allow4k}
	return s.create(ctx, invite.Email, username, password, invite.Role, true, settings, "UsersService.CreateFromInvite")
}

func (s *UsersService) create(ctx context.Context, email, username, password, roleName string, isActive bool, settings model.RequestSettings, op string) (model.User, error) {
	// Validation — collect every field problem before returning.
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
	if roleName == "" {
		roleName = model.RoleRequester
	}
	if !model.IsAssignableRole(roleName) {
		fields = append(fields, apperrors.Field("body.role", "must be one of: requester, admin"))
	}
	if len(fields) > 0 {
		return model.User{}, apperrors.Validation("invalid user", fields...).Op(op)
	}

	passwordHash, err := pw.Hash(password)
	if err != nil {
		return model.User{}, apperrors.Internalf("failed to hash password: %v", err).Op(op)
	}

	// The user, their role and their request settings are written together so a
	// failed role assignment never leaves a user with no role. Repo translates unique violation to Conflict.
	var user model.User
	err = s.repo.InTx(ctx, func(tx *repo.Repository) error {
		role, err := tx.GetRoleByName(ctx, roleName)
		if err != nil {
			return err
		}
		user, err = tx.CreateUser(ctx, repo.CreateUserParams{
			Email:        email,
			Username:     username,
			PasswordHash: passwordHash,
			IsActive:     isActive,
		})
		if err != nil {
			return err
		}
		if err := tx.AssignRole(ctx, user.ID, role.ID); err != nil {
			return err
		}
		return tx.ReplaceUserGrants(ctx, user.ID, authz.RequestSettingsKeys(), authz.RequestSettingsGrants(settings))
	})
	if err != nil {
		return model.User{}, err
	}
	user.AutoApprove, user.Allow4k = settings.AutoApprove, settings.Allow4k
	return user, nil
}

// Update updates user fields (not password)
func (s *UsersService) Update(ctx context.Context, id uuid.UUID, email, username string, isActive bool) (model.User, error) {
	email = strings.TrimSpace(email)
	username = strings.TrimSpace(username)

	var fields []apperrors.FieldError
	if email == "" {
		fields = append(fields, apperrors.Field("body.email", "required"))
	}
	if username == "" {
		fields = append(fields, apperrors.Field("body.username", "required"))
	}
	if len(fields) > 0 {
		return model.User{}, apperrors.Validation("invalid user", fields...).Op("UsersService.Update")
	}

	// Saving a user as inactive revokes every session in the same transaction, so
	// a disabled user can't refresh their way back in. Re-enabling restores
	// nothing: the old sessions stay revoked and the user signs in again.
	var (
		user    model.User
		revoked int64
	)
	err := s.withLastAdminGuard(ctx, "cannot deactivate the last active admin", "UsersService.Update", func(r *repo.Repository) error {
		var err error
		user, err = r.UpdateUser(ctx, repo.UpdateUserParams{
			ID:       id,
			Email:    email,
			Username: username,
			IsActive: isActive,
		})
		if err != nil {
			return err
		}
		if !isActive {
			revoked, err = r.RevokeAllSessionsForUser(ctx, id)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.User{}, err
	}
	// Audited after commit: the write is best-effort, and a failed insert inside
	// the transaction would abort the disable it records.
	if revoked > 0 {
		s.sessions.audit(ctx, id, auditSessionRevoked, auditDetail{Scope: "disabled", Count: revoked})
	}
	return user, nil
}

// UpdatePassword changes a user's password
func (s *UsersService) UpdatePassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	var fields []apperrors.FieldError
	if newPassword == "" {
		fields = append(fields, apperrors.Field("body.password", "required"))
	} else if len(newPassword) < 8 {
		fields = append(fields, apperrors.Field("body.password", "must be at least 8 characters"))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid password", fields...).Op("UsersService.UpdatePassword")
	}

	hash, err := pw.Hash(newPassword)
	if err != nil {
		return apperrors.Internalf("failed to hash password: %v", err).Op("UsersService.UpdatePassword")
	}

	return s.repo.UpdateUserPassword(ctx, userID, hash)
}

// AssignRole assigns a single role to a user (replaces existing roles)
func (s *UsersService) AssignRole(ctx context.Context, userID uuid.UUID, roleName string) error {
	role, err := s.repo.GetRoleByName(ctx, roleName)
	if err != nil {
		// Repo's NotFound for the role uses the role's name; surface that.
		return err
	}

	err = s.withLastAdminGuard(ctx, "cannot demote the last active admin", "UsersService.AssignRole", func(tx *repo.Repository) error {
		if err := tx.UnassignAllRoles(ctx, userID); err != nil {
			return err
		}
		return tx.AssignRole(ctx, userID, role.ID)
	})
	if err != nil {
		return err
	}

	// Roles changed → the user's cached grant set is stale. The user's request
	// settings are user-subject grants, so they survive the role change: inert
	// under admin, in force again under requester.
	s.authz.Bust(userID)

	return nil
}

// SetRequestSettings replaces the user's request settings. The settings are
// stored only as the user's own grants (authz.RequestSettingsGrants), swapped in
// one transaction; role grants are untouched. Requests already made are not
// revisited: turning auto-approve on leaves queued requests queued.
func (s *UsersService) SetRequestSettings(ctx context.Context, userID uuid.UUID, settings model.RequestSettings) (model.User, error) {
	err := s.repo.InTx(ctx, func(tx *repo.Repository) error {
		if _, err := tx.GetUserByID(ctx, userID); err != nil {
			return err
		}
		return tx.ReplaceUserGrants(ctx, userID, authz.RequestSettingsKeys(), authz.RequestSettingsGrants(settings))
	})
	if err != nil {
		return model.User{}, err
	}
	s.authz.Bust(userID)
	return s.repo.GetUser(ctx, userID)
}

// Delete removes a user
func (s *UsersService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.withLastAdminGuard(ctx, "cannot delete the last active admin", "UsersService.Delete", func(tx *repo.Repository) error {
		// User-subject grants have no foreign key to the user; drop them with it.
		if err := tx.ReplaceUserGrants(ctx, id, authz.RequestSettingsKeys(), nil); err != nil {
			return err
		}
		return tx.DeleteUser(ctx, id)
	})
}

// withLastAdminGuard runs change in a transaction and refuses it as a Conflict
// when it takes the install from at least one active admin to none. Every
// path that can remove an active admin goes through here: the admin role row
// is locked first, so concurrent guarded changes serialize and each one counts
// the others' committed result rather than a stale snapshot.
func (s *UsersService) withLastAdminGuard(ctx context.Context, refusal, op string, change func(tx *repo.Repository) error) error {
	return s.repo.InTx(ctx, func(tx *repo.Repository) error {
		admin, err := tx.LockRoleByName(ctx, model.RoleAdmin)
		if err != nil {
			return err
		}
		before, err := tx.CountActiveUsersByRole(ctx, admin.ID)
		if err != nil {
			return err
		}
		if err := change(tx); err != nil {
			return err
		}
		after, err := tx.CountActiveUsersByRole(ctx, admin.ID)
		if err != nil {
			return err
		}
		if before > 0 && after == 0 {
			return apperrors.Conflictf("%s", refusal).Op(op)
		}
		return nil
	})
}

// ListRoles returns all available roles
func (s *UsersService) ListRoles(ctx context.Context) ([]model.Role, error) {
	return s.repo.ListRoles(ctx)
}
