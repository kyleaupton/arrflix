// Package resolvers contains the matcher's identity-signal implementations.
// Each resolver is independent: it knows how to read one kind of signal from
// a file and emit candidate identities with resolver-local confidence. The
// aggregator (in the parent matcher package) combines them, validates against
// the metadata provider, and assigns the final banded outcome.
package resolvers

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/metadata"
)

// PathEmbed is the Tier-1 resolver for TRaSH-style embedded provider IDs
// in file or folder names:
//
//	.../Movie Title (2020) {tmdb-12345}/Movie.Title.2020.mkv
//	.../Series Title {tvdb-67890}/Season 01/S01E03.mkv
//	.../Movie Title (2020) {imdb-tt1234567}.mkv
//
// Resolver-local confidence is 1.0 — the regex is ours and the match is
// unambiguous. The aggregator's validation step against the metadata
// provider downgrades the final number (0.99 / 0.95 / 0.0) per the
// validation→confidence ladder in the matching spec.
//
// Episode numbering (S##E## anywhere in the path) is extracted opportunistically
// and attached to the candidate when present; movies just leave it nil.
//
// Phase-3 cutover: `internal/identity/` and its callers (scan.go) are still
// live; the regex below is a *port* of that package's logic, copied
// verbatim so the original can be deleted cleanly when Phase 3 lands.
type PathEmbed struct{}

// embeddedIDPattern matches TRaSH-style provider IDs in a path. The
// expression is a verbatim port of internal/identity/identity.go's
// `(tvdb|tmdb|imdb)-((?:tt)?\d+)` — it deliberately doesn't anchor on
// brackets or braces so both `{tmdb-X}` and `[tmdb-X]` shapes match,
// matching the existing test corpus and the TRaSH naming convention.
var embeddedIDPattern = regexp.MustCompile(`(tvdb|tmdb|imdb)-((?:tt)?\d+)`)

// seasonEpisodePattern matches S##E## anywhere in the path. Verbatim
// port of internal/identity/identity.go's `[sS](\d+)[eE](\d+)` — kept
// permissive (no anchoring on separators) to match Sonarr/Plex naming
// variants in the wild.
var seasonEpisodePattern = regexp.MustCompile(`[sS](\d+)[eE](\d+)`)

// NewPathEmbed returns a PathEmbed resolver. The resolver carries no
// state — construction is here for symmetry with NameParse, whose
// metadata-provider dependency makes the constructor non-optional.
func NewPathEmbed() *PathEmbed { return &PathEmbed{} }

// Name returns the resolver's stable identifier. Matches the catalog in
// specs/modules/matching/README.md.
func (p *PathEmbed) Name() string { return "path-embed" }

// Tier returns Tier1 — the path embed is a ground-truth signal that
// short-circuits the rest of the pipeline when it validates.
func (p *PathEmbed) Tier() matcher.Tier { return matcher.Tier1 }

// Available reports whether the resolver can run. PathEmbed is always
// available: it's pure path parsing with no upstream dependency.
func (p *PathEmbed) Available(_ context.Context) bool { return true }

// Resolve scans the file's path for embedded provider IDs and emits one
// candidate per ID found. The path is scanned as a single string —
// FindAllStringSubmatch over the whole path catches IDs in any parent
// directory (the typical TRaSH layout has the ID in the show/movie
// folder, not on the leaf filename).
//
// Each match becomes a Candidate with the resolver-local confidence at
// 1.0. Cross-provider IDs (IMDb/TVDB) are emitted in their own source
// namespace — the aggregator's validation step calls the provider to
// resolve them to TMDB, recording the mapping in evidence.
//
// Multiple embeds in the same path (e.g. someone wrote both `{tmdb-X}`
// and `{imdb-ttY}` on a folder) all flow through as separate candidates;
// the aggregator's cross-resolver merging will compound their
// corroboration if they validate to the same canonical identity.
func (p *PathEmbed) Resolve(_ context.Context, file matcher.FileRef) (matcher.ResolverResult, error) {
	matches := embeddedIDPattern.FindAllStringSubmatch(file.Path, -1)
	if len(matches) == 0 {
		return matcher.ResolverResult{}, nil
	}

	episode := seasonEpisodeFromPath(file.Path)

	// Dedup identical (source, id) pairs — the same ID often appears
	// in both the folder and the filename. Without dedup the
	// aggregator's corroboration step would inflate confidence from a
	// single piece of evidence.
	seen := map[matcher.ExternalRef]struct{}{}
	cands := make([]matcher.Candidate, 0, len(matches))
	// Evidence records each raw match with its source kind for the
	// decision log. Kept compact — see aggregator.evidenceCapBytes.
	type matchLog struct {
		Source string `json:"source"`
		ID     string `json:"id"`
		Raw    string `json:"raw"`
	}
	type evidencePayload struct {
		Matches []matchLog          `json:"matches"`
		Episode *matcher.EpisodeRef `json:"episode,omitempty"`
	}
	ev := evidencePayload{Episode: episode}

	for _, m := range matches {
		// m[1] is the provider, m[2] is the id (with possible "tt" prefix for imdb).
		src, ok := sourceFromToken(m[1])
		if !ok {
			continue
		}
		ref := matcher.ExternalRef{Source: src, ExternalID: m[2]}
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		cands = append(cands, matcher.Candidate{
			Ref:        ref,
			Episode:    episode,
			Confidence: 1.0,
		})
		ev.Matches = append(ev.Matches, matchLog{
			Source: m[1],
			ID:     m[2],
			Raw:    m[0],
		})
	}

	if len(cands) == 0 {
		return matcher.ResolverResult{}, nil
	}

	raw, err := json.Marshal(ev)
	if err != nil {
		// Marshalling our own evidence shouldn't fail; surface the
		// candidates without evidence rather than dropping the match.
		return matcher.ResolverResult{Candidates: cands}, nil
	}
	return matcher.ResolverResult{Candidates: cands, Evidence: raw}, nil
}

// sourceFromToken maps a regex-captured provider token to the
// metadata.ExternalSource enum. Unknown tokens drop the candidate
// (defensive — the regex only emits the three known values).
func sourceFromToken(tok string) (metadata.ExternalSource, bool) {
	switch tok {
	case "tmdb":
		return metadata.SourceTMDB, true
	case "tvdb":
		return metadata.SourceTVDB, true
	case "imdb":
		return metadata.SourceIMDB, true
	default:
		return "", false
	}
}

// seasonEpisodeFromPath extracts an S##E## pair from anywhere in the
// path, returning nil when no match is found. Errors from strconv are
// silently swallowed — the regex's `\d+` capture is bounded by the int
// width on every realistic input, and a panic-inducing overflow would
// be a bug in the corpus, not the resolver.
func seasonEpisodeFromPath(path string) *matcher.EpisodeRef {
	m := seasonEpisodePattern.FindStringSubmatch(path)
	if len(m) == 0 {
		return nil
	}
	season, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	episode, err := strconv.Atoi(m[2])
	if err != nil {
		return nil
	}
	return &matcher.EpisodeRef{Season: season, Episode: episode}
}

// Compile-time check.
var _ matcher.Resolver = (*PathEmbed)(nil)
