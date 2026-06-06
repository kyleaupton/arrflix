package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kyleaupton/arrflix/internal/model"
)

// Problem is a single authoring-time validation failure, located by its
// position in the tree (e.g. "children[1].right"; "" is the root).
type Problem struct {
	Loc string `json:"loc"`
	Msg string `json:"msg"`
}

// ValidationError aggregates every problem found in a tree. Validation
// collects all problems rather than failing on the first, so the editor can
// flag everything at once; the consuming service translates this into its own
// error model.
type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		if p.Loc == "" {
			msgs[i] = p.Msg
		} else {
			msgs[i] = p.Loc + ": " + p.Msg
		}
	}
	return "invalid condition tree: " + strings.Join(msgs, "; ")
}

// Validate type-checks a condition tree against the field registry at
// authoring time: every field path must exist, every operator must be valid
// for its operand types, every literal must be compatible with the field it
// is compared against, and logical arity must hold. It is pure and total,
// reads only the registry, and returns nil or a *ValidationError carrying all
// problems found.
func (r *Registry) Validate(cond *Condition) error {
	v := &validator{reg: r}
	v.node(cond, "")
	if len(v.problems) > 0 {
		return &ValidationError{Problems: v.problems}
	}
	return nil
}

// Operand value classes for type-checking. Fields map by catalog ValueType,
// literals by LiteralType; an operator is valid only for compatible classes.
const (
	classString = "string"
	classNumber = "number"
	classBool   = "bool"
	classList   = "list"
	classOther  = "other" // time.Time, []int, … — not comparable in v1
)

// opType describes one operand for type-checking: its value class plus the
// underlying field metadata or literal (exactly one is set).
type opType struct {
	class string
	field *model.ContextFieldInfo // non-nil for field operands
	lit   *Literal                // non-nil for literal operands
}

// describe renders the operand for problem messages.
func (t *opType) describe() string {
	if t.field != nil {
		return fmt.Sprintf("field %q (%s)", t.field.Path, t.field.Type)
	}
	return fmt.Sprintf("%s literal", t.lit.Type)
}

// fieldClass maps a catalog field to its comparison class.
func fieldClass(info model.ContextFieldInfo) string {
	switch info.ValueType {
	case "string":
		return classString
	case "int", "int64", "float64":
		return classNumber
	case "bool":
		return classBool
	case "[]string":
		return classList
	default:
		return classOther
	}
}

// literalClass maps a literal type to its comparison class.
func literalClass(t LiteralType) (string, bool) {
	switch t {
	case LitString, LitEnum:
		return classString, true
	case LitInt, LitFloat:
		return classNumber, true
	case LitBool:
		return classBool, true
	case LitList:
		return classList, true
	}
	return "", false
}

type validator struct {
	reg      *Registry
	problems []Problem
}

func (v *validator) addf(loc, format string, args ...any) {
	v.problems = append(v.problems, Problem{Loc: loc, Msg: fmt.Sprintf(format, args...)})
}

// at joins a tree location with a child segment.
func at(loc, seg string) string {
	if loc == "" {
		return seg
	}
	return loc + "." + seg
}

func (v *validator) node(c *Condition, loc string) {
	switch {
	case c == nil:
		v.addf(loc, "condition is nil")
	case c.Op.IsLogical():
		v.logical(c, loc)
	case c.Op.IsComparison():
		v.comparison(c, loc)
	default:
		v.addf(loc, "unknown operator %q", c.Op)
	}
}

func (v *validator) logical(c *Condition, loc string) {
	if c.Left != nil || c.Right != nil {
		v.addf(loc, "logical operator %q must not have operands", c.Op)
	}
	switch {
	case c.Op == OpNot && len(c.Children) != 1:
		v.addf(loc, "%q requires exactly one child, got %d", c.Op, len(c.Children))
	case c.Op != OpNot && len(c.Children) < 2:
		v.addf(loc, "%q requires at least two children, got %d", c.Op, len(c.Children))
	}
	for i, child := range c.Children {
		v.node(child, at(loc, fmt.Sprintf("children[%d]", i)))
	}
}

func (v *validator) comparison(c *Condition, loc string) {
	if len(c.Children) > 0 {
		v.addf(loc, "comparison operator %q must not have children", c.Op)
	}
	left := v.operand(c.Left, at(loc, "left"))
	right := v.operand(c.Right, at(loc, "right"))
	if left == nil || right == nil {
		return // structural problems already recorded
	}
	// A literal-vs-literal comparison is constant — require at least one
	// field. Cross-field (field vs field) is allowed; classes still must
	// be compatible below.
	if left.field == nil && right.field == nil {
		v.addf(loc, "comparison requires at least one field operand")
		return
	}
	v.checkTypes(c.Op, left, right, loc)
}

// operand checks an operand's internal structure and resolves its type.
// Returns nil (with problems recorded) when the operand is too malformed to
// type-check.
func (v *validator) operand(o *Operand, loc string) *opType {
	if o == nil {
		v.addf(loc, "missing operand")
		return nil
	}
	switch o.Kind {
	case OperandField:
		if o.Literal != nil {
			v.addf(loc, "field operand must not carry a literal")
		}
		if o.Path == "" {
			v.addf(loc, "field operand has empty path")
			return nil
		}
		info, ok := v.reg.Field(o.Path)
		if !ok {
			v.addf(loc, "unknown field path %q", o.Path)
			return nil
		}
		return &opType{class: fieldClass(info), field: &info}
	case OperandLiteral:
		if o.Path != "" {
			v.addf(loc, "literal operand must not have a path")
		}
		if o.Literal == nil {
			v.addf(loc, "literal operand has no literal")
			return nil
		}
		class, ok := literalClass(o.Literal.Type)
		if !ok {
			v.addf(loc, "unknown literal type %q", o.Literal.Type)
			return nil
		}
		return &opType{class: class, lit: o.Literal}
	default:
		v.addf(loc, "unknown operand kind %q", o.Kind)
		return nil
	}
}

// checkTypes enforces the operator-type rules between two resolved operands.
func (v *validator) checkTypes(op Operator, left, right *opType, loc string) {
	switch {
	case op == OpEq || op == OpNe:
		v.checkEquality(op, left, right, loc)
	case op.IsOrdering():
		// Ordering is numeric only — an enum has no inherent order, so
		// `quality.resolution > "1080p"` is unbuildable here, by design.
		for _, t := range []*opType{left, right} {
			if t.class != classNumber {
				v.addf(loc, "%q requires number operands; got %s", op, t.describe())
			}
		}
	case op == OpContains:
		if left.class != classString && left.class != classList {
			v.addf(loc, "%q requires a string or list left operand; got %s", op, left.describe())
		}
		if right.class != classString {
			v.addf(loc, "%q requires a string right operand; got %s", op, right.describe())
		}
	case op == OpIn || op == OpNotIn:
		v.checkMembership(op, left, right, loc)
	}
}

func (v *validator) checkEquality(op Operator, left, right *opType, loc string) {
	for _, t := range []*opType{left, right} {
		if t.class == classOther {
			v.addf(loc, "%s does not support comparison", t.describe())
			return
		}
	}
	if left.class != right.class {
		v.addf(loc, "type mismatch: %s %s %s", left.describe(), op, right.describe())
		return
	}
	// An enum field compared against a string/enum literal: the literal's
	// value must be one of the field's enum values.
	for _, pair := range [][2]*opType{{left, right}, {right, left}} {
		field, other := pair[0], pair[1]
		if field.field == nil || len(field.field.EnumValues) == 0 || other.lit == nil {
			continue
		}
		if !slices.Contains(field.field.EnumValues, other.lit.Str) {
			v.addf(loc, "%q is not a valid value for enum field %q (valid: %s)",
				other.lit.Str, field.field.Path, strings.Join(field.field.EnumValues, ", "))
		}
	}
}

func (v *validator) checkMembership(op Operator, left, right *opType, loc string) {
	if right.lit == nil || right.lit.Type != LitList {
		v.addf(loc, "%q requires a list literal right operand; got %s", op, right.describe())
		return
	}
	// v1 list literals hold string/enum elements only, so membership is
	// defined for string-valued left operands. Numeric lists are a future
	// extension via a separate typed list field.
	if left.class != classString {
		v.addf(loc, "%q requires a string left operand; got %s", op, left.describe())
		return
	}
	if left.field != nil && len(left.field.EnumValues) > 0 {
		for _, el := range right.lit.List {
			if !slices.Contains(left.field.EnumValues, el) {
				v.addf(loc, "%q is not a valid value for enum field %q (valid: %s)",
					el, left.field.Path, strings.Join(left.field.EnumValues, ", "))
			}
		}
	}
}
