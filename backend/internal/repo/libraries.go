package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type LibraryRepo interface {
	ListLibraries(ctx context.Context) ([]dbgen.Library, error)
	GetLibrary(ctx context.Context, id pgtype.UUID) (dbgen.Library, error)
	GetDefaultLibrary(ctx context.Context, typ string) (dbgen.Library, error)
	CreateLibrary(ctx context.Context, name, typ, rootPath string, enabled bool, isDefault bool) (dbgen.Library, error)
	UpdateLibrary(ctx context.Context, id pgtype.UUID, name, typ, rootPath string, enabled bool, isDefault bool) (dbgen.Library, error)
	DeleteLibrary(ctx context.Context, id pgtype.UUID) error
}

func (r *Repository) ListLibraries(ctx context.Context) ([]dbgen.Library, error) {
	libs, err := r.Q.ListLibraries(ctx)
	return libs, apperrors.FromPg(err, "list libraries")
}

func (r *Repository) GetLibrary(ctx context.Context, id pgtype.UUID) (dbgen.Library, error) {
	lib, err := r.Q.GetLibrary(ctx, id)
	return lib, apperrors.FromPg(err, "library %s not found", id)
}

func (r *Repository) GetDefaultLibrary(ctx context.Context, typ string) (dbgen.Library, error) {
	lib, err := r.Q.GetDefaultLibrary(ctx, typ)
	return lib, apperrors.FromPg(err, "default %s library not found", typ)
}

func (r *Repository) CreateLibrary(ctx context.Context, name, typ, rootPath string, enabled bool, isDefault bool) (dbgen.Library, error) {
	lib, err := r.Q.CreateLibrary(ctx, dbgen.CreateLibraryParams{
		Name:      name,
		Type:      typ,
		RootPath:  rootPath,
		Enabled:   enabled,
		IsDefault: isDefault,
	})
	return lib, apperrors.FromPg(err, "create library %q", name)
}

func (r *Repository) UpdateLibrary(ctx context.Context, id pgtype.UUID, name, typ, rootPath string, enabled bool, isDefault bool) (dbgen.Library, error) {
	lib, err := r.Q.UpdateLibrary(ctx, dbgen.UpdateLibraryParams{
		ID:        id,
		Name:      name,
		Type:      typ,
		RootPath:  rootPath,
		Enabled:   enabled,
		IsDefault: isDefault,
	})
	return lib, apperrors.FromPg(err, "update library %s", id)
}

func (r *Repository) DeleteLibrary(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteLibrary(ctx, id), "delete library %s", id)
}
