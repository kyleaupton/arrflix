// Package routing is the dispatch consumer of the rules substrate: it
// evaluates an ordered list of user-authored rules against a Subject at the
// grab moment and reduces matches to an action set (downloader, library,
// name template). The package is pure — it imports internal/rules,
// internal/model, and the stdlib only. Persistence, fallbacks to configured
// defaults, and HTTP live in the service/handler layers.
package routing

import (
	"sort"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// ActionSet is routing's reduction: where the grab goes. Slots are nullable —
// an unset slot contributes nothing and is filled from the configured default
// by the service after dispatch.
type ActionSet struct {
	DownloaderID   *uuid.UUID `json:"downloaderId,omitempty"`
	LibraryID      *uuid.UUID `json:"libraryId,omitempty"`
	NameTemplateID *uuid.UUID `json:"nameTemplateId,omitempty"`
}

// Complete reports whether every slot is set.
func (a ActionSet) Complete() bool {
	return a.DownloaderID != nil && a.LibraryID != nil && a.NameTemplateID != nil
}

// Rule is one dispatch rule: a condition tree over the Subject plus the
// action set it applies on match. Rules evaluate in Position order; the first
// match fires and stops dispatch unless Continue, in which case later matches
// fill only still-unset slots (conflicts go to the earlier match). A rule
// with nulled-out action targets (a deleted downloader/library/template) is
// incomplete: it still matches, contributes nothing for that slot, and the
// evaluation record flags it.
type Rule struct {
	ID         uuid.UUID
	Name       string
	Enabled    bool
	Position   int32
	Conditions *rules.Condition
	Actions    ActionSet
	Continue   bool
}

// RuleEvaluation is the per-rule entry in the dispatch record: the rule's
// identity, its tri-state result with the neutral trace behind it, and what
// dispatch did with it. Skipped rules (disabled, or ordered after a terminal
// match) carry no result or trace.
type RuleEvaluation struct {
	ID      uuid.UUID    `json:"id"`
	Name    string       `json:"name"`
	Skipped bool         `json:"skipped,omitempty"`
	Result  rules.Tri    `json:"result,omitempty"`
	Trace   *rules.Trace `json:"trace,omitempty"`
	// Fired marks a matched rule whose action set was applied. Incomplete
	// flags a fired rule with one or more unset action slots.
	Fired      bool `json:"fired"`
	Incomplete bool `json:"incomplete,omitempty"`
}

// Fallbacks records which action slots no rule filled — the service fills
// them from the configured defaults.
type Fallbacks struct {
	Downloader   bool `json:"downloader"`
	Library      bool `json:"library"`
	NameTemplate bool `json:"nameTemplate"`
}

// Evaluation is routing's dispatch record, built from the neutral rules
// traces: every rule with its tri-state result, the fired rule ids in order,
// the final action set, and which slots fell back to defaults. It is returned
// by dry-run and logged on the grab path.
type Evaluation struct {
	Rules    []RuleEvaluation `json:"rules"`
	Fired    []uuid.UUID      `json:"fired"`
	Final    ActionSet        `json:"final"`
	FellBack Fallbacks        `json:"fellBack"`
}

// Dispatch evaluates the rule list against the subject at the grab moment and
// returns the merged action set plus the evaluation record. Rules run in
// Position order; disabled rules are skipped. Unknown maps to not-matched —
// routing's safe default for a condition that isn't knowable at grab. The
// first match fires and stops dispatch unless it has Continue, in which case
// evaluation proceeds and later matches fill only still-unset slots. Slots no
// rule filled are flagged in the record's Fallbacks; the caller fills them
// from the configured defaults (Evaluation.Final reflects the pre-fallback
// set until the caller updates it).
func Dispatch(reg *rules.Registry, rs []Rule, s model.Subject) (ActionSet, Evaluation) {
	ordered := make([]Rule, len(rs))
	copy(ordered, rs)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })

	eval := Evaluation{Rules: make([]RuleEvaluation, 0, len(ordered)), Fired: []uuid.UUID{}}
	var final ActionSet
	stopped := false

	for _, rule := range ordered {
		re := RuleEvaluation{ID: rule.ID, Name: rule.Name}
		if !rule.Enabled || stopped {
			re.Skipped = true
			eval.Rules = append(eval.Rules, re)
			continue
		}

		out := reg.Eval(rule.Conditions, s, model.PhaseGrab)
		re.Result = out.Result
		trace := out.Trace
		re.Trace = &trace

		// Unknown is not-matched: an unanswerable condition must not dispatch.
		if out.Result != rules.True {
			eval.Rules = append(eval.Rules, re)
			continue
		}

		re.Fired = true
		re.Incomplete = !rule.Actions.Complete()
		eval.Fired = append(eval.Fired, rule.ID)

		// Merge: later matches fill only unset slots; the earlier match wins
		// conflicts.
		if final.DownloaderID == nil {
			final.DownloaderID = rule.Actions.DownloaderID
		}
		if final.LibraryID == nil {
			final.LibraryID = rule.Actions.LibraryID
		}
		if final.NameTemplateID == nil {
			final.NameTemplateID = rule.Actions.NameTemplateID
		}

		if !rule.Continue {
			stopped = true
		}
		eval.Rules = append(eval.Rules, re)
	}

	eval.Final = final
	eval.FellBack = Fallbacks{
		Downloader:   final.DownloaderID == nil,
		Library:      final.LibraryID == nil,
		NameTemplate: final.NameTemplateID == nil,
	}
	return final, eval
}
