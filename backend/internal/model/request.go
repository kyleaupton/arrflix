package model

import (
	"time"

	"github.com/google/uuid"
)

// RequestStatus is the lifecycle of a request: created pending, approved (or
// denied) by an operator, then spawned into a tracking — or canceled (withdrawn
// by the requester, or cut by an operator). The DB stores it as TEXT; these
// typed consts give services and tests safe names.
type RequestStatus string

const (
	RequestPending  RequestStatus = "pending"
	RequestApproved RequestStatus = "approved"
	RequestSpawned  RequestStatus = "spawned"
	RequestDenied   RequestStatus = "denied"
	RequestCanceled RequestStatus = "canceled"
)

// Request is the domain shape for a request row — the frozen user-intent
// artifact. SpawnedTrackingID is nil until the request spawns a tracking.
// ScopeRule is the requester's series-scope choice ('all'/'future_only'; the
// 'all' default for movies, which never consult it).
//
// DecidedBy/DecidedAt/DecisionAuto are the decision provenance: who approved,
// denied, or canceled the request and when, and — for an approve — whether it
// was the automatic (per-tier permission) path (true) or a manual operator
// decision (false). They are nil on a still-pending request; DecisionAuto stays
// nil for a cancel. Mirrors dbgen.Request.
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
	DecidedBy         *uuid.UUID `json:"decidedBy"`
	DecidedAt         *time.Time `json:"decidedAt"`
	DecisionAuto      *bool      `json:"decisionAuto"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}
