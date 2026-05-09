package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type AuthRepo interface {
	// User reads
	GetUserByEmail(ctx context.Context, email string) (model.User, error)
	GetUserByLogin(ctx context.Context, login string) (model.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (model.User, error)
	// User CRUD
	ListUsers(ctx context.Context) ([]model.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (model.User, error)
	CreateUser(ctx context.Context, params CreateUserParams) (model.User, error)
	CreateUserNoPassword(ctx context.Context, email, username string, isActive bool) (model.User, error)
	UpdateUser(ctx context.Context, params UpdateUserParams) (model.User, error)
	UpdateUserPassword(ctx context.Context, userID uuid.UUID, newHash string) error
	DeleteUser(ctx context.Context, id uuid.UUID) error
	// Role management
	ListRoles(ctx context.Context) ([]model.Role, error)
	ListUserRoles(ctx context.Context, userID uuid.UUID) ([]model.Role, error)
	GetRoleByName(ctx context.Context, name string) (model.Role, error)
	AssignRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error
	UnassignAllRoles(ctx context.Context, userID uuid.UUID) error
	CountUsersByRole(ctx context.Context, roleID uuid.UUID) (int64, error)
	// Identity
	GetIdentityByProviderSubject(ctx context.Context, provider model.AuthProvider, subject string) (model.Identity, error)
	UpsertIdentity(ctx context.Context, params UpsertIdentityParams) (model.Identity, error)
}

// CreateUserParams is the domain-shaped input for CreateUser. Mirrors the
// writeable subset of model.User (omits server-managed ID/CreatedAt/UpdatedAt
// and the role assignments tracked on the join table).
type CreateUserParams struct {
	Email        string
	Username     string
	PasswordHash string
	IsActive     bool
}

// UpdateUserParams is the domain-shaped input for UpdateUser. Mirrors the
// writeable subset of model.User plus the target ID. Password is updated via
// UpdateUserPassword, not here.
type UpdateUserParams struct {
	ID       uuid.UUID
	Email    string
	Username string
	IsActive bool
}

// UpsertIdentityParams is the domain-shaped parameter struct for UpsertIdentity.
// It mirrors dbgen.UpsertIdentityParams but uses idiomatic Go types so service
// callers don't import dbgen.
type UpsertIdentityParams struct {
	UserID         uuid.UUID
	Provider       model.AuthProvider
	Subject        string
	Username       *string
	AccessToken    *string
	RefreshToken   *string
	TokenExpiresAt *time.Time
	Raw            json.RawMessage
}

// toModelRole translates the persistence-shaped dbgen.Role into the
// domain-shaped model.Role.
func toModelRole(row dbgen.Role) model.Role {
	return model.Role{
		ID:          uuidFromPgtype(row.ID),
		Name:        row.Name,
		Description: row.Description,
		BuiltIn:     row.BuiltIn,
		CreatedAt:   row.CreatedAt,
	}
}

// rolesFromAggregate decodes the json_agg-produced "roles" column into a
// []model.Role. The SQL emits role objects with id/name/description (no
// built_in or created_at — those stay zero-valued in the slice). The column
// arrives as either a []byte (raw JSONB) or nil; both are handled.
func rolesFromAggregate(raw interface{}) []model.Role {
	if raw == nil {
		return nil
	}
	b, ok := raw.([]byte)
	if !ok {
		return nil
	}
	if len(b) == 0 {
		return nil
	}
	// json_agg with the FILTER guard returns "[]" when no rows match; treat
	// that as the empty slice so callers don't need to special-case it.
	type rolePayload struct {
		ID          uuid.UUID `json:"id"`
		Name        string    `json:"name"`
		Description *string   `json:"description"`
	}
	var payload []rolePayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil
	}
	out := make([]model.Role, 0, len(payload))
	for _, p := range payload {
		out = append(out, model.Role{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
		})
	}
	return out
}

// toModelUser translates a dbgen.AppUser plus an optional resolved roles slice
// into the domain-shaped model.User. Callers that only have the bare row pass
// nil for roles; callers that have aggregated roles via json_agg use the
// fan-in helpers (toModelUserFromGetRow, toModelUserFromListRow) instead.
func toModelUser(row dbgen.AppUser, roles []model.Role) model.User {
	return model.User{
		ID:           uuidFromPgtype(row.ID),
		Email:        row.Email,
		Username:     row.Username,
		AvatarURL:    row.AvatarUrl,
		IsActive:     row.IsActive,
		Roles:        roles,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		PasswordHash: row.PasswordHash,
	}
}

func toModelUserFromGetRow(row dbgen.GetUserRow) model.User {
	return model.User{
		ID:           uuidFromPgtype(row.ID),
		Email:        row.Email,
		Username:     row.Username,
		AvatarURL:    row.AvatarUrl,
		IsActive:     row.IsActive,
		Roles:        rolesFromAggregate(row.Roles),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		PasswordHash: row.PasswordHash,
	}
}

func toModelUserFromListRow(row dbgen.ListUsersRow) model.User {
	return model.User{
		ID:           uuidFromPgtype(row.ID),
		Email:        row.Email,
		Username:     row.Username,
		AvatarURL:    row.AvatarUrl,
		IsActive:     row.IsActive,
		Roles:        rolesFromAggregate(row.Roles),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		PasswordHash: row.PasswordHash,
	}
}

// toModelIdentity translates the persistence-shaped dbgen.UserIdentity into
// the domain-shaped model.Identity.
func toModelIdentity(row dbgen.UserIdentity) model.Identity {
	var expiresAt *time.Time
	if row.TokenExpiresAt.Valid {
		t := row.TokenExpiresAt.Time
		expiresAt = &t
	}
	return model.Identity{
		ID:             uuidFromPgtype(row.ID),
		UserID:         uuidFromPgtype(row.UserID),
		Provider:       model.AuthProvider(row.Provider),
		Subject:        row.Subject,
		Username:       row.Username,
		AccessToken:    row.AccessToken,
		RefreshToken:   row.RefreshToken,
		TokenExpiresAt: expiresAt,
		Raw:            json.RawMessage(row.Raw),
		CreatedAt:      row.CreatedAt,
	}
}

// User read methods

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (model.User, error) {
	u, err := r.Q.GetUserByEmail(ctx, email)
	if err != nil {
		return model.User{}, apperrors.FromPg(err, "user %q not found", email)
	}
	return toModelUser(u, nil), nil
}

func (r *Repository) GetUserByLogin(ctx context.Context, login string) (model.User, error) {
	u, err := r.Q.GetUserByLogin(ctx, login)
	if err != nil {
		return model.User{}, apperrors.FromPg(err, "user %q not found", login)
	}
	return toModelUser(u, nil), nil
}

func (r *Repository) GetUserByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	u, err := r.Q.GetUserByID(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.User{}, apperrors.FromPg(err, "user %s not found", id)
	}
	return toModelUser(u, nil), nil
}

// User CRUD

func (r *Repository) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := r.Q.ListUsers(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list users")
	}
	out := make([]model.User, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelUserFromListRow(row))
	}
	return out, nil
}

func (r *Repository) GetUser(ctx context.Context, id uuid.UUID) (model.User, error) {
	row, err := r.Q.GetUser(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.User{}, apperrors.FromPg(err, "user %s not found", id)
	}
	return toModelUserFromGetRow(row), nil
}

func (r *Repository) CreateUser(ctx context.Context, params CreateUserParams) (model.User, error) {
	row, err := r.Q.CreateUser(ctx, dbgen.CreateUserParams{
		Email:        &params.Email,
		Username:     params.Username,
		PasswordHash: &params.PasswordHash,
		IsActive:     params.IsActive,
	})
	if err != nil {
		return model.User{}, apperrors.FromPg(err, "create user %q", params.Username)
	}
	return toModelUser(row, nil), nil
}

func (r *Repository) CreateUserNoPassword(ctx context.Context, email, username string, isActive bool) (model.User, error) {
	row, err := r.Q.CreateUser(ctx, dbgen.CreateUserParams{
		Email:        &email,
		Username:     username,
		PasswordHash: nil,
		IsActive:     isActive,
	})
	if err != nil {
		return model.User{}, apperrors.FromPg(err, "create user %q", username)
	}
	return toModelUser(row, nil), nil
}

func (r *Repository) UpdateUser(ctx context.Context, params UpdateUserParams) (model.User, error) {
	row, err := r.Q.UpdateUser(ctx, dbgen.UpdateUserParams{
		ID:       pgtypeFromUUID(params.ID),
		Email:    &params.Email,
		Username: params.Username,
		IsActive: params.IsActive,
	})
	if err != nil {
		return model.User{}, apperrors.FromPg(err, "update user %s", params.ID)
	}
	return toModelUser(row, nil), nil
}

func (r *Repository) UpdateUserPassword(ctx context.Context, userID uuid.UUID, newHash string) error {
	return apperrors.FromPg(r.Q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
		ID:           pgtypeFromUUID(userID),
		PasswordHash: &newHash,
	}), "update password for user %s", userID)
}

func (r *Repository) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteUser(ctx, pgtypeFromUUID(id)), "delete user %s", id)
}

// Role management

func (r *Repository) ListRoles(ctx context.Context) ([]model.Role, error) {
	rows, err := r.Q.ListRoles(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list roles")
	}
	out := make([]model.Role, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelRole(row))
	}
	return out, nil
}

func (r *Repository) ListUserRoles(ctx context.Context, userID uuid.UUID) ([]model.Role, error) {
	rows, err := r.Q.ListUserRoles(ctx, pgtypeFromUUID(userID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list roles for user %s", userID)
	}
	out := make([]model.Role, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelRole(row))
	}
	return out, nil
}

func (r *Repository) GetRoleByName(ctx context.Context, name string) (model.Role, error) {
	row, err := r.Q.GetRoleByName(ctx, name)
	if err != nil {
		return model.Role{}, apperrors.FromPg(err, "role %q not found", name)
	}
	return toModelRole(row), nil
}

func (r *Repository) AssignRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error {
	return apperrors.FromPg(r.Q.AssignRole(ctx, dbgen.AssignRoleParams{
		UserID: pgtypeFromUUID(userID),
		RoleID: pgtypeFromUUID(roleID),
	}), "assign role %s to user %s", roleID, userID)
}

func (r *Repository) UnassignAllRoles(ctx context.Context, userID uuid.UUID) error {
	return apperrors.FromPg(r.Q.UnassignAllRoles(ctx, pgtypeFromUUID(userID)), "unassign roles for user %s", userID)
}

func (r *Repository) CountUsersByRole(ctx context.Context, roleID uuid.UUID) (int64, error) {
	count, err := r.Q.CountUsersByRole(ctx, pgtypeFromUUID(roleID))
	return count, apperrors.FromPg(err, "count users by role %s", roleID)
}

// Identity

func (r *Repository) GetIdentityByProviderSubject(ctx context.Context, provider model.AuthProvider, subject string) (model.Identity, error) {
	ident, err := r.Q.GetIdentityByProviderSubject(ctx, dbgen.GetIdentityByProviderSubjectParams{
		Provider: dbgen.AuthProvider(provider),
		Subject:  subject,
	})
	if err != nil {
		return model.Identity{}, apperrors.FromPg(err, "identity %s/%q not found", provider, subject)
	}
	return toModelIdentity(ident), nil
}

func (r *Repository) UpsertIdentity(ctx context.Context, params UpsertIdentityParams) (model.Identity, error) {
	var expires pgtype.Timestamptz
	if params.TokenExpiresAt != nil {
		expires = pgtype.Timestamptz{Time: *params.TokenExpiresAt, Valid: true}
	}
	var raw interface{}
	if len(params.Raw) > 0 {
		raw = []byte(params.Raw)
	}
	ident, err := r.Q.UpsertIdentity(ctx, dbgen.UpsertIdentityParams{
		UserID:         pgtypeFromUUID(params.UserID),
		Provider:       dbgen.AuthProvider(params.Provider),
		Subject:        params.Subject,
		Username:       params.Username,
		AccessToken:    params.AccessToken,
		RefreshToken:   params.RefreshToken,
		TokenExpiresAt: expires,
		Column8:        raw,
	})
	if err != nil {
		return model.Identity{}, apperrors.FromPg(err, "upsert identity %s/%q", params.Provider, params.Subject)
	}
	return toModelIdentity(ident), nil
}

