package parsing

import (
	"regexp"
	"strings"
)

// Language detection ported from Sonarr's LanguageParser (RE2-adapted). The
// .NET lookbehind/lookahead in the originals — the Spanish "(?!\(Latino\))",
// the DL "(?<!WEB[-_. ]?)", and the case-sensitive codes' SUB exclusion — are
// replaced by Go-side checks. Detected language names match the oracle's
// (German, Italian, English, French, Original, …); no detection means Unknown,
// which we represent as an empty slice (the harness treats Unknown as empty).
var (
	langEnglish   = regexp.MustCompile(`(?i)(?:\W|_)(?:ing|eng)\b`)
	langItalian   = regexp.MustCompile(`(?i)\b(?:ita|italian)\b`)
	langGerman    = regexp.MustCompile(`(?i)german\b|videomann|ger[. ]dub`)
	langFrench    = regexp.MustCompile(`(?i)(?:\W|_)(?:FR|VF|VF2|VFF|VFI|VFQ|TRUEFRENCH|FRENCH)(?:\W|_)`)
	langRussian   = regexp.MustCompile(`(?i)\b(?:rus|ru)\b`)
	langSpanish   = regexp.MustCompile(`(?i)\b(?:español|castellano|esp|spa)\b`)
	langChinese   = regexp.MustCompile(`\[(?:CH[ST]|BIG5|GB)\]|简|繁|字幕`)
	langPolishDub = regexp.MustCompile(`(?i)\b(?:PL\W?DUB|DUB\W?PL|LEK\W?PL|PL\W?LEK)\b`)

	dlToken   = regexp.MustCompile(`(?i)\bDL\b`)
	mlToken   = regexp.MustCompile(`(?i)\bML\b`)
	webDL     = regexp.MustCompile(`(?i)WEB[-_. ]?DL`)
	subMarker = regexp.MustCompile(`(?i)sub`)
)

// caseSensitiveLangs are 2-letter codes that double as subtitle markers, so
// they only count as an audio language when not adjacent to "SUB".
var caseSensitiveLangs = []struct{ code, name string }{
	{"LT", "Lithuanian"}, {"CZ", "Czech"}, {"PL", "Polish"}, {"BG", "Bulgarian"}, {"SK", "Slovak"},
}

// containsLangs are detected by a simple case-insensitive substring.
var containsLangs = []struct{ sub, name string }{
	{"danish", "Danish"}, {"dutch", "Dutch"}, {"japanese", "Japanese"},
	{"portuguese", "Portuguese"}, {"swedish", "Swedish"}, {"norwegian", "Norwegian"},
}

// parseLanguages returns the advertised language(s), in first-seen order. An
// empty result means none detected (Unknown).
func parseLanguages(title string) []string {
	seen := map[string]bool{}
	var langs []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			langs = append(langs, name)
		}
	}

	if langEnglish.MatchString(title) {
		add("English")
	}
	if langItalian.MatchString(title) {
		add("Italian")
	}
	if langGerman.MatchString(title) {
		add("German")
	}
	if langFrench.MatchString(title) {
		add("French")
	}
	if langRussian.MatchString(title) {
		add("Russian")
	}
	if langSpanish.MatchString(title) {
		add("Spanish")
	}
	if langChinese.MatchString(title) {
		add("Chinese")
	}
	if langPolishDub.MatchString(title) {
		add("Polish")
	}
	for _, c := range caseSensitiveLangs {
		if matchCodeLang(title, c.code) {
			add(c.name)
		}
	}
	lower := strings.ToLower(title)
	for _, c := range containsLangs {
		if strings.Contains(lower, c.sub) {
			add(c.name)
		}
	}

	// German dual/multi-language: when German is the only detected language, a
	// standalone DL (not WEB-DL) adds the Original track; ML adds Original+English.
	if len(langs) == 1 && langs[0] == "German" {
		switch {
		case dlToken.MatchString(webDL.ReplaceAllString(title, " ")):
			add("Original")
		case mlToken.MatchString(title):
			add("Original")
			add("English")
		}
	}

	return langs
}

// matchCodeLang reports whether a 2-letter language code appears as a standalone
// token that is not adjacent to a "SUB" subtitle marker.
func matchCodeLang(title, code string) bool {
	re := regexp.MustCompile(`(?i)(^|[\W_])` + code + `([\W_]|$)`)
	for _, loc := range re.FindAllStringIndex(title, -1) {
		pre := title[:loc[0]]
		post := title[loc[1]:]
		// reject "<code>" used as a subtitle tag (e.g. "CZ.SUB" / "SUB.CZ").
		if endsWithSub(pre) || startsWithSub(post) {
			continue
		}
		return true
	}
	return false
}

func endsWithSub(s string) bool {
	if len(s) < 3 {
		return false
	}
	return subMarker.MatchString(s[len(s)-3:])
}

func startsWithSub(s string) bool {
	s = strings.TrimLeft(s, "._ -[](){}")
	return len(s) >= 3 && subMarker.MatchString(s[:3])
}
