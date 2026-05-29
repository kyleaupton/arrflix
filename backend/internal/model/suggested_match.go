package model

import "encoding/json"

// SuggestedMatch is one ranked alternative in a match_decision's
// ranked_candidates set. It maps to the matcher's per-resolver candidate
// view: a canonical (source, external_id) identity, the aggregated
// confidence the matcher computed for that identity, the resolvers that
// voted for it, and the raw evidence payload (the same per-resolver JSON
// the match_decision row carries, capped at ~2KB per entry on write).
//
// Display fields (Title, Year, Type) are denormalized at write-time from
// the metadata.Item the aggregator validated against, so the matcher
// inbox can render a card without a second TMDB call. They are nullable
// because a low_confidence outcome may surface a suggestion the
// aggregator only loosely validated (Tier-3 search results don't carry
// a fresh title/year — they come from the provider's search response
// and Scan writes those through).
//
// Shape follows specs/modules/matching/README.md § "What evolves":
//
//	{ external_ref, confidence, contributing_resolvers, evidence,
//	  title, year, type }
type SuggestedMatch struct {
	// ExternalRef is the canonical (source, external_id) for the
	// suggested identity. Always populated.
	ExternalRef SuggestedExternalRef `json:"externalRef"`
	// Confidence is the matcher's final aggregated value for this
	// candidate, in [0..1]. For low_confidence outcomes this is one
	// strong candidate; for ambiguous outcomes the matcher writes up
	// to 5 suggestions ranked by confidence.
	Confidence float64 `json:"confidence"`
	// ContributingResolvers names the resolvers whose candidates
	// merged into this suggestion (e.g. ["path-embed", "name-parse"]).
	// Read by the matcher inbox's "why didn't this match" affordance
	// and by re-match flows in Phase 4.
	ContributingResolvers []string `json:"contributingResolvers,omitempty"`
	// Evidence is the per-resolver JSON payload the matcher emitted
	// for this suggestion. Whole-row capping (8KB) lives at the
	// match_decision level; per-suggestion entries are capped softer
	// (~2KB) at scan persist time so 5 suggestions still fit in a
	// reasonably-sized JSONB column.
	Evidence json.RawMessage `json:"evidence,omitempty"`
	// Title/Year/Type are denormalized for display. Empty/zero when
	// the aggregator didn't validate the candidate (e.g. a Tier-3
	// search result the matcher decided wasn't strong enough to
	// validate up-front).
	Title string `json:"title,omitempty"`
	Year  int    `json:"year,omitempty"`
	Type  string `json:"type,omitempty"` // "movie" or "series"
}

// SuggestedExternalRef mirrors matcher.ExternalRef on the wire without
// importing the matcher package upward into model. Source is the lowercase
// provider token ("tmdb" / "imdb" / "tvdb"); ExternalID is the
// provider's namespaced ID.
type SuggestedExternalRef struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
}
