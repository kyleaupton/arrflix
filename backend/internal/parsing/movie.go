package parsing

import (
	"regexp"
	"strconv"
)

// moviePatterns ports Radarr's ReportMovieTitleRegex (RE2-adapted): an ordered,
// first-match-wins list extracting the movie title and year. The dominant form
// is "Title <year> <quality...>"; the year delimits the title. Edition is
// already extracted by the quality engine. Folder-context patterns
// (Radarr's ReportMovieTitleFolderRegex) are deferred to path-flavor mode.
var moviePatterns = []pattern{
	{
		// "Movie Title (2014) ...", "Movie.Title.2014.1080p...", "Title 2014 ..."
		// Title is everything up to the first year token; the year must be
		// followed by a separator or end (so "2160p" is not read as a year).
		re: regexp.MustCompile(`^(?P<title>.+?)[ ._(\[-]+(?P<year>(?:19|20)\d{2})(?:[ ._)\]-]|$)`),
	},
}

// parseMovieIdentity fills Title and Year from the Radarr movie patterns.
func parseMovieIdentity(p *ParsedRelease, input string) {
	groups, ok := matchOrdered(moviePatterns, input)
	if !ok {
		return
	}
	if t := normalizeTitle(groups["title"]); t != "" {
		p.Identity.Title = Field[string]{Value: t, Confidence: confDetected, Evidence: "movie title pattern"}
	}
	if n, err := strconv.Atoi(groups["year"]); err == nil && n != 0 {
		p.Identity.Year = Field[int]{Value: n, Confidence: confDetected, Evidence: "movie year pattern"}
	}
}
