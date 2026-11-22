package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// VolumeAliasParams is the domain-shaped input for one alias of a volume
// write. Mirrors the writeable subset of model.VolumeAlias.
type VolumeAliasParams struct {
	DownloaderID uuid.UUID
	Base         string
}

// SaveVolumeParams is the domain-shaped input for CreateVolume and
// UpdateVolume. Mirrors the writeable subset of model.Volume; Aliases replace
// the volume's whole alias set.
type SaveVolumeParams struct {
	Name        string
	ArrflixBase string
	Aliases     []VolumeAliasParams
}

func toModelVolume(row dbgen.Volume, aliases []dbgen.VolumeAlias) model.Volume {
	out := model.Volume{
		ID:          uuidFromPgtype(row.ID),
		Name:        row.Name,
		ArrflixBase: row.ArrflixBase,
		Aliases:     make([]model.VolumeAlias, 0, len(aliases)),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	for _, a := range aliases {
		out.Aliases = append(out.Aliases, model.VolumeAlias{
			DownloaderID: uuidFromPgtype(a.DownloaderID),
			Base:         a.Base,
			Flavor:       a.Flavor,
		})
	}
	return out
}

// ListVolumes returns every volume with its aliases.
func (r *Repository) ListVolumes(ctx context.Context) ([]model.Volume, error) {
	rows, err := r.Q.ListVolumes(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list volumes")
	}
	aliases, err := r.Q.ListVolumeAliases(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list volume aliases")
	}
	byVolume := make(map[uuid.UUID][]dbgen.VolumeAlias, len(rows))
	for _, a := range aliases {
		id := uuidFromPgtype(a.VolumeID)
		byVolume[id] = append(byVolume[id], a)
	}
	out := make([]model.Volume, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelVolume(row, byVolume[uuidFromPgtype(row.ID)]))
	}
	return out, nil
}

// GetVolume returns one volume with its aliases.
func (r *Repository) GetVolume(ctx context.Context, id uuid.UUID) (model.Volume, error) {
	row, err := r.Q.GetVolume(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Volume{}, apperrors.FromPg(err, "volume %s not found", id)
	}
	return r.withAliases(ctx, row)
}

// CreateVolume inserts a volume and its aliases atomically.
func (r *Repository) CreateVolume(ctx context.Context, params SaveVolumeParams) (model.Volume, error) {
	var out model.Volume
	err := r.InTx(ctx, func(tx *Repository) error {
		row, err := tx.Q.CreateVolume(ctx, dbgen.CreateVolumeParams{
			Name:        params.Name,
			ArrflixBase: params.ArrflixBase,
		})
		if err != nil {
			return apperrors.FromPg(err, "create volume %q", params.Name)
		}
		if err := tx.insertAliases(ctx, uuidFromPgtype(row.ID), params); err != nil {
			return err
		}
		out, err = tx.withAliases(ctx, row)
		return err
	})
	return out, err
}

// UpdateVolume replaces a volume's fields and its whole alias set atomically.
func (r *Repository) UpdateVolume(ctx context.Context, id uuid.UUID, params SaveVolumeParams) (model.Volume, error) {
	var out model.Volume
	err := r.InTx(ctx, func(tx *Repository) error {
		row, err := tx.Q.UpdateVolume(ctx, dbgen.UpdateVolumeParams{
			ID:          pgtypeFromUUID(id),
			Name:        params.Name,
			ArrflixBase: params.ArrflixBase,
		})
		if err != nil {
			return apperrors.FromPg(err, "update volume %s", id)
		}
		if err := apperrors.FromPg(tx.Q.DeleteVolumeAliases(ctx, row.ID), "replace aliases of volume %s", id); err != nil {
			return err
		}
		if err := tx.insertAliases(ctx, id, params); err != nil {
			return err
		}
		out, err = tx.withAliases(ctx, row)
		return err
	})
	return out, err
}

// DeleteVolume removes a volume; its aliases cascade.
func (r *Repository) DeleteVolume(ctx context.Context, id uuid.UUID) error {
	n, err := r.Q.DeleteVolume(ctx, pgtypeFromUUID(id))
	if err != nil {
		return apperrors.FromPg(err, "delete volume %s", id)
	}
	if n == 0 {
		return apperrors.NotFoundf("volume %s not found", id)
	}
	return nil
}

func (r *Repository) insertAliases(ctx context.Context, volumeID uuid.UUID, params SaveVolumeParams) error {
	for _, a := range params.Aliases {
		if _, err := r.Q.CreateVolumeAlias(ctx, dbgen.CreateVolumeAliasParams{
			VolumeID:     pgtypeFromUUID(volumeID),
			DownloaderID: pgtypeFromUUID(a.DownloaderID),
			Base:         a.Base,
		}); err != nil {
			return apperrors.FromPg(err, "create alias %q of volume %q", a.Base, params.Name)
		}
	}
	return nil
}

func (r *Repository) withAliases(ctx context.Context, row dbgen.Volume) (model.Volume, error) {
	aliases, err := r.Q.ListVolumeAliasesForVolume(ctx, row.ID)
	if err != nil {
		return model.Volume{}, apperrors.FromPg(err, "list aliases of volume %s", uuidFromPgtype(row.ID))
	}
	return toModelVolume(row, aliases), nil
}
