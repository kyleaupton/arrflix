package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type AuthRepo interface {
	GetUserByEmail(ctx context.Context, email string) (dbgen.AppUser, error)
	GetUserByLogin(ctx context.Context, login string) (dbgen.AppUser, error)
	GetUserByID(ctx context.Context, id pgtype.UUID) (dbgen.AppUser, error)
	UpdateUserPassword(ctx context.Context, userID pgtype.UUID, newHash string) error
	// User CRUD
	ListUsers(ctx context.Context) ([]dbgen.ListUsersRow, error)
	GetUser(ctx context.Context, id pgtype.UUID) (dbgen.GetUserRow, error)
	CreateUser(ctx context.Context, email, username, passwordHash string, isActive bool) (dbgen.AppUser, error)
	CreateUserNoPassword(ctx context.Context, email, username string, isActive bool) (dbgen.AppUser, error)
	UpdateUser(ctx context.Context, id pgtype.UUID, email, username string, isActive bool) (dbgen.AppUser, error)
	DeleteUser(ctx context.Context, id pgtype.UUID) error
	// Role Management
	ListRoles(ctx context.Context) ([]dbgen.Role, error)
	ListUserRoles(ctx context.Context, userID pgtype.UUID) ([]dbgen.Role, error)
	GetRoleByName(ctx context.Context, name string) (dbgen.Role, error)
	AssignRole(ctx context.Context, userID, roleID pgtype.UUID) error
	UnassignAllRoles(ctx context.Context, userID pgtype.UUID) error
	CountUsersByRole(ctx context.Context, roleID pgtype.UUID) (int64, error)
	// Identity
	GetIdentityByProviderSubject(ctx context.Context, provider dbgen.AuthProvider, subject string) (dbgen.UserIdentity, error)
	UpsertIdentity(ctx context.Context, params dbgen.UpsertIdentityParams) (dbgen.UserIdentity, error)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (dbgen.AppUser, error) {
	u, err := r.Q.GetUserByEmail(ctx, email)
	return u, apperrors.FromPg(err, "user %q not found", email)
}

func (r *Repository) GetUserByLogin(ctx context.Context, login string) (dbgen.AppUser, error) {
	u, err := r.Q.GetUserByLogin(ctx, login)
	return u, apperrors.FromPg(err, "user %q not found", login)
}

func (r *Repository) UpdateUserPassword(ctx context.Context, userID pgtype.UUID, newHash string) error {
	return apperrors.FromPg(r.Q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{ID: userID, PasswordHash: &newHash}), "update password for user %s", userID)
}

// User CRUD implementations

func (r *Repository) ListUsers(ctx context.Context) ([]dbgen.ListUsersRow, error) {
	users, err := r.Q.ListUsers(ctx)
	return users, apperrors.FromPg(err, "list users")
}

func (r *Repository) GetUser(ctx context.Context, id pgtype.UUID) (dbgen.GetUserRow, error) {
	u, err := r.Q.GetUser(ctx, id)
	return u, apperrors.FromPg(err, "user %s not found", id)
}

func (r *Repository) CreateUser(ctx context.Context, email, username, passwordHash string, isActive bool) (dbgen.AppUser, error) {
	u, err := r.Q.CreateUser(ctx, dbgen.CreateUserParams{
		Email:        &email,
		Username:     username,
		PasswordHash: &passwordHash,
		IsActive:     isActive,
	})
	return u, apperrors.FromPg(err, "create user %q", username)
}

func (r *Repository) UpdateUser(ctx context.Context, id pgtype.UUID, email, username string, isActive bool) (dbgen.AppUser, error) {
	u, err := r.Q.UpdateUser(ctx, dbgen.UpdateUserParams{
		ID:       id,
		Email:    &email,
		Username: username,
		IsActive: isActive,
	})
	return u, apperrors.FromPg(err, "update user %s", id)
}

func (r *Repository) DeleteUser(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteUser(ctx, id), "delete user %s", id)
}

// Role Management implementations

func (r *Repository) ListRoles(ctx context.Context) ([]dbgen.Role, error) {
	roles, err := r.Q.ListRoles(ctx)
	return roles, apperrors.FromPg(err, "list roles")
}

func (r *Repository) ListUserRoles(ctx context.Context, userID pgtype.UUID) ([]dbgen.Role, error) {
	roles, err := r.Q.ListUserRoles(ctx, userID)
	return roles, apperrors.FromPg(err, "list roles for user %s", userID)
}

func (r *Repository) GetRoleByName(ctx context.Context, name string) (dbgen.Role, error) {
	role, err := r.Q.GetRoleByName(ctx, name)
	return role, apperrors.FromPg(err, "role %q not found", name)
}

func (r *Repository) AssignRole(ctx context.Context, userID, roleID pgtype.UUID) error {
	return apperrors.FromPg(r.Q.AssignRole(ctx, dbgen.AssignRoleParams{
		UserID: userID,
		RoleID: roleID,
	}), "assign role %s to user %s", roleID, userID)
}

func (r *Repository) UnassignAllRoles(ctx context.Context, userID pgtype.UUID) error {
	return apperrors.FromPg(r.Q.UnassignAllRoles(ctx, userID), "unassign roles for user %s", userID)
}

func (r *Repository) CountUsersByRole(ctx context.Context, roleID pgtype.UUID) (int64, error) {
	count, err := r.Q.CountUsersByRole(ctx, roleID)
	return count, apperrors.FromPg(err, "count users by role %s", roleID)
}

func (r *Repository) GetUserByID(ctx context.Context, id pgtype.UUID) (dbgen.AppUser, error) {
	u, err := r.Q.GetUserByID(ctx, id)
	return u, apperrors.FromPg(err, "user %s not found", id)
}

func (r *Repository) CreateUserNoPassword(ctx context.Context, email, username string, isActive bool) (dbgen.AppUser, error) {
	u, err := r.Q.CreateUser(ctx, dbgen.CreateUserParams{
		Email:        &email,
		Username:     username,
		PasswordHash: nil,
		IsActive:     isActive,
	})
	return u, apperrors.FromPg(err, "create user %q", username)
}

// Identity implementations

func (r *Repository) GetIdentityByProviderSubject(ctx context.Context, provider dbgen.AuthProvider, subject string) (dbgen.UserIdentity, error) {
	ident, err := r.Q.GetIdentityByProviderSubject(ctx, dbgen.GetIdentityByProviderSubjectParams{
		Provider: provider,
		Subject:  subject,
	})
	return ident, apperrors.FromPg(err, "identity %s/%q not found", provider, subject)
}

func (r *Repository) UpsertIdentity(ctx context.Context, params dbgen.UpsertIdentityParams) (dbgen.UserIdentity, error) {
	ident, err := r.Q.UpsertIdentity(ctx, params)
	return ident, apperrors.FromPg(err, "upsert identity %s/%q", params.Provider, params.Subject)
}
