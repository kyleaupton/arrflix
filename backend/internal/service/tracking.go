package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// TrackingService exposes thin reads over the tracking primitive and its wants
// and requesters — enough for observability and tests. State transitions
// (pause/cancel/archive) are out of PoC scope; the spawn write surface lives on
// RequestService.
type TrackingService struct {
	repo *repo.Repository
}

func NewTrackingService(r *repo.Repository) *TrackingService {
	return &TrackingService{repo: r}
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

// GetByTmdbID resolves the acquisition state for a movie the frontend knows
// only by TMDB id: the media item, its tracking, and that tracking's wants.
// Movie-only in the PoC, so the domain is fixed. A NotFound at either the media
// item (never requested) or the tracking (media item exists but untracked) step
// surfaces unchanged as 404 — the "this movie is not tracked" signal.
func (s *TrackingService) GetByTmdbID(ctx context.Context, tmdbID int64) (model.Tracking, []model.Want, error) {
	mediaItem, err := s.repo.GetMediaItemByTmdbIDAndType(ctx, tmdbID, string(parsing.DomainMovie))
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
