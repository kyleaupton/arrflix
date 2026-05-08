package repo

import (
	"context"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type SetupRepo interface {
	GetSystemInitialized(ctx context.Context) (bool, error)
	SetSystemInitialized(ctx context.Context) error
	CountUsers(ctx context.Context) (int64, error)
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
