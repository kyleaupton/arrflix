package rules

import "fmt"

// Tri is a three-valued boolean: True, False, or Unknown. Unknown arises when
// a condition references a field that is not available yet — a post_download
// field evaluated while Release.MediaInfo is nil — so a pre-download pass
// never produces a spurious reject; the same tree re-run post-download is
// definite. Each consumer maps Unknown to its own safe default.
type Tri uint8

const (
	False Tri = iota
	True
	Unknown
)

// String renders the tri-state as "false", "true", or "unknown".
func (t Tri) String() string {
	switch t {
	case False:
		return "false"
	case True:
		return "true"
	default:
		return "unknown"
	}
}

// MarshalJSON encodes the tri-state as the JSON string "false"/"true"/"unknown".
func (t Tri) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.String() + `"`), nil
}

// UnmarshalJSON decodes the string form produced by MarshalJSON.
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
