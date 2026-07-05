package repo

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// AddWantReleaseExclusionParams is the domain-shaped input for
// AddWantReleaseExclusion. Mirrors the writeable subset of
// model.WantReleaseExclusion (omits the server-managed created_at).
type AddWantReleaseExclusionParams struct {
	WantID    uuid.UUID
	IndexerID int64
	GUID      string
	Reason    model.ExclusionReason
	Detail    *string
}

// toModelWantReleaseExclusion translates a persistence-shaped
// dbgen.WantReleaseExclusion into the domain-shaped model type.
func toModelWantReleaseExclusion(row dbgen.WantReleaseExclusion) model.WantReleaseExclusion {
	return model.WantReleaseExclusion{
		WantID:    uuidFromPgtype(row.WantID),
		IndexerID: row.IndexerID,
		GUID:      row.Guid,
		Reason:    row.Reason,
		Detail:    row.Detail,
		CreatedAt: row.CreatedAt,
	}
}

// AddWantReleaseExclusion records that a release must not be re-picked for a
// want. Idempotent (ON CONFLICT DO NOTHING) — a re-add of the same release is a
// benign no-op, so a plain error return suffices.
func (r *Repository) AddWantReleaseExclusion(ctx context.Context, params AddWantReleaseExclusionParams) error {
	return apperrors.FromPg(r.Q.AddWantReleaseExclusion(ctx, dbgen.AddWantReleaseExclusionParams{
		WantID:    pgtypeFromUUID(params.WantID),
		IndexerID: params.IndexerID,
		Guid:      params.GUID,
		Reason:    string(params.Reason),
		Detail:    params.Detail,
	}), "add release exclusion for want %s", params.WantID)
}

// ListWantReleaseExclusionsForWants returns the exclusions for a set of wants in
// one round trip — the front-half consults the processed want plus its in-flight
// season siblings before searching. The pgtype.UUID slice mirrors
// GrabWantsForPack's id-array shape.
func (r *Repository) ListWantReleaseExclusionsForWants(ctx context.Context, wantIDs []uuid.UUID) ([]model.WantReleaseExclusion, error) {
	pgIDs := make([]pgtype.UUID, len(wantIDs))
	for i, id := range wantIDs {
		pgIDs[i] = pgtypeFromUUID(id)
	}
	rows, err := r.Q.ListWantReleaseExclusionsForWants(ctx, pgIDs)
	if err != nil {
		return nil, apperrors.FromPg(err, "list release exclusions for wants")
	}
	out := make([]model.WantReleaseExclusion, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWantReleaseExclusion(row))
	}
	return out, nil
}
