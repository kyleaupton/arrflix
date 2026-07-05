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
