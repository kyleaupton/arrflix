package parsing

import (
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
)

// group.go ports the release-group parsers verbatim from the pinned upstream
// source. Sonarr and Radarr DIVERGE here (different regex bodies, different
// exception lists, and — critically — opposite check order between the exact
// and suffixed exception lists), so both are ported separately:
//
//   - parseSeriesGroup → Sonarr's Parser.cs ParseReleaseGroup (Parser.cs:908)
//     plus its six group regexes (Parser.cs:549-573).
//   - parseMovieGroup  → Radarr's ReleaseGroupParser.ParseReleaseGroup
//     (ReleaseGroupParser.cs:28) plus its regexes/exception lists
//     (ReleaseGroupParser.cs:9-26).
//
// Each parser reproduces its own internal pre-pipeline (trim, RemoveFileExtension,
// PreSubstitution, WebsitePrefix, CleanTorrentSuffix) exactly as upstream's
// ParseReleaseGroup does — upstream is handed the threaded releaseTitle but
// re-applies these stages internally, so we do too (callers pass the release /
// simpleReleaseTitle string and these helpers run the upstream pipeline on it).
//
// getSubGroup / getReleaseHash read the subgroup / hash named groups off the
// winning IDENTITY match (Sonarr GetSubGroup/GetReleaseHash Parser.cs:1292/1304;
// Radarr GetSubGroup/GetReleaseHash Parser.cs:608/620).
//
// Every pattern is transcribed verbatim through mustCompile (regexp2, .NET
// semantics + ReDoS timeout). RegexOptions.IgnoreCase maps to regexp2.IgnoreCase
// and is passed where upstream sets it; RegexOptions.Compiled is dropped.

// ---------------------------------------------------------------------------
// Sonarr group regexes (Parser.cs:549-573).
// ---------------------------------------------------------------------------

// Sonarr Parser.cs:549 CleanReleaseGroupRegex (RegexReplace → string.Empty).
var seriesCleanReleaseGroupRegex = mustCompile(
	`^(.*?[-._ ](S\d+E\d+)[-._ ])|(-(RP|1|NZBGeek|Obfuscated|Scrambled|sample|Pre|postbot|xpost|Rakuv[a-z0-9]*|WhiteRev|BUYMORE|AsRequested|AlternativeToRequested|GEROV|Z0iDS3N|Chamele0n|4P|4Planet|AlteZachen|RePACKPOST))+$`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:560 ReleaseGroupRegex.
var seriesReleaseGroupRegex = mustCompile(
	`-(?<releasegroup>[a-z0-9]+(?<part2>-[a-z0-9]+)?(?!.+?(?:480p|576p|720p|1080p|2160p)))(?<!(?:WEB-DL|Blu-Ray|480p|576p|720p|1080p|2160p|DTS-HD|DTS-X|DTS-MA|DTS-ES|-ES|-EN|-CAT|[ ._]\d{4}-\d{2}|-\d{2})(?:\k<part2>)?)(?:\b|[-._ ]|$)|[-._ ]\[(?<releasegroup>[a-z0-9]+)\]$`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:563 InvalidReleaseGroupRegex.
var seriesInvalidReleaseGroupRegex = mustCompile(`^([se]\d+|[0-9a-f]{8})$`, regexp2.IgnoreCase)

// Sonarr Parser.cs:565 AnimeReleaseGroupRegex.
var seriesAnimeReleaseGroupRegex = mustCompile(
	`^(?:\[(?<subgroup>(?!\s).+?(?<!\s))\](?:_|-|\s|\.)?)`, regexp2.IgnoreCase)

// Sonarr Parser.cs:570 ExceptionReleaseGroupRegexExact (manual list — name only).
var seriesExceptionReleaseGroupRegexExact = mustCompile(
	`(?<releasegroup>(?:D\-Z0N3|Fight-BB|VARYG|E\.N\.D|KRaLiMaRKo|BluDragon|DarQ|KCRT|BEN[_. ]THE[_. ]MEN)\b)`,
	regexp2.IgnoreCase)

// Sonarr Parser.cs:573 ExceptionReleaseGroupRegex (groups ending with RlsGroup) or RlsGroup]).
var seriesExceptionReleaseGroupRegex = mustCompile(
	`(?<=[._ \[])(?<releasegroup>(Silence|afm72|Panda|Ghost|MONOLITH|Tigole|Joy|ImE|UTR|t3nzin|Anime Time|Project Angel|Hakata Ramen|HONE|Vyndros|SEV|Garshasp|Kappa|Natty|RCVR|SAMPA|YOGI|r00t|EDGE2020|RZeroX)(?=\]|\)))`,
	regexp2.IgnoreCase)

// ---------------------------------------------------------------------------
// Radarr group regexes (ReleaseGroupParser.cs:9-26).
// ---------------------------------------------------------------------------

// Radarr ReleaseGroupParser.cs:9 ReleaseGroupRegex (extra negative-lookbehinds:
// WEB-(DL|Rip), -ENG/-JAP/-GER/-FRA/-FRE/-ITA, -HDRip, bit-depth, tmdbid-, tt#######).
var movieReleaseGroupRegex = mustCompile(
	`-(?<releasegroup>[a-z0-9]+(?<part2>-[a-z0-9]+)?(?!.+?(?:480p|576p|720p|1080p|2160p)))(?<!(?:WEB-(DL|Rip)|Blu-Ray|480p|576p|720p|1080p|2160p|DTS-HD|DTS-X|DTS-MA|DTS-ES|-ES|-EN|-CAT|-ENG|-JAP|-GER|-FRA|-FRE|-ITA|-HDRip|\d{1,2}-bit|[ ._]\d{4}-\d{2}|-\d{2}|tmdb(id)?-(?<tmdbid>\d+)|(?<imdbid>tt\d{7,8}))(?:\k<part2>)?)(?:\b|[-._ ]|$)|[-._ ]\[(?<releasegroup>[a-z0-9]+)\]$`,
	regexp2.IgnoreCase)

// Radarr ReleaseGroupParser.cs:12 InvalidReleaseGroupRegex (same text as Sonarr).
var movieInvalidReleaseGroupRegex = mustCompile(`^([se]\d+|[0-9a-f]{8})$`, regexp2.IgnoreCase)

// Radarr ReleaseGroupParser.cs:14 AnimeReleaseGroupRegex (same text as Sonarr).
var movieAnimeReleaseGroupRegex = mustCompile(
	`^(?:\[(?<subgroup>(?!\s).+?(?<!\s))\](?:_|-|\s|\.)?)`, regexp2.IgnoreCase)

// Radarr ReleaseGroupParser.cs:19 ExceptionReleaseGroupRegexExact (longer manual
// list than Sonarr — YIFY/YTS/QxR/Koten_Gars/etc).
var movieExceptionReleaseGroupRegexExact = mustCompile(
	`\b(?<releasegroup>KRaLiMaRKo|E\.N\.D|D\-Z0N3|Koten_Gars|BluDragon|ZØNEHD|HQMUX|VARYG|YIFY|YTS(.(MX|LT|AG))?|TMd|Eml HDTeam|LMain|DarQ|BEN THE MEN|TAoE|QxR|126811)\b`,
	regexp2.IgnoreCase)

// Radarr ReleaseGroupParser.cs:22 ExceptionReleaseGroupRegex (longer suffixed list
// than Sonarr — adds GiLG/FreetheFish/Kametsu/CtrlHD/etc).
var movieExceptionReleaseGroupRegex = mustCompile(
	`(?<=[._ \[])(?<releasegroup>(Silence|afm72|Panda|Ghost|MONOLITH|Tigole|Joy|ImE|UTR|t3nzin|Anime Time|Project Angel|Hakata Ramen|HONE|GiLG|Vyndros|SEV|Garshasp|Kappa|Natty|RCVR|SAMPA|YOGI|r00t|EDGE2020|RZeroX|FreetheFish|Anna|Bandi|Qman|theincognito|HDO|DusIctv|DHD|CtrlHD|-ZR-|ADC|XZVN|RH|Kametsu)(?=\]|\)))`,
	regexp2.IgnoreCase)

// Radarr ReleaseGroupParser.cs:24 CleanReleaseGroupRegex (RegexReplace → string.Empty;
// note: NO leading "S\d+E\d+" alternative — that is Sonarr-only — and adds
// "Obfuscation").
var movieCleanReleaseGroupRegex = mustCompile(
	`(-(RP|1|NZBGeek|Obfuscated|Obfuscation|Scrambled|sample|Pre|postbot|xpost|Rakuv[a-z0-9]*|WhiteRev|BUYMORE|AsRequested|AlternativeToRequested|GEROV|Z0iDS3N|Chamele0n|4P|4Planet|AlteZachen|RePACKPOST))+$`,
	regexp2.IgnoreCase)

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// lastMatchGroup returns the "releasegroup" named-group value of the LAST match
// of re over s, or "" when there is no match. Mirrors upstream's
// re.Matches(title).OfType<Match>().Last().Groups["releasegroup"].Value.
func lastMatchGroup(re *regexp2.Regexp, s string) (string, bool) {
	m, err := re.FindStringMatch(s)
	if err != nil || m == nil {
		return "", false
	}
	last := m
	for {
		next, nerr := re.FindNextMatch(last)
		if nerr != nil || next == nil {
			break
		}
		last = next
	}
	g := last.GroupByName("releasegroup")
	if g == nil {
		return "", false
	}
	return g.String(), true
}

// ---------------------------------------------------------------------------
// parseSeriesGroup — Sonarr ParseReleaseGroup (Parser.cs:908).
// ---------------------------------------------------------------------------

// parseSeriesGroup ports Sonarr's ParseReleaseGroup (Parser.cs:908). It returns
// the empty string for "no group" (upstream returns null). The input is the
// Sonarr releaseTitle (cleanSeries(input).release); ParseReleaseGroup re-applies
// its own pre-pipeline (trim/ext/presubst/website-prefix/torrent-suffix), faithfully
// reproduced here.
func parseSeriesGroup(releaseTitle string) string {
	title := strings.TrimSpace(releaseTitle)
	title = removeFileExtensionClean(title)
	// PreSubstitution: upstream breaks on the FIRST rule that replaces (Parser.cs:912).
	for _, rule := range preSubstitutionRules {
		if isMatch(rule.re, title) {
			title = replaceAll(rule.re, title, rule.repl)
			break
		}
	}
	title = replaceAll(cleanWebsitePrefixRegex, title, "")
	title = replaceAll(cleanTorrentSuffixRegex, title, "")

	// Anime [subgroup] takes priority (Parser.cs:923).
	if am, err := seriesAnimeReleaseGroupRegex.FindStringMatch(title); err == nil && am != nil {
		if g := am.GroupByName("subgroup"); g != nil {
			return g.String()
		}
	}

	title = replaceAll(seriesCleanReleaseGroupRegex, title, "")

	// Sonarr order: suffixed exception list BEFORE exact (Parser.cs:932-944).
	if g, ok := lastMatchGroup(seriesExceptionReleaseGroupRegex, title); ok {
		return g
	}
	if g, ok := lastMatchGroup(seriesExceptionReleaseGroupRegexExact, title); ok {
		return g
	}

	if g, ok := lastMatchGroup(seriesReleaseGroupRegex, title); ok {
		if _, err := strconv.Atoi(g); err == nil {
			return ""
		}
		if isMatch(seriesInvalidReleaseGroupRegex, g) {
			return ""
		}
		return g
	}

	return ""
}

// ---------------------------------------------------------------------------
// parseMovieGroup — Radarr ReleaseGroupParser.ParseReleaseGroup
// (ReleaseGroupParser.cs:28).
// ---------------------------------------------------------------------------

// parseMovieGroup ports Radarr's ReleaseGroupParser.ParseReleaseGroup
// (ReleaseGroupParser.cs:28). It returns "" for "no group" (upstream null). The
// input is Radarr's title-blanked simpleReleaseTitle (buildSimpleReleaseTitle);
// ParseReleaseGroup re-applies its own pre-pipeline (trim/ext/presubst[empty]/
// website-prefix/torrent-suffix), faithfully reproduced here. Critically, Radarr
// checks the EXACT exception list BEFORE the suffixed list — the opposite order
// from Sonarr (PORT_NOTES §8 point 3).
func parseMovieGroup(simpleReleaseTitle string) string {
	title := strings.TrimSpace(simpleReleaseTitle)
	title = removeFileExtensionClean(title)
	// Radarr PreSubstitution is Array.Empty (ParserCommon.cs:9): no-op.
	title = replaceAll(cleanWebsitePrefixRegex, title, "")
	title = replaceAll(cleanTorrentSuffixRegex, title, "")

	// Anime [subgroup] takes priority (ReleaseGroupParser.cs:43).
	if am, err := movieAnimeReleaseGroupRegex.FindStringMatch(title); err == nil && am != nil {
		if g := am.GroupByName("subgroup"); g != nil {
			return g.String()
		}
	}

	title = replaceAll(movieCleanReleaseGroupRegex, title, "")

	// Radarr order: EXACT exception list BEFORE suffixed (ReleaseGroupParser.cs:52-64).
	if g, ok := lastMatchGroup(movieExceptionReleaseGroupRegexExact, title); ok {
		return g
	}
	if g, ok := lastMatchGroup(movieExceptionReleaseGroupRegex, title); ok {
		return g
	}

	if g, ok := lastMatchGroup(movieReleaseGroupRegex, title); ok {
		if _, err := strconv.Atoi(g); err == nil {
			return ""
		}
		if isMatch(movieInvalidReleaseGroupRegex, g) {
			return ""
		}
		return g
	}

	return ""
}

// ---------------------------------------------------------------------------
// getSubGroup / getReleaseHash — read off the winning IDENTITY match.
// ---------------------------------------------------------------------------

// getSubGroup ports GetSubGroup (Sonarr Parser.cs:1292 / Radarr Parser.cs:608):
// the "subgroup" named group of the winning identity match, or "" when absent.
func getSubGroup(m *regexp2.Match) string {
	g := m.GroupByName("subgroup")
	if g == nil || len(g.Captures) == 0 {
		return ""
	}
	return g.String()
}

// getReleaseHash ports GetReleaseHash (Sonarr Parser.cs:1304 / Radarr
// Parser.cs:620): the "hash" named group of the winning identity match, trimmed
// of surrounding brackets/parens. The special "1280x720" value is dropped (it's a
// resolution, not a hash). Sonarr trims "[](" and ")"; Radarr trims only "[]".
// Trimming both bracket sets is a superset that matches upstream on real hashes
// (which are always wrapped in one matched pair).
func getReleaseHash(m *regexp2.Match) string {
	g := m.GroupByName("hash")
	if g == nil || len(g.Captures) == 0 {
		return ""
	}
	hashValue := strings.Trim(g.String(), "[]()")
	if hashValue == "1280x720" {
		return ""
	}
	return hashValue
}
