package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// newValidationOnlyService builds a service whose repo is nil. The validation
// paths exercised here run entirely off the in-memory registry and never touch
// the repo — an empty Formats list skips the only repo-dependent branch.
func newValidationOnlyService() *QualityProfileService {
	return &QualityProfileService{reg: rules.NewRegistry()}
}

func bk(s parsing.Source, r parsing.Resolution, m parsing.Modifier) parsing.BinKey {
	return parsing.BinKey{Source: s, Resolution: r, Modifier: m}
}

// repackTree marshals the canonical quality.is_repack == true scorer — a tree
// that passes registry validation.
func repackTree(t *testing.T) json.RawMessage {
	t.Helper()
	tree := rules.Condition{
		Op:    rules.OpEq,
		Left:  &rules.Operand{Kind: rules.OperandField, Path: "quality.is_repack"},
		Right: &rules.Operand{Kind: rules.OperandLiteral, Literal: &rules.Literal{Type: rules.LitBool, Bool: true}},
	}
	b, err := json.Marshal(&tree)
	if err != nil {
		t.Fatalf("marshal repack tree: %v", err)
	}
	return b
}

// unknownFieldTree marshals a tree referencing a field path the registry doesn't
// know — registry validation flags it.
func unknownFieldTree(t *testing.T) json.RawMessage {
	t.Helper()
	tree := rules.Condition{
		Op:    rules.OpEq,
		Left:  &rules.Operand{Kind: rules.OperandField, Path: "quality.does_not_exist"},
		Right: &rules.Operand{Kind: rules.OperandLiteral, Literal: &rules.Literal{Type: rules.LitBool, Bool: true}},
	}
	b, err := json.Marshal(&tree)
	if err != nil {
		t.Fatalf("marshal unknown-field tree: %v", err)
	}
	return b
}

// hasFieldErrorAt matches by prefix: registry validation appends the in-tree
// node location (e.g. ".left") to the gate/condition root, so an exact match
// would be brittle.
func hasFieldErrorAt(err error, location string) bool {
	for _, f := range apperrors.FieldsOf(err) {
		if f.Location == location || strings.HasPrefix(f.Location, location+".") {
			return true
		}
	}
	return false
}

func TestValidateProfileInput_CutoffNotInBins(t *testing.T) {
	t.Parallel()
	s := newValidationOnlyService()
	bluray := bk(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone)
	webdl := bk(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone)

	_, err := s.validateProfileInput(context.Background(), QualityProfileInput{
		Name:   "HD",
		Domain: "movie",
		Bins:   []parsing.BinKey{bluray},
		Cutoff: webdl, // not a member of Bins
	}, "test")

	if !apperrors.IsValidation(err) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if !hasFieldErrorAt(err, "body.cutoff") {
		t.Errorf("expected a body.cutoff field error, got fields %+v", apperrors.FieldsOf(err))
	}
}

func TestValidateProfileInput_BadGateTree(t *testing.T) {
	t.Parallel()
	s := newValidationOnlyService()
	bluray := bk(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone)

	gates, err := json.Marshal([]map[string]any{
		{"name": "junk", "tree": json.RawMessage(unknownFieldTree(t))},
	})
	if err != nil {
		t.Fatalf("marshal gates: %v", err)
	}

	_, verr := s.validateProfileInput(context.Background(), QualityProfileInput{
		Name:   "HD",
		Domain: "movie",
		Bins:   []parsing.BinKey{bluray},
		Cutoff: bluray,
		Gates:  gates,
	}, "test")

	if !apperrors.IsValidation(verr) {
		t.Fatalf("err = %v, want a validation error", verr)
	}
	if !hasFieldErrorAt(verr, "body.gates[0].tree") {
		t.Errorf("expected a body.gates[0].tree field error, got fields %+v", apperrors.FieldsOf(verr))
	}
}

func TestValidateProfileInput_Valid(t *testing.T) {
	t.Parallel()
	s := newValidationOnlyService()
	bluray := bk(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone)

	gates, err := json.Marshal([]map[string]any{
		{"name": "ok", "tree": json.RawMessage(repackTree(t))},
	})
	if err != nil {
		t.Fatalf("marshal gates: %v", err)
	}

	canonical, verr := s.validateProfileInput(context.Background(), QualityProfileInput{
		Name:   "HD",
		Domain: "movie",
		Bins:   []parsing.BinKey{bluray},
		Cutoff: bluray,
		Gates:  gates,
	}, "test")
	if verr != nil {
		t.Fatalf("validateProfileInput = %v, want nil", verr)
	}
	if len(canonical) == 0 {
		t.Error("expected canonical gates bytes, got empty")
	}
}

func TestValidateCustomFormatInput_BadTree(t *testing.T) {
	t.Parallel()
	s := newValidationOnlyService()

	_, err := s.validateCustomFormatInput(CustomFormatInput{
		Name:       "Junk",
		Domain:     "movie",
		Conditions: unknownFieldTree(t),
	}, "test")

	if !apperrors.IsValidation(err) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if !hasFieldErrorAt(err, "body.conditions") {
		t.Errorf("expected a body.conditions field error, got fields %+v", apperrors.FieldsOf(err))
	}
}

func TestValidateCustomFormatInput_BadDomain(t *testing.T) {
	t.Parallel()
	s := newValidationOnlyService()

	_, err := s.validateCustomFormatInput(CustomFormatInput{
		Name:       "Repack",
		Domain:     "audiobook",
		Conditions: repackTree(t),
	}, "test")

	if !apperrors.IsValidation(err) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if !hasFieldErrorAt(err, "body.domain") {
		t.Errorf("expected a body.domain field error, got fields %+v", apperrors.FieldsOf(err))
	}
}
