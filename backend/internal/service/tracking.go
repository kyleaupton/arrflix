package service

import (
	"context"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// TrackingService exposes reads over the tracking primitive and its wants and
// requesters, plus the cancel (stop-tracking) write. The spawn write surface
// lives on RequestService; pause/resume/archive remain out of scope. It holds
// WantService so cancel reuses the tested want-cancel + download-job cascade.
type TrackingService struct {
	repo  *repo.Repository
	wants *WantService
	authz *AuthzService
}

func NewTrackingService(r *repo.Repository, wants *WantService, authz *AuthzService) *TrackingService {
	return &TrackingService{repo: r, wants: wants, authz: authz}
}

func (s *TrackingService) Get(ctx context.Context, id uuid.UUID) (model.Tracking, error) {
	return s.repo.GetTracking(ctx, id)
}

// SetAutonomy dials a tracking's per-segment acquisition autonomy and reconciles
// its live wants to match. Each segment must be 'auto', 'propose', or 'manual'.
//
// The whole change runs in one transaction: set both dials, then for each
// segment that actually changed, reconcile its wants. manual holds them
// (needs_pick); auto releases held ones. propose is a pure dial change — no want
// mutation: pending wants stay claimable and are each proposed on their next tick.
// Only pending/searching wants are ever affected. Searching wants are held too,
// not just pending ones: the reschedule/reclaim queries return a searcher to
// pending without touching hold, so holding only pending would let a perpetual
// searcher slip past the manual gate. Grabbed/downloading/imported and terminal
// wants are left alone — a grab already in flight when its segment goes manual
// completes normally (an acceptable race; the file lands and the want advances).
// No SSE is emitted: the acting client invalidates via the mutation; other
// clients stay stale until the next want event.
func (s *TrackingService) SetAutonomy(ctx context.Context, trackingID uuid.UUID, backfill, ongoing string) (model.Tracking, error) {
	const op = "TrackingService.SetAutonomy"

	var fields []apperrors.FieldError
	if !isSettableAutonomy(backfill) {
		fields = append(fields, apperrors.Field("body.backfill", "must be 'auto', 'propose', or 'manual'"))
	}
	if !isSettableAutonomy(ongoing) {
		fields = append(fields, apperrors.Field("body.ongoing", "must be 'auto', 'propose', or 'manual'"))
	}
	if len(fields) > 0 {
		return model.Tracking{}, apperrors.Validation("invalid autonomy", fields...).Op(op)
	}

	var updated model.Tracking
	err := s.repo.InTx(ctx, func(r *repo.Repository) error {
		old, err := r.GetTracking(ctx, trackingID)
		if err != nil {
			return err
		}
		updated, err = r.SetTrackingAutonomy(ctx, trackingID, backfill, ongoing)
		if err != nil {
			return err
		}
		if backfill != old.AutonomyBackfill {
			if err := applyAutonomyToSegment(ctx, r, trackingID, model.WantSegmentBackfill, backfill); err != nil {
				return err
			}
		}
		if ongoing != old.AutonomyOngoing {
			if err := applyAutonomyToSegment(ctx, r, trackingID, model.WantSegmentOngoing, ongoing); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.Tracking{}, err
	}
	return updated, nil
}

// isSettableAutonomy reports whether an autonomy value is one the API accepts:
// 'auto', 'propose', or 'manual' — the full DB vocabulary.
func isSettableAutonomy(a string) bool {
	return a == string(model.AutonomyAuto) ||
		a == string(model.AutonomyPropose) ||
		a == string(model.AutonomyManual)
}

// applyAutonomyToSegment reconciles one segment's live wants to a new autonomy.
// manual holds them (needs_pick, barring the worker). auto and propose both want
// the segment's wants claimable — they differ only at ProcessWant time (auto
// grabs, propose parks a proposal), not in pre-claim gating — so both release any
// needs_pick hold and re-arm. Releasing is scoped to hold='needs_pick', so an
// open proposal's hold='proposed' is never disturbed. Called inside the
// SetAutonomy transaction.
func applyAutonomyToSegment(ctx context.Context, r *repo.Repository, trackingID uuid.UUID, seg model.WantSegment, autonomy string) error {
	if autonomy == string(model.AutonomyManual) {
		_, err := r.HoldWantsForSegment(ctx, trackingID, string(seg))
		return err
	}
	_, err := r.ReleaseHeldWantsForSegment(ctx, trackingID, string(seg))
	return err
}

// Retry re-drives a tracking's acquisition: every terminal ('failed'/'canceled')
// want is re-armed and every 'pending' want is nudged to search immediately, so a
// tracking wedged by a transient outage — or one whose indexer config was just
// fixed — resumes at once instead of waiting out its scheduled cadence. Parked
// wants ('needs_pick'/'proposed' holds), in-flight wants, and 'available' wants
// are all left untouched.
//
// The whole change runs in one transaction. A want re-armed in a manual segment
// is re-held ('needs_pick') so the retry lands back under the manual gate rather
// than slipping into auto-search — the bulk analogue of rearmMovieWant's
// re-stamp. A retry also reactivates a tracking that Cancel parked in 'canceled'.
// Returns the reactivated tracking and the count of wants re-driven; no SSE is
// emitted, so the acting client invalidates and refetches the want list.
func (s *TrackingService) Retry(ctx context.Context, trackingID uuid.UUID) (model.Tracking, int, error) {
	var tracking model.Tracking
	var retried int
	err := s.repo.InTx(ctx, func(r *repo.Repository) error {
		var err error
		tracking, err = r.GetTracking(ctx, trackingID) // 404 flows through
		if err != nil {
			return err
		}

		wants, err := r.RetryTrackingWants(ctx, trackingID)
		if err != nil {
			return err
		}
		retried = len(wants)

		// Re-stamp needs_pick on any manual segment so a re-armed want lands back
		// under the manual gate rather than auto-searching. The query's hold IS
		// NULL guard makes this idempotent on the segment's already-held wants.
		for _, seg := range []model.WantSegment{model.WantSegmentBackfill, model.WantSegmentOngoing} {
			if tracking.AutonomyFor(seg) == model.AutonomyManual {
				if _, err := r.HoldWantsForSegment(ctx, trackingID, string(seg)); err != nil {
					return err
				}
			}
		}

		// A retry reactivates a tracking Cancel parked in 'canceled'; a no-op for
		// an already-active one.
		if tracking.State != string(model.TrackingActive) {
			tracking, err = r.SetTrackingState(ctx, trackingID, string(model.TrackingActive))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.Tracking{}, 0, err
	}
	return tracking, retried, nil
}

func (s *TrackingService) List(ctx context.Context) ([]model.Tracking, error) {
	return s.repo.ListTrackings(ctx)
}

func (s *TrackingService) ListWants(ctx context.Context, trackingID uuid.UUID) ([]model.Want, error) {
	return s.repo.ListWantsByTracking(ctx, trackingID)
}

func (s *TrackingService) ListRequesters(ctx context.Context, trackingID uuid.UUID) ([]model.TrackingRequester, error) {
	return s.repo.ListRequestersByTracking(ctx, trackingID)
}

// GetByTmdbID resolves the acquisition state for a media item the frontend
// knows only by TMDB id and type: the media item, its tracking, and that
// tracking's wants. A NotFound at either the media item (never requested) or the
// tracking (media item exists but untracked) step surfaces unchanged as 404 —
// the "this item is not tracked" signal. A series carries one want per in-scope
// episode (each tagged with its episodeId); a movie carries one.
func (s *TrackingService) GetByTmdbID(ctx context.Context, tmdbID int64, typ string) (model.Tracking, []model.Want, error) {
	mediaItem, err := s.repo.GetMediaItemByTmdbIDAndType(ctx, tmdbID, typ)
	if err != nil {
		return model.Tracking{}, nil, err
	}
	tracking, err := s.repo.FindTrackingByMediaItem(ctx, mediaItem.ID)
	if err != nil {
		return model.Tracking{}, nil, err
	}
	wants, err := s.repo.ListWantsByTracking(ctx, tracking.ID)
	if err != nil {
		return model.Tracking{}, nil, err
	}
	return tracking, wants, nil
}

// Cancel stops tracking a media item: it cancels every non-terminal want (and
// its in-flight download job, via the tested WantService cascade), then
// normalizes the tracking to 'canceled' to cover the all-terminal case where no
// want needed canceling. 'available' wants and their files are deliberately left
// intact — stop means "stop future acquisition + cancel in-flight", not delete.
func (s *TrackingService) Cancel(ctx context.Context, userID, trackingID uuid.UUID) (model.Tracking, error) {
	tracking, err := s.repo.GetTracking(ctx, trackingID) // 404 flows through
	if err != nil {
		return model.Tracking{}, err
	}

	// own/any gate: tracking.cancel.any cancels any tracking; tracking.cancel.own
	// requires the caller to be one of the tracking's requesters. Ownership is
	// membership in the requester set.
	requesters, err := s.repo.ListRequestersByTracking(ctx, trackingID)
	if err != nil {
		return model.Tracking{}, err
	}
	isOwner := false
	for _, r := range requesters {
		if r.UserID == userID {
			isOwner = true
			break
		}
	}
	ok, err := s.authz.CanOwnAny(ctx, userID, "tracking.cancel", isOwner)
	if err != nil {
		return model.Tracking{}, err
	}
	if !ok {
		return model.Tracking{}, apperrors.Forbiddenf("insufficient permissions").Op("TrackingService.Cancel")
	}

	wants, err := s.repo.ListWantsByTracking(ctx, trackingID)
	if err != nil {
		return model.Tracking{}, err
	}
	for _, w := range wants {
		if model.WantStatus(w.Status).IsTerminal() {
			continue
		}
		if _, err := s.wants.CancelWant(ctx, w.ID); err != nil {
			return model.Tracking{}, err
		}
	}

	// CancelWant already flips the tracking to 'canceled' for each want it
	// cancels, but an all-terminal tracking has no such want — set it here so the
	// state is normalized regardless.
	if tracking.State != string(model.TrackingCanceled) {
		return s.repo.SetTrackingState(ctx, trackingID, string(model.TrackingCanceled))
	}
	return tracking, nil
}
