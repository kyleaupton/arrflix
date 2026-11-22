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

// NextSearchAt resolves the want's anchor (episode air_date / movie search
// date) and its tracking's schedule strategy, then defers to cadence.NextRunAt.
// The acquisition worker calls it after every search that found nothing to
// grab, and the movie spawn uses it to create a want dormant until its date.
//
// A movie's anchor is its search date (cadence.MovieSearchFrom): the
// home-release date minus a day, or theatrical + 30 days when no home date is
// known. While that date is ahead, it is the answer whatever the strategy — no
// search is issued for a film before it could plausibly be obtained
// (REQ-UNREL-004).
//
// Anchor fallback: an undated episode or a movie with no dates at all has no
// release moment to ride, so the curve anchors on the want's creation time.
func (s *SchedulerService) NextSearchAt(ctx context.Context, want model.Want, now time.Time) (time.Time, error) {
	// air_date and release dates are DATE columns (no time component), so a real
	// anchor is midnight UTC of a day — the curve's sub-day tiers (peak/low) are
	// reachable only for the CreatedAt fallback, which carries full precision.
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
		if from := MovieSearchFrom(item); from != nil {
			if from.After(now) {
				return *from, nil
			}
			anchor = *from
		}
	}

	tracking, err := s.repo.GetTracking(ctx, want.TrackingID)
	if err != nil {
		return time.Time{}, err
	}
	strategy := cadence.Strategy(tracking.ScheduleStrategy)

	return cadence.NextRunAt(strategy, anchor, now), nil
}

// MovieSearchFrom is a movie's search date read off its stored release dates:
// when searching for it should start. Nil when the movie has no dates at all.
func MovieSearchFrom(item model.MediaItem) *time.Time {
	return cadence.MovieSearchFrom(item.HomeReleaseDate, item.ReleaseDate)
}

// dormantUntil is the next_run_at a fresh or re-armed movie want is given: the
// movie's search date while it is still ahead, nil (due now) otherwise.
func dormantUntil(item model.MediaItem, now time.Time) *time.Time {
	if from := MovieSearchFrom(item); from != nil && from.After(now) {
		return from
	}
	return nil
}
