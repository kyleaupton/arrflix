package qualityprofile

import (
	"fmt"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// Disposition is a release's role in a selection: the one grabbed, a passing
// runner-up, or a gated-out reject.
type Disposition string

const (
	DispositionGrabbed  Disposition = "grabbed"
	DispositionRunnerUp Disposition = "runner_up"
	DispositionRejected Disposition = "rejected"
)

// RejectReason names the gate that rejected a release and a human-readable
// detail. The zero value means "not rejected".
type RejectReason struct {
	Gate   string
	Detail string
}

// Evaluation is a single release's verdict under a profile at a given phase.
type Evaluation struct {
	Subject      model.Subject
	Bin          parsing.BinKey
	Passed       bool
	RejectReason RejectReason
	Score        int
	Disposition  Disposition
	Trace        []rules.Trace
}

// Selection is the outcome of picking among candidate releases: the winner (nil
// if every candidate was gated out) and every evaluation for diagnostics.
type Selection struct {
	Picked *Evaluation
	All    []Evaluation
}

// ReGateResult is the verdict of an import-time re-gate.
type ReGateResult string

const (
	ReGatePass     ReGateResult = "pass"
	ReGatePenalize ReGateResult = "penalize"
	ReGateHardFail ReGateResult = "hard_fail"
)

// ReGateOutcome is the result of re-evaluating gates and formats at import,
// where mediainfo.* is populated and verifiable claims resolve for real.
type ReGateOutcome struct {
	Result   ReGateResult
	Penalty  int
	Reason   RejectReason
	Findings []string
	Trace    []rules.Trace
}

// FileQuality is the held quality of an imported file: its bin identity and the
// custom-format score it earned. It is the baseline StrictlyBetter compares a
// candidate against.
type FileQuality struct {
	Bin   parsing.BinKey
	Score int
}

// Gate is the single hard-gate authority for one release: structural checks
// (bin-allow, min-seeders) then custom gate trees, first failure winning. A
// custom gate rejects only on rules.True — False and Unknown never reject, so an
// import-referencing gate evaluated at search is inert (no spurious rejection).
func (p Profile) Gate(reg *rules.Registry, s model.Subject, at model.Phase) (bool, RejectReason) {
	bin := binOf(s, p.Domain)
	if p.rank(bin) >= len(p.Bins) {
		return false, RejectReason{Gate: "bin", Detail: fmt.Sprintf("%+v not in allowed bins", bin)}
	}
	if s.Release.Candidate.Seeders < p.MinSeeders {
		return false, RejectReason{
			Gate:   "min_seeders",
			Detail: fmt.Sprintf("%d seeders < %d required", s.Release.Candidate.Seeders, p.MinSeeders),
		}
	}
	for _, g := range p.Gates {
		if reg.Eval(g.Tree, s, at).Result == rules.True {
			return false, RejectReason{Gate: g.Name, Detail: "gate matched"}
		}
	}
	// Size is structural — read off the candidate, not via a rules tree. Enforced
	// only when the bin has a band and the matched runtime is known and positive;
	// an absent runtime skips the gate (safe no-op while runtime rolls out). A
	// zero bound means "unbounded" for that side.
	if band, ok := p.Sizes[bin]; ok {
		if rt, hasRT := runtimeOf(s); hasRT && rt > 0 {
			min, max := band.scaled(rt)
			size := s.Release.Candidate.Size
			if max > 0 && size > max {
				return false, RejectReason{
					Gate:   "size_max",
					Detail: fmt.Sprintf("%d bytes exceeds scaled max %d", size, max),
				}
			}
			if min > 0 && size < min {
				return false, RejectReason{
					Gate:   "size_min",
					Detail: fmt.Sprintf("%d bytes below scaled min %d", size, min),
				}
			}
		}
	}
	return true, RejectReason{}
}

// formatEval is one custom format's evaluation, kept in p.Formats order so the
// search and import passes can be zipped by index in ReGate.
type formatEval struct {
	matched bool
	weight  int
	trace   rules.Trace
}

func (p Profile) evalFormats(reg *rules.Registry, s model.Subject, at model.Phase) []formatEval {
	out := make([]formatEval, len(p.Formats))
	for i, f := range p.Formats {
		o := reg.Eval(f.Tree, s, at)
		out[i] = formatEval{matched: o.Result == rules.True, weight: f.Weight, trace: o.Trace}
	}
	return out
}

// Score sums the weights of every custom format whose tree evaluates True at the
// given phase. Unknown and False contribute nothing. Per the v1 phase cap, only
// formats over parsing's search-phase fields (e.g. quality.is_repack) can score
// at PhaseSearch; codec/HDR formats stay Unknown until import — the engine is
// agnostic, it is a property of which trees a profile carries.
func (p Profile) Score(reg *rules.Registry, s model.Subject, at model.Phase) (int, []rules.Trace) {
	fes := p.evalFormats(reg, s, at)
	score := 0
	traces := make([]rules.Trace, 0, len(fes))
	for _, fe := range fes {
		traces = append(traces, fe.trace)
		if fe.matched {
			score += fe.weight
		}
	}
	return score, traces
}

// Pick selects the best release among candidates at search time. Selection is
// bin-first: among the releases that pass the gate, the best-ranked bin present
// wins outright (a higher bin always beats a lower bin regardless of score),
// then within that bin the highest custom-format score, then a deterministic
// tiebreak (smaller size, then candidate.guid). Picked is nil if every candidate
// was gated out.
func (p Profile) Pick(reg *rules.Registry, subjects []model.Subject) Selection {
	evals := make([]Evaluation, len(subjects))
	for i, s := range subjects {
		passed, reason := p.Gate(reg, s, model.PhaseSearch)
		score, trace := p.Score(reg, s, model.PhaseSearch)
		evals[i] = Evaluation{
			Subject:      s,
			Bin:          binOf(s, p.Domain),
			Passed:       passed,
			RejectReason: reason,
			Score:        score,
			Disposition:  DispositionRejected,
			Trace:        trace,
		}
	}

	best := -1
	for i := range evals {
		if !evals[i].Passed {
			continue
		}
		if best == -1 || p.better(evals[i], evals[best]) {
			best = i
		}
	}

	sel := Selection{All: evals}
	if best == -1 {
		return sel
	}
	for i := range evals {
		if evals[i].Passed {
			evals[i].Disposition = DispositionRunnerUp
		}
	}
	evals[best].Disposition = DispositionGrabbed
	sel.Picked = &evals[best]
	return sel
}

// better reports whether candidate a should be preferred over the current best b
// (both already known to pass). Bin rank dominates; score breaks a bin tie; then
// seeders (more is healthier) before size then guid make the order total and
// deterministic. Seeders stays below bin and score so it only ever reorders
// otherwise-equal releases — it never promotes a lower bin or lower-scored release.
func (p Profile) better(a, b Evaluation) bool {
	ra, rb := p.rank(a.Bin), p.rank(b.Bin)
	if ra != rb {
		return ra < rb
	}
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if sa, sb := a.Subject.Release.Candidate.Seeders, b.Subject.Release.Candidate.Seeders; sa != sb {
		return sa > sb
	}
	// Within a bin, prefer the size closest to the band's scaled preferred when a
	// band and a runtime are available (ra==rb means a.Bin==b.Bin — a bin holds
	// one rank). Without them, fall back to smaller-size → guid.
	if band, ok := p.Sizes[a.Bin]; ok && band.PreferredBytesPerMin > 0 {
		if rt, hasRT := runtimeOf(a.Subject); hasRT && rt > 0 {
			pref := band.PreferredBytesPerMin * int64(rt)
			da := abs64(a.Subject.Release.Candidate.Size - pref)
			db := abs64(b.Subject.Release.Candidate.Size - pref)
			if da != db {
				return da < db
			}
		}
	}
	sa, sb := a.Subject.Release.Candidate.Size, b.Subject.Release.Candidate.Size
	if sa != sb {
		return sa < sb
	}
	return a.Subject.Release.Candidate.GUID < b.Subject.Release.Candidate.GUID
}

// abs64 is the absolute value of an int64 distance.
func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// ReGate re-evaluates a release at import, where mediainfo.* is populated. A hard
// gate that now matches fails the import outright. Otherwise, any custom format
// that scored at search but no longer matches at import is a penalty (its claim
// was advertised but ffprobe disagrees); the penalty is the sum of those flipped
// weights and Findings names them.
func (p Profile) ReGate(reg *rules.Registry, s model.Subject) ReGateOutcome {
	out := ReGateOutcome{Result: ReGatePass}

	for _, g := range p.Gates {
		o := reg.Eval(g.Tree, s, model.PhaseImport)
		out.Trace = append(out.Trace, o.Trace)
		if o.Result == rules.True {
			out.Result = ReGateHardFail
			out.Reason = RejectReason{Gate: g.Name, Detail: "gate matched at import"}
			return out
		}
	}

	// The penalty path is dormant under the v1 phase cap: a tree that matches at
	// search references only search-phase fields, whose values are identical at
	// import, so it matches at import too — a search-True→import-False flip is
	// unreachable today. It becomes live with the custom-format fast-follow, when
	// advertised codec/HDR/audio are search-phase fields a re-probe can contradict.
	search := p.evalFormats(reg, s, model.PhaseSearch)
	imp := p.evalFormats(reg, s, model.PhaseImport)
	for i := range p.Formats {
		out.Trace = append(out.Trace, imp[i].trace)
		if search[i].matched && !imp[i].matched {
			out.Penalty += p.Formats[i].Weight
			out.Findings = append(out.Findings, p.Formats[i].Name)
		}
	}
	if out.Penalty > 0 {
		out.Result = ReGatePenalize
	}
	return out
}

// StrictlyBetter reports whether a candidate is a worthwhile upgrade over the
// currently held file. A strictly better bin always wins. Within the same bin,
// the candidate must beat the held score by more than antiFlapDelta (the margin
// that stops trivial re-grabs). A worse bin never upgrades. Cutoff governs
// whether to seek upgrades at all (see BelowCutoff), not this comparison.
func (p Profile) StrictlyBetter(reg *rules.Registry, current FileQuality, candidate model.Subject, antiFlapDelta int) bool {
	cand := p.rank(binOf(candidate, p.Domain))
	cur := p.rank(current.Bin)
	if cand != cur {
		return cand < cur
	}
	score, _ := p.Score(reg, candidate, model.PhaseSearch)
	return score-current.Score > antiFlapDelta
}
