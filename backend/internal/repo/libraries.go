package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type LibraryRepo interface {
	ListLibraries(ctx context.Context) ([]model.Library, error)
	GetLibrary(ctx context.Context, id uuid.UUID) (model.Library, error)
	GetDefaultLibrary(ctx context.Context, typ string) (model.Library, error)
	CreateLibrary(ctx context.Context, params CreateLibraryParams) (model.Library, error)
	UpdateLibrary(ctx context.Context, params UpdateLibraryParams) (model.Library, error)
	DeleteLibrary(ctx context.Context, id uuid.UUID) error
}

// CreateLibraryParams is the domain-shaped input for CreateLibrary. Mirrors
// the writeable subset of model.Library (omits server-managed ID/CreatedAt/UpdatedAt).
type CreateLibraryParams struct {
	Name      string
	Type      string
	RootPath  string
	Enabled   bool
	IsDefault bool
}

// UpdateLibraryParams is the domain-shaped input for UpdateLibrary. Mirrors
// the writeable subset of model.Library plus the target ID.
type UpdateLibraryParams struct {
	ID        uuid.UUID
	Name      string
	Type      string
	RootPath  string
	Enabled   bool
	IsDefault bool
}

// toModelLibrary translates the persistence-shaped dbgen.Library into the
// domain-shaped model.Library. Lives next to the methods that use it; every
// repo file that returns model types owns its own translator(s).
func toModelLibrary(row dbgen.Library) model.Library {
	return model.Library{
		ID:        uuidFromPgtype(row.ID),
		Name:      row.Name,
		Type:      row.Type,
		RootPath:  row.RootPath,
		Enabled:   row.Enabled,
		Default:   row.Default,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func (r *Repository) ListLibraries(ctx context.Context) ([]model.Library, error) {
	rows, err := r.Q.ListLibraries(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list libraries")
	}
	out := make([]model.Library, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelLibrary(row))
	}
	return out, nil
}

func (r *Repository) GetLibrary(ctx context.Context, id uuid.UUID) (model.Library, error) {
	row, err := r.Q.GetLibrary(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Library{}, apperrors.FromPg(err, "library %s not found", id)
	}
	return toModelLibrary(row), nil
}

func (r *Repository) GetDefaultLibrary(ctx context.Context, typ string) (model.Library, error) {
	row, err := r.Q.GetDefaultLibrary(ctx, typ)
	if err != nil {
		return model.Library{}, apperrors.FromPg(err, "default %s library not found", typ)
	}
	return toModelLibrary(row), nil
}

func (r *Repository) CreateLibrary(ctx context.Context, params CreateLibraryParams) (model.Library, error) {
	row, err := r.Q.CreateLibrary(ctx, dbgen.CreateLibraryParams{
		Name:      params.Name,
		Type:      params.Type,
		RootPath:  params.RootPath,
		Enabled:   params.Enabled,
		IsDefault: params.IsDefault,
	})
	if err != nil {
		return model.Library{}, apperrors.FromPg(err, "create library %q", params.Name)
	}
	return toModelLibrary(row), nil
}

func (r *Repository) UpdateLibrary(ctx context.Context, params UpdateLibraryParams) (model.Library, error) {
	row, err := r.Q.UpdateLibrary(ctx, dbgen.UpdateLibraryParams{
		ID:        pgtypeFromUUID(params.ID),
		Name:      params.Name,
		Type:      params.Type,
		RootPath:  params.RootPath,
		Enabled:   params.Enabled,
		IsDefault: params.IsDefault,
	})
	if err != nil {
		return model.Library{}, apperrors.FromPg(err, "update library %s", params.ID)
	}
	return toModelLibrary(row), nil
}

func (r *Repository) DeleteLibrary(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteLibrary(ctx, pgtypeFromUUID(id)), "delete library %s", id)
}
