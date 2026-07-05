package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// CreateWantParams is the domain-shaped input for CreateWant. Mirrors the
// writeable subset of model.Want (omits server-managed ID/timestamps and the
// runtime-managed attempt_count/last_error fields). NextRunAt is an optional
// creation input: nil keeps the column's DEFAULT now() ("search immediately"),
// while a value defers the first claim until that time — the series reconciler
// stamps an episode's air date so a future episode's want sits pending until air.
type CreateWantParams struct {
	TrackingID       uuid.UUID
	MediaItemID      uuid.UUID
	EpisodeID        *uuid.UUID
	QualityProfileID uuid.UUID
	Status           string
	Segment          string
	Hold             *string
	NextRunAt        *time.Time
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
		Segment:          row.Segment,
		Hold:             row.Hold,
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
		Segment:          params.Segment,
		Hold:             params.Hold,
		NextRunAt:        params.NextRunAt,
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
		Segment:          params.Segment,
		Hold:             params.Hold,
		NextRunAt:        params.NextRunAt,
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

// ListWantsByDownloadJob returns the wants a download_job advances, joined
// through download_job_want. Backs the download worker's lifecycle mirror and
// the import fan-out. In P1 a job links exactly one want; the slice is the M:N
// shape the season-pack work fans out over.
func (r *Repository) ListWantsByDownloadJob(ctx context.Context, jobID uuid.UUID) ([]model.Want, error) {
	rows, err := r.Q.ListWantsByDownloadJob(ctx, pgtypeFromUUID(jobID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list wants for download job %s", jobID)
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
}

// ListInFlightWantsForTrackingSeason returns the still-acquirable episode wants
// (pending/searching) of one tracking+season — the siblings a season pack can
// cover. Backs the front-half coverage computation.
func (r *Repository) ListInFlightWantsForTrackingSeason(ctx context.Context, trackingID, seasonID uuid.UUID) ([]model.Want, error) {
	rows, err := r.Q.ListInFlightWantsForTrackingSeason(ctx, dbgen.ListInFlightWantsForTrackingSeasonParams{
		TrackingID: pgtypeFromUUID(trackingID),
		SeasonID:   pgtypeFromUUID(seasonID),
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list in-flight wants for tracking %s season %s", trackingID, seasonID)
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

// GrabWantsForPack batch-claims the in-flight wants a season pack covers, flipping
// each 'pending'/'searching' want to 'grabbed'. It returns exactly the wants this
// grab claimed (the RETURNING set) — a want already advanced or terminal is skipped
// silently, so the caller links only the returned rows to the pack job. The
// multi-want analogue of GrabWant.
func (r *Repository) GrabWantsForPack(ctx context.Context, ids []uuid.UUID) ([]model.Want, error) {
	pgIDs := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		pgIDs[i] = pgtypeFromUUID(id)
	}
	rows, err := r.Q.GrabWantsForPack(ctx, pgIDs)
	if err != nil {
		return nil, apperrors.FromPg(err, "grab wants for pack")
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
}

// ReleaseWantFromGrab returns an under-covered want (a pack carried no file for it)
// to 'pending' via compare-and-swap so it re-searches. The bool reports whether the
// release landed: false (a 0-row CAS, surfaced as pgx.ErrNoRows) means the want
// wasn't in an in-flight ('grabbed'/'downloading') state — already terminal or
// reset — so the release is a benign no-op, not an error.
func (r *Repository) ReleaseWantFromGrab(ctx context.Context, id uuid.UUID, lastError string) (model.Want, bool, error) {
	row, err := r.Q.ReleaseWantFromGrab(ctx, dbgen.ReleaseWantFromGrabParams{
		ID:        pgtypeFromUUID(id),
		LastError: &lastError,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "release want %s from grab", id)
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

// RescheduleWantRecheck reschedules a still-'searching' want via
// compare-and-swap for the "no eligible release yet" path, resetting
// attempt_count to 0 — successfully reaching the indexer clears the
// consecutive-error counter that drives the retry backoff (ScheduleWantRetry).
// Reuses ScheduleWantRetryParams; the bool reports the same CAS-ownership
// contract: false (a 0-row CAS, surfaced as pgx.ErrNoRows) means the want is no
// longer 'searching' (reaper reset, another worker re-claimed), a benign no-op.
func (r *Repository) RescheduleWantRecheck(ctx context.Context, params ScheduleWantRetryParams) (model.Want, bool, error) {
	row, err := r.Q.RescheduleWantRecheck(ctx, dbgen.RescheduleWantRecheckParams{
		ID:        pgtypeFromUUID(params.ID),
		LastError: &params.LastError,
		NextRunAt: params.NextRunAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "reschedule recheck for want %s", params.ID)
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

// RetryTrackingWants re-drives a tracking's still-acquirable wants — re-arming
// terminal ('failed'/'canceled') ones and nudging 'pending' ones to search now —
// returning the wants it touched. Parked (held) and in-flight/'available' wants
// are left untouched by the query. Re-stamping the manual gate on a re-armed want
// in a manual segment is the service's concern (see TrackingService.Retry). Backs
// the tracking-level retry endpoint.
func (r *Repository) RetryTrackingWants(ctx context.Context, trackingID uuid.UUID) ([]model.Want, error) {
	rows, err := r.Q.RetryTrackingWants(ctx, pgtypeFromUUID(trackingID))
	if err != nil {
		return nil, apperrors.FromPg(err, "retry wants for tracking %s", trackingID)
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
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

// HoldWantsForSegment stamps hold='needs_pick' on a tracking's still-acquirable
// wants in one segment when that segment flips to manual, returning the wants it
// held (searching wants included — see the query comment). Called inside the
// SetAutonomy transaction.
func (r *Repository) HoldWantsForSegment(ctx context.Context, trackingID uuid.UUID, segment string) ([]model.Want, error) {
	rows, err := r.Q.HoldWantsForSegment(ctx, dbgen.HoldWantsForSegmentParams{
		TrackingID: pgtypeFromUUID(trackingID),
		Segment:    segment,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "hold %s wants for tracking %s", segment, trackingID)
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
}

// ReleaseHeldWantsForSegment clears the needs_pick hold on a tracking's wants in
// one segment when it flips back to auto and re-arms next_run_at, returning the
// wants it released. Called inside the SetAutonomy transaction.
func (r *Repository) ReleaseHeldWantsForSegment(ctx context.Context, trackingID uuid.UUID, segment string) ([]model.Want, error) {
	rows, err := r.Q.ReleaseHeldWantsForSegment(ctx, dbgen.ReleaseHeldWantsForSegmentParams{
		TrackingID: pgtypeFromUUID(trackingID),
		Segment:    segment,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "release %s wants for tracking %s", segment, trackingID)
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
}

// SetWantHold sets (nil clears) the hold on a single want. Backs the movie-rearm
// edge — a grab clears hold, so a re-request onto a manual segment re-stamps it —
// and test seeding.
func (r *Repository) SetWantHold(ctx context.Context, id uuid.UUID, hold *string) (model.Want, error) {
	row, err := r.Q.SetWantHold(ctx, dbgen.SetWantHoldParams{
		ID:   pgtypeFromUUID(id),
		Hold: hold,
	})
	if err != nil {
		return model.Want{}, apperrors.FromPg(err, "set hold for want %s", id)
	}
	return toModelWant(row), nil
}

// HoldProposedWants stamps hold='proposed' on the wants a proposal covers,
// parking them while the operator decides. It returns exactly the wants this call
// parked (still-claimable, unheld) — the propose-time analogue of GrabWantsForPack.
func (r *Repository) HoldProposedWants(ctx context.Context, ids []uuid.UUID) ([]model.Want, error) {
	pgIDs := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		pgIDs[i] = pgtypeFromUUID(id)
	}
	rows, err := r.Q.HoldProposedWants(ctx, pgIDs)
	if err != nil {
		return nil, apperrors.FromPg(err, "hold proposed wants")
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
}

// RearmProposedWant returns a proposed want to the pool via compare-and-swap,
// clearing the hold and re-arming for immediate search. The bool reports whether
// the re-arm landed: false (a 0-row CAS, surfaced as pgx.ErrNoRows) means the want
// no longer carries hold='proposed' (grabbed or already re-armed) — a benign
// no-op. Backs decline and supersede's coverage-shrink.
func (r *Repository) RearmProposedWant(ctx context.Context, id uuid.UUID, lastError string) (model.Want, bool, error) {
	row, err := r.Q.RearmProposedWant(ctx, dbgen.RearmProposedWantParams{
		ID:        pgtypeFromUUID(id),
		LastError: &lastError,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Want{}, false, nil
		}
		return model.Want{}, false, apperrors.FromPg(err, "rearm proposed want %s", id)
	}
	return toModelWant(row), true, nil
}

// FindLiveWantForEpisode returns the still-acquirable (pending/searching) want a
// tracking holds for one episode. A pgx.ErrNoRows flows through FromPg as
// NotFound — the series manual-grab path reads that as "no live want to join."
func (r *Repository) FindLiveWantForEpisode(ctx context.Context, trackingID, episodeID uuid.UUID) (model.Want, error) {
	row, err := r.Q.FindLiveWantForEpisode(ctx, dbgen.FindLiveWantForEpisodeParams{
		TrackingID: pgtypeFromUUID(trackingID),
		EpisodeID:  pgtypeFromUUID(episodeID),
	})
	if err != nil {
		return model.Want{}, apperrors.FromPg(err, "live want for tracking %s episode %s not found", trackingID, episodeID)
	}
	return toModelWant(row), nil
}
