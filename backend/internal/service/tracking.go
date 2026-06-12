package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
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
