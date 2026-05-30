// Package matcher resolves a file on disk to a canonical media identity
// (provider + external ID + optional episode/edition refs) and records
// the decision in `match_decision`. The pipeline is a tiered fan-out of
// `Resolver`s feeding an aggregator that handles confidence math,
// validation against the metadata provider, and outcome banding.
//
// See specs/modules/matching/README.md for the design. This package
// ships the resolver seam, the aggregator, and the persistence shape;
// the concrete resolvers (pathembed, nameparse) register into it.
package matcher

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

// Tier names where a resolver runs in the matcher pipeline. Tiers are an
// optimization: Tier 1 produces ground-truth signals (path-embedded IDs)
// that, when present and valid, let the aggregator short-circuit before
// the expensive resolvers run. The math is identical across tiers.
type Tier int

const (
	// Tier1 resolvers run first. A high-confidence Tier-1 candidate that
	// validates against the metadata provider short-circuits the pipeline.
	Tier1 Tier = 1
	// Tier2 resolvers run when Tier 1 doesn't produce a confident answer.
	// (osdb-hash lives here in v2.)
	Tier2 Tier = 2
	// Tier3 resolvers run when neither earlier tier produces a confident
	// answer. (name-parse, path-context live here.)
	Tier3 Tier = 3
)

// Resolver is the contract every identity signal implements. Resolvers
// don't know about each other or about confidence math — they emit
// candidates with their own per-resolver confidence and let the
// aggregator combine them.
type Resolver interface {
	// Name is the resolver's stable identifier. Used for evidence
	// payloads, registry lookup, and per-library toggles (v2). Lowercase
	// kebab-case to match the catalog in the matching spec.
	Name() string
	// Tier names where the resolver runs in the pipeline.
	Tier() Tier
	// Available reports whether the resolver can run right now (sidecar
	// up, API key present, etc.). Unavailable resolvers are skipped
	// silently rather than producing a no_match.
	Available(ctx context.Context) bool
	// Resolve produces 0..N candidates plus raw evidence. Returning an
	// error is reserved for genuinely-broken upstreams; an empty result
	// is the expected zero-candidate case.
	Resolve(ctx context.Context, file FileRef) (ResolverResult, error)
}

// ResolverResult is what a single resolver returns for a file. Evidence
// is the raw payload the decision log persists — capped at 8KB after
// aggregation (per matching spec OQ#10).
type ResolverResult struct {
	Candidates []Candidate
	Evidence   json.RawMessage
}

// Candidate is one possible identity surfaced by a resolver. Confidence
// is resolver-local: the aggregator validates and combines, producing
// the final number that drives the outcome band.
type Candidate struct {
	Ref ExternalRef
	// Title and Year are the resolver's best-known display identity for
	// this candidate — what the inbox renders on the suggestion card. They
	// may be empty/zero when the resolver couldn't determine them (e.g. a
	// cross-provider ID the resolver never expanded). The aggregator carries
	// them through to the ranked suggestions so Tier-3 (search-derived)
	// candidates render without a second provider call.
	Title string
	Year  int
	// Type is the candidate's display kind — metadata.EntityType as a
	// string ("movie" / "series"). The inbox reads it to pick the
	// suggestion icon and to decide whether a match-by-ID needs episode
	// inputs. Empty when the resolver couldn't determine it.
	Type string
	// Episode is set only for series-shaped identities. Nil means the
	// resolver didn't have enough signal to pin the episode; the
	// aggregator may surface that as partial_series if the series itself
	// resolves confidently.
	Episode *EpisodeRef
	// Edition is the movie cut (e.g. "directors_cut", "extended"). Free
	// text for now; a future enum lands when edition-aware matching ships
	// (matching spec § Killer UX moves #8).
	Edition *string
	// Confidence is the resolver-local certainty, 0..1.
	Confidence float64
}

// ExternalRef is a (source, id) pair in a provider's namespace. The
// aggregator validates these against the metadata provider before
// promoting them to a match_decision.
type ExternalRef struct {
	Source     metadata.ExternalSource
	ExternalID string
}

// EpisodeRef is always _canonical_ provider episode identity, never a
// raw scene/absolute number. A Western release resolves straight to it;
// anime releases (whose numbering disagrees with the provider's seasons)
// go through the future `episode-numbering` resolver (matching spec
// § Resolver catalog), which consumes parsing's numbering-namespace tag
// plus the resolved series identity.
type EpisodeRef struct {
	Season  int
	Episode int
}

// FileRef is the matcher's view of a file. Scan produces these; the
// matcher consumes them. Parsing is best-effort and nil-tolerant.
type FileRef struct {
	// ID is the stable identifier of the file in match_decision.file_id.
	// Scan's caller populates it from the file row it creates before
	// matching; direct callers (tests, drop-in flows) can mint a fresh
	// uuid per file. Intentionally not an FK in the schema — see the
	// migration's column comment.
	ID uuid.UUID
	// Path is the absolute on-disk path of the file.
	Path string
	// LibraryID is the library the file was discovered under. Used by
	// the aggregator for per-library resolver toggles (v2) and by the
	// match_decision row's audit trail.
	LibraryID uuid.UUID
	// LibraryRoot is the library's root path. Resolvers that read
	// relative path context (path-embed, path-context) consume it.
	LibraryRoot string
	// Parsed carries the unified parser's output for this file. Nil
	// when scan hasn't run the parser yet or when parsing failed.
	Parsed *parsing.ParsedRelease
}

// Registry is the matcher's resolver catalog. It's registry-shaped from
// v1 — resolvers register at startup, the aggregator iterates the
// registered set per tier. Per-library toggle is deferred (v2); the
// shape is here so it drops in without a restructure.
type Registry struct {
	resolvers []Resolver
}

// NewRegistry returns an empty registry. Resolvers are added via
// Register; order within a tier follows registration order.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a resolver to the registry. The resolver's tier is taken
// from its Tier() method.
func (r *Registry) Register(res Resolver) {
	r.resolvers = append(r.resolvers, res)
}

// ByTier returns the resolvers registered in the given tier, in
// registration order. The slice is freshly allocated so callers can
// safely modify it.
func (r *Registry) ByTier(t Tier) []Resolver {
	out := make([]Resolver, 0, len(r.resolvers))
	for _, res := range r.resolvers {
		if res.Tier() == t {
			out = append(out, res)
		}
	}
	return out
}

// All returns every registered resolver, in registration order.
func (r *Registry) All() []Resolver {
	out := make([]Resolver, len(r.resolvers))
	copy(out, r.resolvers)
	return out
}
