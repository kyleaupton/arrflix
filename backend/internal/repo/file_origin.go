package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// CreateFileOriginParams is the domain-shaped input for CreateFileOriginIfAbsent.
// Mirrors the writeable columns of model.FileOrigin. The caller (an orchestration
// helper — see jobutil.FileOriginParams) has already projected the bin and
// decided nullability, so the bin_* fields arrive as *string: nil when the parse
// yielded no recognized bin, all three set otherwise.
type CreateFileOriginParams struct {
	FileID        uuid.UUID
	Origin        string
	SourceTitle   *string
	BinSource     *string
	BinResolution *string
	BinModifier   *string
	ReleaseGroup  *string
	Edition       *string
	Parsed        []byte
	IndexerID     *int64
	Guid          *string
	DownloadJobID *uuid.UUID
}

// toModelFileOrigin translates the persistence-shaped dbgen.FileOrigin into the
// domain-shaped model.FileOrigin.
func toModelFileOrigin(row dbgen.FileOrigin) model.FileOrigin {
	return model.FileOrigin{
		FileID:        uuidFromPgtype(row.FileID),
		Origin:        row.Origin,
		SourceTitle:   row.SourceTitle,
		BinSource:     row.BinSource,
		BinResolution: row.BinResolution,
		BinModifier:   row.BinModifier,
		ReleaseGroup:  row.ReleaseGroup,
		Edition:       row.Edition,
		Parsed:        row.Parsed,
		IndexerID:     row.IndexerID,
		Guid:          row.Guid,
		DownloadJobID: uuidPtrFromPgtype(row.DownloadJobID),
		CapturedAt:    row.CapturedAt,
	}
}

// CreateFileOriginIfAbsent stamps a file's provenance the first time its row is
// minted. Insert-once: ON CONFLICT (file_id) DO NOTHING means later identity
// updates and re-scans never overwrite the captured source_title — the row is
// cold provenance, not a hot fact like file_state. Safe to call unconditionally.
func (r *Repository) CreateFileOriginIfAbsent(ctx context.Context, params CreateFileOriginParams) error {
	return apperrors.FromPg(r.Q.CreateFileOriginIfAbsent(ctx, dbgen.CreateFileOriginIfAbsentParams{
		FileID:        pgtypeFromUUID(params.FileID),
		Origin:        params.Origin,
		SourceTitle:   params.SourceTitle,
		BinSource:     params.BinSource,
		BinResolution: params.BinResolution,
		BinModifier:   params.BinModifier,
		ReleaseGroup:  params.ReleaseGroup,
		Edition:       params.Edition,
		Parsed:        params.Parsed,
		IndexerID:     params.IndexerID,
		Guid:          params.Guid,
		DownloadJobID: pgtypeFromUUIDPtr(params.DownloadJobID),
	}), "create file origin for %s", params.FileID)
}

func (r *Repository) GetFileOrigin(ctx context.Context, fileID uuid.UUID) (model.FileOrigin, error) {
	row, err := r.Q.GetFileOrigin(ctx, pgtypeFromUUID(fileID))
	if err != nil {
		return model.FileOrigin{}, apperrors.FromPg(err, "file origin for %s not found", fileID)
	}
	return toModelFileOrigin(row), nil
}
