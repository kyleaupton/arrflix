package routing

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// subject is a grab-moment movie subject the test rules evaluate against.
func subject() model.Subject {
	return model.Subject{
		Release: model.Release{
			Candidate: model.CandidateFields{
				Title:   "Dune.2021.2160p.BluRay.REMUX-FraMeSToR",
				Seeders: 50,
			},
			Quality: model.QualityFields{
				Resolution: parsing.Field[string]{Value: "2160p"},
			},
		},
		Media: model.MediaFields{
			Type:  "movie",
			Title: "Dune",
		},
	}
}

// Condition helpers.

func matchCond() *rules.Condition { // True against subject()
	return &rules.Condition{
		Op:    rules.OpEq,
		Left:  &rules.Operand{Kind: rules.OperandField, Path: "media.type"},
		Right: &rules.Operand{Kind: rules.OperandLiteral, Literal: &rules.Literal{Type: rules.LitEnum, Str: "movie"}},
	}
}

func missCond() *rules.Condition { // False against subject()
	return &rules.Condition{
		Op:    rules.OpEq,
		Left:  &rules.Operand{Kind: rules.OperandField, Path: "media.type"},
		Right: &rules.Operand{Kind: rules.OperandLiteral, Literal: &rules.Literal{Type: rules.LitEnum, Str: "series"}},
	}
}

func unknownCond() *rules.Condition { // import-phase field at grab → Unknown
	return &rules.Condition{
		Op:    rules.OpEq,
		Left:  &rules.Operand{Kind: rules.OperandField, Path: "mediainfo.video_codec"},
		Right: &rules.Operand{Kind: rules.OperandLiteral, Literal: &rules.Literal{Type: rules.LitEnum, Str: "H.265"}},
	}
}

func uuidPtr() *uuid.UUID {
	id := uuid.New()
	return &id
}

func fullActions() ActionSet {
	return ActionSet{DownloaderID: uuidPtr(), LibraryID: uuidPtr(), NameTemplateID: uuidPtr()}
}

func rule(name string, pos int32, cond *rules.Condition, actions ActionSet) Rule {
	return Rule{ID: uuid.New(), Name: name, Enabled: true, Position: pos, Conditions: cond, Actions: actions}
}

func TestDispatchFirstMatchStops(t *testing.T) {
	t.Parallel()
	reg := rules.NewRegistry()

	first := rule("first", 0, matchCond(), fullActions())
	second := rule("second", 1, matchCond(), fullActions())

	final, eval := Dispatch(reg, []Rule{first, second}, subject())

	if final != first.Actions {
		t.Errorf("final = %+v, want first rule's actions %+v", final, first.Actions)
	}
	if len(eval.Fired) != 1 || eval.Fired[0] != first.ID {
		t.Errorf("Fired = %v, want exactly [%s]", eval.Fired, first.ID)
	}
	if !eval.Rules[0].Fired || eval.Rules[0].Result != rules.True {
		t.Errorf("rules[0] = %+v, want fired/True", eval.Rules[0])
	}
	if !eval.Rules[1].Skipped {
		t.Errorf("rules[1] = %+v, want skipped after terminal match", eval.Rules[1])
	}
	if eval.FellBack != (Fallbacks{}) {
		t.Errorf("FellBack = %+v, want none", eval.FellBack)
	}
}

func TestDispatchPositionOrder(t *testing.T) {
	t.Parallel()
	reg := rules.NewRegistry()

	// Supplied out of order — position decides, not slice order.
	winner := rule("winner", 0, matchCond(), fullActions())
	loser := rule("loser", 5, matchCond(), fullActions())

	final, eval := Dispatch(reg, []Rule{loser, winner}, subject())

	if final != winner.Actions {
		t.Errorf("final = %+v, want position-0 rule's actions", final)
	}
	if eval.Rules[0].ID != winner.ID {
		t.Errorf("rules[0].ID = %s, want position-0 rule first in record", eval.Rules[0].ID)
	}
}

func TestDispatchDisabledSkipped(t *testing.T) {
	t.Parallel()
	reg := rules.NewRegistry()

	disabled := rule("disabled", 0, matchCond(), fullActions())
	disabled.Enabled = false
	active := rule("active", 1, matchCond(), fullActions())

	final, eval := Dispatch(reg, []Rule{disabled, active}, subject())

	if final != active.Actions {
		t.Errorf("final = %+v, want the enabled rule's actions", final)
	}
	if !eval.Rules[0].Skipped || eval.Rules[0].Trace != nil {
		t.Errorf("rules[0] = %+v, want skipped with no trace", eval.Rules[0])
	}
}

func TestDispatchUnknownIsNotMatched(t *testing.T) {
	t.Parallel()
	reg := rules.NewRegistry()

	indeterminate := rule("import-phase", 0, unknownCond(), fullActions())
	fallback := rule("fallback", 1, matchCond(), fullActions())

	final, eval := Dispatch(reg, []Rule{indeterminate, fallback}, subject())

	if final != fallback.Actions {
		t.Errorf("final = %+v, want the later rule's actions (Unknown must not dispatch)", final)
	}
	if eval.Rules[0].Result != rules.Unknown || eval.Rules[0].Fired {
		t.Errorf("rules[0] = %+v, want Unknown/not-fired", eval.Rules[0])
	}
	if eval.Rules[0].Trace == nil {
		t.Error("rules[0].Trace = nil, want the neutral trace recorded")
	}
}

func TestDispatchContinueMerge(t *testing.T) {
	t.Parallel()
	reg := rules.NewRegistry()

	// First match sets only the library and continues; the second sets all
	// three. Later matches fill unset slots; the earlier match wins conflicts.
	libOnly := rule("lib-only", 0, matchCond(), ActionSet{LibraryID: uuidPtr()})
	libOnly.Continue = true
	full := rule("full", 1, matchCond(), fullActions())

	final, eval := Dispatch(reg, []Rule{libOnly, full}, subject())

	if final.LibraryID != libOnly.Actions.LibraryID {
		t.Errorf("LibraryID = %v, want the earlier match's (conflicts go to the earlier match)", final.LibraryID)
	}
	if final.DownloaderID != full.Actions.DownloaderID || final.NameTemplateID != full.Actions.NameTemplateID {
		t.Errorf("unset slots not filled by the later match: %+v", final)
	}
	if len(eval.Fired) != 2 || eval.Fired[0] != libOnly.ID || eval.Fired[1] != full.ID {
		t.Errorf("Fired = %v, want both rules in order", eval.Fired)
	}
	// The partial rule is flagged incomplete; it still matched and fired.
	if !eval.Rules[0].Fired || !eval.Rules[0].Incomplete {
		t.Errorf("rules[0] = %+v, want fired+incomplete", eval.Rules[0])
	}
	if eval.Rules[1].Incomplete {
		t.Errorf("rules[1] = %+v, want complete", eval.Rules[1])
	}
}

func TestDispatchContinueWithNoLaterMatchFallsBack(t *testing.T) {
	t.Parallel()
	reg := rules.NewRegistry()

	libOnly := rule("lib-only", 0, matchCond(), ActionSet{LibraryID: uuidPtr()})
	libOnly.Continue = true
	miss := rule("miss", 1, missCond(), fullActions())

	final, eval := Dispatch(reg, []Rule{libOnly, miss}, subject())

	if final.LibraryID == nil || final.DownloaderID != nil || final.NameTemplateID != nil {
		t.Errorf("final = %+v, want only LibraryID set", final)
	}
	want := Fallbacks{Downloader: true, NameTemplate: true}
	if eval.FellBack != want {
		t.Errorf("FellBack = %+v, want %+v", eval.FellBack, want)
	}
}

func TestDispatchEmptyRuleList(t *testing.T) {
	t.Parallel()
	reg := rules.NewRegistry()

	final, eval := Dispatch(reg, nil, subject())

	if final != (ActionSet{}) {
		t.Errorf("final = %+v, want empty set", final)
	}
	if len(eval.Rules) != 0 || len(eval.Fired) != 0 {
		t.Errorf("eval = %+v, want empty rules/fired", eval)
	}
	want := Fallbacks{Downloader: true, Library: true, NameTemplate: true}
	if eval.FellBack != want {
		t.Errorf("FellBack = %+v, want everything fell back", eval.FellBack)
	}
}

func TestDispatchNoMatch(t *testing.T) {
	t.Parallel()
	reg := rules.NewRegistry()

	miss := rule("miss", 0, missCond(), fullActions())
	final, eval := Dispatch(reg, []Rule{miss}, subject())

	if final != (ActionSet{}) {
		t.Errorf("final = %+v, want empty set", final)
	}
	if eval.Rules[0].Result != rules.False || eval.Rules[0].Fired {
		t.Errorf("rules[0] = %+v, want False/not-fired", eval.Rules[0])
	}
	want := Fallbacks{Downloader: true, Library: true, NameTemplate: true}
	if eval.FellBack != want {
		t.Errorf("FellBack = %+v, want everything fell back", eval.FellBack)
	}
}
