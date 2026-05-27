package parsing

import (
	"regexp"
	"strings"
)

// pattern is one identity regex plus an optional validator. Sonarr/Radarr use
// .NET lookahead/lookbehind in nearly every title pattern, which Go's RE2 does
// not support; we port each pattern by matching broadly with the RE2 regex and
// rejecting false positives in validate (the same match-then-filter method the
// release-group parser already uses). A nil validate means the regex alone is
// authoritative.
type pattern struct {
	re       *regexp.Regexp
	validate func(groups map[string]string) bool
}

// matchOrdered tries patterns in declaration order (first-match-wins, like
// Sonarr's ReportTitleRegex and Radarr's ReportMovieTitleRegex) and returns the
// named capture groups of the first whose regex matches and whose validator, if
// present, accepts.
func matchOrdered(patterns []pattern, input string) (map[string]string, bool) {
	for _, p := range patterns {
		m := p.re.FindStringSubmatch(input)
		if m == nil {
			continue
		}
		groups := namedGroups(p.re, m)
		if p.validate != nil && !p.validate(groups) {
			continue
		}
		return groups, true
	}
	return nil, false
}

// namedGroups maps a submatch slice to its non-empty named capture groups.
func namedGroups(re *regexp.Regexp, match []string) map[string]string {
	groups := make(map[string]string)
	for i, name := range re.SubexpNames() {
		if name == "" || i >= len(match) || match[i] == "" {
			continue
		}
		groups[name] = match[i]
	}
	return groups
}

var titleSeparators = strings.NewReplacer(".", " ", "_", " ")

var multiSpace = regexp.MustCompile(`\s+`)

// leadingTag matches a leading "[SubGroup]" / "[Dolby Vision]" tag, which
// Sonarr/Radarr strip from the title (it's the release/subgroup prefix, not the
// work title).
var leadingTag = regexp.MustCompile(`^\s*\[[^\]]*\]\s*`)

// normalizeTitle turns a raw matched title into the display form the oracle
// reports (seriesTitleInfo.title / movieTitle): a leading bracket tag is
// dropped, separators become spaces, runs of whitespace collapse, and the
// result is trimmed. It does not lowercase or strip accents — that heavier
// normalization is CleanTitle's job, used for matching, not the display title.
func normalizeTitle(raw string) string {
	t := leadingTag.ReplaceAllString(raw, "")
	t = titleSeparators.Replace(t)
	t = multiSpace.ReplaceAllString(t, " ")
	return strings.TrimSpace(t)
}

// detectDomain guesses series vs movie for the auto-detect path: a title that
// matches a series numbering pattern (season/episode, absolute, or daily) is a
// series; otherwise it is treated as a movie. Domain-aware callers bypass this
// by passing AsSeries()/AsMovie().
func detectDomain(input string) Domain {
	if _, ok := matchOrdered(seriesPatterns, input); ok {
		return DomainSeries
	}
	return DomainMovie
}
