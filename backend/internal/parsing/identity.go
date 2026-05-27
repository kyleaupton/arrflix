package parsing

// detectDomain guesses series vs movie for the auto-detect path: a title that
// matches a Sonarr series numbering pattern (season/episode, absolute, or daily)
// is a series; otherwise it is treated as a movie. Domain-aware callers bypass
// this by passing AsSeries()/AsMovie().
//
// The check runs the same simplify pipeline + ReportTitleRegex table the series
// identity parser uses, so detection and parsing agree: if cleanSeries rejects
// the input, or no pattern matches the simple title, it is not a series.
func detectDomain(input string) Domain {
	c := cleanSeries(input)
	if c.rejected {
		return DomainMovie
	}
	for _, re := range seriesPatterns {
		if m, err := re.FindStringMatch(c.simple); err == nil && m != nil {
			return DomainSeries
		}
	}
	return DomainMovie
}
