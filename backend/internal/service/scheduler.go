package service

import (
	"context"
	"time"

	"github.com/kyleaupton/arrflix/internal/cadence"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// SchedulerService resolves a want's air-date-aware search cadence. It is the
// persistence-aware half of the scheduler: it reads the want's release anchor
// and the tracking's schedule strategy from the repo, then defers to the pure
// cadence module for the curve. Mirrors ReconcileService — holds the repo,
// resolves inputs, calls a pure domain function.
type SchedulerService struct {
	repo *repo.Repository
	log  *logger.Logger
}

func NewSchedulerService(r *repo.Repository, l *logger.Logger) *SchedulerService {
	return &SchedulerService{repo: r, log: l}
}

// NextSearchAt resolves the want's anchor (episode air_date / movie
// release_date) and its tracking's schedule strategy, then defers to
// cadence.NextRunAt. The acquisition worker calls it for the "no eligible
// release" recheck (S1); reconcile will reuse it to pre-create future wants at
// air_date (S2).
//
// Anchor fallback: an undated episode or unreleased movie has no release moment
// to ride, so the curve anchors on the want's creation time instead — matching
// the spec's "anchor on request time if the movie is unreleased".
func (s *SchedulerService) NextSearchAt(ctx context.Context, want model.Want, now time.Time) (time.Time, error) {
	// air_date and release_date are DATE columns (no time component), so a real
	// anchor is midnight UTC of that day — the curve's sub-day tiers (peak/low)
	// are reachable only for the CreatedAt fallback, which carries full precision.
	anchor := want.CreatedAt

	if want.EpisodeID != nil {
		ep, err := s.repo.GetEpisode(ctx, *want.EpisodeID)
		if err != nil {
			return time.Time{}, err
		}
		if ep.AirDate != nil {
			anchor = *ep.AirDate
		}
	} else {
		item, err := s.repo.GetMediaItem(ctx, want.MediaItemID)
		if err != nil {
			return time.Time{}, err
		}
		if item.ReleaseDate != nil {
			anchor = *item.ReleaseDate
		}
	}

	tracking, err := s.repo.GetTracking(ctx, want.TrackingID)
	if err != nil {
		return time.Time{}, err
	}
	strategy := cadence.Strategy(tracking.ScheduleStrategy)

	return cadence.NextRunAt(strategy, anchor, now), nil
}
