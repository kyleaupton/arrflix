package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// CreateWantParams is the domain-shaped input for CreateWant. Mirrors the
// writeable subset of model.Want (omits server-managed ID/timestamps and the
// runtime-managed next_run_at/attempt_count/last_error fields).
type CreateWantParams struct {
	TrackingID       uuid.UUID
	MediaItemID      uuid.UUID
	QualityProfileID uuid.UUID
	Status           string
}

// toModelWant translates a persistence-shaped dbgen.Want into the
// domain-shaped model.Want.
func toModelWant(row dbgen.Want) model.Want {
	return model.Want{
		ID:               uuidFromPgtype(row.ID),
		TrackingID:       uuidFromPgtype(row.TrackingID),
		MediaItemID:      uuidFromPgtype(row.MediaItemID),
		QualityProfileID: uuidFromPgtype(row.QualityProfileID),
		Status:           row.Status,
		NextRunAt:        row.NextRunAt,
		AttemptCount:     row.AttemptCount,
		LastError:        row.LastError,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func (r *Repository) CreateWant(ctx context.Context, params CreateWantParams) (model.Want, error) {
	row, err := r.Q.CreateWant(ctx, dbgen.CreateWantParams{
		TrackingID:       pgtypeFromUUID(params.TrackingID),
		MediaItemID:      pgtypeFromUUID(params.MediaItemID),
		QualityProfileID: pgtypeFromUUID(params.QualityProfileID),
		Status:           params.Status,
	})
	if err != nil {
		return model.Want{}, apperrors.FromPg(err, "create want for tracking %s", params.TrackingID)
	}
	return toModelWant(row), nil
}

func (r *Repository) GetWant(ctx context.Context, id uuid.UUID) (model.Want, error) {
	row, err := r.Q.GetWant(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Want{}, apperrors.FromPg(err, "want %s not found", id)
	}
	return toModelWant(row), nil
}

func (r *Repository) ListWantsByTracking(ctx context.Context, trackingID uuid.UUID) ([]model.Want, error) {
	rows, err := r.Q.ListWantsByTracking(ctx, pgtypeFromUUID(trackingID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list wants for tracking %s", trackingID)
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
}

// ClaimRunnableWants atomically claims due pending wants, flipping them to
// 'searching' under FOR UPDATE SKIP LOCKED so a concurrent claim skips them.
// The future AcquisitionWorker's entry point.
func (r *Repository) ClaimRunnableWants(ctx context.Context, limit int32) ([]model.Want, error) {
	rows, err := r.Q.ClaimRunnableWants(ctx, limit)
	if err != nil {
		return nil, apperrors.FromPg(err, "claim runnable wants")
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
}

func (r *Repository) SetWantStatus(ctx context.Context, id uuid.UUID, status string) (model.Want, error) {
	row, err := r.Q.SetWantStatus(ctx, dbgen.SetWantStatusParams{
		ID:     pgtypeFromUUID(id),
		Status: status,
	})
	if err != nil {
		return model.Want{}, apperrors.FromPg(err, "set status for want %s", id)
	}
	return toModelWant(row), nil
}

// ScheduleWantRetryParams is the domain-shaped input for ScheduleWantRetry.
// Mirrors the columns the AcquisitionWorker writes when scheduling a backoff
// retry. Unlike ScheduleDownloadJobRetryParams there's no Kind — want has no
// error_kind column.
type ScheduleWantRetryParams struct {
	ID        uuid.UUID
	LastError string
	NextRunAt time.Time
}

func (r *Repository) ScheduleWantRetry(ctx context.Context, params ScheduleWantRetryParams) (model.Want, error) {
	row, err := r.Q.ScheduleWantRetry(ctx, dbgen.ScheduleWantRetryParams{
		ID:        pgtypeFromUUID(params.ID),
		LastError: &params.LastError,
		NextRunAt: params.NextRunAt,
	})
	if err != nil {
		return model.Want{}, apperrors.FromPg(err, "schedule retry for want %s", params.ID)
	}
	return toModelWant(row), nil
}

func (r *Repository) MarkWantFailed(ctx context.Context, id uuid.UUID, lastError string) (model.Want, error) {
	row, err := r.Q.MarkWantFailed(ctx, dbgen.MarkWantFailedParams{
		ID:        pgtypeFromUUID(id),
		LastError: &lastError,
	})
	if err != nil {
		return model.Want{}, apperrors.FromPg(err, "mark want %s failed", id)
	}
	return toModelWant(row), nil
}
