package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type NameTemplatesService struct {
	repo *repo.Repository
}

func NewNameTemplatesService(r *repo.Repository) *NameTemplatesService {
	return &NameTemplatesService{repo: r}
}

func (s *NameTemplatesService) List(ctx context.Context) ([]model.NameTemplate, error) {
	return s.repo.ListNameTemplates(ctx)
}

func (s *NameTemplatesService) Get(ctx context.Context, id uuid.UUID) (model.NameTemplate, error) {
	return s.repo.GetNameTemplate(ctx, id)
}

func (s *NameTemplatesService) GetDefault(ctx context.Context, typ string) (model.NameTemplate, error) {
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

func (s *NameTemplatesService) Create(ctx context.Context, name, typ, template string, showTemplate, seasonTemplate, movieDirTemplate *string, isDefault bool) (model.NameTemplate, error) {
	if err := validateNameTemplateInput(name, typ, template, movieDirTemplate); err != nil {
		return model.NameTemplate{}, err.Op("NameTemplatesService.Create")
	}

	// If setting as default, unset other defaults of the same type
	if isDefault {
		if err := s.unsetOtherDefaults(ctx, typ); err != nil {
			return model.NameTemplate{}, err
		}
	}

	return s.repo.CreateNameTemplate(ctx, repo.CreateNameTemplateParams{
		Name:                 name,
		Type:                 typ,
		Template:             template,
		SeriesShowTemplate:   showTemplate,
		SeriesSeasonTemplate: seasonTemplate,
		MovieDirTemplate:     movieDirTemplate,
		IsDefault:            isDefault,
	})
}

func (s *NameTemplatesService) Update(ctx context.Context, id uuid.UUID, name, typ, template string, showTemplate, seasonTemplate, movieDirTemplate *string, isDefault bool) (model.NameTemplate, error) {
	if err := validateNameTemplateInput(name, typ, template, movieDirTemplate); err != nil {
		return model.NameTemplate{}, err.Op("NameTemplatesService.Update")
	}

	// If setting as default, unset other defaults of the same type (excluding this one)
	if isDefault {
		if err := s.unsetOtherDefaultsExcluding(ctx, typ, id); err != nil {
			return model.NameTemplate{}, err
		}
	}

	return s.repo.UpdateNameTemplate(ctx, repo.UpdateNameTemplateParams{
		ID:                   id,
		Name:                 name,
		Type:                 typ,
		Template:             template,
		SeriesShowTemplate:   showTemplate,
		SeriesSeasonTemplate: seasonTemplate,
		MovieDirTemplate:     movieDirTemplate,
		IsDefault:            isDefault,
	})
}

func (s *NameTemplatesService) Delete(ctx context.Context, id uuid.UUID) error {
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
			_, err := s.repo.UpdateNameTemplate(ctx, repo.UpdateNameTemplateParams{
				ID:                   t.ID,
				Name:                 t.Name,
				Type:                 t.Type,
				Template:             t.Template,
				SeriesShowTemplate:   t.SeriesShowTemplate,
				SeriesSeasonTemplate: t.SeriesSeasonTemplate,
				MovieDirTemplate:     t.MovieDirTemplate,
				IsDefault:            false,
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// unsetOtherDefaultsExcluding unsets default flags for templates of the given type, excluding the specified ID
func (s *NameTemplatesService) unsetOtherDefaultsExcluding(ctx context.Context, typ string, excludeID uuid.UUID) error {
	templates, err := s.repo.ListNameTemplates(ctx)
	if err != nil {
		return err
	}

	for _, t := range templates {
		if t.Type == typ && t.Default && t.ID != excludeID {
			_, err := s.repo.UpdateNameTemplate(ctx, repo.UpdateNameTemplateParams{
				ID:                   t.ID,
				Name:                 t.Name,
				Type:                 t.Type,
				Template:             t.Template,
				SeriesShowTemplate:   t.SeriesShowTemplate,
				SeriesSeasonTemplate: t.SeriesSeasonTemplate,
				MovieDirTemplate:     t.MovieDirTemplate,
				IsDefault:            false,
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Default templates installed on startup. Optional parts (edition, media
// info, release group) carry their leading separator inside the conditional,
// so a missing part leaves no doubled space or empty brackets.
const (
	defaultMovieTemplate    = `{{.Media.CleanTitle}} ({{.Media.Year}}){{if .Identity.Edition}} {edition-{{.Identity.Edition}}}{{end}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Encode.ReleaseGroup}}-{{.Encode.ReleaseGroup}}{{end}}`
	defaultMovieDirTemplate = `{{.Media.CleanTitle}} ({{.Media.Year}}) {tmdb-{{.Media.TmdbID}}}`

	defaultSeriesTemplate       = `{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}} - {{clean .Media.EpisodeTitle}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Encode.ReleaseGroup}}-{{.Encode.ReleaseGroup}}{{end}}`
	defaultSeriesShowTemplate   = `{{.Media.Title}} ({{.Media.Year}})`
	defaultSeriesSeasonTemplate = `Season {{.Media.Season}}`
)

// SeedDefaults installs a default movie and series template for each type
// that has none, so routing can always resolve a template. It is idempotent
// and never touches a type that already has a default (including one the user
// created or edited).
func (s *NameTemplatesService) SeedDefaults(ctx context.Context) error {
	movieDir, showDir, seasonDir := defaultMovieDirTemplate, defaultSeriesShowTemplate, defaultSeriesSeasonTemplate
	seeds := []repo.CreateNameTemplateParams{
		{
			Name:             "Default Movie Template",
			Type:             "movie",
			Template:         defaultMovieTemplate,
			MovieDirTemplate: &movieDir,
			IsDefault:        true,
		},
		{
			Name:                 "Default Series Template",
			Type:                 "series",
			Template:             defaultSeriesTemplate,
			SeriesShowTemplate:   &showDir,
			SeriesSeasonTemplate: &seasonDir,
			IsDefault:            true,
		},
	}

	for _, p := range seeds {
		if _, err := s.repo.GetDefaultNameTemplate(ctx, p.Type); err == nil {
			continue
		} else if !apperrors.IsNotFound(err) {
			return err
		}
		// A concurrent seeder or a same-named user template surfaces as a
		// conflict; the unique default index keeps the result at most one.
		if _, err := s.repo.CreateNameTemplate(ctx, p); err != nil && !apperrors.IsConflict(err) {
			return err
		}
	}
	return nil
}
