package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// TrackingRequester is the domain shape for a tracking_requester row — live
// per-requester intent and the dedup association between a user and a tracking.
// Mirrors dbgen.TrackingRequester.
//
// Scope is held per-requester: ScopeRule is the preset (see internal/scope),
// ScopeSeason its only parameter (non-nil only for the 'season' rule), and
// ScopeOverrides the JSONB include/exclude lists. Movie requesters carry the
// 'all' default but never consult it (single-atom tracking).
type TrackingRequester struct {
	TrackingID     uuid.UUID       `json:"trackingId"`
	UserID         uuid.UUID       `json:"userId"`
	Tier           string          `json:"tier"`
	ScopeRule      string          `json:"scopeRule"`
	ScopeSeason    *int32          `json:"scopeSeason,omitempty"`
	ScopeOverrides json.RawMessage `json:"scopeOverrides,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}
