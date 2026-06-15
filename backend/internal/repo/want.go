package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	EpisodeID        *uuid.UUID
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
		EpisodeID:        uuidPtrFromPgtype(row.EpisodeID),
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
		EpisodeID:        pgtypeFromUUIDPtr(params.EpisodeID),
		QualityProfileID: pgtypeFromUUIDOrNull(params.QualityProfileID),
		Status:           params.Status,
	})
	if err != nil {
		return model.Want{}, apperrors.FromPg(err, "create want for tracking %s", params.TrackingID)
	}
	return toModelWant(row), nil
}

// CreateWantIfAbsent is the idempotent want insert the series reconciler routes
// through, one want per (tracking, episode). The bool reports whether THIS call
// inserted the row: true → freshly created; false (a 0-row ON CONFLICT, surfaced
// as pgx.ErrNoRows) → a concurrent reconcile already created it (benign).
// Mirrors CreateTrackingIfAbsent's CAS-returns-bool convention.
func (r *Repository) CreateWantIfAbsent(ctx context.Context, params CreateWantParams) (model.Want, bool, error) {
	row, err := r.Q.CreateWantIfAbsent(ctx, dbgen.CreateWantIfAbsentParams{
		TrackingID:       pgtypeFromUUID(params.TrackingID),
		MediaItemID:      pgtypeFromUUID(params.MediaItemID),
		EpisodeID:        pgtypeFromUUIDPtr(params.EpisodeID),
		QualityProfileID: pgtypeFromUUIDOrNull(params.QualityProfileID),
		Status:           params.Status,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "create want for tracking %s", params.TrackingID)
	}
	return toModelWant(row), true, nil
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

// CancelWant terminally cancels a non-terminal want via compare-and-swap. The
// bool reports whether the cancel landed: false (a 0-row CAS, surfaced as
// pgx.ErrNoRows) means the want was already terminal — the service distinguishes
// idempotent (already 'canceled') from conflict ('available'/'failed').
func (r *Repository) CancelWant(ctx context.Context, id uuid.UUID) (model.Want, bool, error) {
	row, err := r.Q.CancelWant(ctx, pgtypeFromUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "cancel want %s", id)
	}
	return toModelWant(row), true, nil
}

// MirrorWantStatus is the terminal-sticky mirror the workers route status
// updates through. The bool reports whether the mirror landed: false (a 0-row
// CAS, surfaced as pgx.ErrNoRows) means the want is terminal and the mirror was
// blocked — the resurrection guard. A false return is benign, not an error.
func (r *Repository) MirrorWantStatus(ctx context.Context, id uuid.UUID, status string) (model.Want, bool, error) {
	row, err := r.Q.MirrorWantStatus(ctx, dbgen.MirrorWantStatusParams{
		ID:     pgtypeFromUUID(id),
		Status: status,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "mirror status for want %s", id)
	}
	return toModelWant(row), true, nil
}

// GrabWant claims a still-'searching' want for grab via compare-and-swap. The
// bool reports ownership: true when the want was flipped to 'grabbed', false
// when it's no longer 'searching' (a 0-row CAS, surfaced as pgx.ErrNoRows) —
// meaning the reaper reset it and another worker re-claimed, or a concurrent
// grab won. A false return is benign, not an error.
func (r *Repository) GrabWant(ctx context.Context, id uuid.UUID) (model.Want, bool, error) {
	row, err := r.Q.GrabWant(ctx, pgtypeFromUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "grab want %s", id)
	}
	return toModelWant(row), true, nil
}

// GrabWantManual flips a want to 'grabbed' for the manual-grab path via
// compare-and-swap. The bool reports whether the flip landed: false (a 0-row
// CAS, surfaced as pgx.ErrNoRows) means the want wasn't in a grabbable state
// ('pending'/'searching'/'failed'/'canceled') — already in flight or available
// — so the service rejects it as a conflict. Mirrors GrabWant's contract.
func (r *Repository) GrabWantManual(ctx context.Context, id uuid.UUID) (model.Want, bool, error) {
	row, err := r.Q.GrabWantManual(ctx, pgtypeFromUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "grab want %s manually", id)
	}
	return toModelWant(row), true, nil
}

// ReclaimStaleSearchingWants resets wants wedged in 'searching' past staleBefore
// back to 'pending'. The crash-window reaper's entry point.
func (r *Repository) ReclaimStaleSearchingWants(ctx context.Context, staleBefore time.Time, lastError string) ([]model.Want, error) {
	rows, err := r.Q.ReclaimStaleSearchingWants(ctx, dbgen.ReclaimStaleSearchingWantsParams{
		LastError:   &lastError,
		StaleBefore: staleBefore,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "reclaim stale searching wants")
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
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

// ScheduleWantRetry reschedules a still-'searching' want via compare-and-swap.
// The bool reports ownership: false (a 0-row CAS, surfaced as pgx.ErrNoRows)
// means the want is no longer 'searching' — the reaper reset it and another
// worker re-claimed — so the reschedule is a benign no-op, not an error.
func (r *Repository) ScheduleWantRetry(ctx context.Context, params ScheduleWantRetryParams) (model.Want, bool, error) {
	row, err := r.Q.ScheduleWantRetry(ctx, dbgen.ScheduleWantRetryParams{
		ID:        pgtypeFromUUID(params.ID),
		LastError: &params.LastError,
		NextRunAt: params.NextRunAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "schedule retry for want %s", params.ID)
	}
	return toModelWant(row), true, nil
}

// RearmWant resumes a terminal want when its movie is re-requested, flipping a
// 'failed'/'canceled' want back to 'pending' (attempt counter and backoff reset)
// so the AcquisitionWorker re-claims it. The single-atom invariant means the one
// existing want is re-armed rather than a second created. The bool reports
// whether the re-arm landed: false (a 0-row CAS, surfaced as pgx.ErrNoRows)
// means the want is in-flight or already 'available' — nothing to resume.
func (r *Repository) RearmWant(ctx context.Context, id uuid.UUID) (model.Want, bool, error) {
	row, err := r.Q.RearmWant(ctx, pgtypeFromUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "rearm want %s", id)
	}
	return toModelWant(row), true, nil
}

// MarkWantFailed terminally fails a still-'searching' want via compare-and-swap.
// The bool reports ownership: false (a 0-row CAS, surfaced as pgx.ErrNoRows)
// means the want is no longer 'searching' — the reaper reset it and another
// worker re-claimed — so the fail is a benign no-op, not an error.
func (r *Repository) MarkWantFailed(ctx context.Context, id uuid.UUID, lastError string) (model.Want, bool, error) {
	row, err := r.Q.MarkWantFailed(ctx, dbgen.MarkWantFailedParams{
		ID:        pgtypeFromUUID(id),
		LastError: &lastError,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "mark want %s failed", id)
	}
	return toModelWant(row), true, nil
}
