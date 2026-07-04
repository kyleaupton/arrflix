package model

import (
	"time"

	"github.com/google/uuid"
)

// WantStatus is the lifecycle of a want as it moves through the acquisition
// pipeline. The DB stores it as TEXT; these typed consts give services and
// tests safe names. pending/searching/grabbed/downloading/imported are
// non-terminal; available/failed/canceled are terminal.
type WantStatus string

const (
	WantPending     WantStatus = "pending"
	WantSearching   WantStatus = "searching"
	WantGrabbed     WantStatus = "grabbed"
	WantDownloading WantStatus = "downloading"
	WantImported    WantStatus = "imported"
	WantAvailable   WantStatus = "available"
	WantFailed      WantStatus = "failed"
	WantCanceled    WantStatus = "canceled"
)

// WantSegment classifies a want against its tracking's autonomy dial: backfill
// (the atom's air/release date precedes the tracking's creation) or ongoing
// (dated after, or undated). Stamped once at creation and never reclassified.
type WantSegment string

const (
	WantSegmentBackfill WantSegment = "backfill"
	WantSegmentOngoing  WantSegment = "ongoing"
)

// WantHoldNeedsPick is the hold value stamped on a want owned by a manual
// segment: it stays pending/searching and visible but is never claimed by the
// acquisition worker, because the user picks the release.
const WantHoldNeedsPick = "needs_pick"

// SegmentFor classifies a want by its atom's air/release date against the
// tracking's creation time: a date strictly before createdAt is backfill;
// on-or-after, or an unknown (nil) date, is ongoing. Pure and total.
func SegmentFor(atomDate *time.Time, trackingCreatedAt time.Time) WantSegment {
	if atomDate == nil {
		return WantSegmentOngoing
	}
	if atomDate.Before(trackingCreatedAt) {
		return WantSegmentBackfill
	}
	return WantSegmentOngoing
}

// HoldForNewWant returns the hold a freshly-created want in the given segment is
// born with: 'needs_pick' when that segment's autonomy is manual, else nil (no
// hold). The result feeds CreateWantParams.Hold directly.
func HoldForNewWant(t Tracking, seg WantSegment) *string {
	if t.AutonomyFor(seg) == AutonomyManual {
		hold := WantHoldNeedsPick
		return &hold
	}
	return nil
}

// IsTerminal reports whether a want has reached a terminal state
// (available/failed/canceled) — one the acquisition pipeline won't advance out
// of on its own. The series reconciler treats only non-terminal wants as live
// when deciding what to create or cancel.
func (s WantStatus) IsTerminal() bool {
	switch s {
	case WantAvailable, WantFailed, WantCanceled:
		return true
	default:
		return false
	}
}

// Want is the domain shape for a want row — the work item, shaped as a durable
// work-dispatch queue. MediaItemID and QualityProfileID are denormalized/
// snapshotted at creation for claim convenience. EpisodeID is set for series
// wants (one want per in-scope episode) and NULL for movies. NextRunAt is the
// scheduler's home. Mirrors dbgen.Want.
type Want struct {
	ID               uuid.UUID  `json:"id"`
	TrackingID       uuid.UUID  `json:"trackingId"`
	MediaItemID      uuid.UUID  `json:"mediaItemId"`
	EpisodeID        *uuid.UUID `json:"episodeId,omitempty"`
	QualityProfileID uuid.UUID  `json:"qualityProfileId"`
	Status           string     `json:"status"`
	Segment          string     `json:"segment"`
	Hold             *string    `json:"hold,omitempty"`
	NextRunAt        time.Time  `json:"nextRunAt"`
	AttemptCount     int32      `json:"attemptCount"`
	LastError        *string    `json:"lastError"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}
