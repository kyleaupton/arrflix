package repo

import (
	"context"

	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type SettingsRepo interface {
	Get(ctx context.Context, key string) (model.Setting, error)
	List(ctx context.Context) ([]model.Setting, error)
	Upsert(ctx context.Context, key, typ string, valueJson []byte) error
}

// toModelSetting translates the persistence-shaped dbgen.AppSetting into the
// domain-shaped model.Setting. The []byte value_json round-trips through
// json.RawMessage so callers see an idiomatic JSON-bytes type.
func toModelSetting(row dbgen.AppSetting) model.Setting {
	return model.Setting{
		Key:       row.Key,
		Type:      row.Type,
		ValueJSON: row.ValueJson,
		Version:   row.Version,
		UpdatedAt: row.UpdatedAt,
	}
}

func (r *Repository) Get(ctx context.Context, key string) (model.Setting, error) {
	row, err := r.Q.GetSetting(ctx, key)
	if err != nil {
		return model.Setting{}, apperrors.FromPg(err, "setting %q not found", key)
	}
	return toModelSetting(row), nil
}

func (r *Repository) List(ctx context.Context) ([]model.Setting, error) {
	rows, err := r.Q.ListSettings(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list settings")
	}
	out := make([]model.Setting, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelSetting(row))
	}
	return out, nil
}

func (r *Repository) Upsert(ctx context.Context, key, typ string, valueJson []byte) error {
	return apperrors.FromPg(r.Q.UpsertSetting(ctx, dbgen.UpsertSettingParams{Key: key, Type: typ, ValueJson: valueJson}), "upsert setting %q", key)
}
