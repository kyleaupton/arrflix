// Package rules is the shared predicate substrate: a typed condition tree, a
// pure three-valued evaluator over a model.Subject at an explicit evaluation
// moment, and registry-driven authoring-time validation. Routing and quality
// profiles both stand on it — each consumer owns its own reduction of the
// boolean answer; this package only answers "does this subject match?".
//
// The package is pure: it imports internal/model and the stdlib only. No
// repo, no DB, no logger, no HTTP, no consumer types.
package rules

// Operator identifies what a condition node does. Comparison operators form
// leaves (two operands); logical operators form branches (child conditions).
type Operator string

const (
	OpEq       Operator = "=="
	OpNe       Operator = "!="
	OpGt       Operator = ">"
	OpGte      Operator = ">="
	OpLt       Operator = "<"
	OpLte      Operator = "<="
	OpContains Operator = "contains"
	OpIn       Operator = "in"
	OpNotIn    Operator = "not in"
	OpAnd      Operator = "and"
	OpOr       Operator = "or"
	OpNot      Operator = "not"
)

// IsComparison reports whether o is a comparison (leaf) operator.
func (o Operator) IsComparison() bool {
	switch o {
	case OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpContains, OpIn, OpNotIn:
		return true
	}
	return false
}

// IsLogical reports whether o is a logical (branch) operator.
func (o Operator) IsLogical() bool {
	switch o {
	case OpAnd, OpOr, OpNot:
		return true
	}
	return false
}

// IsOrdering reports whether o is an ordering comparison (valid on numbers only).
func (o Operator) IsOrdering() bool {
	switch o {
	case OpGt, OpGte, OpLt, OpLte:
		return true
	}
	return false
}

// OperandKind discriminates the two operand forms. The kind is a stored fact
// decided at authoring time — the evaluator never sniffs a string's shape to
// guess whether it is a field path or a literal.
type OperandKind string

const (
	OperandField   OperandKind = "field"
	OperandLiteral OperandKind = "literal"
)

// Operand is one side of a comparison: either a field reference (by registry
// path, e.g. "quality.resolution") or a typed literal.
type Operand struct {
	Kind    OperandKind `json:"kind"`
	Path    string      `json:"path,omitempty"`    // when Kind == field
	Literal *Literal    `json:"literal,omitempty"` // when Kind == literal
}

// LiteralType discriminates the typed slot a Literal populates.
type LiteralType string

const (
	LitString LiteralType = "string"
	LitInt    LiteralType = "int"
	LitFloat  LiteralType = "float"
	LitBool   LiteralType = "bool"
	LitEnum   LiteralType = "enum" // a string constrained to a field's EnumValues
	LitList   LiteralType = "list" // homogeneous string/enum list, for in / not in
)

// Literal holds one concretely-typed field per kind — not a single `any`.
// Decoding JSON into `any` would turn every number into float64 and every
// array into []any, breaking lossless storage round-trips (these bytes get
// persisted) and forcing type assertions in Eval. Exactly one field is
// populated, selected by Type.
type Literal struct {
	Type  LiteralType `json:"type"`
	Str   string      `json:"str,omitempty"`   // Type == string or enum
	Int   int64       `json:"int,omitempty"`   // Type == int
	Float float64     `json:"float,omitempty"` // Type == float
	Bool  bool        `json:"bool,omitempty"`  // Type == bool
	List  []string    `json:"list,omitempty"`  // Type == list
}

// value returns the literal's bare typed value, selected by Type. An unknown
// Type yields nil, which Eval treats defensively (never a panic).
func (l *Literal) value() any {
	switch l.Type {
	case LitString, LitEnum:
		return l.Str
	case LitInt:
		return l.Int
	case LitFloat:
		return l.Float
	case LitBool:
		return l.Bool
	case LitList:
		return l.List
	}
	return nil
}

// Condition is one node of a rule tree, discriminated by Op: a comparison
// leaf carries Left/Right operands; a logical branch carries Children. The
// rule tree is just its root *Condition — trees are always loaded and stored
// whole, and this JSON form is the storage/wire contract.
type Condition struct {
	Op Operator `json:"op"`

	// Comparison leaf (Op is a comparison operator):
	Left  *Operand `json:"left,omitempty"`
	Right *Operand `json:"right,omitempty"`

	// Logical branch (Op is and/or/not):
	Children []*Condition `json:"children,omitempty"`
}
