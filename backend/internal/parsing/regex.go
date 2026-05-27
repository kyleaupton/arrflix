package parsing

import (
	"time"

	"github.com/dlclark/regexp2"
)

// We parse with github.com/dlclark/regexp2 rather than the standard library's
// RE2-based regexp. The Sonarr/Radarr parsers we port lean on .NET lookahead,
// lookbehind, and named backreferences in nearly every title pattern (93 of
// Sonarr's 98 ReportTitleRegex entries; all 10 of Radarr's), none of which RE2
// supports. regexp2 matches the .NET engine by default, so those patterns port
// near-verbatim from the pinned upstream source (submodules/) instead of being
// rewritten — fidelity, and a trivial path for tracking upstream drift.
//
// The cost is that regexp2 backtracks and so has no linear-time guarantee — the
// ReDoS surface RE2 closes for free. matchTimeout caps it: a real release title
// matches in microseconds, so any match still running after a full second is
// adversarial. On timeout the match returns an error, which the parser treats as
// "no match" (see findFirst); parsing is total, so a hostile title yields an
// empty parse, never a hung goroutine. This is the same guard .NET itself uses.
const matchTimeout = time.Second

// mustCompile compiles a .NET-syntax pattern and arms the ReDoS timeout. It
// panics on an invalid pattern: every pattern is a package-level value ported
// from upstream, so a compile failure is a programming error to catch at startup,
// not a runtime condition.
//
// Upstream's RegexOptions.Compiled is a .NET JIT hint with no regexp2 analogue
// and is dropped. RegexOptions.IgnoreCase maps to regexp2.IgnoreCase and is
// passed explicitly per pattern, mirroring the upstream declaration so the port
// stays a visible 1:1.
func mustCompile(pattern string, opts regexp2.RegexOptions) *regexp2.Regexp {
	re := regexp2.MustCompile(pattern, opts)
	re.MatchTimeout = matchTimeout
	return re
}

// findFirst returns the named capture groups of the first match of re in s, or
// nil if there is no match. A timeout or other engine error is reported as no
// match (nil) — failing closed keeps the parser total. Only named groups are
// returned; regexp2 names unnamed groups by their index, which we skip.
func findFirst(re *regexp2.Regexp, s string) map[string]string {
	m, err := re.FindStringMatch(s)
	if err != nil || m == nil {
		return nil
	}
	groups := make(map[string]string)
	for _, g := range m.Groups() {
		if g.Name == "" || isIndexName(g.Name) || g.Length == 0 {
			continue
		}
		groups[g.Name] = g.String()
	}
	return groups
}

// isIndexName reports whether a regexp2 group name is an auto-assigned positional
// index (all digits) rather than a real (?<name>) capture name.
func isIndexName(name string) bool {
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(name) > 0
}
