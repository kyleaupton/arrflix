package parsing

import (
	"strings"
	"unicode"

	"github.com/dlclark/regexp2"
)

// clean.go ports the Sonarr/Radarr pre-match "simplify" pipeline verbatim from
// the pinned upstream source (submodules/sonarr|radarr/src/NzbDrone.Core/Parser/
// Parser.cs and ParserCommon.cs). It produces the three working strings the
// identity parsers thread:
//
//   - original — the untouched input, fed later to the quality parser.
//   - release  — extension-stripped + fullwidth-bracket-normalized + (Sonarr)
//     PreSubstitution applied. Fed later to match-interpretation, token/group
//     extraction.
//   - simple   — release with quality/codec stripped (SimpleTitleRegex), website
//     prefix/postfix removed, torrent suffix removed, quality brackets cleaned,
//     and six-digit airdates normalized. This is the string the identity regex
//     TABLE is matched against in later phases.
//
// This pipeline is NOT wired into Parse() yet — P3/P4 call it. It is a port of
// the simplify stages only; the matching-oriented normalize helpers
// (CleanSeriesTitle/CleanMovieTitle, NormalizeTitle) belong to the matching
// module and are intentionally not ported here.
//
// Every pattern below is transcribed verbatim from upstream and compiled through
// mustCompile (regexp2, .NET semantics + ReDoS timeout). RegexOptions.IgnoreCase
// is passed where upstream sets it; RegexOptions.Compiled is dropped (no
// analogue). RegexOptions.IgnorePatternWhitespace maps to regexp2.IgnorePatternWhitespace.

// cleaned is the output of the simplify pipeline: the three working strings plus
// a reject signal. When rejected is true the input failed ValidateBeforeParsing
// (password+yenc, no alphanumerics, a hashed-release pattern, or — Sonarr only —
// a bare season-folder name); callers should treat it as unparseable.
type cleaned struct {
	original string
	release  string
	simple   string
	rejected bool
}

// ---------------------------------------------------------------------------
// Shared regexes (identical text across Sonarr and Radarr unless noted).
// ---------------------------------------------------------------------------

// Sonarr Parser.cs:529 FileExtensionRegex / Radarr FileExtensions.cs:9
var cleanFileExtensionRegex = mustCompile(`\.[a-z0-9]{2,4}$`, regexp2.IgnoreCase)

// mediaFileExtensions is the set RemoveFileExtension strips, from
// MediaFileExtensions.Extensions (Sonarr & Radarr share the same list) plus the
// usenet extensions .par2/.nzb. Sonarr MediaFileExtensions.cs:13; Radarr
// FileExtensions.cs:12 (UsenetExtensions).
var mediaFileExtensions = map[string]bool{
	".webm": true,
	".m4v":  true, ".3gp": true, ".nsv": true, ".ty": true, ".strm": true,
	".rm": true, ".rmvb": true, ".m3u": true, ".ifo": true, ".mov": true,
	".qt": true, ".divx": true, ".xvid": true, ".bivx": true, ".nrg": true,
	".pva": true, ".wmv": true, ".asf": true, ".asx": true, ".ogm": true,
	".ogv": true, ".m2v": true, ".avi": true, ".bin": true, ".dat": true,
	".dvr-ms": true, ".mpg": true, ".mpeg": true, ".mp4": true, ".avc": true,
	".vp3": true, ".svq3": true, ".nuv": true, ".viv": true, ".dv": true,
	".fli": true, ".flv": true, ".wpl": true,
	".img": true, ".iso": true, ".vob": true,
	".mkv": true, ".ts": true, ".wtv": true,
	".m2ts": true,
	// Usenet extensions (FileExtensions.cs UsenetExtensions / Sonarr inline).
	".par2": true, ".nzb": true,
}

// Sonarr Parser.cs:532 SimpleTitleRegex
var cleanSimpleTitleRegexSeries = mustCompile(
	`(?:(480|540|576|720|1080|2160)[ip]|[xh][\W_]?26[45]|DD\W?5\W1|[<>?*]|848x480|1280x720|1920x1080|3840x2160|4096x2160|(?<![a-f0-9])(8|10)(b(?![a-z0-9])|bit)|10-bit)\s*?`,
	regexp2.IgnoreCase)

// Radarr Parser.cs:115 SimpleTitleRegex (note: different bit-depth boundary than
// Sonarr — trailing (?![a-b0-9]) lookahead instead of Sonarr's leading lookbehind).
var cleanSimpleTitleRegexMovie = mustCompile(
	`(?:(480|540|576|720|1080|2160)[ip]|[xh][\W_]?26[45]|DD\W?5\W1|[<>?*]|848x480|1280x720|1920x1080|3840x2160|4096x2160|(8|10)b(it)?|10-bit)\s*?(?![a-b0-9])`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:538 WebsitePrefixRegex / Radarr ParserCommon.cs:12 (identical text)
var cleanWebsitePrefixRegex = mustCompile(
	`^(?:(?:\[|\()\s*)?(?:www\.)?[-a-z0-9-]{1,256}\.(?<!Naruto-Kun\.)(?:[a-z]{2,6}\.[a-z]{2,6}|xn--[a-z0-9-]{4,}|[a-z]{2,})\b(?:\s*(?:\]|\))|[ -]{2,})[ -]*`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:542 WebsitePostfixRegex / Radarr ParserCommon.cs:16 (identical text)
var cleanWebsitePostfixRegex = mustCompile(
	`(?:\[\s*)?(?:www\.)?[-a-z0-9-]{1,256}\.(?:xn--[a-z0-9-]{4,}|[a-z]{2,6})\b(?:\s*\])$`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:553 CleanTorrentSuffixRegex / Radarr ParserCommon.cs:20 (identical text)
var cleanTorrentSuffixRegex = mustCompile(
	`\[(?:ettv|rartv|rarbg|cttv|publichd)\]$`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:557 CleanQualityBracketsRegex / Radarr Parser.cs:123 (identical text)
var cleanQualityBracketsRegex = mustCompile(
	`\[[a-z0-9 ._-]+\]$`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:546 SixDigitAirDateRegex (Sonarr only — movies have no airdate concept).
var cleanSixDigitAirDateRegex = mustCompile(
	`(?<=[_.-])(?<airdate>(?<!\d)(?<airyear>[1-9]\d{1})(?<airmonth>[0-1][0-9])(?<airday>[0-3][0-9]))(?=[_.-])`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:521 ReversedTitleRegex (matches the episode pattern variant too).
var cleanReversedTitleRegexSeries = mustCompile(
	`(?:^|[-._ ])(p027|p0801|\d{2,3}E\d{2}S)[-._ ]`, 0)

// Radarr Parser.cs:99 ReversedTitleRegex (no episode-pattern variant).
var cleanReversedTitleRegexMovie = mustCompile(
	`(?:^|[-._ ])(p027|p0801)[-._ ]`, 0)

// ---------------------------------------------------------------------------
// Reject-stage regexes (ValidateBeforeParsing).
// ---------------------------------------------------------------------------

// Sonarr Parser.cs:471 RejectHashedReleasesRegexes (14 patterns, in order).
var rejectHashedSeriesRegexes = []*regexp2.Regexp{
	mustCompile(`^[0-9a-zA-Z]{32}`, 0),               // md5 / mixed-case hashes
	mustCompile(`^[a-z0-9]{24}$`, 0),                 // shorter lower-case hashes
	mustCompile(`^[A-Z]{11}\d{3}$`, 0),               // NZBGeek format
	mustCompile(`^[a-z]{12}\d{3}$`, 0),               // NZBGeek format
	mustCompile(`^Backup_\d{5,}S\d{2}-\d{2}$`, 0),    // backup filename
	mustCompile(`^123$`, 0),                          // 123
	mustCompile(`^abc$`, regexp2.IgnoreCase),         // abc
	mustCompile(`^abc[-_. ]xyz`, regexp2.IgnoreCase), // abc-xyz
	mustCompile(`^b00bs$`, regexp2.IgnoreCase),       // b00bs
	mustCompile(`^\d{6}_\d{2}$`, 0),                  // 170424_26
	mustCompile(`^[0-9a-zA-Z]{30}`, 0),               // 30-char mixed-case hash
	mustCompile(`^[0-9a-zA-Z]{26}`, 0),               // 26-char mixed-case hash
	mustCompile(`^[0-9a-zA-Z]{39}`, 0),               // 39-char mixed-case hash
	mustCompile(`^[0-9a-zA-Z]{24}`, 0),               // 24-char mixed-case hash
}

// Radarr Parser.cs:69 RejectHashedReleasesRegex (9 patterns — drops Sonarr's
// 170424_26 and the 30/26/39/24-char generic hashes).
var rejectHashedMovieRegexes = []*regexp2.Regexp{
	mustCompile(`^[0-9a-zA-Z]{32}`, 0),
	mustCompile(`^[a-z0-9]{24}$`, 0),
	mustCompile(`^[A-Z]{11}\d{3}$`, 0),
	mustCompile(`^[a-z]{12}\d{3}$`, 0),
	mustCompile(`^Backup_\d{5,}S\d{2}-\d{2}$`, 0),
	mustCompile(`^123$`, 0),
	mustCompile(`^abc$`, regexp2.IgnoreCase),
	mustCompile(`^abc[-_. ]xyz`, regexp2.IgnoreCase),
	mustCompile(`^b00bs$`, regexp2.IgnoreCase),
}

// Sonarr Parser.cs:515 SeasonFolderRegexes (Sonarr only — Radarr has no
// season-folder reject).
var rejectSeasonFolderRegexes = []*regexp2.Regexp{
	mustCompile(`^(Season[ ._-]*\d+|Specials)$`, 0),
}

// ---------------------------------------------------------------------------
// Replacement helper.
// ---------------------------------------------------------------------------

// replaceAll runs re.Replace over the whole string, failing open: a regexp2
// engine error (e.g. a match timeout) leaves the input unchanged. These stages
// are normalizers, not gates, so a replace failure must never drop the string.
func replaceAll(re *regexp2.Regexp, s, repl string) string {
	out, err := re.Replace(s, repl, -1, -1)
	if err != nil {
		return s
	}
	return out
}

// isMatch reports whether re matches s, treating an engine error as no match
// (the same fail-closed convention as findFirst).
func isMatch(re *regexp2.Regexp, s string) bool {
	ok, err := re.MatchString(s)
	if err != nil {
		return false
	}
	return ok
}

// ---------------------------------------------------------------------------
// Shared pipeline helpers.
// ---------------------------------------------------------------------------

// removeFileExtensionClean mirrors Sonarr RemoveFileExtension (Parser.cs:968) /
// Radarr FileExtensions.RemoveFileExtension: it strips a trailing .ext only when
// the extension is a recognized media or usenet extension. The engine.go
// removeFileExtension is a coarser stdlib variant used by the quality path; this
// is the faithful FileExtensionRegex-driven port the simplify pipeline needs.
func removeFileExtensionClean(title string) string {
	m, err := cleanFileExtensionRegex.FindStringMatch(title)
	if err != nil || m == nil {
		return title
	}
	ext := strings.ToLower(m.String())
	if mediaFileExtensions[ext] {
		return title[:m.Index]
	}
	return title
}

// normalizeFullwidthBrackets replaces fullwidth CJK brackets with ASCII ones,
// matching the inline .Replace upstream applies right after RemoveFileExtension
// (Sonarr Parser.cs:718, Radarr Parser.cs:196).
func normalizeFullwidthBrackets(s string) string {
	s = strings.ReplaceAll(s, "【", "[")
	s = strings.ReplaceAll(s, "】", "]")
	return s
}

// stripWebsiteAndTorrent applies the website prefix/postfix strippers and the
// torrent-suffix stripper, in upstream order (Sonarr Parser.cs:732-735, Radarr
// Parser.cs:210-213). Shared between the two pipelines.
func stripWebsiteAndTorrent(simple string) string {
	simple = replaceAll(cleanWebsitePrefixRegex, simple, "")
	simple = replaceAll(cleanWebsitePostfixRegex, simple, "")
	simple = replaceAll(cleanTorrentSuffixRegex, simple, "")
	return simple
}

// stripQualityBrackets removes a trailing [..] from simple ONLY when it is NOT a
// recognized quality, mirroring Sonarr Parser.cs:737 / Radarr Parser.cs:215. The
// quality recognizer is engine.go's parseRaw: a non-Unknown result is the
// analogue of upstream's QualityParser.ParseQualityName(m.Value).Quality !=
// Quality.Unknown. This is the intended seam — when quality is re-ported, only
// the body of isRecognizedQuality changes, not this call site.
func stripQualityBrackets(simple string) string {
	m, err := cleanQualityBracketsRegex.FindStringMatch(simple)
	if err != nil || m == nil {
		return simple
	}
	if isRecognizedQuality(m.String()) {
		return simple[:m.Index] + simple[m.Index+m.Length:]
	}
	return simple
}

// isRecognizedQuality reports whether s names a real quality, standing in for
// upstream QualityParser.ParseQualityName(s).Quality != Quality.Unknown.
func isRecognizedQuality(s string) bool {
	return parseRaw(s).quality != Unknown
}

// normalizeSixDigitAirDate rewrites a YYMMDD token between separators to
// 20YY.MM.DD when month or day is non-zero, mirroring Sonarr Parser.cs:747. Only
// the first match is rewritten (upstream uses .Match, not .Matches), and the
// rewrite is a literal string replace of the matched airdate (replacing every
// occurrence of that exact token, as .Replace(value, fixed) does in C#).
func normalizeSixDigitAirDate(simple string) string {
	m, err := cleanSixDigitAirDateRegex.FindStringMatch(simple)
	if err != nil || m == nil {
		return simple
	}
	airdate := m.GroupByName("airdate")
	airyear := m.GroupByName("airyear")
	airmonth := m.GroupByName("airmonth")
	airday := m.GroupByName("airday")
	if airdate == nil || airyear == nil || airmonth == nil || airday == nil {
		return simple
	}
	if airmonth.String() == "00" && airday.String() == "00" {
		return simple
	}
	fixed := "20" + airyear.String() + "." + airmonth.String() + "." + airday.String()
	return strings.ReplaceAll(simple, airdate.String(), fixed)
}

// reverseString reverses s by rune, matching upstream's char-array reverse used
// for reversed-title correction.
func reverseString(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// applyReversedTitle mirrors the reversed-title correction (Sonarr
// Parser.cs:706, Radarr Parser.cs:181): if the reversed-title regex matches,
// strip the extension, reverse those characters, and prepend the remainder
// (which keeps a trailing extension in place, just as upstream concatenates the
// reversed prefix with title[len:]).
func applyReversedTitle(re *regexp2.Regexp, title string) string {
	if !isMatch(re, title) {
		return title
	}
	withoutExt := removeFileExtensionClean(title)
	reversed := reverseString(withoutExt)
	return reversed + title[len(withoutExt):]
}

// hasLetterOrDigit reports whether s contains any alphanumeric rune, mirroring
// upstream's title.Any(char.IsLetterOrDigit) reject guard.
func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
		// Fall back to Unicode letter/digit for non-ASCII alphabets (CJK etc.).
		if r > 127 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

// passwordYenc reports whether the lowercased title contains both "password" and
// "yenc" (the obfuscated-download reject), matching upstream ValidateBeforeParsing.
func passwordYenc(title string) bool {
	lower := strings.ToLower(title)
	return strings.Contains(lower, "password") && strings.Contains(lower, "yenc")
}

// anyMatch reports whether any of res matches s.
func anyMatch(res []*regexp2.Regexp, s string) bool {
	for _, re := range res {
		if isMatch(re, s) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// PreSubstitution (Sonarr only — Radarr's PreSubstitution is Array.Empty).
// ---------------------------------------------------------------------------

// Sonarr Parser.cs:20 PreSubstitutionRegex — 12 ordered rewrite rules. Each is
// applied if it matches; this is NOT first-match-stop (upstream loops over every
// rule and applies each that matches). Ports verbatim; ${name} replacement
// references map to regexp2's $name syntax (regexp2 accepts both ${name} and
// $name; kept as upstream wrote them).
var preSubstitutionRules = []struct {
	re   *regexp2.Regexp
	repl string
}{
	// Korean series without season number → S01Exxx and remove airdate.
	{mustCompile(`\.E(\d{2,4})\.\d{6}\.(.*-NEXT)$`, 0), ".S01E$1.$2"},

	// Chinese anime with English+Chinese titles → normal anime pattern.
	{mustCompile(`^\[(?:(?<subgroup>[^\]]+?)(?:[\x{4E00}-\x{9FCC}]+)?)\]\[(?<title>[^\]]+?)(?:\s(?<chinesetitle>[\x{4E00}-\x{9FCC}][^\]]*?))\]\[(?:(?:[\x{4E00}-\x{9FCC}]+?)?(?<episode>\d{1,4})(?:[\x{4E00}-\x{9FCC}]+?)?)\]`, 0), "[${subgroup}] ${title} - ${episode} - "},

	// LoliHouse/ZERO/Lilith-Raws/Skymoon-Raws/orion origin → bracket normalize.
	{mustCompile(`^\[(?<subgroup>[^\]]*?(?:LoliHouse|ZERO|Lilith-Raws|Skymoon-Raws|orion origin)[^\]]*?)\](?<title>[^\[\]]+?)(?: - (?<episode>[0-9-]+)\s*|\[第?(?<episode>[0-9]+(?:-[0-9]+)?)话?(?:END|完)?\])\[`, 0), "[${subgroup}][${title}][${episode}]["},

	// Most Chinese anime — strip junk, S0x → Sx.
	{mustCompile(`^\[(?<subgroup>[^\]]+)\](?:\s?★[^\[ -]+\s?)?\[?(?:(?<chinesetitle>(?=[^\]]*?[\x{4E00}-\x{9FCC}])[^\]]*?)(?:\]\[|\s*[_/·]\s*)){0,2}(?<title>[^\[\]]+?)(?:\s(?:S?(?<!\d+)((0)(?<season>\d)|(?<season>[1-9]\d))(?!\d+)))\]?(?:\[\d{4}\])?\[第?(?<episode>[0-9]+(?:-[0-9]+)?)(?:话|集)?(?: ?END|完| ?Fin)?\]`, 0), "[${subgroup}] ${title} S${season} - ${episode} "},

	// Chinese releases with no separation between CJK and English titles.
	{mustCompile(`^\[(?<subgroup>[^\]]+)\]\[(?<chinesetitle>(?<![^a-zA-Z0-9])[^a-zA-Z0-9]+)(?<title>[^\]]+?)\](?:\[\d{4}\])?\[第?(?<episode>[0-9]+(?:-[0-9]+)?)(?:话|集)?(?: ?END|完| ?Fin)?\]`, 0), "[${subgroup}] ${title} - ${episode} "},

	// Most Chinese anime — strip junk, normal anime pattern.
	{mustCompile(`^\[(?<subgroup>[^\]]+)\](?:\s?★[^\[ -]+\s?)?\[?(?:(?<chinesetitle>(?=[^\]]*?[\x{4E00}-\x{9FCC}])[^\]]*?)(?:\]\[|\s*[_/·]\s*)){0,2}(?<title>[^\]]+?)\]?(?:\[\d{4}\])?\[第?(?<episode>[0-9]{1,4}(?:-[0-9]{1,4})?)(?:话|集)?(?: ?END|完| ?Fin)?\]`, 0), "[${subgroup}] ${title} - ${episode} "},

	// Chinese+English, S0x → Sx (separated by " / ").
	{mustCompile(`^\[(?<subgroup>[^\]]+)\](?:\s)(?:(?<chinesetitle>(?=[^\]]*?[\x{4E00}-\x{9FCC}])[^\]]*?)(?:\s/\s))(?<title>[^\[\]]+?)(?:\s(?:S?(?<!\d+)((0)(?<season>\d)|(?<season>[1-9]\d))(?!\d+)))(?:[- ]+)(?<episode>[0-9]+(?:-[0-9]+)?)话?(?:END|完)?`, 0), "[${subgroup}] ${title} S${season} - ${episode} "},

	// English+Chinese (English first, separated by " / ").
	{mustCompile(`^\[(?<subgroup>[^\]]+)\](?:\s)(?:(?<title>[^\]]+?)(?:\s/\s))(?<chinesetitle>(?=[^\]]*?[\x{4E00}-\x{9FCC}])[^\]]*?)(?:[- ]+)(?<episode>[0-9]+(?:-[0-9]+)?(?![a-z]))话?(?:END|完)?`, 0), "[${subgroup}] ${title} - ${episode} "},

	// Chinese+English (Chinese first, separated by " / ").
	{mustCompile(`^\[(?<subgroup>[^\]]+)\](?:\s)(?:(?<chinesetitle>(?=[^\]]*?[\x{4E00}-\x{9FCC}])[^\]]*?)(?:\s/\s))(?<title>[^\]]+?)(?:[- ]+)(?<episode>[0-9]+(?:-[0-9]+)?(?![a-z]))话?(?:END|完)?`, 0), "[${subgroup}] ${title} - ${episode} "},

	// GM-Team releases with lots of square brackets.
	{mustCompile(`^\[(?<subgroup>[^\]]+)\](?:(?<chinesubgroup>\[(?=[^\]]*?[\x{4E00}-\x{9FCC}])[^\]]*\])+)\[(?<title>[^\]]+?)\](?<junk>\[[^\]]+\])*\[(?<episode>[0-9]+(?:-[0-9]+)?)( END| Fin)?\]`, 0), "[${subgroup}] ${title} - ${episode} "},

	// Chinese+English separated by | instead of /.
	{mustCompile(`^\[(?<subgroup>[^\]]+)\](?:\s)(?:(?<chinesetitle>(?=[^\]]*?[\x{4E00}-\x{9FCC}])[^\]]*?)(?:\s\|\s))(?<title>[^\]]+?)(?:[- ]+)(?<episode>[0-9]+(?:-[0-9]+)?(?![a-z]))话?(?:END|完)?`, 0), "[${subgroup}] ${title} - ${episode} "},

	// Spanish releases with information in brackets.
	{mustCompile(`^(?<title>.+?(?=[ ._-]\()).+?\((?<year>\d{4})\/(?<info>S[^\/]+)`, 0), "${title} (${year}) - ${info} "},
}

// applyPreSubstitution runs every PreSubstitution rule that matches, in order
// (Sonarr Parser.cs:720). Each rule replaces fail-open.
func applyPreSubstitution(release string) string {
	for _, rule := range preSubstitutionRules {
		if isMatch(rule.re, release) {
			release = replaceAll(rule.re, release, rule.repl)
		}
	}
	return release
}

// ---------------------------------------------------------------------------
// Pipelines.
// ---------------------------------------------------------------------------

// cleanSeries runs the Sonarr ParseTitle simplify pipeline (Parser.cs:695). The
// stage order is faithful: validate/reject → reversed-title → RemoveFileExtension
// → fullwidth brackets → PreSubstitution → SimpleTitleRegex → website strip →
// torrent suffix → quality brackets → six-digit airdate.
func cleanSeries(input string) cleaned {
	c := cleaned{original: input}

	// Stage 1 — ValidateBeforeParsing (Parser.cs:1262).
	if validateRejectSeries(input) {
		c.rejected = true
		return c
	}

	// Stage 2 — reversed-title detection (Parser.cs:706).
	title := applyReversedTitle(cleanReversedTitleRegexSeries, input)

	// Stage 3 — RemoveFileExtension (Parser.cs:716).
	release := removeFileExtensionClean(title)

	// Stage 4 — fullwidth brackets (Parser.cs:718).
	release = normalizeFullwidthBrackets(release)

	// Stage 5 — PreSubstitution (Parser.cs:720).
	release = applyPreSubstitution(release)
	c.release = release

	// Stage 6 — SimpleTitleRegex (Parser.cs:729).
	simple := replaceAll(cleanSimpleTitleRegexSeries, release, "")

	// Stages 7-8 — website prefix/postfix + torrent suffix (Parser.cs:732-735).
	simple = stripWebsiteAndTorrent(simple)

	// Stage 9 — quality brackets (Parser.cs:737).
	simple = stripQualityBrackets(simple)

	// Stage 10 — six-digit airdate (Parser.cs:747).
	simple = normalizeSixDigitAirDate(simple)
	c.simple = simple

	return c
}

// cleanMovie runs the Radarr ParseMovieTitle simplify pipeline (Parser.cs:169).
// It differs from cleanSeries by: no season-folder reject; a Trim('-','_') after
// RemoveFileExtension; an empty PreSubstitution (no-op); and the movie-flavored
// SimpleTitleRegex / ReversedTitleRegex / hashed-reject list.
func cleanMovie(input string) cleaned {
	c := cleaned{original: input}

	// Stage 1 — ValidateBeforeParsing (Parser.cs:584). No season-folder reject.
	if validateRejectMovie(input) {
		c.rejected = true
		return c
	}

	// Stage 2 — reversed-title detection (Parser.cs:181).
	title := applyReversedTitle(cleanReversedTitleRegexMovie, input)

	// Stage 3 — RemoveFileExtension (Parser.cs:191).
	release := removeFileExtensionClean(title)

	// Stage 4 — Radarr-specific trailing dash/underscore trim (Parser.cs:194).
	release = strings.Trim(release, "-_")

	// Stage 5 — fullwidth brackets (Parser.cs:196).
	release = normalizeFullwidthBrackets(release)

	// Stage 6 — PreSubstitution is Array.Empty in Radarr (ParserCommon.cs:9): no-op.
	c.release = release

	// Stage 7 — SimpleTitleRegex (Parser.cs:207).
	simple := replaceAll(cleanSimpleTitleRegexMovie, release, "")

	// Stages 8-9 — website prefix/postfix + torrent suffix (Parser.cs:210-213).
	simple = stripWebsiteAndTorrent(simple)

	// Stage 10 — quality brackets (Parser.cs:215). Radarr has no six-digit airdate.
	simple = stripQualityBrackets(simple)
	c.simple = simple

	return c
}

// validateRejectSeries reports whether the Sonarr ValidateBeforeParsing checks
// reject the input (true == reject). Mirrors Parser.cs:1262.
func validateRejectSeries(title string) bool {
	if passwordYenc(title) {
		return true
	}
	if !hasLetterOrDigit(title) {
		return true
	}
	withoutExt := removeFileExtensionClean(title)
	if anyMatch(rejectHashedSeriesRegexes, withoutExt) {
		return true
	}
	if anyMatch(rejectSeasonFolderRegexes, withoutExt) {
		return true
	}
	return false
}

// validateRejectMovie reports whether the Radarr ValidateBeforeParsing checks
// reject the input (true == reject). Mirrors Parser.cs:584 — same as series minus
// the season-folder reject, and using Radarr's 9-pattern hashed list.
func validateRejectMovie(title string) bool {
	if passwordYenc(title) {
		return true
	}
	if !hasLetterOrDigit(title) {
		return true
	}
	withoutExt := removeFileExtensionClean(title)
	return anyMatch(rejectHashedMovieRegexes, withoutExt)
}
