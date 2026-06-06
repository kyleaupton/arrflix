package rules

import (
	"slices"
	"strings"

	"github.com/kyleaupton/arrflix/internal/model"
)

// Outcome is the result of evaluating a condition tree: the tri-state result
// and a neutral trace mirroring the tree. The trace knows nothing about
// actions, scores, or HTTP — consumers adapt it into their own records.
type Outcome struct {
	Result Tri
	Trace  Trace
}

// Trace records one evaluated node: its operator, resolved operands (for
// comparison leaves), child traces (for logical branches), and the node's
// tri-state result.
type Trace struct {
	Op       Operator  `json:"op"`
	Left     *Resolved `json:"left,omitempty"`
	Right    *Resolved `json:"right,omitempty"`
	Children []Trace   `json:"children,omitempty"`
	Result   Tri       `json:"result"`
}

// Resolved records how an operand resolved during evaluation. For a field
// operand: the path, the resolved bare value, and availability. For a literal
// operand: the literal value (Available always true).
type Resolved struct {
	Path      string `json:"path,omitempty"`
	Value     any    `json:"value"`
	Available bool   `json:"available"`
}

// Eval walks the tree against the subject at the given evaluation moment and
// returns a tri-state Outcome. It is total: it never errors and never panics.
// Validation guarantees well-typed trees; a malformed node that somehow
// reaches eval resolves defensively (Unknown, or False for equality) rather
// than failing.
//
// The moment is an explicit parameter, not derived from which Subject halves
// happen to be populated — callers know their moment intrinsically (quality =
// search, routing = grab, importer = import), and (tree, subject, at) fully
// determines the verdict. A comparison over an unavailable field is Unknown.
// A field is available iff its phase rank ≤ the moment's rank AND its half of
// the Subject is actually populated (population is a defensive backstop: an
// unassembled half — nil Want, nil MediaInfo — is "not knowable", distinct
// from a populated half holding empty values, which evaluates definitively).
// A field is also unavailable when its path is unknown to the registry or its
// resolved value is nil (an unset optional field such as media.season on a
// movie). Logical branches combine children with Kleene three-valued logic.
//
// Eval reads bare values (model.Subject.GetField unwraps Field[T] to .Value)
// and ignores confidence — the seam exists; v1 doesn't consume it.
func (r *Registry) Eval(cond *Condition, s model.Subject, at model.Phase) Outcome {
	trace := r.evalNode(cond, &s, at)
	return Outcome{Result: trace.Result, Trace: trace}
}

// evalNode evaluates one node, returning its trace (which carries the result).
func (r *Registry) evalNode(cond *Condition, s *model.Subject, at model.Phase) Trace {
	switch {
	case cond == nil:
		return Trace{Result: Unknown}
	case cond.Op.IsLogical():
		return r.evalLogical(cond, s, at)
	case cond.Op.IsComparison():
		return r.evalComparison(cond, s, at)
	default:
		return Trace{Op: cond.Op, Result: Unknown}
	}
}

// evalLogical evaluates every child (no short-circuit, so the trace is
// complete) and combines results with Kleene logic: for `and`, False
// dominates, then Unknown; for `or`, True dominates, then Unknown. A branch
// with no children — or a `not` with the wrong arity — is Unknown (defensive;
// validation enforces arity).
func (r *Registry) evalLogical(cond *Condition, s *model.Subject, at model.Phase) Trace {
	children := make([]Trace, 0, len(cond.Children))
	for _, child := range cond.Children {
		children = append(children, r.evalNode(child, s, at))
	}
	trace := Trace{Op: cond.Op, Children: children, Result: Unknown}
	if len(children) == 0 {
		return trace
	}

	switch cond.Op {
	case OpNot:
		if len(children) == 1 {
			trace.Result = triNot(children[0].Result)
		}
	case OpAnd:
		trace.Result = True
		for _, c := range children {
			if c.Result == False {
				trace.Result = False
				break
			}
			if c.Result == Unknown {
				trace.Result = Unknown
			}
		}
	case OpOr:
		trace.Result = False
		for _, c := range children {
			if c.Result == True {
				trace.Result = True
				break
			}
			if c.Result == Unknown {
				trace.Result = Unknown
			}
		}
	}
	return trace
}

// evalComparison resolves both operands and compares. If either operand is
// missing or unavailable, the leaf is Unknown.
func (r *Registry) evalComparison(cond *Condition, s *model.Subject, at model.Phase) Trace {
	left := r.resolve(cond.Left, s, at)
	right := r.resolve(cond.Right, s, at)
	trace := Trace{Op: cond.Op, Left: left, Right: right, Result: Unknown}
	if left == nil || right == nil || !left.Available || !right.Available {
		return trace
	}
	trace.Result = compare(cond.Op, left.Value, right.Value)
	return trace
}

// resolve resolves an operand to its bare value and availability. Literals
// are always available. A field is unavailable (Available false, Value nil)
// when its phase is later than the evaluation moment, when its half of the
// Subject is unassembled (the population backstop), when its path is unknown,
// or when the resolved value is nil. Availability is derived from the
// registry's phase metadata and the Subject's population, never from
// string-matching GetField errors. A nil or malformed operand resolves to nil.
func (r *Registry) resolve(op *Operand, s *model.Subject, at model.Phase) *Resolved {
	if op == nil {
		return nil
	}
	switch op.Kind {
	case OperandLiteral:
		if op.Literal == nil {
			return nil
		}
		return &Resolved{Value: op.Literal.value(), Available: true}
	case OperandField:
		info, ok := r.fields[op.Path]
		if !ok {
			return &Resolved{Path: op.Path}
		}
		// Phase gates first: a later-phase field is not yet knowable at this
		// moment, regardless of what happens to be populated.
		if info.Phase.Rank() > at.Rank() {
			return &Resolved{Path: op.Path}
		}
		if !populated(op.Path, s) {
			return &Resolved{Path: op.Path}
		}
		value, err := s.GetField(op.Path)
		if err != nil || value == nil {
			return &Resolved{Path: op.Path}
		}
		return &Resolved{Path: op.Path, Value: value, Available: true}
	default:
		return nil
	}
}

// populated reports whether the Subject half holding the field's namespace is
// assembled. mediainfo.* lives behind Release.MediaInfo; want.* behind Want;
// every other namespace is a value struct, always present. An unassembled
// half is "not yet knowable" — distinct from a populated half holding empty
// values, which resolves and evaluates definitively.
func populated(path string, s *model.Subject) bool {
	switch {
	case strings.HasPrefix(path, "mediainfo."):
		return s.Release.MediaInfo != nil
	case strings.HasPrefix(path, "want."):
		return s.Want != nil
	default:
		return true
	}
}

// compare evaluates one comparison between two available bare values. It is
// defensive: a type combination validation didn't catch yields False for
// equality (and True for its negation) and Unknown for everything else —
// never a panic.
func compare(op Operator, left, right any) Tri {
	switch op {
	case OpEq:
		return triFrom(equal(left, right))
	case OpNe:
		return triNot(triFrom(equal(left, right)))
	case OpGt, OpGte, OpLt, OpLte:
		return ordering(op, left, right)
	case OpContains:
		return contains(left, right)
	case OpIn:
		return membership(left, right)
	case OpNotIn:
		return triNot(membership(left, right))
	}
	return Unknown
}

// equal is typed equality: strings (and enums) as strings, numbers
// numerically (int/int64/float64 coerced to float64), bools by value, and
// []string by order-independent set equality. Cross-type or unsupported
// values are never equal.
func equal(left, right any) bool {
	if lf, ok := toFloat(left); ok {
		rf, ok := toFloat(right)
		return ok && lf == rf
	}
	switch lv := left.(type) {
	case string:
		rv, ok := right.(string)
		return ok && lv == rv
	case bool:
		rv, ok := right.(bool)
		return ok && lv == rv
	case []string:
		rv, ok := right.([]string)
		return ok && setEqual(lv, rv)
	}
	return false
}

// ordering compares numerically — both sides coerced to float64. A
// non-number operand is Unknown (defensive; validation guarantees numbers).
func ordering(op Operator, left, right any) Tri {
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if !lok || !rok {
		return Unknown
	}
	switch op {
	case OpGt:
		return triFrom(lf > rf)
	case OpGte:
		return triFrom(lf >= rf)
	case OpLt:
		return triFrom(lf < rf)
	case OpLte:
		return triFrom(lf <= rf)
	}
	return Unknown
}

// contains: a string left is substring match; a []string left is membership.
// The right operand must be a string scalar. Anything else is Unknown.
func contains(left, right any) Tri {
	rv, ok := right.(string)
	if !ok {
		return Unknown
	}
	switch lv := left.(type) {
	case string:
		return triFrom(strings.Contains(lv, rv))
	case []string:
		return triFrom(slices.Contains(lv, rv))
	}
	return Unknown
}

// membership: a scalar string left against a []string list right — true iff
// left ∈ list. Anything else is Unknown.
func membership(left, right any) Tri {
	list, ok := right.([]string)
	if !ok {
		return Unknown
	}
	lv, ok := left.(string)
	if !ok {
		return Unknown
	}
	return triFrom(slices.Contains(list, lv))
}

// toFloat coerces the numeric types the field catalog produces (int, int32,
// int64, float64) to float64 for comparison.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// setEqual is order-independent set equality (duplicates ignored).
func setEqual(a, b []string) bool {
	as := make(map[string]struct{}, len(a))
	for _, s := range a {
		as[s] = struct{}{}
	}
	bs := make(map[string]struct{}, len(b))
	for _, s := range b {
		bs[s] = struct{}{}
	}
	if len(as) != len(bs) {
		return false
	}
	for s := range as {
		if _, ok := bs[s]; !ok {
			return false
		}
	}
	return true
}
