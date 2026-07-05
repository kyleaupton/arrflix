// Package cadence answers "when should this want be searched next?" It is the
// pure domain concept behind smart scheduling: search frequency rides a curve
// anchored on a release moment (an episode's air_date, a movie's release_date)
// rather than a flat interval — frequent right after air, when releases land,
// and decaying to a daily poll for cold back-catalog gaps.
//
// This package is deliberately pure: it imports only time + stdlib, has no
// repo/db/model dependencies. The resolver in internal/service/ reads the
// want's anchor and the tracking's strategy from persistence and feeds them
// here.
package cadence

import "time"

// Strategy names a tracking's scheduling mode. The set mirrors the values
// stored in tracking.schedule_strategy.
type Strategy string

const (
	StrategySmart Strategy = "smart"
	StrategyFixed Strategy = "fixed"
)

// Smart-curve tier intervals, keyed by Δ = now - anchor (time since the release
// moment). The peak window is deliberately the tightest: in the 1–6h band after
// air, releases are actively landing and a 15m recheck catches them quickly.
const (
	tierLow    = 1 * time.Hour    // 0 ≤ Δ < 1h: releases rarely up yet
	tierPeak   = 15 * time.Minute // 1h ≤ Δ < 6h: peak release window
	tierMedium = 1 * time.Hour    // 6h ≤ Δ < 24h: late drops
	tierCatch  = 6 * time.Hour    // 24h ≤ Δ < 7d: catch-up
	tierCold   = 24 * time.Hour   // Δ ≥ 7d: cold, flat daily
)

// fixedInterval is the placeholder cadence for StrategyFixed until a
// per-tracking configurable interval lands (the fast-follow).
const fixedInterval = 6 * time.Hour

// Tier boundaries for the smart curve.
const (
	boundLow    = 1 * time.Hour
	boundPeak   = 6 * time.Hour
	boundMedium = 24 * time.Hour
	boundCatch  = 7 * 24 * time.Hour
)

// NextRunAt is pure and total: given the schedule strategy, the anchor (episode
// air_date or movie release_date), and now, it returns when the want should
// next be searched. An unknown strategy is treated as smart.
func NextRunAt(strategy Strategy, anchor, now time.Time) time.Time {
	if strategy == StrategyFixed {
		return now.Add(fixedInterval)
	}
	return smartNextRunAt(anchor, now)
}

// smartNextRunAt walks the 6-tier air-date curve. A not-yet-aired anchor fires
// exactly at the anchor — the want sits dormant until the release moment, then
// the first post-air search lands in the low tier. (This tier is unreachable in
// S1, since reconcile only creates wants for already-aired episodes; S2's
// pre-created future wants exercise it.)
func smartNextRunAt(anchor, now time.Time) time.Time {
	delta := now.Sub(anchor)
	switch {
	case delta < 0:
		return anchor
	case delta < boundLow:
		return now.Add(tierLow)
	case delta < boundPeak:
		return now.Add(tierPeak)
	case delta < boundMedium:
		return now.Add(tierMedium)
	case delta < boundCatch:
		return now.Add(tierCatch)
	default:
		return now.Add(tierCold)
	}
}

// ── Metadata refresh cadence ────────────────────────────────────────────────
//
// A second, coarser curve: not "when to search a want" but "when to re-consult
// the provider for an item's metadata". It rides item *state* (status, air
// dates, release age) rather than a release anchor, encoding the spec's
// per-state cadence tables. Also pure and total: unknown/empty inputs fall to a
// safe weekly default, never an error.

// Metadata refresh intervals, keyed by item state.
const (
	metadataDaily     = 24 * time.Hour
	metadataWeekly    = 7 * 24 * time.Hour
	metadataMonthly   = 30 * 24 * time.Hour
	metadataQuarterly = 90 * 24 * time.Hour
)

// Air-activity windows that promote an in-production series to the daily tier,
// and the released-movie age boundary between monthly and quarterly.
const (
	metadataAiringLead  = 30 * 24 * time.Hour  // next episode within 30d → daily
	metadataAiringTrail = 14 * 24 * time.Hour  // last episode within 14d → daily
	metadataMovieRecent = 365 * 24 * time.Hour // released < 1y → monthly, else quarterly
)

// Back-off growth for a failed sync: base·2^(attempt-1), capped.
const (
	metadataBackoffBase = 15 * time.Minute
	metadataBackoffCap  = 24 * time.Hour
)

// Canonical media-status tokens the cadence keys on. They mirror the locked set
// in model.MediaStatus (what model.CanonicalizeStatus writes); duplicated as
// literals here to keep this package pure — stdlib-only, no model import.
const (
	statusContinuing = "continuing"
	statusEnded      = "ended"
	statusCanceled   = "canceled"
)

// MetadataRefreshInput carries the item state the refresh cadence reads. All
// fields are primitives / *time.Time so the package stays free of repo and
// model dependencies; the service reads them off the details payload at
// apply-time and feeds them here.
type MetadataRefreshInput struct {
	Type           string     // "movie" | "series"
	Status         string     // canonical status token, may be ""
	ReleaseDate    *time.Time // movie release / series first-air
	LastAirDate    *time.Time // series only
	NextEpisodeAir *time.Time // series only
	LastEpisodeAir *time.Time // series only
	InProduction   bool
}

// MetadataRefreshAt returns when an item's metadata is next due for refresh,
// derived from its state. Pure and total.
func MetadataRefreshAt(in MetadataRefreshInput, now time.Time) time.Time {
	if in.Type == "series" {
		return now.Add(seriesRefreshInterval(in, now))
	}
	return now.Add(movieRefreshInterval(in, now))
}

// seriesRefreshInterval encodes the series cadence table: ended/canceled series
// are structurally frozen (monthly); a continuing or in-production series with
// live air activity — a new episode within 30 days or one aired in the last 14
// — refreshes daily (that window is where new episodes and slipped air dates
// surface); everything else (quiet in-production, upcoming, unknown, unset)
// falls to weekly.
func seriesRefreshInterval(in MetadataRefreshInput, now time.Time) time.Duration {
	if in.Status == statusEnded || in.Status == statusCanceled {
		return metadataMonthly
	}

	if in.Status == statusContinuing || in.InProduction {
		if in.NextEpisodeAir != nil {
			if d := in.NextEpisodeAir.Sub(now); d >= 0 && d <= metadataAiringLead {
				return metadataDaily
			}
		}
		if in.LastEpisodeAir != nil {
			if d := now.Sub(*in.LastEpisodeAir); d >= 0 && d <= metadataAiringTrail {
				return metadataDaily
			}
		}
	}

	return metadataWeekly
}

// movieRefreshInterval encodes the movie cadence table: an unreleased movie
// (future or unknown release date) can still shift → weekly; released within
// the last year still accrues late edits → monthly; older than a year is
// effectively frozen → quarterly.
func movieRefreshInterval(in MetadataRefreshInput, now time.Time) time.Duration {
	if in.ReleaseDate == nil || in.ReleaseDate.After(now) {
		return metadataWeekly
	}
	if now.Sub(*in.ReleaseDate) <= metadataMovieRecent {
		return metadataMonthly
	}
	return metadataQuarterly
}

// MetadataBackoffAt returns the next due time after a failed sync: base·2^(n-1)
// from now, capped at ~24h. attempt is the failing attempt's 1-based count
// (first failure is 1). Pure and total.
func MetadataBackoffAt(attempt int, now time.Time) time.Time {
	return now.Add(metadataBackoff(attempt))
}

func metadataBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// The cap is reached by attempt 8 (15m·2^7 = 32h > 24h); clamp the exponent
	// so a stuck row that keeps incrementing never triggers a pathological shift.
	shift := attempt - 1
	if shift > 8 {
		return metadataBackoffCap
	}
	if d := metadataBackoffBase << shift; d < metadataBackoffCap {
		return d
	}
	return metadataBackoffCap
}
