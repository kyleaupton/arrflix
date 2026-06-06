package rules

import (
	"errors"
	"strings"
	"testing"
)

// validLeaf is a well-typed leaf for padding logical branches.
func validLeaf() *Condition {
	return leaf(OpEq, fld("media.type"), litEnum("movie"))
}

func TestValidateAccepts(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	tests := []struct {
		name string
		cond *Condition
	}{
		{"enum eq", leaf(OpEq, fld("quality.resolution"), litEnum("2160p"))},
		{"string literal against enum field", leaf(OpEq, fld("quality.resolution"), litStr("2160p"))},
		{"text eq", leaf(OpEq, fld("media.title"), litStr("Dune"))},
		{"number ordering", leaf(OpGt, fld("candidate.seeders"), litInt(10))},
		{"float ordering", leaf(OpLte, fld("candidate.age_hours"), litFloat(24))},
		{"bool eq", leaf(OpEq, fld("quality.is_repack"), litBool(true))},
		{"string contains", leaf(OpContains, fld("candidate.title"), litStr("REMUX"))},
		{"list field contains", leaf(OpContains, fld("candidate.categories"), litStr("Movies"))},
		{"in over text field", leaf(OpIn, fld("encode.release_group"), litList("FLUX", "FraMeSToR"))},
		{"in over enum field with valid elements", leaf(OpIn, fld("quality.resolution"), litList("1080p", "2160p"))},
		{"list field eq list literal", leaf(OpEq, fld("candidate.categories"), litList("Movies"))},
		{"cross-field same class", leaf(OpEq, fld("media.title"), fld("media.clean_title"))},
		{"cross-field contains", leaf(OpContains, fld("candidate.title"), fld("media.title"))},
		{"post_download field", leaf(OpEq, fld("mediainfo.video_codec"), litEnum("H.265"))},
		{"canonical nested tree", branch(OpAnd,
			leaf(OpEq, fld("quality.resolution"), litEnum("2160p")),
			leaf(OpIn, fld("encode.release_group"), litList("FLUX", "FraMeSToR")),
		)},
		{"not", branch(OpNot, validLeaf())},
		{"nested logical", branch(OpOr, validLeaf(), branch(OpAnd, validLeaf(), branch(OpNot, validLeaf())))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := reg.Validate(tt.cond); err != nil {
				t.Errorf("Validate(%s) = %v, want nil", tt.name, err)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	tests := []struct {
		name string
		cond *Condition
		want string // substring of one collected problem
	}{
		{"nil condition", nil, "condition is nil"},
		{"unknown operator", &Condition{Op: "matches"}, `unknown operator "matches"`},
		{"unknown field path", leaf(OpEq, fld("bogus.path"), litStr("x")), `unknown field path "bogus.path"`},
		{"ordering on enum", leaf(OpGt, fld("quality.resolution"), litEnum("1080p")), `">" requires number operands`},
		{"ordering on text", leaf(OpLt, fld("media.title"), litStr("M")), `"<" requires number operands`},
		{"enum literal not in set", leaf(OpEq, fld("quality.resolution"), litEnum("4320p")), `"4320p" is not a valid value for enum field "quality.resolution"`},
		{"number field vs string literal", leaf(OpEq, fld("candidate.size"), litStr("big")), "type mismatch"},
		{"bool field vs string literal", leaf(OpEq, fld("quality.is_repack"), litStr("yes")), "type mismatch"},
		{"cross-field type mismatch", leaf(OpEq, fld("candidate.size"), fld("media.title")), "type mismatch"},
		{"in without list literal", leaf(OpIn, fld("encode.release_group"), litStr("FLUX")), `"in" requires a list literal right operand`},
		{"not in without list literal", leaf(OpNotIn, fld("encode.release_group"), litEnum("FLUX")), `"not in" requires a list literal right operand`},
		{"in against number field", leaf(OpIn, fld("candidate.seeders"), litList("50")), `"in" requires a string left operand`},
		{"in list element not in enum", leaf(OpIn, fld("quality.resolution"), litList("1080p", "4K")), `"4K" is not a valid value for enum field "quality.resolution"`},
		{"contains on number field", leaf(OpContains, fld("candidate.seeders"), litStr("5")), `"contains" requires a string or list left operand`},
		{"contains with list right", leaf(OpContains, fld("candidate.title"), litList("a")), `"contains" requires a string right operand`},
		{"not arity", branch(OpNot, validLeaf(), validLeaf()), `"not" requires exactly one child, got 2`},
		{"and arity", branch(OpAnd, validLeaf()), `"and" requires at least two children, got 1`},
		{"or arity", branch(OpOr), `"or" requires at least two children, got 0`},
		{"literal vs literal", leaf(OpEq, litStr("a"), litStr("a")), "comparison requires at least one field operand"},
		{"missing right operand", &Condition{Op: OpEq, Left: fld("media.title")}, "missing operand"},
		{"field operand with empty path", leaf(OpEq, &Operand{Kind: OperandField}, litStr("x")), "field operand has empty path"},
		{"literal operand without literal", leaf(OpEq, fld("media.title"), &Operand{Kind: OperandLiteral}), "literal operand has no literal"},
		{"unknown operand kind", leaf(OpEq, &Operand{Kind: "value"}, litStr("x")), `unknown operand kind "value"`},
		{"unknown literal type", leaf(OpEq, fld("media.title"), lit(Literal{Type: "decimal"})), `unknown literal type "decimal"`},
		{"logical with operands", &Condition{Op: OpAnd, Left: fld("media.title"), Children: []*Condition{validLeaf(), validLeaf()}}, `logical operator "and" must not have operands`},
		{"comparison with children", &Condition{Op: OpEq, Left: fld("media.title"), Right: litStr("Dune"), Children: []*Condition{validLeaf()}}, `comparison operator "==" must not have children`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := reg.Validate(tt.cond)
			if err == nil {
				t.Fatalf("Validate(%s) = nil, want error containing %q", tt.name, tt.want)
			}
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Validate(%s) returned %T, want *ValidationError", tt.name, err)
			}
			if !hasProblem(verr, tt.want) {
				t.Errorf("Validate(%s) problems %v missing %q", tt.name, verr.Problems, tt.want)
			}
		})
	}
}

func TestValidateCollectsAllProblems(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	// One tree, several coexisting problems: bad arity at the root, an
	// unknown path, ordering-on-enum, and a bad enum value.
	cond := branch(OpNot,
		leaf(OpEq, fld("bogus.path"), litStr("x")),
		leaf(OpGt, fld("quality.resolution"), litEnum("1080p")),
		leaf(OpEq, fld("quality.resolution"), litEnum("4320p")),
	)

	err := reg.Validate(cond)
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Validate = %v, want *ValidationError", err)
	}
	for _, want := range []string{
		`"not" requires exactly one child, got 3`,
		`unknown field path "bogus.path"`,
		`">" requires number operands`,
		`"4320p" is not a valid value`,
	} {
		if !hasProblem(verr, want) {
			t.Errorf("problems %v missing %q", verr.Problems, want)
		}
	}
	if len(verr.Problems) < 4 {
		t.Errorf("collected %d problems, want at least 4", len(verr.Problems))
	}
}

func TestValidateProblemLocations(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	cond := branch(OpAnd,
		validLeaf(),
		leaf(OpEq, fld("bogus.path"), litStr("x")),
	)
	var verr *ValidationError
	if err := reg.Validate(cond); !errors.As(err, &verr) {
		t.Fatalf("Validate = %v, want *ValidationError", err)
	}
	if len(verr.Problems) != 1 || verr.Problems[0].Loc != "children[1].left" {
		t.Errorf("problems = %v, want one at children[1].left", verr.Problems)
	}
	if !strings.Contains(verr.Error(), "children[1].left: unknown field path") {
		t.Errorf("Error() = %q, want location-prefixed message", verr.Error())
	}
}

func hasProblem(verr *ValidationError, substr string) bool {
	for _, p := range verr.Problems {
		if strings.Contains(p.Msg, substr) {
			return true
		}
	}
	return false
}
