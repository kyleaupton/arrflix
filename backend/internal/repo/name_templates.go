package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type NameTemplateRepo interface {
	ListNameTemplates(ctx context.Context) ([]dbgen.NameTemplate, error)
	GetNameTemplate(ctx context.Context, id pgtype.UUID) (dbgen.NameTemplate, error)
	GetDefaultNameTemplate(ctx context.Context, typ string) (dbgen.NameTemplate, error)
	CreateNameTemplate(ctx context.Context, name, typ, template string, showTemplate, seasonTemplate, movieDirTemplate *string, isDefault bool) (dbgen.NameTemplate, error)
	UpdateNameTemplate(ctx context.Context, id pgtype.UUID, name, typ, template string, showTemplate, seasonTemplate, movieDirTemplate *string, isDefault bool) (dbgen.NameTemplate, error)
	DeleteNameTemplate(ctx context.Context, id pgtype.UUID) error
}

func (r *Repository) ListNameTemplates(ctx context.Context) ([]dbgen.NameTemplate, error) {
	tmpls, err := r.Q.ListNameTemplates(ctx)
	return tmpls, apperrors.FromPg(err, "list name templates")
}

func (r *Repository) GetNameTemplate(ctx context.Context, id pgtype.UUID) (dbgen.NameTemplate, error) {
	tmpl, err := r.Q.GetNameTemplate(ctx, id)
	return tmpl, apperrors.FromPg(err, "name template %s not found", id)
}

func (r *Repository) GetDefaultNameTemplate(ctx context.Context, typ string) (dbgen.NameTemplate, error) {
	tmpl, err := r.Q.GetDefaultNameTemplate(ctx, typ)
	return tmpl, apperrors.FromPg(err, "default %s name template not found", typ)
}

func (r *Repository) CreateNameTemplate(ctx context.Context, name, typ, template string, showTemplate, seasonTemplate, movieDirTemplate *string, isDefault bool) (dbgen.NameTemplate, error) {
	tmpl, err := r.Q.CreateNameTemplate(ctx, dbgen.CreateNameTemplateParams{
		Name:                 name,
		Type:                 typ,
		Template:             template,
		SeriesShowTemplate:   showTemplate,
		SeriesSeasonTemplate: seasonTemplate,
		MovieDirTemplate:     movieDirTemplate,
		IsDefault:            isDefault,
	})
	return tmpl, apperrors.FromPg(err, "create name template %q", name)
}

func (r *Repository) UpdateNameTemplate(ctx context.Context, id pgtype.UUID, name, typ, template string, showTemplate, seasonTemplate, movieDirTemplate *string, isDefault bool) (dbgen.NameTemplate, error) {
	tmpl, err := r.Q.UpdateNameTemplate(ctx, dbgen.UpdateNameTemplateParams{
		ID:                   id,
		Name:                 name,
		Type:                 typ,
		Template:             template,
		SeriesShowTemplate:   showTemplate,
		SeriesSeasonTemplate: seasonTemplate,
		MovieDirTemplate:     movieDirTemplate,
		IsDefault:            isDefault,
	})
	return tmpl, apperrors.FromPg(err, "update name template %s", id)
}

func (r *Repository) DeleteNameTemplate(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteNameTemplate(ctx, id), "delete name template %s", id)
}
