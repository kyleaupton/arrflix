package rules

import "fmt"

// Tri is a three-valued boolean: True, False, or Unknown. Unknown arises when
// a condition references a field that is not knowable yet — an import-phase
// field evaluated at search, or a namespace whose Subject half is unassembled
// — so an early-moment pass never produces a spurious reject; the same tree
// re-run at a later moment is definite. Each consumer maps Unknown to its own
// safe default.
//
// Tri is string-valued ("false"/"true"/"unknown") so it serializes naturally
// — the same strings ride the wire in dry-run evaluations, and schema
// generation sees a string, not an opaque integer.
type Tri string

const (
	False   Tri = "false"
	True    Tri = "true"
	Unknown Tri = "unknown"
)

// String renders the tri-state as "false", "true", or "unknown".
func (t Tri) String() string {
	return string(t)
}

// UnmarshalJSON decodes the string form, rejecting anything outside the three
// states so a malformed stored trace can't smuggle in a fourth value.
func (t *Tri) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"false"`:
		*t = False
	case `"true"`:
		*t = True
	case `"unknown"`:
		*t = Unknown
	default:
		return fmt.Errorf("invalid Tri value: %s", b)
	}
	return nil
}

// triFrom lifts a definite boolean into the tri-state.
func triFrom(b bool) Tri {
	if b {
		return True
	}
	return False
}

// triNot is Kleene negation: not Unknown = Unknown.
func triNot(t Tri) Tri {
	switch t {
	case True:
		return False
	case False:
		return True
	default:
		return Unknown
	}
}
