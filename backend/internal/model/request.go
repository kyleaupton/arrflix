package model

import (
	"time"

	"github.com/google/uuid"
)

// RequestStatus is the lifecycle of a request: created pending, approved (or
// denied) by policy, then spawned into a tracking. The DB stores it as TEXT;
// these typed consts give services and tests safe names.
type RequestStatus string

const (
	RequestPending  RequestStatus = "pending"
	RequestApproved RequestStatus = "approved"
	RequestSpawned  RequestStatus = "spawned"
	RequestDenied   RequestStatus = "denied"
)

// Request is the domain shape for a request row — the frozen user-intent
// artifact. SpawnedTrackingID is nil until the request spawns a tracking.
// ScopeRule is the requester's series-scope choice ('all'/'future_only'; the
// 'all' default for movies, which never consult it). Mirrors dbgen.Request.
type Request struct {
	ID                uuid.UUID  `json:"id"`
	RequestedBy       uuid.UUID  `json:"requestedBy"`
	TmdbID            int64      `json:"tmdbId"`
	Type              string     `json:"type"`
	Tier              string     `json:"tier"`
	ScopeRule         string     `json:"scopeRule"`
	Status            string     `json:"status"`
	SpawnedTrackingID *uuid.UUID `json:"spawnedTrackingId"`
	DeniedReason      *string    `json:"deniedReason"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}
