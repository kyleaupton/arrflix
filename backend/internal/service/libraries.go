package service

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type LibrariesService struct {
	repo *repo.Repository
	log  *logger.Logger
}

func NewLibrariesService(r *repo.Repository, l *logger.Logger) *LibrariesService {
	return &LibrariesService{repo: r, log: l}
}

func (s *LibrariesService) List(ctx context.Context) ([]model.Library, error) {
	return s.repo.ListLibraries(ctx)
}

func (s *LibrariesService) Get(ctx context.Context, id uuid.UUID) (model.Library, error) {
	return s.repo.GetLibrary(ctx, id)
}

func (s *LibrariesService) GetDefault(ctx context.Context, typ string) (model.Library, error) {
	return s.repo.GetDefaultLibrary(ctx, typ)
}

func (s *LibrariesService) Create(ctx context.Context, name, typ, rootPath string, enabled bool, isDefault bool) (model.Library, error) {
	if err := s.validateLibraryInput(name, typ, rootPath); err != nil {
		return model.Library{}, err.Op("LibrariesService.Create")
	}
	return s.repo.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name:      name,
		Type:      typ,
		RootPath:  rootPath,
		Enabled:   enabled,
		IsDefault: isDefault,
	})
}

func (s *LibrariesService) Update(ctx context.Context, id uuid.UUID, name, typ, rootPath string, enabled bool, isDefault bool) (model.Library, error) {
	if err := s.validateLibraryInput(name, typ, rootPath); err != nil {
		return model.Library{}, err.Op("LibrariesService.Update")
	}
	return s.repo.UpdateLibrary(ctx, repo.UpdateLibraryParams{
		ID:        id,
		Name:      name,
		Type:      typ,
		RootPath:  rootPath,
		Enabled:   enabled,
		IsDefault: isDefault,
	})
}

func (s *LibrariesService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteLibrary(ctx, id)
}

// validateLibraryInput collects every field-level problem with the supplied
// library payload before returning. Callers see all validation errors at once
// rather than one-at-a-time. Returns nil when the input is valid; otherwise
// returns a *apperrors.Error so the caller can chain .Op() with its own
// method name. The os.Stat failure is logged in full; only the underlying
// error's text is exposed to the caller (path-level errors like "permission
// denied" or "not a directory" are user-safe).
func (s *LibrariesService) validateLibraryInput(name, typ, rootPath string) *apperrors.Error {
	var fields []apperrors.FieldError
	if name == "" {
		fields = append(fields, apperrors.Field("body.name", "required"))
	}
	if typ != "movie" && typ != "series" {
		fields = append(fields, apperrors.Field("body.type", "must be 'movie' or 'series'"))
	}
	if rootPath == "" {
		fields = append(fields, apperrors.Field("body.root_path", "required"))
	} else if _, err := os.Stat(rootPath); err != nil {
		// Log the original error so ops can see syscall-level context.
		// Surface a path-level summary to the caller — path-error text is
		// user-safe ("permission denied", "not a directory", etc.).
		s.log.Warn().
			Err(err).
			Str("root_path", rootPath).
			Msg("library root path not accessible")
		fields = append(fields, apperrors.Field("body.root_path", fmt.Sprintf("not accessible: %v", err)))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid library", fields...)
	}
	return nil
}
