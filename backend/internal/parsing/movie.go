package parsing

import (
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
)

// movie.go ports Radarr's movie identity path verbatim from the pinned upstream
// source (submodules/radarr/src/NzbDrone.Core/Parser/Parser.cs). It threads the
// P2 simplify pipeline (cleanMovie in clean.go) and then runs the faithful
// ReportMovieTitleRegex table + ParseMovieMatchCollection interpreter. Movies
// carry no episode/season/airdate numbering — only title, year, edition, and
// AKA alternates.
//
// Every pattern is transcribed verbatim through mustCompile (regexp2, .NET
// semantics + ReDoS timeout). RegexOptions.IgnoreCase maps to regexp2.IgnoreCase
// and is passed where upstream sets it; RegexOptions.Compiled is dropped (no
// analogue). The patterns are 100% lookaround-dependent and are NOT rewritten
// into RE2 form. Several embed EditionRegex by string concatenation — that is
// reproduced here by concatenating editionPattern, mirroring how Radarr builds
// them.

// editionPattern is Radarr's EditionRegex body (Parser.cs:18). Several
// ReportMovieTitleRegex entries concatenate it inline, and ReportEditionRegex
// prepends "^.+?" to it. Kept as a string constant so the concatenation matches
// the upstream `@"..." + EditionRegex` construction 1:1.
const editionPattern = `\(?\b(?<edition>(((Recut.|Extended.|Ultimate.)?(Director.?s|Collector.?s|Theatrical|Ultimate|Extended|Despecialized|(Special|Rouge|Final|Assembly|Imperial|Diamond|Signature|Hunter|Rekall)(?=(.(Cut|Edition|Version)))|\d{2,3}(th)?.Anniversary)(?:.(Cut|Edition|Version))?(.(Extended|Uncensored|Remastered|Unrated|Uncut|Open.?Matte|IMAX|Fan.?Edit))?|((Uncensored|Remastered|Unrated|Uncut|Open?.Matte|IMAX|Fan.?Edit|Restored|((2|3|4)in1))))))\b\)?`

// reportEditionRegex ports Radarr's ReportEditionRegex (Parser.cs:20):
// "^.+?" + EditionRegex. Used by parseEdition over the title-blanked working
// string.
var reportEditionRegex = mustCompile(`^.+?`+editionPattern, regexp2.IgnoreCase)

// moviePatterns ports Radarr's ReportMovieTitleRegex (Parser.cs:25) VERBATIM, in
// declaration order (first-match-wins). All 10 active patterns are included; the
// commented-out /* */ block at Parser.cs:47 is skipped. The folder-context table
// ReportMovieTitleFolderRegex (Parser.cs:63) is deferred to path-flavor mode and
// is NOT ported here.
var moviePatterns = []*regexp2.Regexp{
	// Parser.cs:28 — Anime [Subgroup] and Year
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+?)?(?:(?:[-_\W](?<![)\[!]))*(?<year>(1(8|9)|20)\d{2}(?!p|i|x|\d+|\]|\W\d+)))+.*?(?<hash>\[\w{8}\])?(?:$|\.)`, regexp2.IgnoreCase),

	// Parser.cs:31 — Anime [Subgroup] no year, versioned title, hash
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+?)((v)(?:\d{1,2})(?:([-_. ])))(\[.*)?(?:[\[(][^])])?.*?(?<hash>\[\w{8}\])(?:$|\.)`, regexp2.IgnoreCase),

	// Parser.cs:34 — Anime [Subgroup] no year, info in double sets of brackets, hash
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+?)(\[.*).*?(?<hash>\[\w{8}\])(?:$|\.)`, regexp2.IgnoreCase),

	// Parser.cs:37 — Anime [Subgroup] no year, info in parentheses or brackets, hash
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+)(?:[\[(][^])]).*?(?<hash>\[\w{8}\])(?:$|\.)`, regexp2.IgnoreCase),

	// Parser.cs:40 — Some german or french tracker formats (German / TrueFrench),
	// embeds EditionRegex by concatenation.
	mustCompile(`^(?<title>(?![(\[]).+?)((\W|_))(`+editionPattern+`.{1,3})?(?:(?<!(19|20)\d{2}.*?)(?<!(?:Good|The)[_ .-])(German|TrueFrench))(.+?)(?=((19|20)\d{2}|$))(?<year>(19|20)\d{2}(?!p|i|\d+|\]|\W\d+))?(\W+|_|$)(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:43 — Special, Despecialized, etc. Edition Movies (edition before
	// year), embeds EditionRegex by concatenation.
	mustCompile(`^(?<title>(?![(\[]).+?)?(?:(?:[-_\W](?<![)\[!]))*`+editionPattern+`.{1,3}(?<year>(1(8|9)|20)\d{2}(?!p|i|\d+|\]|\W\d+)))+(\W+|_|$)(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:51 — Normal movie format, e.g: Mission.Impossible.3.2011
	mustCompile(`^(?<title>(?![(\[]).+?)?(?:(?:[-_\W](?<![)\[!]))*(?<year>(1(8|9)|20)\d{2}(?!p|i|(1(8|9)|20)\d{2}|\]|\W(1(8|9)|20)\d{2})))+(\W+|_|$)(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:54 — PassThePopcorn Torrent names: Star.Wars[PassThePopcorn]
	mustCompile(`^(?<title>.+?)?(?:(?:[-_\W](?<![()\[!]))*(?<year>(\[\w *\])))+(\W+|_|$)(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:57 — Maybe some tool uses [] for years.
	mustCompile(`^(?<title>(?![(\[]).+?)?(?:(?:[-_\W](?<![)!]))*(?<year>(1(8|9)|20)\d{2}(?!p|i|\d+|\W\d+)))+(\W+|_|$)(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:60 — Last resort for movies that have ( or [ in their title.
	mustCompile(`^(?<title>.+?)?(?:(?:[-_\W](?<![)\[!]))*(?<year>(1(8|9)|20)\d{2}(?!p|i|\d+|\]|\W\d+)))+(\W+|_|$)(?!\\)`, regexp2.IgnoreCase),
}

// movieAlternativeTitleRegex ports Radarr's AlternativeTitleRegex (Parser.cs:102):
// splits movie titles on " AKA " / " / ".
var movieAlternativeTitleRegex = mustCompile(`[ ]+(?:AKA|\/)[ ]+`, regexp2.IgnoreCase)

// movieBracketedAlternativeTitleRegex ports BracketedAlternativeTitleRegex
// (Parser.cs:105): un-brackets a "(aka ...)" alternative title.
var movieBracketedAlternativeTitleRegex = mustCompile(`(.*) \([ ]*AKA[ ]+(.*)\)`, regexp2.IgnoreCase)

// movieNormalizeAlternativeTitleRegex ports NormalizeAlternativeTitleRegex
// (Parser.cs:107): rewrites "A.K.A." to " AKA ".
var movieNormalizeAlternativeTitleRegex = mustCompile(`[ ]+(?:A\.K\.A\.)[ ]+`, regexp2.IgnoreCase)

// movieRequestInfoRegex ports RequestInfoRegex (Parser.cs:135): a leading run of
// "[...]" bracket tags, stripped from the movie name.
var movieRequestInfoRegex = mustCompile(`^(?:\[.+?\])+`, 0)

// movieSimpleReleaseTitleRegex ports SimpleReleaseTitleRegex (Parser.cs:119):
// strips "<>?*|" characters when building simpleReleaseTitle for the
// group/language/edition pass.
var movieSimpleReleaseTitleRegex = mustCompile(`\s*(?:[<>?*|])`, regexp2.IgnoreCase)

// parseMovieIdentity runs the faithful Radarr ParseMovieTitle identity pipeline
// (Parser.cs:169) over input and fills Identity.Title/Year/Edition/AllTitles.
// Quality/language are parsed independently elsewhere and are NOT gated here.
func parseMovieIdentity(p *ParsedRelease, input string) {
	c := cleanMovie(input)
	if c.rejected {
		// ValidateBeforeParsing failed → Radarr returns null. Leave identity empty.
		return
	}

	// Main loop (Parser.cs:232): first regex with a match wins. The table is
	// matched against the quality/codec-stripped simple title.
	for _, re := range moviePatterns {
		m, err := re.FindStringMatch(c.simple)
		if err != nil || m == nil {
			continue
		}
		res, ok := parseMovieMatchCollection(m)
		if !ok {
			// ParseMovieMatchCollection rejected (missing/"(" title) → upstream
			// returns null and the loop tries the next regex.
			continue
		}

		// Title / year / AKA from the match interpreter.
		if res.title != "" {
			p.Identity.Title = Field[string]{Value: res.title, Confidence: confDetected, Evidence: "movie title pattern"}
		}
		if res.year != 0 {
			p.Identity.Year = Field[int]{Value: res.year, Confidence: confDetected, Evidence: "movie year pattern"}
		}
		if len(res.allTitles) > 0 {
			p.Identity.AllTitles = Field[[]string]{Value: res.allTitles, Confidence: confDetected, Evidence: "movie AKA parser"}
		}

		// Edition (Parser.cs:281): use the match's edition group if present, else
		// fall back to ParseEdition over the title-blanked working string.
		edition := res.edition
		if edition == "" {
			edition = parseMovieEdition(buildSimpleReleaseTitle(c.release, m, res.title))
		}
		if edition != "" {
			p.Identity.Edition = Field[string]{Value: edition, Confidence: confDetected, Evidence: "movie edition parser"}
		}
		return
	}
}

// movieMatch is the subset of ParsedMovieInfo the identity port produces:
// title (PrimaryMovieTitle), year, edition (from the match group), and the full
// AKA-expanded title list.
type movieMatch struct {
	title     string
	year      int
	edition   string
	allTitles []string
}

// parseMovieMatchCollection ports Radarr's ParseMovieMatchCollection
// (Parser.cs:494). It returns ok=false when the title group is absent or equals
// "(" (upstream returns null), which lets the caller try the next pattern.
func parseMovieMatchCollection(m *regexp2.Match) (movieMatch, bool) {
	titleGroup := m.GroupByName("title")
	if titleGroup == nil || len(titleGroup.Captures) == 0 || titleGroup.String() == "(" {
		return movieMatch{}, false
	}

	movieName := strings.ReplaceAll(titleGroup.String(), "_", " ")
	movieName = replaceAll(movieNormalizeAlternativeTitleRegex, movieName, " AKA ")
	movieName = strings.Trim(replaceAll(movieRequestInfoRegex, movieName, ""), " ")
	movieName = deDotAcronyms(movieName)

	year := 0
	if yg := m.GroupByName("year"); yg != nil && len(yg.Captures) > 0 {
		if n, err := strconv.Atoi(yg.String()); err == nil {
			year = n
		}
	}

	res := movieMatch{title: movieName, year: year}

	if eg := m.GroupByName("edition"); eg != nil && len(eg.Captures) > 0 {
		res.edition = strings.ReplaceAll(eg.String(), ".", " ")
	}

	// MovieTitles: primary, then AKA-expanded, non-empty + de-duplicated.
	movieTitles := []string{movieName}
	unbracketed := replaceAll(movieBracketedAlternativeTitleRegex, movieName, "$1 AKA $2")
	for _, alt := range splitRegex(movieAlternativeTitleRegex, unbracketed) {
		if strings.TrimSpace(alt) != "" && alt != movieName {
			movieTitles = append(movieTitles, alt)
		}
	}
	res.allTitles = movieTitles

	return res, true
}

// deDotAcronyms ports the acronym de-dotting loop in ParseMovieMatchCollection
// (Parser.cs:505). It splits movieName on '.' and reassembles: single-char parts
// that look like initials keep their trailing dot (reconstructing "S.H.I.E.L.D"),
// "a"/"dr" are special-cased, otherwise a part's dots become spaces.
func deDotAcronyms(movieName string) string {
	parts := strings.Split(movieName, ".")
	var b strings.Builder
	previousAcronym := false
	for n, part := range parts {
		nextPart := ""
		if len(parts) >= n+2 {
			nextPart = parts[n+1]
		}

		lower := strings.ToLower(part)
		_, partIsInt := atoiOK(part)
		_, nextIsInt := atoiOK(nextPart)

		switch {
		case len([]rune(part)) == 1 && lower != "a" && !partIsInt &&
			(previousAcronym || n < len(parts)-1) &&
			(previousAcronym || len([]rune(nextPart)) != 1 || !nextIsInt):
			b.WriteString(part + ".")
			previousAcronym = true
		case lower == "a" && (previousAcronym || len([]rune(nextPart)) == 1):
			b.WriteString(part + ".")
			previousAcronym = true
		case lower == "dr":
			b.WriteString(part + ".")
			previousAcronym = true
		default:
			if previousAcronym {
				b.WriteString(" ")
				previousAcronym = false
			}
			b.WriteString(part + " ")
		}
	}
	return strings.Trim(b.String(), " ")
}

// atoiOK reports whether s parses as an int, mirroring C#'s int.TryParse used in
// the acronym de-dotting guards.
func atoiOK(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// splitRegex splits s on every match of re, mirroring .NET Regex.Split (which
// keeps the inter-match segments, including leading/trailing empties).
func splitRegex(re *regexp2.Regexp, s string) []string {
	var out []string
	prev := 0
	runes := []rune(s)
	m, err := re.FindStringMatch(s)
	for err == nil && m != nil {
		out = append(out, string(runes[prev:m.Index]))
		prev = m.Index + m.Length
		m, err = re.FindNextMatch(m)
	}
	out = append(out, string(runes[prev:]))
	return out
}

// buildSimpleReleaseTitle ports the simpleReleaseTitle construction in
// ParseMovieTitle (Parser.cs:246): strip "<>?*|" via SimpleReleaseTitleRegex,
// then blank out the matched title text — replacing it with "A.Movie" (dotted
// title) or "A Movie" — so title words don't pollute edition parsing. The blank
// uses the title group's rune offset into the (un-stripped) releaseTitle when the
// group succeeded; otherwise it replaces the primary title text. Upstream applies
// the SimpleReleaseTitleRegex strip to releaseTitle first, but indexes with the
// title group's offset into that stripped string — faithfully reproduced here by
// computing the blank against the stripped string, falling back to a literal
// replace when offsets don't line up (the group-fails branch upstream).
func buildSimpleReleaseTitle(releaseTitle string, m *regexp2.Match, primaryTitle string) string {
	simple := replaceAll(movieSimpleReleaseTitleRegex, releaseTitle, "")

	titleGroup := m.GroupByName("title")
	hasTitleGroup := titleGroup != nil && len(titleGroup.Captures) > 0

	var replaceSrc string
	if hasTitleGroup {
		replaceSrc = titleGroup.String()
	} else {
		replaceSrc = primaryTitle
	}
	if strings.TrimSpace(replaceSrc) == "" {
		return simple
	}

	dotted := "A Movie"
	if strings.Contains(replaceSrc, ".") {
		dotted = "A.Movie"
	}

	if hasTitleGroup {
		// Mirror C# Remove(index,len).Insert(index, ...). The match ran against
		// simpleTitle, not this stripped string, so the offsets only coincide when
		// the SimpleReleaseTitleRegex strip changed nothing before the title — the
		// usual case (titles rarely contain <>?*|). When the offset is in range we
		// splice by rune offset; otherwise we fall back to a literal replace.
		runes := []rune(simple)
		idx, length := titleGroup.Index, titleGroup.Length
		if idx >= 0 && idx+length <= len(runes) && string(runes[idx:idx+length]) == replaceSrc {
			return string(runes[:idx]) + dotted + string(runes[idx+length:])
		}
	}
	return strings.Replace(simple, replaceSrc, dotted, 1)
}

// parseMovieEdition ports Radarr's ParseEdition (Parser.cs:354): runs
// ReportEditionRegex over the title-blanked working string and returns the
// "edition" group with dots replaced by spaces, or "" when absent. (Named to
// avoid colliding with engine.go's quality-path parseEdition, which is untouched.)
func parseMovieEdition(languageTitle string) string {
	m, err := reportEditionRegex.FindStringMatch(languageTitle)
	if err != nil || m == nil {
		return ""
	}
	eg := m.GroupByName("edition")
	if eg == nil || len(eg.Captures) == 0 || strings.TrimSpace(eg.String()) == "" {
		return ""
	}
	return strings.ReplaceAll(eg.String(), ".", " ")
}
