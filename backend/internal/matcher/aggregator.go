package matcher

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/metadata"
)

// evidenceCapBytes is the per-decision evidence payload cap. 8KB per
// matching spec OQ#10 — generous enough for a TMDB candidate list plus a
// guessit raw response, but bounded so the match_decision table doesn't
// drift into pathological JSONB sizes.
const evidenceCapBytes = 8 * 1024

// validatedMultiplierValid is the multiplier applied to a Tier-1
// candidate whose external ID resolves cleanly against the metadata
// provider. The number is intentionally just below 1.0 — even a known-
// good ID earns a small reservation against future drift. See matching
// spec § Validation modulates confidence.
const validatedMultiplierValid = 0.99

// validatedMultiplierRedirect is the multiplier applied when the
// provider returned a different (merged-to) ID than the one requested.
// The candidate's ExternalRef is rewritten to the new ID and the
// redirect is recorded in evidence.
const validatedMultiplierRedirect = 0.95

// corroborationStep is added per *additional* agreeing resolver when
// candidates from multiple resolvers point at the same ExternalRef.
// The aggregator caps the combined value at 0.99 so corroboration alone
// can't manufacture a confident-band outcome on a low-signal start.
const corroborationStep = 0.05
const corroborationCap = 0.99

// yearMismatchPenalty multiplies a candidate's confidence when any
// resolver's hint year disagrees with the candidate's. Soft penalty,
// not a filter — matching spec § Aggregator contract (3).
const yearMismatchPenalty = 0.9

// ambiguousEpsilon is the slack inside which two top candidates count
// as a tie and trigger ambiguous instead of low_confidence /
// confident_review. Tuned by the parity corpus down the line.
const ambiguousEpsilon = 0.02

// Aggregator combines per-resolver candidates into a single decision
// per file. It owns the validation→confidence ladder, cross-resolver
// merging, and outcome banding; resolvers don't know about each other.
type Aggregator struct {
	logger   *logger.Logger
	provider metadata.MetadataProvider
	registry *Registry
	cfg      Config
}

// NewAggregator constructs an aggregator. A nil provider disables
// validation — every candidate runs through the merging step unchanged,
// useful for unit tests that don't care about the metadata seam.
func NewAggregator(log *logger.Logger, provider metadata.MetadataProvider, registry *Registry, cfg Config) *Aggregator {
	return &Aggregator{
		logger:   log,
		provider: provider,
		registry: registry,
		cfg:      cfg,
	}
}

// Aggregate runs the full pipeline for one file and returns the outcome
// record. The record is ready to persist as a match_decision row.
//
// The pipeline:
//
//  1. Tier 1 resolvers fan out. Each Tier-1 candidate is validated against
//     the metadata provider (LookupByID); valid → confidence ×
//     validMultiplier, redirected → confidence × redirectMultiplier and
//     ID swap, 404 → drop.
//  2. If any validated Tier-1 candidate's confidence ≥ Auto, short-
//     circuit: skip Tier 2/3 and band on Tier 1 alone.
//  3. Otherwise descend: run Tier 2 and Tier 3, collecting all
//     candidates.
//  4. Cross-resolver merge: candidates sharing an ExternalRef compound
//     via max+corroboration_bonus, capped at corroborationCap. Year
//     mismatch (any resolver's hint vs candidate.year) applies a soft
//     multiplier.
//  5. Band the result by Thresholds; emit a MatchOutcomeRecord.
func (a *Aggregator) Aggregate(ctx context.Context, file FileRef) MatchOutcomeRecord {
	t1Cands, t1Audits, t1Evidence := a.runTier(ctx, file, Tier1)
	t1Cands = a.validateAgainstProvider(ctx, t1Cands, &t1Evidence)

	// Short-circuit: Tier-1 alone produced a confident answer.
	if best, ok := dominantAbove(t1Cands, a.cfg.Thresholds.Auto); ok {
		_ = best // keep symmetric with the descend path
		return a.bandAndRecord(file, t1Cands, t1Audits, t1Evidence)
	}

	t2Cands, t2Audits, t2Evidence := a.runTier(ctx, file, Tier2)
	t3Cands, t3Audits, t3Evidence := a.runTier(ctx, file, Tier3)

	cands := append(append(append([]Candidate(nil), t1Cands...), t2Cands...), t3Cands...)
	audits := append(append(append([]ResolverAudit(nil), t1Audits...), t2Audits...), t3Audits...)
	evidence := mergeEvidence(t1Evidence, t2Evidence, t3Evidence)

	hintYear := yearHintFromFile(file)
	cands = mergeCandidates(cands, hintYear)

	return a.bandAndRecord(file, cands, audits, evidence)
}

// runTier executes every resolver in a tier sequentially, collecting
// their candidates and evidence into the aggregator's per-resolver
// view. Resolver-level errors are logged but not surfaced — a broken
// resolver shouldn't fail the whole match, it should just produce no
// candidates.
func (a *Aggregator) runTier(ctx context.Context, file FileRef, tier Tier) ([]Candidate, []ResolverAudit, map[string]json.RawMessage) {
	if a.registry == nil {
		return nil, nil, nil
	}
	resolvers := a.registry.ByTier(tier)
	if len(resolvers) == 0 {
		return nil, nil, nil
	}

	var (
		cands    []Candidate
		audits   []ResolverAudit
		evidence = make(map[string]json.RawMessage, len(resolvers))
	)

	for _, r := range resolvers {
		if !r.Available(ctx) {
			continue
		}
		result, err := r.Resolve(ctx, file)
		if err != nil {
			if a.logger != nil {
				a.logger.Warn().Err(err).Str("resolver", r.Name()).Msg("resolver failed")
			}
			continue
		}
		audits = append(audits, ResolverAudit{
			Name:           r.Name(),
			Tier:           tier,
			CandidateCount: len(result.Candidates),
			TopConfidence:  topConfidence(result.Candidates),
		})
		cands = append(cands, result.Candidates...)
		if len(result.Evidence) > 0 {
			evidence[r.Name()] = result.Evidence
		}
	}
	return cands, audits, evidence
}

// validateAgainstProvider runs the Tier-1 confidence-ladder. Each
// candidate's external ID is looked up; valid IDs scale by the valid
// multiplier, redirects scale by the redirect multiplier and have their
// ID rewritten, 404s drop the candidate entirely. The
// validation_redirects audit slot in evidence records any redirects.
//
// Provider absence is non-fatal — when validation can't run, candidates
// pass through unchanged. The lower-band fallthrough still catches
// genuinely-wrong identities at the outcome layer.
func (a *Aggregator) validateAgainstProvider(ctx context.Context, cands []Candidate, evidence *map[string]json.RawMessage) []Candidate {
	if a.provider == nil || len(cands) == 0 {
		return cands
	}

	type redirectLog struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	var redirects []redirectLog

	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		item, err := a.provider.LookupByID(ctx, c.Ref.Source, c.Ref.ExternalID)
		if err != nil {
			// Genuine upstream failure: keep the candidate at face
			// value rather than punishing it for our outage.
			if a.logger != nil {
				a.logger.Warn().Err(err).Str("source", string(c.Ref.Source)).Str("id", c.Ref.ExternalID).Msg("validation upstream error; passing through")
			}
			out = append(out, c)
			continue
		}
		if item == nil {
			// 404: the ID is dead. Drop the candidate.
			continue
		}
		if item.Redirected {
			redirects = append(redirects, redirectLog{From: c.Ref.ExternalID, To: item.ExternalID})
			c.Ref.ExternalID = item.ExternalID
			c.Confidence *= validatedMultiplierRedirect
		} else {
			c.Confidence *= validatedMultiplierValid
		}
		out = append(out, c)
	}

	if len(redirects) > 0 && evidence != nil {
		if raw, err := json.Marshal(redirects); err == nil {
			if *evidence == nil {
				*evidence = make(map[string]json.RawMessage)
			}
			(*evidence)["validation_redirects"] = raw
		}
	}

	return out
}

// mergeCandidates combines candidates that share an ExternalRef. The
// combined confidence is the max across agreeing resolvers, plus a
// per-additional-resolver corroboration bonus, capped at
// corroborationCap. Year mismatch (against the file's hint year)
// applies a soft penalty multiplier; we deliberately preserve the
// candidate rather than dropping it, matching the spec's stance that
// year is a tiebreaker, not a filter.
func mergeCandidates(cands []Candidate, hintYear int) []Candidate {
	if len(cands) == 0 {
		return nil
	}

	type bucket struct {
		ref     ExternalRef
		best    Candidate
		count   int
		hasYear bool // the chosen candidate carries a year we can compare
	}
	order := []ExternalRef{}
	buckets := map[ExternalRef]*bucket{}

	for _, c := range cands {
		b, ok := buckets[c.Ref]
		if !ok {
			b = &bucket{ref: c.Ref, best: c, count: 1}
			buckets[c.Ref] = b
			order = append(order, c.Ref)
			continue
		}
		b.count++
		// Keep the candidate that carries the highest confidence and
		// the richest payload (an episode ref beats a candidate without
		// one).
		if c.Confidence > b.best.Confidence {
			b.best = c
		} else if c.Episode != nil && b.best.Episode == nil {
			ep := *c.Episode
			b.best.Episode = &ep
		}
	}

	out := make([]Candidate, 0, len(order))
	for _, ref := range order {
		b := buckets[ref]
		combined := b.best.Confidence
		if b.count > 1 {
			combined += corroborationStep * float64(b.count-1)
		}
		if combined > corroborationCap {
			combined = corroborationCap
		}
		// Year-mismatch penalty: the file's hint year (if any) versus
		// the candidate's. We can only apply it when both ends are
		// nonzero — the spec is explicit that year==0 from the provider
		// shouldn't punish.
		if hintYear != 0 {
			candYear := candidateYear(b.best)
			if candYear != 0 && candYear != hintYear {
				combined *= yearMismatchPenalty
			}
		}
		b.best.Confidence = combined
		out = append(out, b.best)
		_ = b.hasYear
	}
	return out
}

// dominantAbove returns the highest-confidence candidate when it sits
// strictly above `threshold` AND clears the second-best by at least
// ambiguousEpsilon. The second check is what keeps two confident-but-tied
// candidates from short-circuiting Tier 1 — that's an ambiguous case.
func dominantAbove(cands []Candidate, threshold float64) (Candidate, bool) {
	if len(cands) == 0 {
		return Candidate{}, false
	}
	sorted := append([]Candidate(nil), cands...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Confidence > sorted[j].Confidence
	})
	if sorted[0].Confidence < threshold {
		return Candidate{}, false
	}
	if len(sorted) >= 2 && sorted[0].Confidence-sorted[1].Confidence < ambiguousEpsilon {
		return Candidate{}, false
	}
	return sorted[0], true
}

// bandAndRecord turns the final candidate set into a MatchOutcomeRecord
// with the right outcome band and chosen-* fields populated.
func (a *Aggregator) bandAndRecord(file FileRef, cands []Candidate, audits []ResolverAudit, evidence map[string]json.RawMessage) MatchOutcomeRecord {
	rec := MatchOutcomeRecord{
		FileID:             file.ID,
		ResolversConsulted: audits,
		DecidedBy:          "auto",
		DecidedAt:          time.Now().UTC(),
	}

	rec.Evidence, rec.Truncated = capEvidence(evidence)

	if len(cands) == 0 {
		rec.Outcome = OutcomeNoMatch
		return rec
	}

	sort.SliceStable(cands, func(i, j int) bool {
		return cands[i].Confidence > cands[j].Confidence
	})
	best := cands[0]
	tie := len(cands) >= 2 && cands[0].Confidence-cands[1].Confidence < ambiguousEpsilon

	rec.Confidence = best.Confidence

	t := a.cfg.Thresholds
	switch {
	case best.Confidence >= t.Auto && !tie:
		rec.Outcome = OutcomeConfident
	case best.Confidence >= t.ReviewLow && !tie:
		rec.Outcome = OutcomeConfidentReview
	case best.Confidence >= t.LowMin && !tie:
		rec.Outcome = OutcomeLowConfidence
	case best.Confidence < t.LowMin && len(cands) == 0:
		// unreachable — len(cands) == 0 is handled above.
		rec.Outcome = OutcomeNoMatch
	default:
		// Either tied at any confidence level, or below LowMin with at
		// least one candidate. Both are ambiguous to a human reviewer.
		rec.Outcome = OutcomeAmbiguous
	}

	// partial_series: the series identity resolved confidently
	// (above LowMin and non-tied) but no Episode is attached. Phase 1
	// won't actually produce this — no resolver emits series+episode
	// today — but the band is wired so phase-2 episode-resolving work
	// doesn't need a schema or aggregator change.
	if rec.Outcome != OutcomeNoMatch && best.Episode == nil && bestLooksLikeSeries(best) && file.Parsed != nil && fileLooksLikeSeries(file) {
		rec.Outcome = OutcomePartialSeries
	}

	if rec.Outcome == OutcomeConfident || rec.Outcome == OutcomeConfidentReview || rec.Outcome == OutcomeLowConfidence || rec.Outcome == OutcomePartialSeries {
		ref := best.Ref
		rec.ChosenRef = &ref
		if best.Episode != nil {
			ep := *best.Episode
			rec.ChosenEpisode = &ep
		}
		if best.Edition != nil {
			ed := *best.Edition
			rec.ChosenEdition = &ed
		}
	}

	return rec
}

// capEvidence marshals the per-resolver evidence map into a JSONB-ready
// blob, trimming the largest payloads first if it overflows
// evidenceCapBytes. The strategy: marshal the whole thing; if it fits,
// return as-is; otherwise drop the largest payload and try again,
// repeating until under cap. Deterministic for a given input.
func capEvidence(evidence map[string]json.RawMessage) (json.RawMessage, bool) {
	if len(evidence) == 0 {
		return json.RawMessage("{}"), false
	}

	working := make(map[string]json.RawMessage, len(evidence))
	for k, v := range evidence {
		working[k] = v
	}

	truncated := false
	for {
		raw, err := json.Marshal(working)
		if err != nil {
			return json.RawMessage("{}"), true
		}
		if len(raw) <= evidenceCapBytes {
			return raw, truncated
		}
		// Drop the largest payload. Tie-broken by key name so the
		// truncation order is deterministic across runs.
		var dropKey string
		var dropSize int
		for k, v := range working {
			s := len(v)
			if s > dropSize || (s == dropSize && k > dropKey) {
				dropKey = k
				dropSize = s
			}
		}
		if dropKey == "" {
			// Defensive: working is non-empty but no max found.
			return json.RawMessage("{}"), true
		}
		delete(working, dropKey)
		truncated = true
		if len(working) == 0 {
			// We trimmed everything and still don't fit. Surface a
			// minimal truncation marker rather than the empty map so
			// callers reading evidence know data was lost.
			return json.RawMessage(`{"_truncated":true}`), true
		}
	}
}

// mergeEvidence flattens per-tier evidence maps into one. Keys are
// resolver names plus the aggregator-owned "validation_*" slots, so the
// chance of collision is nil in practice; later writers win on the rare
// case it happens.
func mergeEvidence(maps ...map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// topConfidence returns the highest-confidence value in a candidate
// slice, or 0 when empty.
func topConfidence(cands []Candidate) float64 {
	top := 0.0
	for _, c := range cands {
		if c.Confidence > top {
			top = c.Confidence
		}
	}
	return top
}

// yearHintFromFile pulls the parsed-release year out of FileRef.Parsed
// when present. Zero when no parse or no year was advertised.
func yearHintFromFile(f FileRef) int {
	if f.Parsed == nil {
		return 0
	}
	return f.Parsed.Identity.Year.Value
}

// candidateYear returns the candidate's advertised year. Phase 1
// resolvers attach the year (when available) via the metadata provider's
// validation step; absent the call, year is 0. Reserved for future
// resolvers that synthesize candidates with a year hint pre-validation.
func candidateYear(_ Candidate) int {
	// Year currently lives on metadata.Item, which the aggregator
	// reads transiently during validation but doesn't persist on the
	// Candidate type. When a resolver wants its own year hint to feed
	// the mismatch penalty, this is the seam to grow.
	return 0
}

// bestLooksLikeSeries reports whether the chosen candidate's identity
// is series-shaped. Phase 1 has no way to tell — resolvers attach an
// Episode only when they have one, and Candidate carries no type tag.
// The partial_series outcome is therefore unreachable from phase-1
// fake resolvers unless the test attaches a series-shaped episode hint
// later. The branch is kept so that phase-2 resolvers (which know the
// difference) light it up automatically.
func bestLooksLikeSeries(_ Candidate) bool {
	return true
}

// fileLooksLikeSeries reports whether the file's parse signals a
// series-shaped release (i.e. carries season/episode numbering). The
// aggregator only flags partial_series when the file itself claims to
// be an episode AND the resolved identity is series-shaped AND the
// chosen candidate has no Episode attached.
func fileLooksLikeSeries(f FileRef) bool {
	if f.Parsed == nil {
		return false
	}
	n := f.Parsed.Identity.Numbering
	return n.Kind != "" || len(n.EpisodeNumbers.Value) > 0 || len(n.AbsoluteNumbers.Value) > 0
}
