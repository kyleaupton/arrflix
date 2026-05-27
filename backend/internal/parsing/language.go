package parsing

import (
	"strings"

	"github.com/dlclark/regexp2"
)

// language.go ports the LanguageParser.ParseLanguages logic VERBATIM from the
// pinned upstream source. Sonarr and Radarr DIVERGE here — different
// LanguageRegex bodies, different CaseSensitiveLanguageRegex, different
// substring language sets, and different declaration/iteration order — so both
// are ported separately (parseSeriesLanguages → Sonarr LanguageParser.cs:38;
// parseMovieLanguages → Radarr LanguageParser.cs:66).
//
// Every regex is transcribed verbatim through mustCompile (regexp2, .NET
// semantics + ReDoS timeout), including the .NET lookarounds RE2 can't express —
// the Spanish "(?!\(Latino\))", the German DL "(?<!WEB[-_. ]?)", the
// SUB-adjacency lookarounds, the Radarr "(?<!DTS[._ -])".
// RegexOptions.IgnoreCase maps to regexp2.IgnoreCase; the case-SENSITIVE
// CaseSensitiveLanguageRegex is intentionally compiled WITHOUT IgnoreCase
// (upstream embeds inline (?i) only around the SUB lookarounds, which regexp2
// honors), matching upstream's case sensitivity for the 2-letter codes.
//
// Language NAME outputs match the oracle's display names (Language.cs), e.g.
// "Portuguese (Brazil)", "Spanish (Latino)". DistinctBy(l => (int)l) upstream
// dedupes by language id; since each name is unique we dedupe by name, first
// seen wins (preserving order).

// ---------------------------------------------------------------------------
// Sonarr LanguageParser (submodules/sonarr/.../LanguageParser.cs).
// ---------------------------------------------------------------------------

// seriesCleanSeriesTitleRegex ports Sonarr CleanSeriesTitleRegex
// (LanguageParser.cs:18): drops the leading series title up to the first
// S\d\d(E\d\d…) token before language parsing.
var seriesCleanSeriesTitleRegex = mustCompile(
	`.*?[_. ](S\d{2}(?:E\d{2,4})*[_. ].*)`, regexp2.IgnoreCase)

// seriesLanguageRegex ports Sonarr LanguageRegex (LanguageParser.cs:23) VERBATIM.
var seriesLanguageRegex = mustCompile(
	`(?:\W|_)(?<english>\b(?:ing|eng)\b)|(?<italian>\b(?:ita|italian)\b)|(?<german>german\b|videomann|ger[. ]dub)|(?<flemish>flemish)|(?<greek>greek)|(?<french>(?:\W|_)(?:FR|VF|VF2|VFF|VFI|VFQ|TRUEFRENCH|FRENCH)(?:\W|_))|(?<russian>\b(?:rus|ru)\b)|(?<hungarian>\b(?:HUNDUB|HUN)\b)|(?<hebrew>\bHebDub\b)|(?<polish>\b(?:PL\W?DUB|DUB\W?PL|LEK\W?PL|PL\W?LEK)\b)|(?<chinese>\[(?:CH[ST]|BIG5|GB)\]|简|繁|字幕)|(?<bulgarian>\bbgaudio\b)|(?<spanish>\b(?:español|castellano|esp|spa(?!\(Latino\)))\b)|(?<ukrainian>\b(?:\dx?)?(?:ukr))|(?<thai>\b(?:THAI)\b)|(?<romanian>\b(?:RoDubbed|ROMANIAN)\b)|(?<catalan>[-,. ]cat[. ](?:DD|subs)|\b(?:catalan|catalán)\b)|(?<latvian>\b(?:lat|lav|lv)\b)|(?<turkish>\b(?:tur)\b)|(?<original>\b(?:orig|original)\b)`,
	regexp2.IgnoreCase)

// seriesCaseSensitiveLanguageRegex ports Sonarr CaseSensitiveLanguageRegex
// (LanguageParser.cs:26) VERBATIM. Compiled WITHOUT IgnoreCase — the codes are
// matched case-sensitively; the inline (?i) wraps only the SUB lookarounds.
var seriesCaseSensitiveLanguageRegex = mustCompile(
	`(?:(?i)(?<!SUB[\W|_|^]))(?:(?<lithuanian>\bLT\b)|(?<czech>\bCZ\b)|(?<polish>\bPL\b)|(?<bulgarian>\bBG\b)|(?<slovak>\bSK\b))(?:(?i)(?![\W|_|^]SUB))`,
	0)

// seriesGermanDualLanguageRegex ports Sonarr GermanDualLanguageRegex
// (LanguageParser.cs:29) VERBATIM — the lookbehind is back in the pattern.
var seriesGermanDualLanguageRegex = mustCompile(`(?<!WEB[-_. ]?)\bDL\b`, regexp2.IgnoreCase)

// seriesGermanMultiLanguageRegex ports Sonarr GermanMultiLanguageRegex
// (LanguageParser.cs:30) VERBATIM.
var seriesGermanMultiLanguageRegex = mustCompile(`\bML\b`, regexp2.IgnoreCase)

// parseSeriesLanguages ports Sonarr LanguageParser.ParseLanguages
// (LanguageParser.cs:38). Input is result.ReleaseTokens (the substring after the
// S/E portion; Parser.cs:781). Returns the detected language NAMES (empty = none
// detected, which upstream represents as [Unknown] and our harness treats as
// the empty set).
func parseSeriesLanguages(title string) []string {
	// CleanSeriesTitleRegex (LanguageParser.cs:40): break on the first match.
	if isMatch(seriesCleanSeriesTitleRegex, title) {
		title = replaceFirstGroup(seriesCleanSeriesTitleRegex, title)
	}

	lowerTitle := strings.ToLower(title)

	var langs langAccumulator

	// Substring set (LanguageParser.cs:52-180), in upstream declaration order.
	addContains(&langs, lowerTitle, "spanish", "Spanish")
	addContains(&langs, lowerTitle, "danish", "Danish")
	addContains(&langs, lowerTitle, "dutch", "Dutch")
	addContains(&langs, lowerTitle, "japanese", "Japanese")
	addContains(&langs, lowerTitle, "icelandic", "Icelandic")
	if strings.Contains(lowerTitle, "mandarin") || strings.Contains(lowerTitle, "cantonese") || strings.Contains(lowerTitle, "chinese") {
		langs.add("Chinese")
	}
	addContains(&langs, lowerTitle, "korean", "Korean")
	addContains(&langs, lowerTitle, "russian", "Russian")
	addContains(&langs, lowerTitle, "polish", "Polish")
	addContains(&langs, lowerTitle, "vietnamese", "Vietnamese")
	addContains(&langs, lowerTitle, "swedish", "Swedish")
	addContains(&langs, lowerTitle, "norwegian", "Norwegian")
	addContains(&langs, lowerTitle, "finnish", "Finnish")
	addContains(&langs, lowerTitle, "turkish", "Turkish")
	addContains(&langs, lowerTitle, "portuguese", "Portuguese")
	addContains(&langs, lowerTitle, "hungarian", "Hungarian")
	addContains(&langs, lowerTitle, "hebrew", "Hebrew")
	addContains(&langs, lowerTitle, "arabic", "Arabic")
	addContains(&langs, lowerTitle, "hindi", "Hindi")
	addContains(&langs, lowerTitle, "malayalam", "Malayalam")
	addContains(&langs, lowerTitle, "ukrainian", "Ukrainian")
	addContains(&langs, lowerTitle, "bulgarian", "Bulgarian")
	addContains(&langs, lowerTitle, "slovak", "Slovak")
	if strings.Contains(lowerTitle, "brazilian") || strings.Contains(lowerTitle, "dublado") {
		langs.add("Portuguese (Brazil)")
	}
	addContains(&langs, lowerTitle, "latino", "Spanish (Latino)")
	addContains(&langs, lowerTitle, "latvian", "Latvian")

	// RegexLanguage (LanguageParser.cs:182 → 337).
	seriesRegexLanguage(title, &langs)

	// English substring (LanguageParser.cs:189) — appended AFTER the regex pass.
	addContains(&langs, lowerTitle, "english", "English")

	// German dual/multi (LanguageParser.cs:199): when German is the only detected
	// language, a standalone DL (not WEB-DL) adds Original; ML adds Original+English.
	if len(langs.order) == 1 && langs.order[0] == "German" {
		switch {
		case isMatch(seriesGermanDualLanguageRegex, title):
			langs.add("Original")
		case isMatch(seriesGermanMultiLanguageRegex, title):
			langs.add("Original")
			langs.add("English")
		}
	}

	return langs.order
}

// seriesRegexLanguage ports Sonarr RegexLanguage (LanguageParser.cs:337): the
// case-sensitive pass (single Match) then the case-insensitive pass (all
// Matches), each named group mapped to its language NAME in upstream order.
func seriesRegexLanguage(title string, langs *langAccumulator) {
	// Case sensitive (single Match).
	if cs := firstMatchGroups(seriesCaseSensitiveLanguageRegex, title); cs != nil {
		if cs["lithuanian"] {
			langs.add("Lithuanian")
		}
		if cs["czech"] {
			langs.add("Czech")
		}
		if cs["polish"] {
			langs.add("Polish")
		}
		if cs["bulgarian"] {
			langs.add("Bulgarian")
		}
		if cs["slovak"] {
			langs.add("Slovak")
		}
	}

	// Case insensitive (all Matches), upstream group order (LanguageParser.cs:372).
	for _, g := range allMatchGroups(seriesLanguageRegex, title) {
		if g["english"] {
			langs.add("English")
		}
		if g["italian"] {
			langs.add("Italian")
		}
		if g["german"] {
			langs.add("German")
		}
		if g["flemish"] {
			langs.add("Flemish")
		}
		if g["greek"] {
			langs.add("Greek")
		}
		if g["french"] {
			langs.add("French")
		}
		if g["russian"] {
			langs.add("Russian")
		}
		if g["dutch"] { // declared in code though no dutch group in the pattern
			langs.add("Dutch")
		}
		if g["hungarian"] {
			langs.add("Hungarian")
		}
		if g["hebrew"] {
			langs.add("Hebrew")
		}
		if g["polish"] {
			langs.add("Polish")
		}
		if g["chinese"] {
			langs.add("Chinese")
		}
		if g["bulgarian"] {
			langs.add("Bulgarian")
		}
		if g["ukrainian"] {
			langs.add("Ukrainian")
		}
		if g["spanish"] {
			langs.add("Spanish")
		}
		if g["thai"] {
			langs.add("Thai")
		}
		if g["romanian"] {
			langs.add("Romanian")
		}
		if g["catalan"] {
			langs.add("Catalan")
		}
		if g["latvian"] {
			langs.add("Latvian")
		}
		if g["turkish"] {
			langs.add("Turkish")
		}
		if g["original"] {
			langs.add("Original")
		}
	}
}

// ---------------------------------------------------------------------------
// Radarr LanguageParser (submodules/radarr/.../LanguageParser.cs).
// ---------------------------------------------------------------------------

// movieLanguageRegex ports Radarr LanguageRegex (LanguageParser.cs:18) VERBATIM.
// IgnorePatternWhitespace upstream — reproduced via regexp2.IgnorePatternWhitespace
// so the multi-line whitespace-laden pattern is transcribed exactly.
var movieLanguageRegex = mustCompile(
	`(?:\W|_|^)(?<english>\beng\b)|
                                                                            (?<italian>\b(?:ita|italian)\b)|
                                                                            (?<german>(?:swiss)?german\b|videomann|ger[. ]dub|\bger\b)|
                                                                            (?<flemish>flemish)|
                                                                            (?<bulgarian>bgaudio)|
                                                                            (?<romanian>rodubbed)|
                                                                            (?<brazilian>\b(dublado|pt-BR)\b)|
                                                                            (?<greek>greek)|
                                                                            (?<french>\b(?:FR|VO|VF|VFF|VFQ|VFI|VF2|TRUEFRENCH|FRENCH|FRE|FRA)\b)|
                                                                            (?<russian>\b(?:rus|ru)\b)|
                                                                            (?<hungarian>\b(?:HUNDUB|HUN)\b)|
                                                                            (?<hebrew>\b(?:HebDub|HebDubbed)\b)|
                                                                            (?<polish>\b(?:PL\W?DUB|DUB\W?PL|LEK\W?PL|PL\W?LEK)\b)|
                                                                            (?<chinese>\[(?:CH[ST]|BIG5|GB)\]|简|繁|字幕)|
                                                                            (?<ukrainian>(?:(?:\dx)?UKR))|
                                                                            (?<spanish>\b(?:español|castellano)\b)|
                                                                            (?<catalan>\b(?:catalan?|catalán|català)\b)|
                                                                            (?<latvian>\b(?:lat|lav|lv)\b)|
                                                                            (?<telugu>\btel\b)|
                                                                            (?<vietnamese>\bVIE\b)|
                                                                            (?<japanese>\bJAP\b)|
                                                                            (?<korean>\bKOR\b)|
                                                                            (?<urdu>\burdu\b)|
                                                                            (?<romansh>\b(?:romansh|rumantsch|romansch)\b)|
                                                                            (?<mongolian>\b(?:mongolian|khalkha)\b)|
                                                                            (?<georgian>\b(?:georgian|geo|ka|kat)\b)|
                                                                            (?<original>\b(?:orig|original)\b)`,
	regexp2.IgnoreCase|regexp2.IgnorePatternWhitespace)

// movieCaseSensitiveLanguageRegex ports Radarr CaseSensitiveLanguageRegex
// (LanguageParser.cs:47) VERBATIM. Compiled WITHOUT IgnoreCase (case-sensitive
// codes); the inline (?i) wraps only the SUB lookarounds. The Spanish branch
// carries the inline (?<!DTS[._ -]) lookbehind verbatim.
var movieCaseSensitiveLanguageRegex = mustCompile(
	`(?:(?i)(?<!SUB[\W|_|^]))(?:(?<english>\bEN\b)|
                                                                                                          (?<lithuanian>\bLT\b)|
                                                                                                          (?<czech>\bCZ\b)|
                                                                                                          (?<polish>\bPL\b)|
                                                                                                          (?<bulgarian>\bBG\b)|
                                                                                                          (?<slovak>\bSK\b)|
                                                                                                          (?<german>\bDE\b)|
                                                                                                          (?<spanish>\b(?<!DTS[._ -])ES\b))(?:(?i)(?![\W|_|^]SUB))`,
	regexp2.IgnorePatternWhitespace)

// movieGermanDualLanguageRegex ports Radarr GermanDualLanguageRegex
// (LanguageParser.cs:57) VERBATIM.
var movieGermanDualLanguageRegex = mustCompile(`(?<!WEB[-_. ]?)\bDL\b`, regexp2.IgnoreCase)

// movieGermanMultiLanguageRegex ports Radarr GermanMultiLanguageRegex
// (LanguageParser.cs:58) VERBATIM.
var movieGermanMultiLanguageRegex = mustCompile(`\bML\b`, regexp2.IgnoreCase)

// parseMovieLanguages ports Radarr LanguageParser.ParseLanguages
// (LanguageParser.cs:66). Input is simpleReleaseTitle with the release group
// replaced by "RlsGrp" (Parser.cs:275). Returns the detected language NAMES.
func parseMovieLanguages(title string) []string {
	lowerTitle := strings.ToLower(title)

	var langs langAccumulator

	// Substring set (LanguageParser.cs:71-259), in upstream declaration order.
	addContains(&langs, lowerTitle, "english", "English")
	addContains(&langs, lowerTitle, "spanish", "Spanish")
	addContains(&langs, lowerTitle, "danish", "Danish")
	addContains(&langs, lowerTitle, "dutch", "Dutch")
	addContains(&langs, lowerTitle, "japanese", "Japanese")
	addContains(&langs, lowerTitle, "icelandic", "Icelandic")
	if strings.Contains(lowerTitle, "mandarin") || strings.Contains(lowerTitle, "cantonese") || strings.Contains(lowerTitle, "chinese") {
		langs.add("Chinese")
	}
	addContains(&langs, lowerTitle, "korean", "Korean")
	addContains(&langs, lowerTitle, "russian", "Russian")
	addContains(&langs, lowerTitle, "romanian", "Romanian")
	addContains(&langs, lowerTitle, "hindi", "Hindi")
	addContains(&langs, lowerTitle, "arabic", "Arabic")
	addContains(&langs, lowerTitle, "thai", "Thai")
	addContains(&langs, lowerTitle, "bulgarian", "Bulgarian")
	addContains(&langs, lowerTitle, "polish", "Polish")
	addContains(&langs, lowerTitle, "vietnamese", "Vietnamese")
	addContains(&langs, lowerTitle, "swedish", "Swedish")
	addContains(&langs, lowerTitle, "norwegian", "Norwegian")
	addContains(&langs, lowerTitle, "finnish", "Finnish")
	addContains(&langs, lowerTitle, "turkish", "Turkish")
	addContains(&langs, lowerTitle, "portuguese", "Portuguese")
	addContains(&langs, lowerTitle, "brazilian", "Portuguese (Brazil)")
	addContains(&langs, lowerTitle, "hungarian", "Hungarian")
	addContains(&langs, lowerTitle, "hebrew", "Hebrew")
	addContains(&langs, lowerTitle, "ukrainian", "Ukrainian")
	addContains(&langs, lowerTitle, "persian", "Persian")
	addContains(&langs, lowerTitle, "bengali", "Bengali")
	addContains(&langs, lowerTitle, "slovak", "Slovak")
	addContains(&langs, lowerTitle, "latvian", "Latvian")
	addContains(&langs, lowerTitle, "latino", "Spanish (Latino)")
	addContains(&langs, lowerTitle, "tamil", "Tamil")
	addContains(&langs, lowerTitle, "telugu", "Telugu")
	addContains(&langs, lowerTitle, "malayalam", "Malayalam")
	addContains(&langs, lowerTitle, "kannada", "Kannada")
	addContains(&langs, lowerTitle, "albanian", "Albanian")
	addContains(&langs, lowerTitle, "afrikaans", "Afrikaans")
	addContains(&langs, lowerTitle, "marathi", "Marathi")
	addContains(&langs, lowerTitle, "tagalog", "Tagalog")

	// Case-sensitive pass (all Matches) (LanguageParser.cs:262).
	for _, g := range allMatchGroups(movieCaseSensitiveLanguageRegex, title) {
		if g["english"] {
			langs.add("English")
		}
		if g["lithuanian"] {
			langs.add("Lithuanian")
		}
		if g["czech"] {
			langs.add("Czech")
		}
		if g["polish"] {
			langs.add("Polish")
		}
		if g["bulgarian"] {
			langs.add("Bulgarian")
		}
		if g["slovak"] {
			langs.add("Slovak")
		}
		if g["spanish"] {
			langs.add("Spanish")
		}
		if g["german"] {
			langs.add("German")
		}
	}

	// Case-insensitive pass (all Matches) (LanguageParser.cs:308), upstream order.
	for _, g := range allMatchGroups(movieLanguageRegex, title) {
		if g["english"] {
			langs.add("English")
		}
		if g["italian"] {
			langs.add("Italian")
		}
		if g["german"] {
			langs.add("German")
		}
		if g["flemish"] {
			langs.add("Flemish")
		}
		if g["greek"] {
			langs.add("Greek")
		}
		if g["french"] {
			langs.add("French")
		}
		if g["russian"] {
			langs.add("Russian")
		}
		if g["bulgarian"] {
			langs.add("Bulgarian")
		}
		if g["brazilian"] {
			langs.add("Portuguese (Brazil)")
		}
		if g["dutch"] { // declared in code though no dutch group in the pattern
			langs.add("Dutch")
		}
		if g["hungarian"] {
			langs.add("Hungarian")
		}
		if g["hebrew"] {
			langs.add("Hebrew")
		}
		if g["polish"] {
			langs.add("Polish")
		}
		if g["chinese"] {
			langs.add("Chinese")
		}
		if g["spanish"] {
			langs.add("Spanish")
		}
		if g["catalan"] {
			langs.add("Catalan")
		}
		if g["ukrainian"] {
			langs.add("Ukrainian")
		}
		if g["latvian"] {
			langs.add("Latvian")
		}
		if g["romanian"] {
			langs.add("Romanian")
		}
		if g["telugu"] {
			langs.add("Telugu")
		}
		if g["vietnamese"] {
			langs.add("Vietnamese")
		}
		if g["japanese"] {
			langs.add("Japanese")
		}
		if g["korean"] {
			langs.add("Korean")
		}
		if g["urdu"] {
			langs.add("Urdu")
		}
		if g["romansh"] {
			langs.add("Romansh")
		}
		if g["mongolian"] {
			langs.add("Mongolian")
		}
		if g["georgian"] {
			langs.add("Georgian")
		}
		if g["original"] {
			langs.add("Original")
		}
	}

	// German dual/multi (LanguageParser.cs:458): same rule as Sonarr.
	if len(langs.order) == 1 && langs.order[0] == "German" {
		switch {
		case isMatch(movieGermanDualLanguageRegex, title):
			langs.add("Original")
		case isMatch(movieGermanMultiLanguageRegex, title):
			langs.add("Original")
			langs.add("English")
		}
	}

	return langs.order
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// langAccumulator collects language names in first-seen order, deduping by name.
// This mirrors upstream's List<Language> + DistinctBy(l => (int)l): names map
// one-to-one to ids, so name-dedup is equivalent and preserves first-seen order.
type langAccumulator struct {
	order []string
	seen  map[string]bool
}

func (a *langAccumulator) add(name string) {
	if a.seen == nil {
		a.seen = map[string]bool{}
	}
	if a.seen[name] {
		return
	}
	a.seen[name] = true
	a.order = append(a.order, name)
}

// addContains adds name when sub is a substring of lowerTitle (lowerTitle is
// already lowercased), mirroring lowerTitle.Contains(sub).
func addContains(a *langAccumulator, lowerTitle, sub, name string) {
	if strings.Contains(lowerTitle, sub) {
		a.add(name)
	}
}

// firstMatchGroups returns a set of the named groups that participated in the
// FIRST match of re over s, or nil when there is no match. Mirrors a single
// .NET Regex.Match with Groups["name"].Captures.Any() checks.
func firstMatchGroups(re *regexp2.Regexp, s string) map[string]bool {
	m, err := re.FindStringMatch(s)
	if err != nil || m == nil {
		return nil
	}
	return matchGroupSet(m)
}

// allMatchGroups returns, for each non-overlapping match of re over s (the .NET
// MatchCollection), the set of named groups that participated. Order of matches
// is preserved, mirroring the upstream foreach over Regex.Matches.
func allMatchGroups(re *regexp2.Regexp, s string) []map[string]bool {
	var out []map[string]bool
	m, err := re.FindStringMatch(s)
	for err == nil && m != nil {
		out = append(out, matchGroupSet(m))
		m, err = re.FindNextMatch(m)
	}
	return out
}

// matchGroupSet returns the set of named groups that captured in m, mirroring
// match.Groups["name"].Success / .Captures.Any().
func matchGroupSet(m *regexp2.Match) map[string]bool {
	set := map[string]bool{}
	for _, g := range m.Groups() {
		if g.Name == "" || isIndexName(g.Name) || len(g.Captures) == 0 {
			continue
		}
		set[g.Name] = true
	}
	return set
}

// replaceFirstGroup applies the CleanSeriesTitleRegex "$1" replacement: it
// returns capture group 1 of the first match, mirroring RegexReplace(..., "$1").
// The pattern always has group 1 when it matches.
func replaceFirstGroup(re *regexp2.Regexp, s string) string {
	m, err := re.FindStringMatch(s)
	if err != nil || m == nil {
		return s
	}
	g := m.GroupByNumber(1)
	if g == nil || len(g.Captures) == 0 {
		return s
	}
	return g.String()
}
