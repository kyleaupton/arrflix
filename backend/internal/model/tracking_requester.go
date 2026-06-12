package model

import (
	"time"

	"github.com/google/uuid"
)

// TrackingRequester is the domain shape for a tracking_requester row — live
// per-requester intent and the dedup association between a user and a tracking.
// Mirrors dbgen.TrackingRequester.
type TrackingRequester struct {
	TrackingID uuid.UUID `json:"trackingId"`
	UserID     uuid.UUID `json:"userId"`
	Tier       string    `json:"tier"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
