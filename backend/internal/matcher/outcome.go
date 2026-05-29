package matcher

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/metadata"
)

// Outcome is the confidence band the aggregator assigns to a file. Six
// values, per the matching spec's confidence-band table — all six ship
// in v1 and in the match_outcome DB enum, even though phase 1's lack of
// real resolvers means only no_match fires in practice.
type Outcome string

const (
	// OutcomeConfident: ≥ Auto threshold. Auto-written to media_file.
	OutcomeConfident Outcome = "confident"
	// OutcomeConfidentReview: in [ReviewLow, Auto). Auto-written but
	// surfaced in the matcher inbox for human review.
	OutcomeConfidentReview Outcome = "confident_review"
	// OutcomeLowConfidence: in [LowMin, ReviewLow) with a dominating
	// candidate. Written to unmatched_file with one strong suggestion.
	OutcomeLowConfidence Outcome = "low_confidence"
	// OutcomeAmbiguous: multiple candidates within ε of each other, or
	// best candidate below LowMin but candidates exist. Written to
	// unmatched_file with up to 5 suggestions.
	OutcomeAmbiguous Outcome = "ambiguous"
	// OutcomeNoMatch: no candidates above LowMin. Written to
	// unmatched_file with no suggestions.
	OutcomeNoMatch Outcome = "no_match"
	// OutcomePartialSeries: series identity resolved confidently but the
	// episode is unresolved. Phase 1 doesn't have resolvers that produce
	// this; the band exists so phase-3 episode-resolving resolvers don't
	// need a schema change.
	OutcomePartialSeries Outcome = "partial_series"
	// OutcomeDetached: the user explicitly removed the file from the
	// library scope ("this doesn't belong here"). The file may be moved
	// to a configured quarantine path; the match_decision row preserves
	// the audit trail. No follow-up match is expected. Phase 4 user-only
	// outcome — the aggregator never emits this.
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

// Config carries aggregator-tunable knobs. Phase 1 hardcodes the
// Recommended preset at service construction; per-library overrides
// (matcher spec § Per-library resolver toggle UX) land later.
type Config struct {
	Thresholds Thresholds
}

// DefaultConfig returns the recommended preset. The TODO in
// MatcherService points at where the real (settings-table) wiring lands.
func DefaultConfig() Config {
	return Config{Thresholds: PresetRecommended}
}

// MatchOutcomeRecord is the in-memory shape the aggregator emits per
// file. It mirrors the `match_decision` row 1:1 — service.MatchBatch
// hands the record to the repo, which writes it.
type MatchOutcomeRecord struct {
	// FileID is the identifier of the file being matched. In phase 1 the
	// producer of the ID is the caller (typically the future scan loop);
	// the match_decision row carries it without an FK because the file
	// may live in either media_file or unmatched_file depending on the
	// outcome, and the canonical producer is wired in phase 3.
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
	// final aggregated confidence (descending). The slice is capped at
	// RankedCandidatesLimit (5) and excludes candidates below LowMin —
	// it mirrors what the matcher inbox surfaces as ranked suggestions.
	// Empty for no_match; one entry for confident / confident_review /
	// low_confidence (the chosen candidate); up to 5 for ambiguous.
	// partial_series carries the series-level chosen candidate only.
	//
	// Each entry carries the validated metadata.Item when the aggregator's
	// Tier-1 validation populated one (matching by post-rewrite
	// ExternalRef); nil otherwise. Persistence code uses this to write
	// per-suggestion title/year onto unmatched_file.suggested_matches
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
	// re-match / un-match writes "user:<uuid>" via separate code paths
	// (phase 4).
	DecidedBy string
	DecidedAt time.Time
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

// RankedCandidatesLimit caps the post-merge candidate slice the
// aggregator carries on the outcome record. Matches the matching spec's
// "up to 5 suggestions" semantics for ambiguous outcomes; deeper lists
// would inflate unmatched_file.suggested_matches without adding signal.
const RankedCandidatesLimit = 5

// RankedCandidate is one entry in MatchOutcomeRecord.RankedCandidates —
// a post-aggregator candidate paired with its validated metadata.Item
// (when available). Order across the slice is by Confidence descending.
type RankedCandidate struct {
	Ref        ExternalRef
	Episode    *EpisodeRef
	Edition    *string
	Confidence float64
	// Item is the validated metadata.Item when the aggregator ran the
	// Tier-1 validation step and matched this candidate's (post-rewrite)
	// ExternalRef. Nil for Tier-3-only candidates or when the provider
	// was absent — persistence code falls back to the chosen item's
	// title/year in that case.
	Item *metadata.Item
}
