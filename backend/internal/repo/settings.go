package repo

import (
	"context"

	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type SettingsRepo interface {
	Get(ctx context.Context, key string) (dbgen.AppSetting, error)
	List(ctx context.Context) ([]dbgen.AppSetting, error)
	Upsert(ctx context.Context, key, typ string, valueJson []byte) error
}

func (r *Repository) Get(ctx context.Context, key string) (dbgen.AppSetting, error) {
	s, err := r.Q.GetSetting(ctx, key)
	return s, apperrors.FromPg(err, "setting %q not found", key)
}

func (r *Repository) List(ctx context.Context) ([]dbgen.AppSetting, error) {
	settings, err := r.Q.ListSettings(ctx)
	return settings, apperrors.FromPg(err, "list settings")
}

func (r *Repository) Upsert(ctx context.Context, key, typ string, valueJson []byte) error {
	return apperrors.FromPg(r.Q.UpsertSetting(ctx, dbgen.UpsertSettingParams{Key: key, Type: typ, ValueJson: valueJson}), "upsert setting %q", key)
}
