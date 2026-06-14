package parsing

import (
	"strings"
	"unicode"
)

// match.go provides the canonical title-matching key and comparison used to
// decide whether a parsed release identity refers to the same work as a wanted
// media item. It mirrors the intent of Radarr's CleanMovieTitle (Parser.cs:430)
// and the arabic/roman numeral equivalence in
// ParsingService.TryGetMovieBySearchCriteria (ParsingService.cs:228), reduced to
// what matching needs: normalize to a comparable key, then compare for equality
// (allowing numeral variants).

// matchArticles are the words CleanMovieTitle drops so titles compare equal
// regardless of articles/conjunctions. Ported from Radarr's NormalizeRegex
// article set. Unlike Radarr — which preserves an article at the very start —
// we strip articles everywhere, so a release titled "Matrix" still matches a
// wanted "The Matrix"; the start-anchor subtlety only ever caused false misses
// for matching.
var matchArticles = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "of": {},
}

// arabicToRoman maps arabic numerals 1..20 to lowercase roman numerals so sequel
// titles compare equal across numbering styles ("Part 2" == "Part II"). Ported
// from Radarr's RomanNumeralParser prepopulation (DICTIONARY_PREPOPULATION_SIZE
// = 20).
var arabicToRoman = map[string]string{
	"1": "i", "2": "ii", "3": "iii", "4": "iv", "5": "v",
	"6": "vi", "7": "vii", "8": "viii", "9": "ix", "10": "x",
	"11": "xi", "12": "xii", "13": "xiii", "14": "xiv", "15": "xv",
	"16": "xvi", "17": "xvii", "18": "xviii", "19": "xix", "20": "xx",
}

// NormalizeTitleForMatch reduces a title to a comparable key: lowercased,
// article/conjunction words dropped, and every non-alphanumeric rune stripped.
// Pure and total — empty in yields empty out. An all-article title falls back to
// the un-stripped join so it still produces a non-empty key. Accents are not
// folded (matching the import matcher's behavior); that is a possible future
// refinement.
func NormalizeTitleForMatch(s string) string {
	// Tokenize on non-alphanumeric boundaries so article words can be dropped,
	// then concatenate the survivors into a separator-free key.
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	var key strings.Builder
	for _, tok := range tokens {
		if _, isArticle := matchArticles[tok]; isArticle {
			continue
		}
		key.WriteString(tok)
	}
	if key.Len() == 0 {
		// Input was empty or all-articles; preserve the raw join.
		for _, tok := range tokens {
			key.WriteString(tok)
		}
	}
	return key.String()
}

// TitlesMatch reports whether two titles refer to the same work: their match
// keys are equal, or become equal after substituting arabic numerals with roman
// (e.g. "Highlander 2" vs "Highlander II"). Empty keys never match. The numeral
// substitution mirrors Radarr's naive per-mapping Replace.
func TitlesMatch(a, b string) bool {
	ka, kb := NormalizeTitleForMatch(a), NormalizeTitleForMatch(b)
	if ka == "" || kb == "" {
		return false
	}
	if ka == kb {
		return true
	}
	for arabic, roman := range arabicToRoman {
		if strings.ReplaceAll(ka, arabic, roman) == kb || ka == strings.ReplaceAll(kb, arabic, roman) {
			return true
		}
	}
	return false
}
