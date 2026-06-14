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

// Want is the domain shape for a want row — the work item, shaped as a durable
// work-dispatch queue. MediaItemID and QualityProfileID are denormalized/
// snapshotted at creation for claim convenience. NextRunAt is the scheduler's
// home. Mirrors dbgen.Want.
type Want struct {
	ID               uuid.UUID `json:"id"`
	TrackingID       uuid.UUID `json:"trackingId"`
	MediaItemID      uuid.UUID `json:"mediaItemId"`
	QualityProfileID uuid.UUID `json:"qualityProfileId"`
	Status           string    `json:"status"`
	NextRunAt        time.Time `json:"nextRunAt"`
	AttemptCount     int32     `json:"attemptCount"`
	LastError        *string   `json:"lastError"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
