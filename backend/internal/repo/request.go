package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// CreateRequestParams is the domain-shaped input for CreateRequest. Mirrors the
// writeable subset of model.Request (omits server-managed ID/timestamps and the
// spawn/deny fields set by later transitions).
type CreateRequestParams struct {
	RequestedBy uuid.UUID
	TmdbID      int64
	Type        string
	Tier        string
	Status      string
}

// toModelRequest translates a persistence-shaped dbgen.Request into the
// domain-shaped model.Request. SpawnedTrackingID is a nullable FK, surfaced as
// a *uuid.UUID.
func toModelRequest(row dbgen.Request) model.Request {
	return model.Request{
		ID:                uuidFromPgtype(row.ID),
		RequestedBy:       uuidFromPgtype(row.RequestedBy),
		TmdbID:            row.TmdbID,
		Type:              row.Type,
		Tier:              row.Tier,
		Status:            row.Status,
		SpawnedTrackingID: uuidPtrFromPgtype(row.SpawnedTrackingID),
		DeniedReason:      row.DeniedReason,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func (r *Repository) CreateRequest(ctx context.Context, params CreateRequestParams) (model.Request, error) {
	row, err := r.Q.CreateRequest(ctx, dbgen.CreateRequestParams{
		RequestedBy: pgtypeFromUUID(params.RequestedBy),
		TmdbID:      params.TmdbID,
		Type:        params.Type,
		Tier:        params.Tier,
		Status:      params.Status,
	})
	if err != nil {
		return model.Request{}, apperrors.FromPg(err, "create request for tmdb id %d", params.TmdbID)
	}
	return toModelRequest(row), nil
}

func (r *Repository) GetRequest(ctx context.Context, id uuid.UUID) (model.Request, error) {
	row, err := r.Q.GetRequest(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Request{}, apperrors.FromPg(err, "request %s not found", id)
	}
	return toModelRequest(row), nil
}

func (r *Repository) ListRequests(ctx context.Context) ([]model.Request, error) {
	rows, err := r.Q.ListRequests(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list requests")
	}
	out := make([]model.Request, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelRequest(row))
	}
	return out, nil
}

func (r *Repository) SetRequestSpawned(ctx context.Context, id, trackingID uuid.UUID) (model.Request, error) {
	row, err := r.Q.SetRequestSpawned(ctx, dbgen.SetRequestSpawnedParams{
		ID:                pgtypeFromUUID(id),
		SpawnedTrackingID: pgtypeFromUUID(trackingID),
	})
	if err != nil {
		return model.Request{}, apperrors.FromPg(err, "mark request %s spawned", id)
	}
	return toModelRequest(row), nil
}

func (r *Repository) SetRequestDenied(ctx context.Context, id uuid.UUID, deniedReason *string) (model.Request, error) {
	row, err := r.Q.SetRequestDenied(ctx, dbgen.SetRequestDeniedParams{
		ID:           pgtypeFromUUID(id),
		DeniedReason: deniedReason,
	})
	if err != nil {
		return model.Request{}, apperrors.FromPg(err, "mark request %s denied", id)
	}
	return toModelRequest(row), nil
}
