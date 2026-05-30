package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MatchDecision is the domain shape for a match_decision row — the
// matcher's decision-log artifact. One row per consequential identity
// decision (auto-match, manual match, re-match, un-match, detach).
//
// Outcome is the band string written by the matcher's aggregator (one
// of 'confident', 'confident_review', 'low_confidence', 'ambiguous',
// 'no_match', 'partial_series', 'detached'). The model carries it as
// a plain string so the model package doesn't depend on internal/matcher.
//
// Chosen* fields are populated only when a winning candidate exists
// (confident / confident_review / low_confidence / partial_series).
// ResolversConsulted + Evidence carry the audit payload behind the
// matcher inbox's "why didn't this match" affordance.
//
// SupersededAt / SupersededBy track the supersede chain: a re-match
// writes a new row and points the prior row's SupersededBy at the new
// id. Current decision for a file = the row with SupersededAt IS NULL.
//
// See specs/modules/matching/README.md § "The match-decision artifact".
type MatchDecision struct {
	ID                 int64           `json:"id"`
	FileID             uuid.UUID       `json:"fileId"`
	Outcome            string          `json:"outcome"`
	ChosenSource       *string         `json:"chosenSource,omitempty"`
	ChosenExternalID   *string         `json:"chosenExternalId,omitempty"`
	ChosenSeason       *int32          `json:"chosenSeason,omitempty"`
	ChosenEpisode      *int32          `json:"chosenEpisode,omitempty"`
	ChosenEdition      *string         `json:"chosenEdition,omitempty"`
	Confidence         float64         `json:"confidence"`
	RankedCandidates   json.RawMessage `json:"rankedCandidates,omitempty"`
	ResolversConsulted json.RawMessage `json:"resolversConsulted"`
	Evidence           json.RawMessage `json:"evidence"`
	EvidenceTruncated  bool            `json:"evidenceTruncated"`
	DecidedBy          string          `json:"decidedBy"`
	DecidedAt          time.Time       `json:"decidedAt"`
	SupersededAt       *time.Time      `json:"supersededAt,omitempty"`
	SupersededBy       *int64          `json:"supersededBy,omitempty"`
}
