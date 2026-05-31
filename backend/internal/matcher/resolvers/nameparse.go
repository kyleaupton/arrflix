package resolvers

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

// NameParse is the Tier-3 resolver that turns the unified parser's
// identity hint (title/year/season/episode + per-field confidence) into
// ranked TMDB candidates via metadata.MetadataProvider.Search.
//
// NameParse never re-parses the filename: the parser already ran at
// scan time and the result lives on FileRef.Parsed. If Parsed is nil
// (parse failed upstream, or the caller forgot to populate it) the
// resolver returns zero candidates rather than parsing inline — the
// matcher is a *consumer* of the parse, not a parser.
//
// Scoring is deliberately conservative: a name-parse candidate alone
// caps at 0.85 — below the Recommended preset's Auto threshold (0.85).
// Auto-matching requires Tier-1 corroboration; the spec's intent is
// that "string-matched to TMDB" is signal, but not enough signal to
// claim a confident identity on its own.
type NameParse struct {
	provider metadata.MetadataProvider
}

// nameParseBaseScore is the floor for any TMDB match. Even an
// ambiguous result has *some* signal that the title resolved.
const nameParseBaseScore = 0.50

// Per-condition bonuses applied to the base score (see Resolve docstring).
const (
	nameParseUniqueBonus     = 0.10 // single-hit search → less ambiguity
	nameParseYearMatchBonus  = 0.15 // year coincidence is meaningful
	nameParseExactTitleBonus = 0.10 // case/punct-normalized exact title
	nameParseRankDecay       = 0.10 // per-position decay through the list
)

// nameParseScoreCap caps any single resolver-local score at 0.85. With
// the Recommended preset (Auto=0.85), this enforces "name-parse alone
// can't auto-match"; Tier-1 corroboration is what crosses the bar.
const nameParseScoreCap = 0.85

// NewNameParse returns a Tier-3 resolver backed by the given metadata
// provider. A nil provider is permitted (Available() will report false),
// matching the pattern other resolvers use for optional dependencies.
func NewNameParse(provider metadata.MetadataProvider) *NameParse {
	return &NameParse{provider: provider}
}

// Name returns the resolver's stable identifier.
func (n *NameParse) Name() string { return "name-parse" }

// Tier returns Tier3 — name-parse is the primary string-parse signal
// and runs only when ground-truth tiers (path-embed, embedded-tags) haven't
// produced a confident answer.
func (n *NameParse) Tier() matcher.Tier { return matcher.Tier3 }

// Available reports whether the resolver can run. True iff a provider
// was wired; provider's own upstream availability (API key, network) is
// the provider's concern — Search will surface that as an error.
func (n *NameParse) Available(_ context.Context) bool { return n.provider != nil }

// Resolve consumes the parser's identity hint on FileRef.Parsed and
// queries the metadata provider, scoring each result as:
//
//	base = 0.50
//	if unique_search_result:   base += 0.10
//	if year_matches:           base += 0.15
//	if title_exact_match:      base += 0.10
//	base -= 0.10 * rank        // rank 0 = top hit, decays linearly
//	base *= parsed_title_confidence
//	clamp to [0, 0.85]
//
// The cap lands name-parse-only candidates in confident_review at most;
// auto-matching requires Tier-1 corroboration. The math is calibrated
// against the Recommended preset (Auto=0.85, ReviewLow=0.7, LowMin=0.5);
// per the matching spec OQ#8 these are educated guesses pending the
// labeled corpus.
//
// Year disambiguation is *not* applied as a hard filter — the aggregator
// owns the soft year-mismatch penalty so name-parse just surfaces what
// the provider returned, ordered by score. Episode numbering (S##E##)
// is attached from the parse's numbering namespace as-is; anime
// absolute-number conversion to canonical episode identity is the
// future v2 `episode-numbering` resolver's job (matching spec § The
// resolver catalog).
func (n *NameParse) Resolve(ctx context.Context, file matcher.FileRef) (matcher.ResolverResult, error) {
	if n.provider == nil || file.Parsed == nil {
		return matcher.ResolverResult{}, nil
	}
	hint := file.Parsed.Identity
	title := strings.TrimSpace(hint.Title.Value)
	if title == "" {
		return matcher.ResolverResult{}, nil
	}

	// Soft-bias the search by year and entity type when the parser had
	// enough signal. Both are biases, not filters — TmdbProvider.Search
	// reorders but does not drop, matching the spec.
	opts := metadata.SearchOptions{}
	if hint.Year.Value != 0 {
		yr := hint.Year.Value
		opts.Year = &yr
	}
	if et, ok := entityTypeFromHint(hint.TypeHint.Value); ok {
		opts.Type = &et
	}

	results, err := n.provider.Search(ctx, title, opts)
	if err != nil {
		return matcher.ResolverResult{}, err
	}

	episode := episodeRefFromHint(hint)
	cands := make([]matcher.Candidate, 0, len(results))
	for i, r := range results {
		score := scoreSearchResult(r, hint, len(results), i)
		if score <= 0 {
			continue
		}
		cands = append(cands, matcher.Candidate{
			Ref: matcher.ExternalRef{
				Source:     r.Source,
				ExternalID: r.ExternalID,
			},
			Title:      r.Title,
			Year:       r.Year,
			Type:       string(r.Type),
			PosterPath: r.PosterPath,
			Episode:    episode,
			Confidence: score,
		})
	}

	// Evidence captures the inputs that drove the scoring so the
	// decision log can explain a name-parse outcome. Truncation lives
	// in the aggregator; we just emit.
	type evidencePayload struct {
		Query       string               `json:"query"`
		Year        int                  `json:"year,omitempty"`
		TypeHint    string               `json:"typeHint,omitempty"`
		TitleConf   float64              `json:"titleConfidence"`
		Results     []metadata.Candidate `json:"results,omitempty"`
		EpisodeHint *matcher.EpisodeRef  `json:"episode,omitempty"`
	}
	payload := evidencePayload{
		Query:       title,
		Year:        hint.Year.Value,
		TypeHint:    hint.TypeHint.Value,
		TitleConf:   hint.Title.Confidence,
		Results:     results,
		EpisodeHint: episode,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return matcher.ResolverResult{Candidates: cands}, nil
	}
	return matcher.ResolverResult{Candidates: cands, Evidence: raw}, nil
}

// scoreSearchResult applies the per-candidate math documented on Resolve.
// Returns a score in [0, nameParseScoreCap].
func scoreSearchResult(r metadata.Candidate, hint parsing.IdentityAttrs, total, rank int) float64 {
	score := nameParseBaseScore
	if total == 1 {
		score += nameParseUniqueBonus
	}
	if hint.Year.Value != 0 && r.Year != 0 && hint.Year.Value == r.Year {
		score += nameParseYearMatchBonus
	}
	if titleEquivalent(r.Title, hint.Title.Value) {
		score += nameParseExactTitleBonus
	}
	score -= nameParseRankDecay * float64(rank)

	// Propagate parser uncertainty about the title. A confidence of 1.0
	// (the parser nailed the title) leaves the score untouched; a
	// confident 0.5 halves it.
	if conf := hint.Title.Confidence; conf > 0 {
		score *= conf
	}

	if score < 0 {
		return 0
	}
	if score > nameParseScoreCap {
		return nameParseScoreCap
	}
	return score
}

// titleEquivalent compares two titles after lowercasing and stripping
// non-alphanumerics. The exact-match bonus is meant to reward "we got
// the title right at the character level", not "we got the same TMDB
// record" — punctuation drift between release titles and TMDB ("WALL-E"
// vs "Wall E") shouldn't cost the bonus.
func titleEquivalent(a, b string) bool {
	return normalizeTitle(a) == normalizeTitle(b) && a != "" && b != ""
}

func normalizeTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// entityTypeFromHint maps the parser's domain-typed type hint
// ("movie"/"series") to the metadata EntityType the provider biases on.
// An empty hint returns ok=false so the caller leaves the option unset
// (no bias, rather than biasing toward a specific type).
func entityTypeFromHint(s string) (metadata.EntityType, bool) {
	switch s {
	case "movie":
		return metadata.EntityMovie, true
	case "series":
		return metadata.EntitySeries, true
	default:
		return "", false
	}
}

// episodeRefFromHint pulls a canonical (season, episode) pair off the
// parser's numbering namespace when the parse used season-episode
// numbering and has at least one episode number. Absolute (anime) and
// daily namespaces are intentionally NOT translated here — that's the
// future v2 `episode-numbering` resolver's job (matching spec § The
// resolver catalog). Only the season-episode shape is surfaced here;
// anything else returns nil so the aggregator may band the file as
// partial_series when the series identity itself resolves.
func episodeRefFromHint(hint parsing.IdentityAttrs) *matcher.EpisodeRef {
	n := hint.Numbering
	if n.Kind != parsing.NumberingSeasonEpisode {
		return nil
	}
	eps := n.EpisodeNumbers.Value
	if len(eps) == 0 {
		return nil
	}
	return &matcher.EpisodeRef{Season: n.Season.Value, Episode: eps[0]}
}

// Compile-time check.
var _ matcher.Resolver = (*NameParse)(nil)
