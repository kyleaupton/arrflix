package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type NameTemplatesService struct {
	repo *repo.Repository
}

func NewNameTemplatesService(r *repo.Repository) *NameTemplatesService {
	return &NameTemplatesService{repo: r}
}

func (s *NameTemplatesService) List(ctx context.Context) ([]dbgen.NameTemplate, error) {
	return s.repo.ListNameTemplates(ctx)
}

func (s *NameTemplatesService) Get(ctx context.Context, id pgtype.UUID) (dbgen.NameTemplate, error) {
	return s.repo.GetNameTemplate(ctx, id)
}

func (s *NameTemplatesService) GetDefault(ctx context.Context, typ string) (dbgen.NameTemplate, error) {
	return s.repo.GetDefaultNameTemplate(ctx, typ)
}

// validateNameTemplateInput collects every field-level problem with the
// supplied payload. Returns nil when valid.
func validateNameTemplateInput(name, typ, template string, movieDirTemplate *string) *apperrors.Error {
	var fields []apperrors.FieldError
	if name == "" {
		fields = append(fields, apperrors.Field("body.name", "required"))
	}
	if typ != "movie" && typ != "series" {
		fields = append(fields, apperrors.Field("body.type", "must be 'movie' or 'series'"))
	}
	if template == "" {
		fields = append(fields, apperrors.Field("body.template", "required"))
	}
	if typ == "movie" && (movieDirTemplate == nil || *movieDirTemplate == "") {
		fields = append(fields, apperrors.Field("body.movie_dir_template", "required for movie type"))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid name template", fields...)
	}
	return nil
}

func (s *NameTemplatesService) Create(ctx context.Context, name, typ, template string, showTemplate, seasonTemplate, movieDirTemplate *string, isDefault bool) (dbgen.NameTemplate, error) {
	if err := validateNameTemplateInput(name, typ, template, movieDirTemplate); err != nil {
		return dbgen.NameTemplate{}, err.Op("NameTemplatesService.Create")
	}

	// If setting as default, unset other defaults of the same type
	if isDefault {
		if err := s.unsetOtherDefaults(ctx, typ); err != nil {
			return dbgen.NameTemplate{}, err
		}
	}

	return s.repo.CreateNameTemplate(ctx, name, typ, template, showTemplate, seasonTemplate, movieDirTemplate, isDefault)
}

func (s *NameTemplatesService) Update(ctx context.Context, id pgtype.UUID, name, typ, template string, showTemplate, seasonTemplate, movieDirTemplate *string, isDefault bool) (dbgen.NameTemplate, error) {
	if err := validateNameTemplateInput(name, typ, template, movieDirTemplate); err != nil {
		return dbgen.NameTemplate{}, err.Op("NameTemplatesService.Update")
	}

	// If setting as default, unset other defaults of the same type (excluding this one)
	if isDefault {
		if err := s.unsetOtherDefaultsExcluding(ctx, typ, id); err != nil {
			return dbgen.NameTemplate{}, err
		}
	}

	return s.repo.UpdateNameTemplate(ctx, id, name, typ, template, showTemplate, seasonTemplate, movieDirTemplate, isDefault)
}

func (s *NameTemplatesService) Delete(ctx context.Context, id pgtype.UUID) error {
	return s.repo.DeleteNameTemplate(ctx, id)
}

// unsetOtherDefaults unsets all default flags for templates of the given type
func (s *NameTemplatesService) unsetOtherDefaults(ctx context.Context, typ string) error {
	templates, err := s.repo.ListNameTemplates(ctx)
	if err != nil {
		return err
	}

	for _, t := range templates {
		if t.Type == typ && t.Default {
			_, err := s.repo.UpdateNameTemplate(ctx, t.ID, t.Name, t.Type, t.Template, t.SeriesShowTemplate, t.SeriesSeasonTemplate, t.MovieDirTemplate, false)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// unsetOtherDefaultsExcluding unsets default flags for templates of the given type, excluding the specified ID
func (s *NameTemplatesService) unsetOtherDefaultsExcluding(ctx context.Context, typ string, excludeID pgtype.UUID) error {
	templates, err := s.repo.ListNameTemplates(ctx)
	if err != nil {
		return err
	}

	for _, t := range templates {
		if t.Type == typ && t.Default && t.ID != excludeID {
			_, err := s.repo.UpdateNameTemplate(ctx, t.ID, t.Name, t.Type, t.Template, t.SeriesShowTemplate, t.SeriesSeasonTemplate, t.MovieDirTemplate, false)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
