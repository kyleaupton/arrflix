package repo

import (
	"context"

	"github.com/jackc/pgx/v5"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type SetupRepo interface {
	GetSystemInitialized(ctx context.Context) (bool, error)
	SetSystemInitialized(ctx context.Context) error
	CountUsers(ctx context.Context) (int64, error)
	InitializeSystem(ctx context.Context, email, username, passwordHash string) error
}

func (r *Repository) GetSystemInitialized(ctx context.Context) (bool, error) {
	ok, err := r.Q.GetSystemInitialized(ctx)
	return ok, apperrors.FromPg(err, "get system initialized")
}

func (r *Repository) SetSystemInitialized(ctx context.Context) error {
	return apperrors.FromPg(r.Q.SetSystemInitialized(ctx), "set system initialized")
}

func (r *Repository) CountUsers(ctx context.Context) (int64, error) {
	count, err := r.Q.CountUsers(ctx)
	return count, apperrors.FromPg(err, "count users")
}

// InitializeSystem performs the one-time setup transaction:
//   - creates the admin user
//   - assigns the admin role
//   - flips the system.initialized flag
//
// All three steps run in a serializable transaction so concurrent setup
// attempts cannot interleave. Validation (caller-side) is the service's
// responsibility; this method assumes its inputs are non-empty.
//
// Returns Conflict if the system is already initialized; NotFound if the
// admin role row is missing; the underlying pg error wrapped via FromPg
// for everything else.
func (r *Repository) InitializeSystem(ctx context.Context, email, username, passwordHash string) error {
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.Serializable,
	})
	if err != nil {
		return apperrors.FromPg(err, "begin setup tx")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.Q.WithTx(tx)

	initialized, err := q.GetSystemInitialized(ctx)
	if err != nil {
		return apperrors.FromPg(err, "check system initialized")
	}
	if initialized {
		return apperrors.Conflictf("system already initialized")
	}

	user, err := q.CreateUser(ctx, dbgen.CreateUserParams{
		Email:        &email,
		Username:     username,
		PasswordHash: &passwordHash,
		IsActive:     true,
	})
	if err != nil {
		return apperrors.FromPg(err, "create admin user %q", email)
	}

	role, err := q.GetRoleByName(ctx, "admin")
	if err != nil {
		return apperrors.FromPg(err, "admin role not found")
	}

	if err := q.AssignRole(ctx, dbgen.AssignRoleParams{
		UserID: user.ID,
		RoleID: role.ID,
	}); err != nil {
		return apperrors.FromPg(err, "assign admin role to user %s", uuidFromPgtype(user.ID))
	}

	// The admin auto-approves via its role grants (requests.auto_approve:*),
	// resolved by the authz layer — no per-user policy row to seed.

	if err := q.SetSystemInitialized(ctx); err != nil {
		return apperrors.FromPg(err, "mark system initialized")
	}

	if err := tx.Commit(ctx); err != nil {
		return apperrors.FromPg(err, "commit setup tx")
	}
	return nil
}
