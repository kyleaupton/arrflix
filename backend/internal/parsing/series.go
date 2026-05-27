package parsing

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dlclark/regexp2"
	"golang.org/x/text/unicode/norm"
)

// series.go ports Sonarr's series identity path verbatim from the pinned
// upstream source (submodules/sonarr/src/NzbDrone.Core/Parser/Parser.cs). It
// threads the P2 simplify pipeline (cleanSeries in clean.go) and then runs the
// faithful ReportTitleRegex table (Parser.cs:59) plus the full
// ParseMatchCollection interpreter (Parser.cs:1020). Series carry the full
// episode/season/airdate/absolute numbering surface — far richer than the movie
// path's title+year+edition.
//
// Every pattern is transcribed verbatim through mustCompile (regexp2, .NET
// semantics + ReDoS timeout). RegexOptions.IgnoreCase maps to regexp2.IgnoreCase
// and is passed where upstream sets it; RegexOptions.Compiled is dropped (no
// analogue). 93 of the 98 active patterns lean on lookaround / named
// backreferences (\k<sep>) — they are pasted verbatim and NOT rewritten into
// RE2 form. The single commented-out pattern at Parser.cs:63 is skipped.

// seriesPatterns ports Sonarr's ReportTitleRegex (Parser.cs:59) VERBATIM, in
// declaration order (first-match-wins). All 98 active patterns are included.
var seriesPatterns = []*regexp2.Regexp{
	// Parser.cs:67 — Daily episode with year in series title and air time after date (Plex DVR format)
	mustCompile(`^^(?<title>.+?\((?<titleyear>\d{4})\))[-_. ]+(?<airyear>19[4-9]\d|20\d\d)(?<sep>[-_]?)(?<airmonth>0\d|1[0-2])\k<sep>(?<airday>[0-2]\d|3[01])[-_. ]\d{2}[-_. ]\d{2}[-_. ]\d{2}`, regexp2.IgnoreCase),

	// Parser.cs:71 — Daily episodes without title (2018-10-12, 20181012) (Strict pattern to avoid false matches)
	mustCompile(`^(?<airyear>19[6-9]\d|20\d\d)(?<sep>[-_]?)(?<airmonth>0\d|1[0-2])\k<sep>(?<airday>[0-2]\d|3[01])(?!\d)`, regexp2.IgnoreCase),

	// Parser.cs:75 — Multi-Part episodes without a title (S01E05.S01E06)
	mustCompile(`^(?:\W*S(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:e{1,2}(?<episode>\d{1,3}(?!\d+)))+){2,}`, regexp2.IgnoreCase),

	// Parser.cs:79 — Multi-Part episodes without a title (1x05.1x06)
	mustCompile(`^(?:\W*(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:x{1,2}(?<episode>\d{1,3}(?!\d+)))+){2,}`, regexp2.IgnoreCase),

	// Parser.cs:83 — Episodes without a title, Multi (S01E04E05, 1x04x05, etc)
	mustCompile(`^(?:S?(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[-_]|[ex]){1,2}(?<episode>\d{2,3}(?!\d+))){2,})`, regexp2.IgnoreCase),

	// Parser.cs:87 — Split episodes (S01E05a, S01E05b, etc)
	mustCompile(`^(?<title>.+?)(?:S?(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[-_ ]?[ex])(?<episode>\d{2,3}(?!\d+))(?<splitepisode>[a-d])(?:[ _.])))`, regexp2.IgnoreCase),

	// Parser.cs:91 — Episodes without a title, Single (S01E05, 1x05)
	mustCompile(`^(?:S?(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[-_ ]?[ex])(?<episode>\d{2,3}(?!\d+))))`, regexp2.IgnoreCase),

	// Parser.cs:95 — Anime - [SubGroup] Title Absolute (Season+Episode)
	mustCompile(`^(?:\[(?<subgroup>.+?)\](?:_|-|\s|\.)?)(?<title>.+?)[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+))(?:[-_. ])+\((?:S(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:[ex]|\W[ex]){1,2}(?<episode>\d{2}(?!\d+))))(?:v\d+)?(?:\)(?!\d+)).*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:99 — Anime - [SubGroup] Title Absolute - Season+Episode
	mustCompile(`^(?:\[(?<subgroup>.+?)\](?:_|-|\s|\.)?)(?<title>.+?)[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+))(?:[-_. ](?<![()\[!]))+(?:S(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:[ex]|\W[ex]){1,2}(?<episode>\d{2}(?!\d+))))(?:v\d+)?(?:[_. ](?!\d+)).*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:103 — Anime - [SubGroup] Title Season+Episode
	mustCompile(`^(?:\[(?<subgroup>.+?)\](?:_|-|\s|\.)?)(?<title>.+?)(?:[-_\W](?<![()\[!]))+(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:[ex]|\W[ex]){1,2}(?<episode>\d{2}(?!\d+)))+)(?:v\d+)?(?:[_. ](?!\d+)).*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:107 — Anime - [SubGroup] Title Episode Absolute Episode Number ([SubGroup] Series Title Episode 01)
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>.+?)[-_. ]+?(?:Episode)(?:[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+)))+.*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:111 — Anime - [SubGroup] Title Absolute Episode Number + Season+Episode
	mustCompile(`^(?:\[(?<subgroup>.+?)\](?:_|-|\s|\.)?)(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+(?<absoluteepisode>\d{2,3}(\.\d{1,2})?))+(?:_|-|\s|\.)+(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:\-|[ex]|\W[ex]){1,2}(?<episode>\d{2}(?!\d+)))+).*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.)`, regexp2.IgnoreCase),

	// Parser.cs:115 — Anime - [SubGroup] Title Season+Episode + Absolute Episode Number
	mustCompile(`^(?:\[(?<subgroup>.+?)\](?:_|-|\s|\.)?)(?<title>.+?)(?:[-_\W](?<![()\[!]))+(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:\-|[ex]|\W[ex]){1,2}(?<episode>\d{2}(?!\d+)))+)(?:(?:_|-|\s|\.)+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+|\-[a-z])))+.*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:119 — Anime - [SubGroup] Title with trailing number Absolute Episode Number - Batch separated with tilde
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>.+?[^-]+?)(?:(?<![-_. ]|\b[0]\d+) - )[-_. ]?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+))\s?~\s?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+))(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:123 — Anime - [SubGroup] Title with season number in brackets Absolute Episode Number
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>[^-]+?)[_. ]+?\(Season[_. ](?<season>\d+)\)[-_. ]+?(?:[-_. ]?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:127 — Anime - [SubGroup] Title with trailing 3-digit number and sub title - Absolute Episode Number
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>[^]]+?)(?:[-_. ]{3}?(?<absoluteepisode>\d{2}(\.\d{1,2})?(?!-?\d+|-[a-z]+)))+(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:131 — Anime - [SubGroup] Title with trailing number Absolute Episode Number
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>[^-]+?)(?:(?<![-_. ]|\b[0]\d+) - )(?:[-_. ]?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:135 — Anime - [SubGroup] Title with trailing number S## (Full season)
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>.+?)[-_. ]+(?:S(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?![ex]?\d+))).+?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:139 — Anime - [SubGroup] Title with trailing number Absolute Episode Number
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>[^-]+?)(?:(?<![-_. ]|\b[0]\d+)[_ ]+)(?:[-_. ]?(?<absoluteepisode>\d{3}(\.\d{1,2})?(?!\d+|-[a-z]+)))+(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:143 — Anime - [SubGroup] Title - Absolute Episode Number
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>.+?)(?:(?<!\b[0]\d+))(?:[. ]-[. ](?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+|[-])))+(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:147 — Anime - [SubGroup] Title Absolute Episode Number - Absolute Episode Number (batches without full separator between title and absolute episode numbers)
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>.+?)(?:(?<!\b[0]\d+))(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+|[-]))[. ]-[. ](?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+|[-]))(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:151 — Anime - [SubGroup] Title Absolute Episode Number
	mustCompile(`^\[(?<subgroup>.+?)\][-_. ]?(?<title>.+?)[-_. ]+\(?(?:[-_. ]?#?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+|-[a-z]+)))+\)?(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])?(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:155 — Multi-episode Repeated (S01E05 - S01E06)
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+S(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:e|[-_. ]e){1,2}(?<episode>\d{1,3}(?!\d+)))+){2,}`, regexp2.IgnoreCase),

	// Parser.cs:159 — Multi-episode Repeated (1x05 - 1x06)
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:x{1,2}(?<episode>\d{1,3}(?!\d+)))+){2,}`, regexp2.IgnoreCase),

	// Parser.cs:163 — Single episodes with a title (S01E05, 1x05, etc) followed by ".5 [SP]"
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+S?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:[ex]|\W[ex]|_){1,2}(?<episode>\d{2,3}(?!\d+|(?:[ex]|\W[ex]|_|-){1,2}\d+))(?<special>\.5[ .]\[SP\]))`, regexp2.IgnoreCase),

	// Parser.cs:167 — Single episodes with a title (S01E05, 1x05, etc) and trailing info in slashes
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+S?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:[ex]|\W[ex]|_){1,2}(?<episode>\d{2,3}(?!\d+|(?:[ex]|\W[ex]|_|-){1,2}\d+))).+?(?:\[.+?\])(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:171 — Anime - Title Season EpisodeNumber + Absolute Episode Number [SubGroup]
	mustCompile(`^(?<title>.+?)(?:[-_\W](?<![()\[!]))+(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:[ex]|\W[ex]|-){1,2}(?<episode>(?<!\d+)\d{2}(?!\d+)))+)[-_. (]+?(?:[-_. ]?(?<absoluteepisode>(?<!\d+)\d{3}(\.\d{1,2})?(?!\d+|[pi])))+.+?\[(?<subgroup>.+?)\](?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:175 — Multi-Episode with a title (S01E05E06, S01E05-06, S01E05 E06, etc) and trailing info in slashes
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+S?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:[ex]|\W[ex]|_){1,2}(?<episode>\d{2,3}(?!\d+))(?:(?:\-|[ex]|\W[ex]|_){1,2}(?<episode>\d{2,3}(?!\d+)))+).+?(?:\[.+?\])(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:179 — Anime - Title Absolute Episode Number [SubGroup] [Hash]? (Series Title Episode 99-100 [RlsGroup] [ABCD1234])
	mustCompile(`^(?<title>.+?)[-_. ]Episode(?:[-_. ]+(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:.+?)\[(?<subgroup>.+?)\].*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:183 — Anime - Title Absolute Episode Number [SubGroup] [Hash]
	mustCompile(`^(?<title>.+?)(?:(?:_|-|\s|\.)+(?<absoluteepisode>\d{3}(\.\d{1,2})(?!\d+)))+(?:.+?)\[(?<subgroup>.+?)\].*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:187 — Anime - Title Absolute Episode Number (Year) [SubGroup]
	mustCompile(`^(?<title>.+?)[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2}(?!\d+))[-_. ](\(\d{4}\))[-_. ]\[(?<subgroup>.+?)\]`, regexp2.IgnoreCase),

	// Parser.cs:191 — Anime - Title with trailing number, Absolute Episode Number and hash
	mustCompile(`^(?<title>[^-]+?)(?:(?<![-_. ]|\b[0]\d+) - )(?:[-_. ]?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>[(\[]\w{8}[)\]])(?:$|\.mkv)`, regexp2.IgnoreCase),

	// Parser.cs:195 — Anime - Title Absolute Episode Number [Hash]
	mustCompile(`^(?<title>.+?)(?:(?:_|-|\s|\.)+(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:[-_. ]+(?<special>special|ova|ovd))?[-_. ]+.*?(?<hash>[(\[]\w{8}[)\]])$`, regexp2.IgnoreCase),

	// Parser.cs:199 — Episodes with airdate AND season/episode number, capture season/episode only
	mustCompile(`^(?<title>.+?)?\W*(?<airdate>\d{4}\W+[0-1][0-9]\W+[0-3][0-9])(?!\W+[0-3][0-9])[-_. ](?:s?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+)))(?:[ex](?<episode>(?<!\d+)(?:\d{1,3})(?!\d+)))`, regexp2.IgnoreCase),

	// Parser.cs:203 — Episodes with airdate AND season/episode number
	mustCompile(`^(?<title>.+?)?\W*(?<airyear>\d{4})\W+(?<airmonth>[0-1][0-9])\W+(?<airday>[0-3][0-9])(?!\W+[0-3][0-9]).+?(?:s?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+)))(?:[ex](?<episode>(?<!\d+)(?:\d{1,3})(?!\d+)))`, regexp2.IgnoreCase),

	// Parser.cs:207 — Episodes with absolute episode number AND airdate (TJET Wrestling)
	mustCompile(`^(?<title>.+?)?[-_. ](?:e\d{2,3}(?!\d+))[-_. ](?<airyear>\d{4})\W+(?<airmonth>[0-1][0-9])\W+(?<airday>[0-3][0-9])(?!\W+[0-3][0-9]).+?(?:\[[a-z]+\])`, regexp2.IgnoreCase),

	// Parser.cs:211 — Single or multi episode releases with multiple titles, then season and episode numbers after the last title. (Title1 / Title2 / ... / S1E1-2 of 6)
	mustCompile(`^((?<title>.*?)[ ._]\/[ ._])+\(?S(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:\W|_)?E?[ ._]?(?<episode>(?<!\d+)\d{1,2}(?!\d+))(?:-(?<episode>(?<!\d+)\d{1,2}(?!\d+)))?(?:[ ._]of[ ._](?<episodecount>\d{1,2}))?\)?[ ._][\(\[]`, regexp2.IgnoreCase),

	// Parser.cs:215 — Multi-episode with title (S01E99-100, S01E05-06)
	mustCompile(`^(?<title>.+?)(?:[-_\W](?<![()\[!]))+S(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))E(?<episode>\d{2,3}(?!\d+))(?:-(?<episode>\d{2,3}(?!\d+)))+(?:[-_. ]|$)`, regexp2.IgnoreCase),

	// Parser.cs:219 — Multi-episode with title (S01E05-06, S01E05-6)
	mustCompile(`^(?<title>.+?)(?:[-_\W](?<![()\[!]))+S(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))E(?<episode>\d{1,2}(?!\d+))(?:-(?<episode>\d{1,2}(?!\d+)))+(?:[-_. ]|$)`, regexp2.IgnoreCase),

	// Parser.cs:223 — Episodes with a title, Single episodes (S01E05, 1x05, etc) & Multi-episode (S01E05E06, S01E05-06, S01E05 E06, etc)
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+S?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:[ex]|\W[ex]){1,2}(?<episode>\d{2,3}(?!\d+))(?:(?:\-|[ex]|\W[ex]|_){1,2}(?<episode>\d{2,3}(?!\d+)))*)(?:[-_. ]|$)`, regexp2.IgnoreCase),

	// Parser.cs:227 — Episodes with a title, 4 digit season number, Single episodes (S2016E05, etc) & Multi-episode (S2016E05E06, S2016E05-06, S2016E05 E06, etc)
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+S(?<season>(?<!\d+)(?:\d{4})(?!\d+))(?:e|\We|_){1,2}(?<episode>\d{2,4}(?!\d+))(?:(?:\-|e|\We|_){1,2}(?<episode>\d{2,3}(?!\d+)))*)\W?(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:231 — Episodes with a title, 4 digit season number, Single episodes (2016x05, etc) & Multi-episode (2016x05x06, 2016x05-06, 2016x05 x06, etc)
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+(?<season>(?<!\d+)(?:\d{4})(?!\d+))(?:x|\Wx){1,2}(?<episode>\d{2,4}(?!\d+))(?:(?:\-|x|\Wx|_){1,2}(?<episode>\d{2,3}(?!\d+)))*)\W?(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:235 — Episodes with a title, Single episodes (s01.05)
	mustCompile(`^(?<title>.+?)(?:[-_\W](?<![()\[!]))+S(?<season>(?<!\d+)(?:\d{2})(?!\d+))(?:\.)(?<episode>\d{2,3}(?!\d+))(?:[-_. ]|$)`, regexp2.IgnoreCase),

	// Parser.cs:239 — Multi-season pack
	mustCompile(`^(?<title>.+?)(Complete Series)?[-_. ]+(?:S|(?:Season|Saison|Series|Stagione)[_. ])(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:[-_. ]{1}|[-_. ]{3})(?:S|(?:Season|Saison|Series|Stagione)[_. ])?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))`, regexp2.IgnoreCase),

	// Parser.cs:243 — Partial season pack
	mustCompile(`^(?<title>.+?)(?:\W+S(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))\W+(?:(?:(?:Part|Vol)\W?|(?<!\d+\W+)e|p)(?<seasonpart>\d{1,2}(?!\d+)))+)`, regexp2.IgnoreCase),

	// Parser.cs:247 — Anime - 4 digit absolute episode number
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>.+?)[-_. ]+?(?<absoluteepisode>\d{4}(\.\d{1,2})?(?!\d+))`, regexp2.IgnoreCase),

	// Parser.cs:251 — Anime - Title 4-digit Absolute Episode Number [SubGroup]
	mustCompile(`^(?<title>.+?)[-_. ]+(?<absoluteepisode>(?<!\d+)\d{4}(?!\d+))[-_. ]\[(?<subgroup>.+?)\]`, regexp2.IgnoreCase),

	// Parser.cs:255 — Mini-Series with year in title, treated as season 1, episodes are labelled as Part01, Part 01, Part.1
	mustCompile(`^(?<title>.+?\d{4})(?:\W+(?:(?:Part\W?|e)(?<episode>\d{1,2}(?!\d+)))+)`, regexp2.IgnoreCase),

	// Parser.cs:259 — Mini-Series, treated as season 1, multi episodes are labelled as E1-E2
	mustCompile(`^(?<title>.+?)(?:[-._ ][e])(?<episode>\d{2,3}(?!\d+))(?:(?:\-?[e])(?<episode>\d{2,3}(?!\d+)))+`, regexp2.IgnoreCase),

	// Parser.cs:263 — Episodes with airdate and part (2018.04.28.Part.2)
	mustCompile(`^(?<title>.+?)?\W*(?<airyear>\d{4})[-_. ]+(?<airmonth>[0-1][0-9])[-_. ]+(?<airday>[0-3][0-9])(?![-_. ]+[0-3][0-9])[-_. ]+Part[-_. ]?(?<part>[1-9])`, regexp2.IgnoreCase),

	// Parser.cs:267 — Mini-Series, treated as season 1, episodes are labelled as Part01, Part 01, Part.1
	mustCompile(`^(?<title>.+?)(?:\W+(?:(?:(?<!\()Part\W?|(?<!\d+\W+)e)(?<episode>\d{1,2}(?!\d+|\))))+)`, regexp2.IgnoreCase),

	// Parser.cs:271 — Mini-Series, treated as season 1, episodes are labelled as Part One/Two/Three/...Nine, Part.One, Part_One
	mustCompile(`^(?<title>.+?)(?:\W+(?:Part[-._ ](?<episode>One|Two|Three|Four|Five|Six|Seven|Eight|Nine)(?>[-._ ])))`, regexp2.IgnoreCase),

	// Parser.cs:275 — Mini-Series, treated as season 1, episodes are labelled as XofY
	mustCompile(`^(?<title>.+?)(?:\W+(?:(?<episode>(?<!\d+)\d{1,2}(?!\d+))of\d+)+)`, regexp2.IgnoreCase),

	// Parser.cs:279 — Supports Season 01 Episode 03
	mustCompile(`(?:.*(?:\"|^))(?<title>.*?)(?:[-_\W](?<![()\[]))+(?:\W?Season\W?)(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:\W|_)+(?:Episode\W)(?:[-_. ]?(?<episode>(?<!\d+)\d{1,2}(?!\d+)))+`, regexp2.IgnoreCase),

	// Parser.cs:283 — Multi-episode with episodes in square brackets (Series Title [S01E11E12] or Series Title [S01E11-12])
	mustCompile(`(?:.*(?:^))(?<title>.*?)[-._ ]+\[S(?<season>(?<!\d+)\d{2}(?!\d+))(?:[E-]{1,2}(?<episode>(?<!\d+)\d{2}(?!\d+)))+\]`, regexp2.IgnoreCase),

	// Parser.cs:287 — Multi-episode with episodes in brackets (Series Title (S01E11E12) or Series Title (S01E11-12))
	mustCompile(`(?:.*(?:^))(?<title>.*?)[-._ ]+\(S(?<season>(?<!\d+)\d{2}(?!\d+))(?:[E-]{1,2}(?<episode>(?<!\d+)\d{2}(?!\d+)))+\)`, regexp2.IgnoreCase),

	// Parser.cs:291 — Multi-episode release with no space between series title and season (S01E11E12)
	mustCompile(`(?:.*(?:^))(?<title>.*?)S(?<season>(?<!\d+)\d{2}(?!\d+))(?:E(?<episode>(?<!\d+)\d{2}(?!\d+)))+`, regexp2.IgnoreCase),

	// Parser.cs:295 — Multi-episode with single episode numbers (S6.E1-E2, S6.E1E2, S6E1E2, etc)
	mustCompile(`^(?<title>.+?)[-_. ]S(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:[-_. ]?[ex]?(?<episode>(?<!\d+)\d{1,2}(?!\d+)))+`, regexp2.IgnoreCase),

	// Parser.cs:299 — Single or multi episode releases with multiple titles, each followed by season and episode numbers in brackets
	mustCompile(`^(?<title>.*?)[ ._]\(S(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:\W|_)?E?[ ._]?(?<episode>(?<!\d+)\d{1,2}(?!\d+))(?:-(?<episode>(?<!\d+)\d{1,2}(?!\d+)))?\)(?:[ ._]\/[ ._])(?<title>.*?)[ ._]\(`, regexp2.IgnoreCase),

	// Parser.cs:303 — Single episode season or episode S1E1 or S1-E1 or S1.Ep1 or S01.Ep.01
	mustCompile(`(?:.*(?:\"|^))(?<title>.*?)(?:\W?|_)S(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:\W|_)?Ep?[ ._]?(?<episode>(?<!\d+)\d{1,2}(?!\d+))`, regexp2.IgnoreCase),

	// Parser.cs:307 — 3 digit season S010E05
	mustCompile(`(?:.*(?:\"|^))(?<title>.*?)(?:\W?|_)S(?<season>(?<!\d+)\d{3}(?!\d+))(?:\W|_)?E(?<episode>(?<!\d+)\d{1,2}(?!\d+))`, regexp2.IgnoreCase),

	// Parser.cs:311 — 5 digit episode number with a title
	mustCompile(`^(?:(?<title>.+?)(?:_|-|\s|\.)+)(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+)))(?:(?:\-|[ex]|\W[ex]|_){1,2}(?<episode>(?<!\d+)\d{5}(?!\d+)))`, regexp2.IgnoreCase),

	// Parser.cs:315 — 5 digit multi-episode with a title
	mustCompile(`^(?:(?<title>.+?)(?:_|-|\s|\.)+)(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+)))(?:(?:[-_. ]{1,3}ep){1,2}(?<episode>(?<!\d+)\d{5}(?!\d+)))+`, regexp2.IgnoreCase),

	// Parser.cs:319 — Separated season and episode numbers S01 - E01
	mustCompile(`^(?<title>.+?)(?:_|-|\s|\.)+S(?<season>\d{2}(?!\d+))(\W-\W)E(?<episode>(?<!\d+)\d{2}(?!\d+))(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:325 — Season and episode numbers in square brackets (single and multi-episode); Series Title - [02x01] - Episode 1; Series Title - [02x01x02] - Episode 1
	mustCompile(`^(?<title>.+?)?(?:[-_\W](?<![()\[!]))+\[(?:s)?(?<season>(?<!\d+)\d{1,2})(?:(?:[ex])(?<episode>\d{2}))(?:(?:[-ex]){1,2}(?<episode>\d{2}))*\].+?(?:\.|$)`, regexp2.IgnoreCase),

	// Parser.cs:329 — Anime - Title with season number - Absolute Episode Number (Title S01 - EP14)
	mustCompile(`^(?<title>.+?S\d{1,2})[-_. ]{3,}(?:EP)?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+|[-]))`, regexp2.IgnoreCase),

	// Parser.cs:333 — Anime - French titles with single episode numbers, with or without leading sub group ([RlsGroup] Title - Episode 1)
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)[-_. ]+?(?:Episode[-_. ]+?)(?<absoluteepisode>\d{1}(\.\d{1,2})?(?!\d+))`, regexp2.IgnoreCase),

	// Parser.cs:337 — Anime - Absolute episode number in square brackets
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>.+?)[-_. ]+?\[(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+))\]`, regexp2.IgnoreCase),

	// Parser.cs:341 — Japanese variety shows with leading date
	mustCompile(`^(?<airyear>\d{2})(?<airmonth>[0-1][0-9])(?<airday>[0-3][0-9])(?![-_. ]+[0-3][0-9])[-_. ](?<title>.+?)[-_. ](?:Season[-_. ]?(?<season>\d{1,2})[-_. ])?(?:ep|#)(?<episode>\d{2,3})`, regexp2.IgnoreCase),

	// Parser.cs:345 — Season only releases followed by year
	mustCompile(`^(?<title>.+?)[-_. ]+?(?:S|Season|Saison|Series|Stagione)[-_. ]?(?<season>\d{1,2}(?=[-_. ]\d{4}[-_. ]+))(?<extras>EXTRAS|SUBPACK)?(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:349 — Season only releases
	mustCompile(`^(?<title>.+?)[-_. ]+?(?:S|Season|Saison|Series|Stagione)[-_. ]?(?<season>\d{1,2}(?![-_. ]?\d+))(?:[-_. ]|$)+(?<extras>EXTRAS|SUBPACK)?(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:353 — 4 digit season only releases
	mustCompile(`^(?<title>.+?)[-_. ]+?(?:S|Season|Saison|Series|Stagione)[-_. ]?(?<season>\d{4}(?![-_. ]?\d+))(\W+|_|$)(?<extras>EXTRAS|SUBPACK)?(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:357 — Spanish tracker releases
	mustCompile(`^(?<title>.+?)(?:(?:[-_. ]+?Temporada.+?|\[.+?\])\[Cap)(?:[-_. ]+(?<season>(?<!\d+)\d{1,2})(?<episode>(?<!e|x)(?:[1-9][0-9]|[0][1-9])))+(?:\])`, regexp2.IgnoreCase),

	// Parser.cs:361 — Supports 103/113 naming
	mustCompile(`^(?<title>.+?)?(?:(?:[_.-](?<![()\[!]))+(?<season>(?<!\d+)[1-9])(?<episode>[1-9][0-9]|[0][1-9])(?![a-z]|\d+))+(?:[_.]|$)`, regexp2.IgnoreCase),

	// Parser.cs:366 — 4 digit episode number; Episodes without a title, Single (S01E05, 1x05) AND Multi (S01E04E05, 1x04x05, etc)
	mustCompile(`^(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:\-|[ex]|\W[ex]|_){1,2}(?<episode>\d{4}(?!\d+|i|p)))+)(\W+|_|$)(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:371 — 4 digit episode number; Episodes with a title, Single episodes (S01E05, 1x05, etc) & Multi-episode (S01E05E06, S01E05-06, S01E05 E06, etc)
	mustCompile(`^(?<title>.+?)(?:(?:[-_\W](?<![()\[!]|\d{1,2}-))+S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:\-|[ex]|\W[ex]|_){1,2}(?<episode>\d{4}(?!\d+|i|p)))+)\W?(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:375 — Episodes with airdate (2018.04.28)
	mustCompile(`^(?<title>.+?)?\W*(?<airyear>\d{4})[-_. ]+(?<airmonth>[0-1][0-9])[-_. ]+(?<airday>[0-3][0-9])(?![-_. ]+[0-3][0-9])`, regexp2.IgnoreCase),

	// Parser.cs:379 — Turkish tracker releases (01 BLM, 3. Blm, 04.Bolum, etc)
	mustCompile(`^(?<title>.+?)[_. ](?<absoluteepisode>\d{1,4})(?:[_. ]+)(?:BLM|B[oö]l[uü]m)`, regexp2.IgnoreCase),

	// Parser.cs:382 — Episodes with airdate (04.28.2018)
	mustCompile(`^(?<title>.+?)?\W*(?<ambiguousairmonth>[0-1][0-9])[-_. ]+(?<ambiguousairday>[0-3][0-9])[-_. ]+(?<airyear>\d{4})(?!\d+)`, regexp2.IgnoreCase),

	// Parser.cs:386 — Episodes with airdate (28.04.2018)
	mustCompile(`^(?<title>.+?)?\W*(?<ambiguousairday>[0-3][0-9])[-_. ]+(?<ambiguousairmonth>[0-1][0-9])[-_. ]+(?<airyear>\d{4})(?!\d+)`, regexp2.IgnoreCase),

	// Parser.cs:390 — Episodes with airdate (20180428)
	mustCompile(`^(?<title>.+?)?\W*(?<!\d+)(?<airyear>\d{4})(?<airmonth>[0-1][0-9])(?<airday>[0-3][0-9])(?!\d+)`, regexp2.IgnoreCase),

	// Parser.cs:394 — Anime - Title Absolute Episode Number (E195 or E1206)
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:(?:_|-|\s|\.)+(?:e|ep)(?<absoluteepisode>(\d{3}|\d{4})(\.\d{1,2})?))+[-_. ].*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:398 — Supports 1103/1113 naming
	mustCompile(`^(?<title>.+?)?(?:(?:[-_. ](?<![()\[!]))*(?<!\d{1,2}-)(?<season>(?<!\d+|\(|\[|e|x)\d{2})(?<episode>(?<!e|x)(?:[1-9][0-9]|[0][1-9])(?!p|i|\d+|\)|\]|\W\d+|\W(?:e|ep|x)\d+)))+([-_. ]+|$)(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:402 — Dutch/Flemish release titles
	mustCompile(`^(?<title>.+?)[-_. ](?:Se\.(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[-_ ]?afl\.)(?<episode>\d{1,3}(?!\d+))(?:(?:[-]|[-_ ]en[-_ ])(?<episode>\d{1,3}(?!\d+)))*))`, regexp2.IgnoreCase),

	// Parser.cs:406 — Episodes with single digit episode number (S01E1, S01E5E6, etc)
	mustCompile(`^(?<title>.*?)(?:(?:[-_\W](?<![()\[!]))+S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:\-|[ex]){1,2}(?<episode>\d{1}))+)+(\W+|_|$)(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:410 — iTunes Season 1\05 Title (Quality).ext
	mustCompile(`^(?:Season(?:_|-|\s|\.)(?<season>(?<!\d+)\d{1,2}(?!\d+)))(?:_|-|\s|\.)(?<episode>(?<!\d+)\d{1,2}(?!\d+))`, regexp2.IgnoreCase),

	// Parser.cs:414 — iTunes 1-05 Title (Quality).ext
	mustCompile(`^(?:(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:-(?<episode>\d{2,3}(?!\d+))))`, regexp2.IgnoreCase),

	// Parser.cs:418 — Anime Range - Title Absolute Episode Number (ep01-12)
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:_|\s|\.)+(?:e|ep)(?<absoluteepisode>\d{2,3}(\.\d{1,2})?)-(?<absoluteepisode>(?<!\d+)\d{1,2}(\.\d{1,2})?(?!\d+|-)).*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:422 — Anime - Title Absolute Episode Number (e66)
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:(?:_|-|\s|\.)+(?:e|ep)(?<absoluteepisode>\d{2,4}(\.\d{1,2})?))+[-_. ].*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:426 — Anime - Title Episode Absolute Episode Number (Series Title Episode 01)
	mustCompile(`^(?<title>.+?)[-_. ](?:Episode)(?:[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+)))+.*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:430 — Anime Range - Title Absolute Episode Number (1 or 2 digit absolute episode numbers in a range, 1-10)
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)[_. ]+(?<absoluteepisode>(?<!\d+)\d{1,2}(\.\d{1,2})?(?!\d+))-(?<absoluteepisode>(?<!\d+)\d{1,2}(\.\d{1,2})?(?!\d+|-)).*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:434 — Anime - Title Episode/Episodio Absolute Episode Number
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)[-_. ]+(?:Episode|Episodio)(?:[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,4}(\.\d{1,2})?(?!\d+|[ip])))+.*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:438 — Anime - Title [Absolute Episode Number] from AniLibriaTV
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:[-_. ]\[)(?:(?:-?)(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+|[ip])))+(?:\][-_. ]).*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:442 — Anime - Title Absolute Episode Number
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,4}(\.\d{1,2})?(?!\d+|[ip])))+.*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:446 — Anime - Title {Absolute Episode Number}
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:(?:[-_\W](?<![()\[!]))+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+|[ip])))+.*?(?<hash>[(\[]\w{8}[)\]])?$`, regexp2.IgnoreCase),

	// Parser.cs:450 — Extant, terrible multi-episode naming (extant.10708.hdtv-lol.mp4)
	mustCompile(`^(?<title>.+?)[-_. ](?<season>[0]?\d?)(?:(?<episode>\d{2}){2}(?!\d+))[-_. ]`, regexp2.IgnoreCase),

	// Parser.cs:454 — Season only releases for poorly named anime
	mustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ])?(?<title>.+?)[-_. ]+?[\[(](?:S|Season|Saison|Series|Stagione)[-_. ]?(?<season>\d{1,2}(?![-_. ]?\d+))(?:[-_. )\]]|$)+(?<extras>EXTRAS|SUBPACK)?(?!\\)`, regexp2.IgnoreCase),

	// Parser.cs:458 — Episodes without a title, Single episode numbers (S1E1, 1x1)
	mustCompile(`^(?:S?(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[-_ ]?[ex])(?<episode>\d{1}(?!\d+))))`, regexp2.IgnoreCase),
}

// seriesRequestInfoRegex ports Sonarr's RequestInfoRegex (Parser.cs:589): a
// leading run of "[...]" bracket tags, stripped from the series name.
var seriesRequestInfoRegex = mustCompile(`^(?:\[.+?\])+`, 0)

// seriesYearInTitleRegex ports Sonarr's YearInTitleRegex (Parser.cs:575): splits
// a trailing 4-digit year out of the title for SeriesTitleInfo.
var seriesYearInTitleRegex = mustCompile(`^(?<title>.+?)[-_. ]+?[\(\[]?(?<year>\d{4})[\]\)]?`, regexp2.IgnoreCase)

// seriesTitleComponentsRegex ports Sonarr's TitleComponentsRegex (Parser.cs:578):
// splits AllTitles on paren / pipe / AKA variants.
var seriesTitleComponentsRegex = mustCompile(`^(?:(?<title>.+?) \((?<title>.+?)\)|(?<title>.+?) \| (?<title>.+?)|(?<title>.+?) AKA (?<title>.+?))$`, regexp2.IgnoreCase)

// seriesNumberWords ports Sonarr's Numbers array (Parser.cs:591): the English
// word fallback for ParseNumber (index → value, "zero"=0 .. "nine"=9).
var seriesNumberWords = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

// invalidDate is the sentinel error parseSeriesMatchCollection returns to mirror
// Sonarr's InvalidDateException: it aborts the whole ReportTitleRegex loop (no
// further patterns are tried), where a nil result merely lets the loop continue.
type invalidDate struct{}

func (invalidDate) Error() string { return "invalid date" }

// seriesMatch is the subset of ParsedEpisodeInfo the identity port produces.
type seriesMatch struct {
	seriesName       string
	titleWithoutYear string
	year             int
	allTitles        []string

	seasonNumber    int
	hasSeason       bool
	episodeNumbers  []int
	absoluteNumbers []int
	specialAbsolute []float64
	airDate         string

	fullSeason      bool
	isPartialSeason bool
	seasonPart      int
	isMultiSeason   bool
	isMiniSeries    bool
	special         bool
	isSplitEpisode  bool
	isSeasonExtra   bool
	dailyPart       int
	hasDailyPart    bool

	releaseTokens string

	kind NumberingKind
}

// parseSeriesIdentity runs the faithful Sonarr ParseTitle identity pipeline
// (Parser.cs:695) over input and fills Identity.Title/Year/AllTitles plus the
// full Numbering namespace. Quality/language are parsed independently elsewhere
// and are NOT gated here.
func parseSeriesIdentity(p *ParsedRelease, input string) {
	c := cleanSeries(input)
	if c.rejected {
		// ValidateBeforeParsing failed → Sonarr returns null. Leave identity empty.
		return
	}

	// Main loop (Parser.cs:762): first regex with a match wins. The table is
	// matched against the quality/codec-stripped simple title. The interpreter
	// can return: a result (success), a nil result + nil error (retry the next
	// pattern), or an invalidDate error (abort the whole loop).
	for _, re := range seriesPatterns {
		m, err := re.FindStringMatch(c.simple)
		if err != nil || m == nil {
			continue
		}
		res, perr := parseSeriesMatchCollection(re, m, c.release)
		if _, abort := perr.(invalidDate); abort {
			// InvalidDateException → break the loop, leave identity empty.
			return
		}
		if res == nil {
			// null → try the next regex.
			continue
		}
		populateSeriesIdentity(p, res)

		// Languages, mirroring Sonarr ParseTitle (Parser.cs:781): parse from
		// result.ReleaseTokens — the substring of releaseTitle AFTER the S/E
		// portion (res.releaseTokens) — NOT the full title, so title words aren't
		// misread as languages. Only the successful-match branch runs this; an
		// unparsed title returns null upstream and never reaches LanguageParser.
		if langs := parseSeriesLanguages(res.releaseTokens); len(langs) > 0 {
			p.Release.Languages = Field[[]string]{Value: langs, Confidence: confDetected, Evidence: "language parser"}
		}

		// Release group / hash, mirroring Sonarr ParseTitle (Parser.cs:787-797):
		// ParseReleaseGroup over the releaseTitle, then OVERRIDE with the match's
		// subgroup if present, then the release hash off the winning match.
		group := parseSeriesGroup(c.release)
		if sub := getSubGroup(m); sub != "" {
			group = sub
		}
		if group != "" {
			p.Release.ReleaseGroup = Field[string]{Value: group, Confidence: confDetected, Evidence: "release-group parser"}
		}
		if hash := getReleaseHash(m); hash != "" {
			p.Release.ReleaseHash = Field[string]{Value: hash, Confidence: confDetected, Evidence: "release-hash parser"}
		}
		return
	}
}

// populateSeriesIdentity copies a successful seriesMatch into the ParsedRelease
// Field[T] model. The Title is the year-stripped title (SeriesTitleInfo
// .TitleWithoutYear), matching the parity oracle's projection.
func populateSeriesIdentity(p *ParsedRelease, res *seriesMatch) {
	if res.titleWithoutYear != "" {
		p.Identity.Title = Field[string]{Value: res.titleWithoutYear, Confidence: confDetected, Evidence: "series title pattern"}
	}
	if res.year != 0 {
		p.Identity.Year = Field[int]{Value: res.year, Confidence: confDetected, Evidence: "series title year"}
	}
	if len(res.allTitles) > 0 {
		p.Identity.AllTitles = Field[[]string]{Value: res.allTitles, Confidence: confDetected, Evidence: "series AKA parser"}
	}

	n := &p.Identity.Numbering
	if res.hasSeason {
		n.Season = Field[int]{Value: res.seasonNumber, Confidence: confDetected, Evidence: "series numbering"}
	}
	if len(res.episodeNumbers) > 0 {
		n.EpisodeNumbers = Field[[]int]{Value: res.episodeNumbers, Confidence: confDetected, Evidence: "series numbering"}
	}
	if len(res.absoluteNumbers) > 0 {
		n.AbsoluteNumbers = Field[[]int]{Value: res.absoluteNumbers, Confidence: confDetected, Evidence: "series numbering"}
	}
	if len(res.specialAbsolute) > 0 {
		n.SpecialAbsolute = Field[[]float64]{Value: res.specialAbsolute, Confidence: confDetected, Evidence: "series numbering"}
	}
	if res.airDate != "" {
		n.AirDate = Field[string]{Value: res.airDate, Confidence: confDetected, Evidence: "daily pattern"}
	}
	n.FullSeason = boolField(res.fullSeason, "series numbering")
	n.IsPartialSeason = boolField(res.isPartialSeason, "series numbering")
	if res.seasonPart != 0 {
		n.SeasonPart = Field[int]{Value: res.seasonPart, Confidence: confDetected, Evidence: "series numbering"}
	}
	n.IsMultiSeason = boolField(res.isMultiSeason, "series numbering")
	n.IsMiniSeries = boolField(res.isMiniSeries, "series numbering")
	n.Special = boolField(res.special, "series numbering")
	n.IsSplitEpisode = boolField(res.isSplitEpisode, "series numbering")
	n.IsSeasonExtra = boolField(res.isSeasonExtra, "series numbering")
	if res.hasDailyPart {
		n.DailyPart = Field[int]{Value: res.dailyPart, Confidence: confDetected, Evidence: "daily pattern"}
	}
	n.Kind = res.kind
}

// parseSeriesMatchCollection ports Sonarr's ParseMatchCollection (Parser.cs:1020).
// It iterates ALL non-overlapping matches of the winning pattern (re) over the
// release string, mirroring .NET's MatchCollection. It returns:
//   - (*seriesMatch, nil)        → success
//   - (nil, nil)                 → null (first>last etc.) → caller tries next pattern
//   - (nil, invalidDate{})       → InvalidDateException → caller aborts the loop
//
// re is matched against releaseTitle (the same string the interpreter uses for
// ReleaseTokens); this mirrors upstream, which matches simpleTitle for the
// loop-selection step but passes releaseTitle into ParseMatchCollection where the
// MatchCollection is regenerated against releaseTitle. Sonarr actually passes the
// simpleTitle MatchCollection into ParseMatchCollection — so we re-run re against
// the same string the winning match came from. The caller passes c.simple in via
// re already matched on c.simple; here we collect all matches over that string.
func parseSeriesMatchCollection(re *regexp2.Regexp, first *regexp2.Match, releaseTitle string) (*seriesMatch, error) {
	// Collect all non-overlapping matches (the .NET MatchCollection).
	matches := []*regexp2.Match{first}
	for {
		next, err := re.FindNextMatch(matches[len(matches)-1])
		if err != nil || next == nil {
			break
		}
		matches = append(matches, next)
	}
	m0 := matches[0]

	// seriesName (Parser.cs:1022): title group of match[0], . and _ → space,
	// strip leading [..], trim.
	seriesName := groupValue(m0, "title")
	seriesName = strings.ReplaceAll(seriesName, ".", " ")
	seriesName = strings.ReplaceAll(seriesName, "_", " ")
	seriesName = strings.Trim(replaceAll(seriesRequestInfoRegex, seriesName, ""), " ")

	airYear := 0
	if n, ok := atoiOK(groupValue(m0, "airyear")); ok {
		airYear = n
	}

	lastIdx := groupEndIndex(m0, "title")

	res := &seriesMatch{seriesName: seriesName}

	if airYear < 1900 {
		// ----- Standard / absolute / season branch (Parser.cs:1031) -----
		for _, mg := range matches {
			episodeCaps := groupCaptures(mg, "episode")
			absoluteCaps := groupCaptures(mg, "absoluteepisode")

			if len(episodeCaps) > 0 {
				firstEp, err1 := parseNumber(episodeCaps[0].String())
				lastEp, err2 := parseNumber(episodeCaps[len(episodeCaps)-1].String())
				if err1 != nil || err2 != nil {
					// A non-numeric episode capture: upstream throws FormatException,
					// which is swallowed → null result (next pattern).
					return nil, nil
				}
				if firstEp > lastEp {
					return nil, nil
				}
				res.episodeNumbers = rangeInts(firstEp, lastEp)
				lastIdx = maxInt(lastIdx, captureEndIndex(episodeCaps[len(episodeCaps)-1]))

				if groupSuccess(mg, "special") {
					res.special = true
				}
				if groupSuccess(mg, "splitepisode") {
					res.isSplitEpisode = true
				}
			}

			if len(absoluteCaps) > 0 {
				firstAbs, err1 := parseDecimal(absoluteCaps[0].String())
				lastAbs, err2 := parseDecimal(absoluteCaps[len(absoluteCaps)-1].String())
				if err1 != nil || err2 != nil {
					return nil, nil
				}
				if firstAbs > lastAbs {
					return nil, nil
				}
				if !isInt(firstAbs) || !isInt(lastAbs) {
					if len(absoluteCaps) != 1 {
						return nil, nil // Multiple matches not allowed for specials
					}
					res.specialAbsolute = []float64{firstAbs}
					res.special = true
					lastIdx = maxInt(lastIdx, captureEndIndex(absoluteCaps[0]))
				} else {
					res.absoluteNumbers = rangeInts(int(firstAbs), int(lastAbs))
					if groupSuccess(mg, "special") {
						res.special = true
					}
					lastIdx = maxInt(lastIdx, captureEndIndex(absoluteCaps[len(absoluteCaps)-1]))
				}
			}

			if len(episodeCaps) == 0 && len(absoluteCaps) == 0 {
				if strings.TrimSpace(groupValue(m0, "extras")) != "" {
					res.isSeasonExtra = true
				}
				seasonPart := groupValue(m0, "seasonpart")
				if strings.TrimSpace(seasonPart) != "" {
					if n, err := strconv.Atoi(seasonPart); err == nil {
						res.seasonPart = n
					}
					res.isPartialSeason = true
				} else {
					res.fullSeason = true
				}
			}

			// Full-season override (Parser.cs:1132): exactly 2 episode captures +
			// episodecount group present + last == episodecount.
			if len(episodeCaps) == 2 && groupSuccess(m0, "episodecount") &&
				episodeCaps[len(episodeCaps)-1].String() == groupValue(m0, "episodecount") {
				res.episodeNumbers = nil
				res.fullSeason = true
			}
		}

		// Season resolution (Parser.cs:1139).
		var seasons []int
		for _, sc := range groupCaptures(m0, "season") {
			if n, err := strconv.Atoi(sc.String()); err == nil {
				seasons = append(seasons, n)
				lastIdx = maxInt(lastIdx, captureEndIndex(sc))
			}
		}
		if distinctCount(seasons) > 1 {
			res.isMultiSeason = true
		}
		if len(seasons) > 0 {
			res.seasonNumber = seasons[0]
			res.hasSeason = true
		} else if len(res.absoluteNumbers) == 0 && len(res.episodeNumbers) > 0 {
			res.seasonNumber = 1
			res.hasSeason = true
			res.isMiniSeries = true
		}

		// Kind: daily already excluded here. Absolute if absolute present and no
		// season/episode; else season_episode if season or episodes present.
		res.kind = seriesKind(res)
	} else {
		// ----- Daily branch (Parser.cs:1169) -----
		airmonth := 0
		airday := 0

		if groupSuccess(m0, "ambiguousairmonth") && groupSuccess(m0, "ambiguousairday") {
			am, _ := strconv.Atoi(groupValue(m0, "ambiguousairmonth"))
			ad, _ := strconv.Atoi(groupValue(m0, "ambiguousairday"))
			if ad <= 12 && am <= 12 {
				return nil, invalidDate{}
			}
			airmonth = am
			airday = ad
		} else {
			airmonth, _ = strconv.Atoi(groupValue(m0, "airmonth"))
			airday, _ = strconv.Atoi(groupValue(m0, "airday"))
			if airmonth > 12 {
				airmonth, airday = airday, airmonth
			}
		}

		airDate, ok := makeDate(airYear, airmonth, airday)
		if !ok {
			return nil, invalidDate{}
		}
		// Future date (now + 1 day).
		if airDate.After(truncateDay(time.Now().AddDate(0, 0, 1))) {
			return nil, invalidDate{}
		}
		// Before 1970 and no titleyear group → reject (Plex DVR exception).
		if airDate.Before(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) &&
			strings.TrimSpace(groupValue(m0, "titleyear")) == "" {
			return nil, invalidDate{}
		}

		lastIdx = maxInt(lastIdx, groupEndIndex(m0, "airyear"))
		lastIdx = maxInt(lastIdx, groupEndIndex(m0, "airmonth"))
		lastIdx = maxInt(lastIdx, groupEndIndex(m0, "airday"))

		res.airDate = airDate.Format("2006-01-02")
		res.kind = NumberingDaily

		if groupSuccess(m0, "part") {
			if n, err := strconv.Atoi(groupValue(m0, "part")); err == nil {
				res.dailyPart = n
				res.hasDailyPart = true
			}
		}
	}

	// ReleaseTokens (Parser.cs:1245): releaseTitle.Substring(lastSeasonEpisodeStringIndex)
	// when the index is within bounds, else the whole releaseTitle. lastIdx was
	// accumulated as a rune offset over the simpleTitle the match ran on; upstream
	// applies that same index to releaseTitle verbatim (the two strings differ when
	// quality/codec tokens were stripped, but upstream substrings releaseTitle
	// regardless). This feeds LanguageParser.ParseLanguages.
	releaseRunes := []rune(releaseTitle)
	if lastIdx >= 0 && lastIdx < len(releaseRunes) {
		res.releaseTokens = string(releaseRunes[lastIdx:])
	} else {
		res.releaseTokens = releaseTitle
	}

	// Title / year / AKA (Parser.cs:1254 → GetSeriesTitleInfo).
	getSeriesTitleInfo(res, m0)

	return res, nil
}

// seriesKind classifies the non-daily numbering namespace, mirroring
// ParsedEpisodeInfo.IsAbsoluteNumbering (absolute numbers present, no season/
// episode) vs season_episode. A pure full-season/partial-season pack with a
// season but no episodes is season_episode.
func seriesKind(res *seriesMatch) NumberingKind {
	hasSE := res.hasSeason || len(res.episodeNumbers) > 0
	hasAbs := len(res.absoluteNumbers) > 0 || len(res.specialAbsolute) > 0
	if hasAbs && !hasSE {
		return NumberingAbsolute
	}
	if hasSE {
		return NumberingSeasonEpisode
	}
	if hasAbs {
		return NumberingAbsolute
	}
	return NumberingNone
}

// getSeriesTitleInfo ports Sonarr's GetSeriesTitleInfo (Parser.cs:989): sets the
// title, splits a trailing year via YearInTitleRegex, and populates AllTitles via
// TitleComponentsRegex (paren / pipe / AKA) or, failing that, the multiple title
// captures of match[0].
func getSeriesTitleInfo(res *seriesMatch, m0 *regexp2.Match) {
	title := res.seriesName
	res.titleWithoutYear = title

	if ym, err := seriesYearInTitleRegex.FindStringMatch(title); err == nil && ym != nil {
		res.titleWithoutYear = groupValue(ym, "title")
		if y, err := strconv.Atoi(groupValue(ym, "year")); err == nil {
			res.year = y
		}
	}

	if cm, err := seriesTitleComponentsRegex.FindStringMatch(res.titleWithoutYear); err == nil && cm != nil {
		caps := groupCaptures(cm, "title")
		titles := make([]string, 0, len(caps))
		for _, c := range caps {
			titles = append(titles, c.String())
		}
		res.allTitles = titles
	} else if titleCaps := groupCaptures(m0, "title"); len(titleCaps) > 1 {
		titles := make([]string, 0, len(titleCaps))
		for _, c := range titleCaps {
			v := strings.ReplaceAll(c.String(), ".", " ")
			v = strings.ReplaceAll(v, "_", " ")
			titles = append(titles, v)
		}
		res.allTitles = titles
	}
}

// ---------------------------------------------------------------------------
// regexp2 group/capture helpers (faithful .NET Group/Capture semantics).
// ---------------------------------------------------------------------------

// groupValue returns the (last) captured value of a named group, or "" when the
// group is absent from the pattern or did not participate. Mirrors
// match.Groups["name"].Value in .NET (empty string for an unmatched group).
func groupValue(m *regexp2.Match, name string) string {
	g := m.GroupByName(name)
	if g == nil || len(g.Captures) == 0 {
		return ""
	}
	return g.String()
}

// groupSuccess reports whether a named group participated in the match, mirroring
// match.Groups["name"].Success.
func groupSuccess(m *regexp2.Match, name string) bool {
	g := m.GroupByName(name)
	return g != nil && len(g.Captures) > 0
}

// groupCaptures returns all captures of a named group, mirroring
// match.Groups["name"].Captures (a list, empty when absent/unmatched).
func groupCaptures(m *regexp2.Match, name string) []regexp2.Capture {
	g := m.GroupByName(name)
	if g == nil {
		return nil
	}
	return g.Captures
}

// groupEndIndex returns the rune end-index of a group's last capture (Index +
// Length), mirroring the EndIndex() extension; 0 when the group is absent.
func groupEndIndex(m *regexp2.Match, name string) int {
	g := m.GroupByName(name)
	if g == nil || len(g.Captures) == 0 {
		return 0
	}
	return g.Index + g.Length
}

// captureEndIndex returns a capture's rune end-index (Index + Length).
func captureEndIndex(c regexp2.Capture) int {
	return c.Index + c.Length
}

// ---------------------------------------------------------------------------
// Number parsing (faithful ParseNumber / ParseDecimal ports).
// ---------------------------------------------------------------------------

// parseNumber ports Sonarr's ParseNumber (Parser.cs:1323): FormKC-normalize,
// convert unicode digits to ASCII, int-parse; fall back to the English word list
// (zero..nine). Returns an error for a non-number (upstream throws FormatException,
// which the loop swallows into a null result).
func parseNumber(value string) (int, error) {
	normalized := convertToNumerals(norm.NFKC.String(value))
	if n, err := strconv.Atoi(normalized); err == nil {
		return n, nil
	}
	lower := strings.ToLower(value)
	for i, w := range seriesNumberWords {
		if w == lower {
			return i, nil
		}
	}
	return 0, strconv.ErrSyntax
}

// parseDecimal ports Sonarr's ParseDecimal (Parser.cs:1342): FormKC-normalize,
// convert unicode digits, invariant-culture float parse. Returns an error for a
// non-number.
func parseDecimal(value string) (float64, error) {
	normalized := convertToNumerals(norm.NFKC.String(value))
	f, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return 0, err
	}
	return f, nil
}

// convertToNumerals ports Sonarr's ConvertToNumerals (Parser.cs:1354): each
// unicode numeric rune is replaced by its numeric value; other runes pass
// through. After FormKC normalization most fullwidth digits are already ASCII,
// but this handles any residual unicode-number runes (e.g. circled digits).
func convertToNumerals(input string) string {
	var b strings.Builder
	for _, r := range input {
		if v, ok := runeNumericValue(r); ok {
			b.WriteString(v)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// runeNumericValue maps a unicode numeric rune to its decimal-digit string,
// mirroring char.IsNumber + char.GetNumericValue for the single-digit cases the
// parser sees. ASCII digits pass through; decimal digits in other scripts are
// resolved via unicode.Digit. After FormKC normalization most fullwidth digits
// are already ASCII, so this is a residual safety net; anything non-numeric is
// left to the caller.
func runeNumericValue(r rune) (string, bool) {
	if r >= '0' && r <= '9' {
		return string(r), true
	}
	if d := decimalDigitValue(r); d >= 0 {
		return strconv.Itoa(d), true
	}
	return "", false
}

// decimalDigitValue returns the 0..9 value of a unicode decimal-digit rune, or
// -1 if r is not a decimal digit. It mirrors char.GetNumericValue for the
// single-digit decimal case (which is all the parser encounters). Unicode
// decimal digits live in contiguous 0..9 blocks, so the value is the offset from
// the block's zero (found by scanning back while the run stays a digit).
func decimalDigitValue(r rune) int {
	if !unicode.IsDigit(r) {
		return -1
	}
	base := r
	for base > 0 && unicode.IsDigit(base-1) && r-base < 9 {
		base--
	}
	return int(r - base)
}

// ---------------------------------------------------------------------------
// Small numeric/date helpers.
// ---------------------------------------------------------------------------

// rangeInts returns the inclusive integer range [start, end].
func rangeInts(start, end int) []int {
	out := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, i)
	}
	return out
}

// maxInt returns the larger of a, b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// isInt reports whether f has no fractional part (f % 1 == 0 in C#).
func isInt(f float64) bool {
	return f == float64(int64(f))
}

// distinctCount returns the number of distinct values in xs.
func distinctCount(xs []int) int {
	seen := map[int]struct{}{}
	for _, x := range xs {
		seen[x] = struct{}{}
	}
	return len(seen)
}

// makeDate builds a calendar date, returning ok=false when the components are out
// of range (mirroring the DateTime ctor throwing). Go's time.Date normalizes
// out-of-range values, so we validate by round-tripping the components.
func makeDate(year, month, day int) (time.Time, bool) {
	if month < 1 || month > 12 || day < 1 || day > 31 || year < 1 {
		return time.Time{}, false
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return time.Time{}, false
	}
	return t, true
}

// truncateDay zeroes the time-of-day, mirroring DateTime.Now.AddDays(1).Date.
func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
