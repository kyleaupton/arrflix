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
	NextRunAt        time.Time  `json:"nextRunAt"`
	AttemptCount     int32      `json:"attemptCount"`
	LastError        *string    `json:"lastError"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}
