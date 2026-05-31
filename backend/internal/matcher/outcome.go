package matcher

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/metadata"
)

// Outcome is the confidence band assigned to a file. All values are
// present in the match_outcome DB enum, per the matching spec's
// confidence-band table.
type Outcome string

const (
	// OutcomeConfident: ≥ Auto threshold. Identity written automatically.
	OutcomeConfident Outcome = "confident"
	// OutcomeConfidentReview: in [ReviewLow, Auto). Written automatically
	// but surfaced in the matcher inbox for human review.
	OutcomeConfidentReview Outcome = "confident_review"
	// OutcomeLowConfidence: in [LowMin, ReviewLow) with a dominating
	// candidate — one strong ranked suggestion, identity left unset.
	OutcomeLowConfidence Outcome = "low_confidence"
	// OutcomeAmbiguous: multiple candidates within ε of each other, or
	// best candidate below LowMin but candidates exist — up to 5 ranked
	// suggestions.
	OutcomeAmbiguous Outcome = "ambiguous"
	// OutcomeNoMatch: no candidates above LowMin. No suggestions.
	OutcomeNoMatch Outcome = "no_match"
	// OutcomePartialSeries: series identity resolved confidently but the
	// episode is unresolved. The band exists so episode-resolving
	// resolvers won't need a schema change.
	OutcomePartialSeries Outcome = "partial_series"
	// OutcomeDetached: the user explicitly removed the file from the
	// library scope ("this doesn't belong here"). The file may be moved
	// to a configured quarantine path; the match_decision row preserves
	// the audit trail. User-only — the aggregator never emits this.
	OutcomeDetached Outcome = "detached"
)

// Thresholds names where the bands fall on the 0..1 confidence axis. The
// math: confidence >= Auto → confident; [ReviewLow, Auto) →
// confident_review; [LowMin, ReviewLow) → low_confidence (or ambiguous
// if multiple candidates tie); < LowMin → ambiguous if candidates exist,
// no_match otherwise.
type Thresholds struct {
	Auto      float64
	ReviewLow float64
	LowMin    float64
}

// Threshold presets, per spec § Threshold presets. Recommended is the
// default; Strict raises Auto for OCD libraries; Relaxed lowers it for
// "just fill the library" use.
var (
	PresetStrict      = Thresholds{Auto: 0.95, ReviewLow: 0.7, LowMin: 0.5}
	PresetRecommended = Thresholds{Auto: 0.85, ReviewLow: 0.7, LowMin: 0.5}
	PresetRelaxed     = Thresholds{Auto: 0.70, ReviewLow: 0.5, LowMin: 0.5}
)

// PresetName maps a Thresholds value back to the named preset it matches,
// or "custom" when it matches none. Fills decided_with.preset so the
// audit trail records the preset by name, and a future "you changed
// presets — re-evaluate?" prompt can compare the decision's preset to the
// installation's current one without value-by-value math.
func (t Thresholds) PresetName() string {
	switch t {
	case PresetStrict:
		return "strict"
	case PresetRecommended:
		return "recommended"
	case PresetRelaxed:
		return "relaxed"
	default:
		return "custom"
	}
}

// Config carries aggregator-tunable knobs. Today only the hardcoded
// Recommended preset; per-library overrides land later (matcher spec
// § Per-library resolver toggle UX).
type Config struct {
	Thresholds Thresholds
}

// DefaultConfig returns the Recommended preset.
func DefaultConfig() Config {
	return Config{Thresholds: PresetRecommended}
}

// MatchOutcomeRecord is the in-memory shape the aggregator emits per
// file. It mirrors the `match_decision` row 1:1 — service.MatchBatch
// hands the record to the repo, which writes it.
type MatchOutcomeRecord struct {
	// FileID is the file this decision is about — a real FK to file.id.
	// The file row exists before the decision is written.
	FileID uuid.UUID

	Outcome Outcome

	// Chosen* fields are set only when the outcome is confident,
	// confident_review, or low_confidence (i.e. a winning candidate
	// exists). They're nil/zero otherwise.
	ChosenRef     *ExternalRef
	ChosenEpisode *EpisodeRef
	ChosenEdition *string

	// ChosenItem is the validated metadata.Item for the chosen candidate,
	// populated by the aggregator's Tier-1 validation step. Scan reads it
	// in the persist phase to avoid re-fetching movie/series details that
	// the provider already returned — when ChosenItem is set, ChosenRef
	// always refers to the same canonical identity. Nil when no candidate
	// won (no_match, ambiguous) or when validation didn't run (no provider,
	// Tier-3-only path without a follow-up validation step).
	ChosenItem *metadata.Item

	// RankedCandidates is the top-N post-merge candidate set, sorted by
	// final aggregated confidence (descending), capped at
	// RankedCandidatesLimit and excluding candidates below LowMin — what
	// the matcher inbox surfaces. Empty for no_match; one entry for
	// confident / confident_review / low_confidence; up to 5 for
	// ambiguous; the series-level candidate only for partial_series.
	//
	// Each entry carries the validated metadata.Item when Tier-1
	// validation populated one (matched by post-rewrite ExternalRef),
	// else nil — persistence uses it to write per-suggestion title/year
	// without a second LookupByID.
	RankedCandidates []RankedCandidate

	// Confidence is the final aggregated value driving the band. Zero
	// when Outcome is no_match.
	Confidence float64

	// ResolversConsulted is the high-level audit trail: which resolvers
	// ran and what they returned at a glance. Cheap to scan.
	ResolversConsulted []ResolverAudit

	// Evidence is the full per-resolver payload, capped at 8KB. When the
	// cap fires, Truncated is true and the largest evidence payloads are
	// trimmed first (see capEvidence in aggregator.go).
	Evidence  json.RawMessage
	Truncated bool

	// DecidedBy is "auto" for aggregator-emitted decisions; user-driven
	// re-match / un-match writes "user:<uuid>" via separate code paths.
	DecidedBy string
	DecidedAt time.Time

	// ParsedSnapshot is what the parser saw for this file at decision time,
	// denormalized for the inbox decide pane. Display/audit only — nothing
	// reads it back for matching logic. Nil when the file had no parse
	// (file.Parsed nil), so the snapshot is absent even on a no_match.
	ParsedSnapshot *ParsedSnapshot

	// DecidedWith is the threshold band this decision was made under,
	// stamped from the aggregator's config. Display/audit only; carries
	// the preset name via Thresholds.PresetName when persisted. Zero for
	// user-driven decisions that don't run the banding math.
	DecidedWith Thresholds
}

// ParsedSnapshot is the parser's view of a file frozen at decision time —
// the subset the inbox decide pane renders. Season/Episode are pointers
// so "no numbering" (movie, or unparsed) is distinct from season/episode
// zero. Display/audit only; the aggregator fills it from FileRef.Parsed.
type ParsedSnapshot struct {
	Title   string `json:"title"`
	Year    int    `json:"year"`
	Type    string `json:"type"`
	Season  *int   `json:"season,omitempty"`
	Episode *int   `json:"episode,omitempty"`
}

// ResolverAudit is the per-resolver line item in
// MatchOutcomeRecord.ResolversConsulted. Top-line shape: which resolver
// ran, what tier, how many candidates, the best confidence it produced.
// Full per-candidate detail lives in Evidence.
type ResolverAudit struct {
	Name           string  `json:"name"`
	Tier           Tier    `json:"tier"`
	CandidateCount int     `json:"candidateCount"`
	TopConfidence  float64 `json:"topConfidence"`
}

// RankedCandidatesLimit caps the post-merge candidate slice. Matches the
// matching spec's "up to 5 suggestions" for ambiguous outcomes; deeper
// lists add no signal.
const RankedCandidatesLimit = 5

// RankedCandidate is one entry in MatchOutcomeRecord.RankedCandidates —
// a post-aggregator candidate paired with its validated metadata.Item
// (when available). Order across the slice is by Confidence descending.
type RankedCandidate struct {
	Ref        ExternalRef
	Episode    *EpisodeRef
	Edition    *string
	Confidence float64
	// Title and Year are the candidate's display identity, denormalized
	// from the resolver's candidate so the inbox renders a suggestion card
	// without a provider call. Tier-1 candidates source these from the
	// validated Item; Tier-3 candidates from the search response. Empty/
	// zero when the resolver couldn't determine them.
	Title string
	Year  int
	// Type is the candidate's display kind ("movie" / "series"),
	// denormalized alongside Title/Year so the inbox can tag the
	// suggestion without a provider call.
	Type string
	// PosterPath is the candidate's poster image path, denormalized
	// alongside Title/Year/Type so the inbox suggestion card renders a
	// thumbnail without a provider call. A Tier-1 candidate's poster is
	// preferred from the validated Item at persist time when present.
	PosterPath string
	// Item is the validated metadata.Item when the aggregator ran the
	// Tier-1 validation step and matched this candidate's (post-rewrite)
	// ExternalRef. Nil for Tier-3-only candidates or when the provider
	// was absent — persistence code falls back to Title/Year in that case.
	Item *metadata.Item
}
