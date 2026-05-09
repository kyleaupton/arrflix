package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type NameTemplateRepo interface {
	ListNameTemplates(ctx context.Context) ([]model.NameTemplate, error)
	GetNameTemplate(ctx context.Context, id uuid.UUID) (model.NameTemplate, error)
	GetDefaultNameTemplate(ctx context.Context, typ string) (model.NameTemplate, error)
	CreateNameTemplate(ctx context.Context, params CreateNameTemplateParams) (model.NameTemplate, error)
	UpdateNameTemplate(ctx context.Context, params UpdateNameTemplateParams) (model.NameTemplate, error)
	DeleteNameTemplate(ctx context.Context, id uuid.UUID) error
}

// CreateNameTemplateParams is the domain-shaped input for CreateNameTemplate.
// Mirrors the writeable subset of model.NameTemplate.
type CreateNameTemplateParams struct {
	Name                 string
	Type                 string
	Template             string
	SeriesShowTemplate   *string
	SeriesSeasonTemplate *string
	MovieDirTemplate     *string
	IsDefault            bool
}

// UpdateNameTemplateParams is the domain-shaped input for UpdateNameTemplate.
// Mirrors the writeable subset of model.NameTemplate plus the target ID.
type UpdateNameTemplateParams struct {
	ID                   uuid.UUID
	Name                 string
	Type                 string
	Template             string
	SeriesShowTemplate   *string
	SeriesSeasonTemplate *string
	MovieDirTemplate     *string
	IsDefault            bool
}

// toModelNameTemplate translates the persistence-shaped dbgen.NameTemplate
// into the domain-shaped model.NameTemplate.
func toModelNameTemplate(row dbgen.NameTemplate) model.NameTemplate {
	return model.NameTemplate{
		ID:                   uuidFromPgtype(row.ID),
		Name:                 row.Name,
		Type:                 row.Type,
		Template:             row.Template,
		MovieDirTemplate:     row.MovieDirTemplate,
		SeriesShowTemplate:   row.SeriesShowTemplate,
		SeriesSeasonTemplate: row.SeriesSeasonTemplate,
		Default:              row.Default,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}

func (r *Repository) ListNameTemplates(ctx context.Context) ([]model.NameTemplate, error) {
	rows, err := r.Q.ListNameTemplates(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list name templates")
	}
	out := make([]model.NameTemplate, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelNameTemplate(row))
	}
	return out, nil
}

func (r *Repository) GetNameTemplate(ctx context.Context, id uuid.UUID) (model.NameTemplate, error) {
	row, err := r.Q.GetNameTemplate(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.NameTemplate{}, apperrors.FromPg(err, "name template %s not found", id)
	}
	return toModelNameTemplate(row), nil
}

func (r *Repository) GetDefaultNameTemplate(ctx context.Context, typ string) (model.NameTemplate, error) {
	row, err := r.Q.GetDefaultNameTemplate(ctx, typ)
	if err != nil {
		return model.NameTemplate{}, apperrors.FromPg(err, "default %s name template not found", typ)
	}
	return toModelNameTemplate(row), nil
}

func (r *Repository) CreateNameTemplate(ctx context.Context, params CreateNameTemplateParams) (model.NameTemplate, error) {
	row, err := r.Q.CreateNameTemplate(ctx, dbgen.CreateNameTemplateParams{
		Name:                 params.Name,
		Type:                 params.Type,
		Template:             params.Template,
		SeriesShowTemplate:   params.SeriesShowTemplate,
		SeriesSeasonTemplate: params.SeriesSeasonTemplate,
		MovieDirTemplate:     params.MovieDirTemplate,
		IsDefault:            params.IsDefault,
	})
	if err != nil {
		return model.NameTemplate{}, apperrors.FromPg(err, "create name template %q", params.Name)
	}
	return toModelNameTemplate(row), nil
}

func (r *Repository) UpdateNameTemplate(ctx context.Context, params UpdateNameTemplateParams) (model.NameTemplate, error) {
	row, err := r.Q.UpdateNameTemplate(ctx, dbgen.UpdateNameTemplateParams{
		ID:                   pgtypeFromUUID(params.ID),
		Name:                 params.Name,
		Type:                 params.Type,
		Template:             params.Template,
		SeriesShowTemplate:   params.SeriesShowTemplate,
		SeriesSeasonTemplate: params.SeriesSeasonTemplate,
		MovieDirTemplate:     params.MovieDirTemplate,
		IsDefault:            params.IsDefault,
	})
	if err != nil {
		return model.NameTemplate{}, apperrors.FromPg(err, "update name template %s", params.ID)
	}
	return toModelNameTemplate(row), nil
}

func (r *Repository) DeleteNameTemplate(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteNameTemplate(ctx, pgtypeFromUUID(id)), "delete name template %s", id)
}
