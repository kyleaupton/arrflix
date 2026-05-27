package parsing

import (
	"testing"

	"github.com/dlclark/regexp2"
)

// These assert the two reasons we chose regexp2 over stdlib RE2 actually hold:
// .NET lookbehind/lookahead and named backreferences compile and match, and the
// ReDoS timeout is armed on everything mustCompile produces. If a future
// dependency bump regressed either, the whole port's premise would be invalid —
// so it's worth a guard.

func TestRegexp2SupportsLookaroundAndBackrefs(t *testing.T) {
	cases := []struct {
		name, pattern, input string
		want                 map[string]string // expected named groups (subset)
	}{
		{
			// Lookbehind + lookahead digit-boundary guard, the shape that appears
			// in 93 of Sonarr's 98 title patterns and panics stdlib regexp.
			name:    "lookaround digit guard",
			pattern: `(?<=S)(?<season>(?<!\d)\d{1,2}(?!\d))`,
			input:   "The Series S03E07 1080p",
			want:    map[string]string{"season": "03"},
		},
		{
			// Named backreference \k<q>: match a quoted run where the closing
			// quote equals the opening one. RE2 has no backreferences at all.
			name:    "named backreference",
			pattern: `(?<q>['"])(?<inner>.*?)\k<q>`,
			input:   `release "Dual Audio" tag`,
			want:    map[string]string{"inner": "Dual Audio"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			re := mustCompile(tc.pattern, regexp2.IgnoreCase)
			got := findFirst(re, tc.input)
			if got == nil {
				t.Fatalf("no match for %q", tc.input)
			}
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("group %q = %q, want %q", k, got[k], want)
				}
			}
		})
	}
}

func TestMustCompileArmsTimeout(t *testing.T) {
	re := mustCompile(`abc`, regexp2.None)
	if re.MatchTimeout != matchTimeout {
		t.Errorf("MatchTimeout = %v, want %v", re.MatchTimeout, matchTimeout)
	}
}

func TestFindFirstNoMatchReturnsNil(t *testing.T) {
	re := mustCompile(`^\d+$`, regexp2.None)
	if got := findFirst(re, "not a number"); got != nil {
		t.Errorf("expected nil for no match, got %v", got)
	}
}
