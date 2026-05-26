package parsing

import (
	"regexp"
	"strconv"
	"strings"
)

// seriesPatterns ports the high-traffic subset of Sonarr's ReportTitleRegex
// (RE2-adapted, first-match-wins): standard and multi-episode SxxExx, the NxNN
// scene form, daily air-date, and anime absolute numbering. Which named groups
// the winning pattern fills determines the numbering namespace (see
// parseSeriesIdentity) — the same way Sonarr distinguishes them. The long tail
// of Sonarr's ~80 patterns is added as the corpus demands; absolute is parsed
// best-effort and is out of the v1 parity claim.
var seriesPatterns = []pattern{
	{
		// Standard + multi-episode: "Title S01E05", "Title.S03E01-06",
		// "Title S03E01E02", "TitleS06E11" (no separator). The multi connector
		// is a dash (optionally followed by E) so a trailing ".720p" is not
		// mistaken for a second episode.
		re: regexp.MustCompile(`(?i)^(?P<title>.*?)[ ._-]*s(?P<season>\d{1,2})e(?P<episode>\d{1,3})(?:-e?(?P<episodeend>\d{1,3}))?`),
	},
	{
		// Scene NxNN: "Title 1x13", "Title - 19x15".
		re: regexp.MustCompile(`(?i)^(?P<title>.*?)[ ._-]+(?P<season>\d{1,2})x(?P<episode>\d{1,3})`),
	},
	{
		// Season pack, "Season N" form: "Title Season 2".
		re: regexp.MustCompile(`(?i)^(?P<title>.*?)[ ._-]+season[ ._-]?(?P<season>\d{1,2})\b`),
	},
	{
		// Season pack, "SNN" form (no episode): "Title.S01.1080p", "Title S04 ...".
		// Tried after the SxxExx pattern, so a real episode wins first.
		re: regexp.MustCompile(`(?i)^(?P<title>.*?)[ ._-]+s(?P<season>\d{1,2})(?:[ ._-]|$)`),
	},
	{
		// Daily air-date: "Title 2016.03.14", "Title.2011-08-04".
		re: regexp.MustCompile(`(?i)^(?P<title>.*?)[ ._-]+(?P<airyear>(?:19|20)\d{2})[-. ](?P<airmonth>\d{2})[-. ](?P<airday>\d{2})\b`),
	},
	{
		// Anime absolute: "[SubGroup] Title - 363 [hash]". Best-effort.
		re: regexp.MustCompile(`(?i)^\[[^\]]+\][ ._-]*(?P<title>.*?)[ ._-]+(?P<absoluteepisode>\d{1,4})(?:\b|_)`),
	},
}

// parseSeriesIdentity fills Title, Year, and Numbering from the series patterns.
// The numbering namespace is set from which capture groups matched and is never
// coerced across namespaces.
func parseSeriesIdentity(p *ParsedRelease, input string) {
	groups, ok := matchOrdered(seriesPatterns, input)
	if !ok {
		return
	}

	title, year := splitTrailingYear(normalizeTitle(groups["title"]))
	if title != "" {
		p.Identity.Title = Field[string]{Value: title, Confidence: confDetected, Evidence: "series title pattern"}
	}
	if year != 0 {
		p.Identity.Year = Field[int]{Value: year, Confidence: confDetected, Evidence: "series title year"}
	}

	switch {
	case groups["season"] != "" && groups["episode"] != "":
		season := atoiOr0(groups["season"])
		first := atoiOr0(groups["episode"])
		episodes := []int{first}
		if end := atoiOr0(groups["episodeend"]); end > first {
			episodes = rangeInts(first, end)
		}
		p.Identity.Numbering.Season = Field[int]{Value: season, Confidence: confDetected, Evidence: "season/episode pattern"}
		p.Identity.Numbering.EpisodeNumbers = Field[[]int]{Value: episodes, Confidence: confDetected, Evidence: "season/episode pattern"}
		p.Identity.Numbering.Kind = NumberingSeasonEpisode

	case groups["season"] != "":
		// Season pack: a season with no episode.
		season := atoiOr0(groups["season"])
		p.Identity.Numbering.Season = Field[int]{Value: season, Confidence: confDetected, Evidence: "season pack pattern"}
		p.Identity.Numbering.FullSeason = Field[bool]{Value: true, Confidence: confDetected, Evidence: "season pack pattern"}
		p.Identity.Numbering.Kind = NumberingSeasonEpisode

	case groups["airyear"] != "":
		date := groups["airyear"] + "-" + groups["airmonth"] + "-" + groups["airday"]
		p.Identity.Numbering.AirDate = Field[string]{Value: date, Confidence: confDetected, Evidence: "daily pattern"}
		p.Identity.Numbering.Kind = NumberingDaily

	case groups["absoluteepisode"] != "":
		abs := atoiOr0(groups["absoluteepisode"])
		p.Identity.Numbering.AbsoluteNumbers = Field[[]int]{Value: []int{abs}, Confidence: confDetected, Evidence: "absolute pattern"}
		p.Identity.Numbering.Kind = NumberingAbsolute
	}
}

var trailingYear = regexp.MustCompile(`^(.*?)\s+((?:19|20)\d{2})$`)

// splitTrailingYear separates a trailing 4-digit year from a normalized title
// ("The Series 2011" → "The Series", 2011), mirroring Sonarr's
// SeriesTitleInfo.TitleWithoutYear / Year split.
func splitTrailingYear(t string) (string, int) {
	m := trailingYear.FindStringSubmatch(t)
	if m == nil {
		return t, 0
	}
	year, _ := strconv.Atoi(m[2])
	return strings.TrimSpace(m[1]), year
}

func atoiOr0(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func rangeInts(start, end int) []int {
	out := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, i)
	}
	return out
}
