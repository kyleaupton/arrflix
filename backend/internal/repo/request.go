package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// CreateRequestParams is the domain-shaped input for CreateRequest. Mirrors the
// writeable subset of model.Request (omits server-managed ID/timestamps and the
// spawn field set by SetRequestSpawned). The decision fields are optional: nil
// for a pending request, stamped on the auto-approve path (approved at birth
// with self-decided provenance).
type CreateRequestParams struct {
	RequestedBy  uuid.UUID
	TmdbID       int64
	Type         string
	Tier         string
	Status       string
	ScopeRule    string
	DecidedBy    *uuid.UUID
	DecidedAt    *time.Time
	DecisionAuto *bool
}

// toModelRequest translates a persistence-shaped dbgen.Request into the
// domain-shaped model.Request. SpawnedTrackingID and DecidedBy are nullable FKs,
// surfaced as *uuid.UUID; DecidedAt/DecisionAuto are nullable and surface as
// pointers directly.
func toModelRequest(row dbgen.Request) model.Request {
	return model.Request{
		ID:                uuidFromPgtype(row.ID),
		RequestedBy:       uuidFromPgtype(row.RequestedBy),
		TmdbID:            row.TmdbID,
		Type:              row.Type,
		Tier:              row.Tier,
		ScopeRule:         row.ScopeRule,
		Status:            row.Status,
		SpawnedTrackingID: uuidPtrFromPgtype(row.SpawnedTrackingID),
		DeniedReason:      row.DeniedReason,
		DecidedBy:         uuidPtrFromPgtype(row.DecidedBy),
		DecidedAt:         timePtrFromPgTimestamptz(row.DecidedAt),
		DecisionAuto:      row.DecisionAuto,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func (r *Repository) CreateRequest(ctx context.Context, params CreateRequestParams) (model.Request, error) {
	row, err := r.Q.CreateRequest(ctx, dbgen.CreateRequestParams{
		RequestedBy:  pgtypeFromUUID(params.RequestedBy),
		TmdbID:       params.TmdbID,
		Type:         params.Type,
		Tier:         params.Tier,
		Status:       params.Status,
		ScopeRule:    params.ScopeRule,
		DecidedBy:    pgtypeFromUUIDPtr(params.DecidedBy),
		DecidedAt:    pgTimestamptzFromTimePtr(params.DecidedAt),
		DecisionAuto: params.DecisionAuto,
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

// SetRequestApproved transitions a request to 'approved' and stamps the manual
// decision provenance (decided_by = the approver, decision_auto = false). The
// subsequent SetRequestSpawned leaves these fields intact, so a spawned request
// still carries who approved it.
func (r *Repository) SetRequestApproved(ctx context.Context, id, approverID uuid.UUID) (model.Request, error) {
	row, err := r.Q.SetRequestApproved(ctx, dbgen.SetRequestApprovedParams{
		ID:        pgtypeFromUUID(id),
		DecidedBy: pgtypeFromUUID(approverID),
	})
	if err != nil {
		return model.Request{}, apperrors.FromPg(err, "mark request %s approved", id)
	}
	return toModelRequest(row), nil
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

// SetRequestDenied transitions a request to 'denied' with an optional reason and
// stamps the manual decision provenance (decided_by = the denier).
func (r *Repository) SetRequestDenied(ctx context.Context, id, approverID uuid.UUID, deniedReason *string) (model.Request, error) {
	row, err := r.Q.SetRequestDenied(ctx, dbgen.SetRequestDeniedParams{
		ID:           pgtypeFromUUID(id),
		DeniedReason: deniedReason,
		DecidedBy:    pgtypeFromUUID(approverID),
	})
	if err != nil {
		return model.Request{}, apperrors.FromPg(err, "mark request %s denied", id)
	}
	return toModelRequest(row), nil
}

// SetRequestCanceled transitions a request to 'canceled' and records who
// withdrew it (decided_by). decision_auto is left NULL — a cancel is a
// withdrawal, not an approve/deny decision.
func (r *Repository) SetRequestCanceled(ctx context.Context, id, cancellerID uuid.UUID) (model.Request, error) {
	row, err := r.Q.SetRequestCanceled(ctx, dbgen.SetRequestCanceledParams{
		ID:        pgtypeFromUUID(id),
		DecidedBy: pgtypeFromUUID(cancellerID),
	})
	if err != nil {
		return model.Request{}, apperrors.FromPg(err, "mark request %s canceled", id)
	}
	return toModelRequest(row), nil
}
