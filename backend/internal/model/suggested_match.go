package model

import "encoding/json"

// SuggestedMatch is one ranked alternative in a match_decision's
// ranked_candidates set. It maps to the matcher's per-resolver candidate
// view: a canonical (source, external_id) identity, the aggregated
// confidence the matcher computed for that identity, the resolvers that
// voted for it, and the raw evidence payload (the same per-resolver JSON
// the match_decision row carries, capped at ~2KB per entry on write).
//
// Display fields (Title, Year, Type) are denormalized at write-time so the
// matcher inbox can render a card without a second TMDB call. A Tier-1
// candidate sources them from the metadata.Item the aggregator validated
// against; a Tier-3 candidate from the provider's search response (no Type,
// since the search entry's kind isn't carried through). They are nullable
// because a resolver may not have determined the title/year at all.
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
	// and by re-match flows.
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
	// PosterPath is the provider's poster image path (e.g. TMDB
	// "/abc.jpg"), denormalized at write time so the inbox suggestion card
	// renders a thumbnail without a second provider call. A Tier-1
	// candidate prefers the validated Item's poster; empty when neither
	// the resolver nor the provider supplied one.
	PosterPath string `json:"posterPath,omitempty"`
}

// SuggestedExternalRef mirrors matcher.ExternalRef on the wire without
// importing the matcher package upward into model. Source is the lowercase
// provider token ("tmdb" / "imdb" / "tvdb"); ExternalID is the
// provider's namespaced ID.
type SuggestedExternalRef struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
}
