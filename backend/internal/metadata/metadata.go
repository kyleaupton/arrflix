// Package metadata is the matcher-facing slice of the metadata provider
// abstraction. Full provider responsibilities (discovery, series-structure
// sync, image URL composition, capability declaration) live in the metadata
// module proper, which is not yet built; this package exposes only the
// three operations the matcher's aggregator needs to validate candidate
// identities and resolve cross-provider IDs.
//
// The seam is deliberately narrow: it lets `internal/matcher` ship without
// reaching into the `service` package directly, and it gives the future
// metadata module a stable contract to grow into rather than a refactor
// target. See specs/modules/metadata/README.md § "Provider abstraction".
package metadata

import (
	"context"
	"errors"
)

// MetadataProvider is the matcher-facing slice of a metadata provider.
// Full provider abstraction (discovery, series-structure sync, image URL,
// capability declaration) is out of scope for this seam; those land with
// the metadata module proper.
type MetadataProvider interface {
	// LookupByID returns the canonical item for a (source, id) pair, or
	// (nil, nil) if the ID is unknown or deleted. Handles redirects: if
	// the upstream merged this record into another, the result carries
	// the new ID and Redirected==true. The aggregator uses this for the
	// validation→confidence ladder.
	LookupByID(ctx context.Context, source ExternalSource, id string) (*Item, error)

	// Search returns candidates for a free-text query. Optional year and
	// entity-type narrow the result set; both are soft — they prefer
	// matching candidates rather than excluding non-matching ones. Year
	// mismatch is reflected in candidate ordering / score, not by drop.
	Search(ctx context.Context, query string, opts SearchOptions) ([]Candidate, error)

	// ResolveCrossProviderID converts an ID in one source's namespace
	// into a TMDB ID, if a mapping exists. Wraps TMDB's FindByID under
	// the seam (IMDb→TMDB, TVDB→TMDB). Returns ("", nil) if no mapping.
	ResolveCrossProviderID(ctx context.Context, fromSource ExternalSource, id string) (tmdbID string, err error)
}

// ExternalSource names a provider's external-ID namespace. Drawn from an
// open registry: today tmdb / imdb / tvdb, with anidb reserved. Values are
// lowercase strings to match the wire shape used elsewhere in the codebase
// and the upstream TMDB `external_source` parameter.
type ExternalSource string

const (
	SourceTMDB ExternalSource = "tmdb"
	SourceIMDB ExternalSource = "imdb"
	SourceTVDB ExternalSource = "tvdb"
)

// EntityType names the kind of media identity behind a provider record.
// The aggregator uses it to route LookupByID to the right upstream endpoint
// (movie vs series) and to bias Search results.
type EntityType string

const (
	EntityMovie   EntityType = "movie"
	EntitySeries  EntityType = "series"
	EntityEpisode EntityType = "episode"
)

// Item is the canonical identity record returned by LookupByID. Fields are
// intentionally minimal — the matcher uses them for validation + ranking,
// not for display. Display reads from `media_item` rows the matcher (and
// the wider metadata module) write separately.
type Item struct {
	Source     ExternalSource
	ExternalID string
	Type       EntityType
	Title      string
	// Year is 0 when the provider doesn't carry a release date or the
	// date hasn't been set. The aggregator's year-penalty treats 0 as
	// "unknown" rather than "1900-style hard miss".
	Year int
	// Redirected is true when LookupByID followed a merge — i.e. the
	// upstream returned a different ID than the one requested. ExternalID
	// in that case is the new (canonical) ID; callers should record the
	// redirect in match-decision evidence and persist the new ID.
	Redirected bool
}

// Candidate is one possible identity returned by Search, ranked by the
// provider. Shape mirrors Item plus a provider-supplied popularity signal
// used as a tiebreaker by the matcher's aggregator.
type Candidate struct {
	Source     ExternalSource
	ExternalID string
	Type       EntityType
	Title      string
	Year       int
	Popularity float64
}

// SearchOptions narrows a Search call. Both filters are soft: an
// implementation may use them to bias ordering but must not silently drop
// candidates that don't match.
type SearchOptions struct {
	// Year, when set, biases ranking toward candidates with a matching
	// release year. Nil means "no bias".
	Year *int
	// Type, when set, biases toward candidates of the requested entity
	// type. Nil means "no bias".
	Type *EntityType
	// Future: MaxPages int — for now the implementation chooses (the TMDB
	// adapter starts with page 1 only; paged-stop-on-confidence lands
	// when the matcher's name-parse resolver does).
}

// Sentinel errors. Use errors.Is at call sites; never reach for HTTP
// status codes. ErrProviderUnavailable is the configuration case (e.g.
// TMDB API key not set); ErrUpstreamUnavailable is the transient
// network/5xx case the aggregator can retry or fall through on.
var (
	ErrProviderUnavailable = errors.New("metadata: provider unavailable")
	ErrUpstreamUnavailable = errors.New("metadata: upstream unavailable")
)
