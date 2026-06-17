package service

import (
	"context"

	"github.com/google/uuid"

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
}

func NewTrackingService(r *repo.Repository, wants *WantService) *TrackingService {
	return &TrackingService{repo: r, wants: wants}
}

func (s *TrackingService) Get(ctx context.Context, id uuid.UUID) (model.Tracking, error) {
	return s.repo.GetTracking(ctx, id)
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
func (s *TrackingService) Cancel(ctx context.Context, trackingID uuid.UUID) (model.Tracking, error) {
	tracking, err := s.repo.GetTracking(ctx, trackingID) // 404 flows through
	if err != nil {
		return model.Tracking{}, err
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
