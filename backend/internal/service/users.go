package service

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	pw "github.com/kyleaupton/arrflix/internal/password"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type UsersService struct {
	repo *repo.Repository
}

func NewUsersService(r *repo.Repository) *UsersService {
	return &UsersService{repo: r}
}

// List returns all users with their roles
func (s *UsersService) List(ctx context.Context) ([]dbgen.ListUsersRow, error) {
	return s.repo.ListUsers(ctx)
}

// Get returns a single user with roles
func (s *UsersService) Get(ctx context.Context, id pgtype.UUID) (dbgen.GetUserRow, error) {
	return s.repo.GetUser(ctx, id)
}

// Create creates a new user with password and role assignment
func (s *UsersService) Create(ctx context.Context, email, username, password string, roleName string, isActive bool) (dbgen.AppUser, error) {
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
	if len(fields) > 0 {
		return dbgen.AppUser{}, apperrors.Validation("invalid user", fields...).Op("UsersService.Create")
	}

	if roleName == "" {
		roleName = "user" // Default to 'user' role
	}

	// Hash password
	passwordHash, err := pw.Hash(password)
	if err != nil {
		return dbgen.AppUser{}, apperrors.Internalf("failed to hash password: %v", err).Op("UsersService.Create")
	}

	// Create user. Repo translates unique violation to Conflict.
	user, err := s.repo.CreateUser(ctx, email, username, passwordHash, isActive)
	if err != nil {
		return dbgen.AppUser{}, err
	}

	// Assign role
	role, err := s.repo.GetRoleByName(ctx, roleName)
	if err != nil {
		// User created but role assignment failed - still return user
		return user, nil
	}

	_ = s.repo.AssignRole(ctx, user.ID, role.ID)

	return user, nil
}

// Update updates user fields (not password)
func (s *UsersService) Update(ctx context.Context, id pgtype.UUID, email, username string, isActive bool) (dbgen.AppUser, error) {
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
		return dbgen.AppUser{}, apperrors.Validation("invalid user", fields...).Op("UsersService.Update")
	}

	return s.repo.UpdateUser(ctx, id, email, username, isActive)
}

// UpdatePassword changes a user's password
func (s *UsersService) UpdatePassword(ctx context.Context, userID pgtype.UUID, newPassword string) error {
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
func (s *UsersService) AssignRole(ctx context.Context, userID pgtype.UUID, roleName string) error {
	role, err := s.repo.GetRoleByName(ctx, roleName)
	if err != nil {
		// Repo's NotFound for the role uses the role's name; surface that.
		return err
	}

	// Unassign all existing roles first
	if err := s.repo.UnassignAllRoles(ctx, userID); err != nil {
		return err
	}

	return s.repo.AssignRole(ctx, userID, role.ID)
}

// Delete removes a user
func (s *UsersService) Delete(ctx context.Context, id pgtype.UUID) error {
	// Check if this is the last admin
	roles, err := s.repo.ListUserRoles(ctx, id)
	if err == nil {
		for _, role := range roles {
			if role.Name == "admin" {
				// Count admins
				adminRole, _ := s.repo.GetRoleByName(ctx, "admin")
				count, _ := s.repo.CountUsersByRole(ctx, adminRole.ID)
				if count <= 1 {
					return apperrors.Conflictf("cannot delete the last admin user").
						Op("UsersService.Delete")
				}
			}
		}
	}

	return s.repo.DeleteUser(ctx, id)
}

// ListRoles returns all available roles
func (s *UsersService) ListRoles(ctx context.Context) ([]dbgen.Role, error) {
	return s.repo.ListRoles(ctx)
}
