package parsing

// quality.go is the quality-detection half of the parser, ported from Radarr's
// QualityParser (submodules/radarr/src/NzbDrone.Core/Parser/QualityParser.cs).
// Radarr's model is the superset: it decomposes quality into three orthogonal
// axes plus a revision — (Source, Resolution, Modifier, Revision) — where Sonarr
// folds remux/raw-ness into extra source values. Porting Radarr yields the
// orthogonal core directly; Sonarr's flattened vocabulary is a lossy projection
// of it, validated downstream.
//
// The patterns are transcribed verbatim through mustCompile (regexp2, .NET
// semantics + ReDoS timeout) — the same engine the rest of the package uses.
// BRDISKRegex in particular is lookahead-heavy and REQUIRES regexp2; stdlib RE2
// cannot express it.

import (
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
)

// Resolution is the resolution axis of the quality core. Values mirror Radarr's
// Resolution enum (QualityParser.cs:778) by pixel height; ResUnknown is the
// not-detected zero state.
type Resolution string

const (
	ResUnknown Resolution = "Unknown"
	ResSD      Resolution = "SD"
	Res480p    Resolution = "480p"
	Res576p    Resolution = "576p"
	Res720p    Resolution = "720p"
	Res1080p   Resolution = "1080p"
	Res2160p   Resolution = "2160p"
)

// Source is the medium axis of the quality core, mirroring Radarr's
// QualitySource (Qualities/QualitySource.cs). Note the orthogonal model: a
// remux's source is BluRay and a Raw-HD's source is HDTV — remux/raw-ness lives
// on the Modifier axis, never here.
type Source string

const (
	SourceUnknown   Source = "Unknown"
	SourceCAM       Source = "CAM"
	SourceTelesync  Source = "Telesync"
	SourceTelecine  Source = "Telecine"
	SourceWorkprint Source = "Workprint"
	SourceDVD       Source = "DVD"
	SourceTV        Source = "TV" // SDTV / HDTV / Raw-HD all share QualitySource.TV
	SourceWEBDL     Source = "WEB-DL"
	SourceWEBRip    Source = "WEBRip"
	SourceBluRay    Source = "BluRay"
)

// Modifier is the orthogonal modifier axis of the quality core, mirroring
// Radarr's Modifier enum (Qualities/Modifier.cs). It carries the remux/full-disc/
// raw distinction that Sonarr instead bakes into extra source values.
//
// SCREENER and REGIONAL exist in Radarr's enum but are deferred (no detection)
// for v1 — see the parsing spec's "Modifier set scope" open question. The stub
// values are present so the enum mirrors upstream, but nothing populates them.
type Modifier string

const (
	ModNone     Modifier = ""
	ModRegional Modifier = "REGIONAL" // deferred: stub, never set
	ModScreener Modifier = "SCREENER" // deferred: stub, never set
	ModRawHD    Modifier = "RAWHD"
	ModBRDisk   Modifier = "BR-DISK"
	ModRemux    Modifier = "REMUX"
)

// Revision mirrors Radarr's Revision (Qualities/Revision.cs): a proper count
// (Version), a REAL count, and the repack flag.
type Revision struct {
	Version  int
	Real     int
	IsRepack bool
}

// qualityCore is the internal result of quality detection: the orthogonal
// (Source, Resolution, Modifier) plus the Revision. Parse wraps it into the
// ParsedRelease model.
type qualityCore struct {
	source     Source
	resolution Resolution
	modifier   Modifier
	revision   Revision
}

// Quality patterns, ported verbatim from Radarr QualityParser.cs. RegexOptions
// .IgnoreCase maps to regexp2.IgnoreCase; .IgnorePatternWhitespace maps to
// regexp2.IgnorePatternWhitespace; .Compiled is dropped (no analogue).
var (
	// QualityParser.cs:17 — the unified source regex. Last match wins (see
	// detectSource). IgnorePatternWhitespace keeps the multi-line layout 1:1.
	sourceRegex = mustCompile(`\b(?:
	                                (?<bluray>M?Blu[-_. ]?Ray|HD[-_. ]?DVD|BD(?!$)|UHD2?BD|BDISO|BDMux|BD25|BD50|BR[-_. ]?DISK)|
	                                (?<webdl>WEB[-_. ]?DL(?:mux)?|AmazonHD|AmazonSD|iTunesHD|MaxdomeHD|NetflixU?HD|WebHD|HBOMaxHD|DisneyHD|[. ]WEB[. ](?:[xh][ .]?26[45]|AVC|HEVC|DDP?5[. ]1)|[. ](?-i:WEB)$|(?:\d{3,4}0p)[-. ](?:Hybrid[-_. ]?)?WEB[-. ]|[-. ]WEB[-. ]\d{3,4}0p|\b\s\/\sWEB\s\/\s\b|(?:AMZN|NF|DP)[. -]WEB[. -](?!Rip))|
	                                (?<webrip>WebRip|Web-Rip|WEBMux)|
	                                (?<hdtv>HDTV)|
	                                (?<bdrip>BDRip|BDLight|HD[-_. ]?DVDRip|UHDBDRip)|
	                                (?<brrip>BRRip)|
	                                (?<dvdr>\d?x?M?DVD-?[R59])|
	                                (?<dvd>DVD(?!-R)|DVDRip|xvidvd)|
	                                (?<dsr>WS[-_. ]DSR|DSR)|
	                                (?<regional>R[0-9]{1}|REGIONAL)|
	                                (?<scr>SCR|SCREENER|DVDSCR|DVDSCREENER)|
	                                (?<ts>TS[-_. ]|TELESYNCH?|HD-TS|HDTS|PDVD|TSRip|HDTSRip)|
	                                (?<tc>TC|TELECINE|HD-TC|HDTC)|
	                                (?<cam>CAMRIP|(?:NEW)?CAM|HD-?CAM(?:Rip)?|HQCAM)|
	                                (?<wp>WORKPRINT|WP)|
	                                (?<pdtv>PDTV)|
	                                (?<sdtv>SDTV)|
	                                (?<tvrip>TVRip)
	                                )(?:\b|$|[ .])`, regexp2.IgnoreCase|regexp2.IgnorePatternWhitespace)

	// QualityParser.cs:39
	rawHDRegex = mustCompile(`\b(?<rawhd>RawHD|Raw[-_. ]HD)\b`, regexp2.IgnoreCase)

	// NTSC/PAL — DVD-origin source signal Sonarr's QualityParser carries in its
	// dvd source group (Sonarr/.../QualityParser.cs:24 `(?<dvd>DVD|DVDRip|NTSC|
	// PAL|xvidvd)`) but Radarr's does not. We keep our shared source regex
	// Radarr-faithful and apply NTSC/PAL as a fallback inside parseQuality
	// instead, so a trailing NTSC token doesn't override an earlier explicit
	// DVD-R / DVD5 / DVD9 / DVDR match (the LastOrDefault precedence would
	// otherwise demote DVD-R bins to plain DVD).
	ntscPalRegex = mustCompile(`\b(?:NTSC|PAL)\b`, regexp2.IgnoreCase)

	// QualityParser.cs:42
	mpeg2Regex = mustCompile(`\b(?<mpeg2>MPEG[-_. ]?2)\b`, regexp2.None)

	// QualityParser.cs:44 — full Blu-ray-disc detection. Lookahead-heavy; the
	// disc is detected via codec/container tells (AVC/VC-1/MPEG-2/BDMV/ISO/BD
	// sizes) alongside Blu-ray, NOT a literal "BR-DISK" token, with negative
	// lookaheads excluding BDRip/720p/XviD/REMUX/etc. Requires regexp2.
	brDiskRegex = mustCompile(`^(?!.*\b((?<!HD[._ -]|HD)DVD|BDRip|720p|MKV|XviD|WMV|d3g|(BD)?REMUX|^(?=.*1080p)(?=.*HEVC)|[xh][-_. ]?26[45]|German.*[DM]L|((?<=\d{4}).*German.*([DM]L)?)(?=.*\b(AVC|HEVC|VC[-_. ]?1|MVC|MPEG[-_. ]?2)\b))\b)(((?=.*\b(Blu[-_. ]?ray|BD|HD[-_. ]?DVD)\b)(?=.*\b(AVC|HEVC|VC[-_. ]?1|MVC|MPEG[-_. ]?2|BDMV|ISO)\b))|^((?=.*\b(((?=.*\b((.*_)?COMPLETE.*|Dis[ck])\b)(?=.*(Blu[-_. ]?ray|HD[-_. ]?DVD)))|3D[-_. ]?BD|BR[-_. ]?DISK|Full[-_. ]?Blu[-_. ]?ray|^((?=.*((BD|UHD)[-_. ]?(25|50|66|100|ISO)))))))).*`, regexp2.IgnoreCase)

	// QualityParser.cs:47
	properRegex = mustCompile(`\b(?<proper>proper)\b`, regexp2.IgnoreCase)

	// QualityParser.cs:50
	repackRegex = mustCompile(`\b(?<repack>repack\d?|rerip\d?)\b`, regexp2.IgnoreCase)

	// QualityParser.cs:53
	versionRegex = mustCompile(`\d[-._ ]?v(?<version>\d)[-._ ]|\[v(?<version>\d)\]|repack(?<version>\d)|rerip(?<version>\d)`, regexp2.IgnoreCase)

	// QualityParser.cs:56
	realRegex = mustCompile(`\b(?<real>REAL)\b`, regexp2.None)

	// QualityParser.cs:59
	resolutionRegex = mustCompile(`\b(?:(?<R360p>360p)|(?<R480p>480p|480i|640x480|848x480)|(?<R540p>540p)|(?<R576p>576p)|(?<R720p>720p|1280x720|960p)|(?<R1080p>1080p|1920x1080|1440p|FHD|1080i|4kto1080p)|(?<R2160p>2160p|3840x2160|4k[-_. ](?:UHD|HEVC|BD|H\.?265)|(?:UHD|HEVC|BD|H\.?265)[-_. ]4k))\b`, regexp2.IgnoreCase)

	// QualityParser.cs:63
	alternativeResolutionRegex = mustCompile(`\b(?<R2160p>UHD)\b|(?<R2160p>\[4K\])`, regexp2.IgnoreCase)

	// QualityParser.cs:66
	codecRegex = mustCompile(`\b(?:(?<x264>x264)|(?<h264>h264)|(?<xvidhd>XvidHD)|(?<xvid>X-?vid)|(?<divx>divx))\b`, regexp2.IgnoreCase)

	// QualityParser.cs:69
	otherSourceRegex = mustCompile(`(?<hdtv>HD[-_. ]TV)|(?<sdtv>SD[-_. ]TV)`, regexp2.IgnoreCase)

	// QualityParser.cs:71
	animeBlurayRegex = mustCompile(`bd(?:720|1080|2160)|(?<=[-_. (\[])bd(?=[-_. )\]])`, regexp2.IgnoreCase)
	// QualityParser.cs:72
	animeWebDlRegex = mustCompile(`\[WEB\]|[\[\(]WEB[ .]`, regexp2.IgnoreCase)

	// QualityParser.cs:74
	highDefPdtvRegex = mustCompile(`hr[-_. ]ws`, regexp2.IgnoreCase)

	// QualityParser.cs:76
	remuxRegex = mustCompile(`(?:[_. \[]|\d{4}p-|\bHybrid-)(?<remux>(?:(BD|UHD)[-_. ]?)?Remux)\b|(?<remux>(?:(BD|UHD)[-_. ]?)?Remux[_. ]\d{4}p)`, regexp2.IgnoreCase)
	// QualityParser.cs:77
	germanRemuxRegex = mustCompile(`((?<=\d{4}).*German.*([DM]L)?)(?=.*\b(AVC|HEVC|VC[_. -]?1|MVC|MPEG[_. -]?2))(?=.*Blu-?ray)`, regexp2.IgnoreCase)
)

// matches reports whether re matches s, failing closed on a timeout/engine error
// (parsing is total). Mirrors regexp2's IsMatch where upstream uses it.
func matches(re *regexp2.Regexp, s string) bool {
	ok, err := re.MatchString(s)
	return err == nil && ok
}

// extensionQualityMap ports Radarr's MediaFileExtensions map
// (MediaFiles/MediaFileExtensions.cs:13) as the orthogonal (source, resolution)
// the extension implies. Radarr's map differs from Sonarr's: .mkv → WEBDL720p,
// disc images (.img/.iso/.vob) → DVD.
type extQuality struct {
	source     Source
	resolution Resolution
}

// The extension map is the one quality data table where Sonarr and Radarr
// genuinely diverge: Radarr maps .mkv → WEBDL720p and disc images → DVD, while
// Sonarr maps .mkv → HDTV720p and keeps a finer SD/DVD split. We carry Sonarr's
// values here because the temporary bin/source render emits Sonarr vocabulary
// and the Tier-1 goldens are Sonarr-keyed — using Radarr's table would regress
// the Sonarr `.mkv`/`.ts` bins. The QualityParser *logic* is the faithful Radarr
// port; only this data table tracks Sonarr.
//
// TEMPORARY: Sonarr extension table; the internal/quality projection picks the
// per-domain table in a later step.
var extensionQualityMap = map[string]extQuality{
	// SDTV
	".avi": {SourceTV, Res480p}, ".m4v": {SourceTV, Res480p}, ".3gp": {SourceTV, Res480p},
	".nsv": {SourceTV, Res480p}, ".ty": {SourceTV, Res480p}, ".strm": {SourceTV, Res480p},
	".rm": {SourceTV, Res480p}, ".rmvb": {SourceTV, Res480p}, ".m3u": {SourceTV, Res480p},
	".ifo": {SourceTV, Res480p}, ".mov": {SourceTV, Res480p}, ".qt": {SourceTV, Res480p},
	".divx": {SourceTV, Res480p}, ".xvid": {SourceTV, Res480p}, ".bivx": {SourceTV, Res480p},
	".nrg": {SourceTV, Res480p}, ".pva": {SourceTV, Res480p}, ".wmv": {SourceTV, Res480p},
	".asf": {SourceTV, Res480p}, ".asx": {SourceTV, Res480p}, ".ogm": {SourceTV, Res480p},
	".ogv": {SourceTV, Res480p}, ".m2v": {SourceTV, Res480p}, ".bin": {SourceTV, Res480p},
	".dat": {SourceTV, Res480p}, ".dvr-ms": {SourceTV, Res480p}, ".mpg": {SourceTV, Res480p},
	".mpeg": {SourceTV, Res480p}, ".mp4": {SourceTV, Res480p}, ".avc": {SourceTV, Res480p},
	".vp3": {SourceTV, Res480p}, ".svq3": {SourceTV, Res480p}, ".nuv": {SourceTV, Res480p},
	".viv": {SourceTV, Res480p}, ".dv": {SourceTV, Res480p}, ".fli": {SourceTV, Res480p},
	".flv": {SourceTV, Res480p}, ".wpl": {SourceTV, Res480p},
	// DVD (disc images)
	".img": {SourceDVD, ResUnknown}, ".iso": {SourceDVD, ResUnknown}, ".vob": {SourceDVD, ResUnknown},
	// HD — Sonarr buckets .mkv/.ts/.wtv as HDTV-720p
	".mkv": {SourceTV, Res720p}, ".ts": {SourceTV, Res720p}, ".wtv": {SourceTV, Res720p},
	// Bluray
	".m2ts": {SourceBluRay, Res720p},
}

// getQualityForExtension returns the (source, resolution) the extension implies,
// or (Unknown, Unknown) for an unmapped extension. Mirrors
// MediaFileExtensions.GetQualityForExtension over the table above.
func getQualityForExtension(name string) extQuality {
	lastDot := strings.LastIndex(name, ".")
	if lastDot == -1 || lastDot == len(name)-1 {
		return extQuality{SourceUnknown, ResUnknown}
	}
	ext := strings.ToLower(name[lastDot:])
	if q, ok := extensionQualityMap[ext]; ok {
		return q
	}
	return extQuality{SourceUnknown, ResUnknown}
}

// parseResolution ports QualityParser.cs:669 ParseResolution: the named-group
// resolution regex with the UHD/[4K] alternative as a 2160p fallback.
func parseResolution(name string) Resolution {
	g := findFirst(resolutionRegex, name)
	if len(g) > 0 {
		switch {
		case g["R360p"] != "":
			return Res480p // Radarr R360p maps to the 480p quality bucket
		case g["R480p"] != "":
			return Res480p
		case g["R540p"] != "":
			return Res480p // R540p falls into the 480p bucket for FindBySourceAndResolution
		case g["R576p"] != "":
			return Res576p
		case g["R720p"] != "":
			return Res720p
		case g["R1080p"] != "":
			return Res1080p
		case g["R2160p"] != "":
			return Res2160p
		}
	}
	if matches(alternativeResolutionRegex, name) {
		return Res2160p
	}
	return ResUnknown
}

// detectSource returns the LAST source-regex match's group name (QualityParser
// .cs:117-118 takes Matches().LastOrDefault()), or "" for no match. regexp2 has
// no Matches() helper, so we iterate FindNextMatch and keep the last.
func detectSource(name string) string {
	m, err := sourceRegex.FindStringMatch(name)
	if err != nil {
		return ""
	}
	var lastGroup string
	for m != nil {
		for _, g := range m.Groups() {
			if g.Name == "" || isIndexName(g.Name) || g.Length == 0 {
				continue
			}
			lastGroup = g.Name
		}
		m, err = sourceRegex.FindNextMatch(m)
		if err != nil {
			break
		}
	}
	return lastGroup
}

// codecFlags is the subset of CodecRegex groups the quality logic branches on.
type codecFlags struct {
	x264, xvid, divx bool
}

func detectCodec(name string) codecFlags {
	g := findFirst(codecRegex, name)
	return codecFlags{x264: g["x264"] != "", xvid: g["xvid"] != "", divx: g["divx"] != ""}
}

// containsIgnoreCase mirrors the .NET ContainsIgnoreCase the parser leans on.
func containsIgnoreCase(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// parseRevision ports QualityParser.cs:739 ParseQualityModifiers — version /
// proper / repack / real, exactly as upstream sequences them.
func parseRevision(name, normalizedName string) Revision {
	var rev Revision
	versionParsed := 0
	hasVersion := false
	if g := findFirst(versionRegex, normalizedName); g["version"] != "" {
		versionParsed, _ = strconv.Atoi(g["version"])
		rev.Version = versionParsed
		hasVersion = true
	}
	if matches(properRegex, normalizedName) {
		if hasVersion {
			rev.Version = versionParsed + 1
		} else {
			rev.Version = 2
		}
	}
	if matches(repackRegex, normalizedName) {
		if hasVersion {
			rev.Version = versionParsed + 1
		} else {
			rev.Version = 2
		}
		rev.IsRepack = true
	}
	// QualityParser.cs:766 — REAL is counted over the raw (non-normalized) name.
	if n := countMatches(realRegex, name); n > 0 {
		rev.Real = n
	}
	return rev
}

// countMatches counts non-overlapping matches of re in s.
func countMatches(re *regexp2.Regexp, s string) int {
	m, err := re.FindStringMatch(s)
	if err != nil {
		return 0
	}
	n := 0
	for m != nil {
		n++
		m, err = re.FindNextMatch(m)
		if err != nil {
			break
		}
	}
	return n
}

// parseQuality extracts the orthogonal quality core (source, resolution, modifier)
// and revision from a release title or filename, ported from Radarr's
// QualityParser.ParseQualityName (QualityParser.cs:112) and the extension
// fallback in ParseQuality (QualityParser.cs:79). The detection ordering and
// precedence mirror upstream exactly; Parse wraps the result into ParsedRelease.
func parseQuality(name string) qualityCore {
	name = strings.TrimSpace(name)
	normalizedName := strings.TrimSpace(strings.ReplaceAll(name, "_", " "))

	result := qualityCore{source: SourceUnknown, resolution: ResUnknown, modifier: ModNone}
	result.revision = parseRevision(name, normalizedName)

	sourceGroup := detectSource(normalizedName)
	resolution := parseResolution(normalizedName)
	codec := detectCodec(normalizedName)
	remuxMatch := matches(remuxRegex, normalizedName) || matches(germanRemuxRegex, normalizedName)
	brDiskMatch := matches(brDiskRegex, normalizedName)

	// QualityParser.cs:124 — Raw-HD, but BR-DISK suppresses it.
	if matches(rawHDRegex, normalizedName) && !brDiskMatch {
		result.source = SourceTV
		result.modifier = ModRawHD
		result.resolution = ResUnknown
		return result
	}

	if sourceGroup != "" {
		switch sourceGroup {
		case "bluray":
			// QualityParser.cs:142
			result.source = SourceBluRay
			if brDiskMatch {
				result.modifier = ModBRDisk
				result.resolution = Res1080p // Radarr BRDISK is defined at 1080p
				return result
			}
			if codec.xvid || codec.divx {
				result.resolution = Res480p
				return result
			}
			switch resolution {
			case Res2160p:
				result.resolution = Res2160p
				if remuxMatch {
					result.modifier = ModRemux
				}
				return result
			case Res1080p:
				result.resolution = Res1080p
				if remuxMatch {
					result.modifier = ModRemux
				}
				return result
			case Res720p:
				result.resolution = Res720p
				return result
			case Res576p:
				result.resolution = Res576p
				return result
			case Res480p:
				result.resolution = Res480p
				return result
			default:
				// Treat a remux without a resolution as 1080p, not 720p.
				if remuxMatch && resolution != Res720p {
					result.resolution = Res1080p
					result.modifier = ModRemux
					return result
				}
				result.resolution = Res720p
				return result
			}

		case "webdl":
			// QualityParser.cs:200
			result.source = SourceWEBDL
			switch resolution {
			case Res2160p:
				result.resolution = Res2160p
			case Res1080p:
				result.resolution = Res1080p
			case Res720p:
				result.resolution = Res720p
			default:
				if strings.Contains(name, "[WEBDL]") {
					result.resolution = Res720p
				} else {
					result.resolution = Res480p
				}
			}
			return result

		case "webrip":
			// QualityParser.cs:230
			result.source = SourceWEBRip
			switch resolution {
			case Res2160p:
				result.resolution = Res2160p
			case Res1080p:
				result.resolution = Res1080p
			case Res720p:
				result.resolution = Res720p
			default:
				result.resolution = Res480p
			}
			return result

		case "scr":
			// QualityParser.cs:254 — Quality.DVDSCR (DVD/480p/SCREENER).
			result.source = SourceDVD
			result.resolution = Res480p
			result.modifier = ModScreener
			return result

		case "cam":
			// QualityParser.cs:260
			result.source = SourceCAM
			return result

		case "ts":
			// QualityParser.cs:266
			result.source = SourceTelesync
			result.resolution = resolution
			return result

		case "tc":
			// QualityParser.cs:273
			result.source = SourceTelecine
			return result

		case "wp":
			// QualityParser.cs:279 — Quality.WORKPRINT
			result.source = SourceWorkprint
			return result

		case "regional":
			// QualityParser.cs:285 — Quality.REGIONAL (DVD/480p/REGIONAL).
			result.source = SourceDVD
			result.resolution = Res480p
			result.modifier = ModRegional
			return result

		case "hdtv":
			// QualityParser.cs:291
			if matches(mpeg2Regex, normalizedName) {
				result.source = SourceTV
				result.modifier = ModRawHD
				return result
			}
			result.source = SourceTV
			switch resolution {
			case Res2160p:
				result.resolution = Res2160p
			case Res1080p:
				result.resolution = Res1080p
			case Res720p:
				result.resolution = Res720p
			default:
				if strings.Contains(name, "[HDTV]") {
					result.resolution = Res720p
				} else {
					result.resolution = Res480p // Quality.SDTV
				}
			}
			return result

		case "bdrip", "brrip":
			// QualityParser.cs:327
			result.source = SourceBluRay
			switch resolution {
			case Res720p:
				result.resolution = Res720p
			case Res1080p:
				result.resolution = Res1080p
			case Res2160p:
				result.resolution = Res2160p
			case Res576p:
				result.resolution = Res576p
			default:
				result.resolution = Res480p
			}
			return result

		case "dvdr":
			// QualityParser.cs:350 — Quality.DVDR (DVD/480p/REMUX).
			result.source = SourceDVD
			result.resolution = Res480p
			result.modifier = ModRemux
			return result

		case "dvd":
			// QualityParser.cs:356
			result.source = SourceDVD
			return result

		case "pdtv", "sdtv", "dsr", "tvrip":
			// QualityParser.cs:362
			result.source = SourceTV
			if resolution == Res1080p || containsIgnoreCase(normalizedName, "1080p") {
				result.resolution = Res1080p
				return result
			}
			if resolution == Res720p || containsIgnoreCase(normalizedName, "720p") {
				result.resolution = Res720p
				return result
			}
			if matches(highDefPdtvRegex, normalizedName) {
				result.resolution = Res720p
				return result
			}
			result.resolution = Res480p // Quality.SDTV
			return result
		}
	}

	// QualityParser.cs:391 — remux without a recognized source, by resolution.
	if sourceGroup == "" && remuxMatch && resolution != ResUnknown {
		switch resolution {
		case Res480p:
			result.source = SourceBluRay
			result.resolution = Res480p
			return result
		case Res720p:
			result.source = SourceBluRay
			result.resolution = Res720p
			return result
		case Res2160p:
			result.source = SourceBluRay
			result.resolution = Res2160p
			result.modifier = ModRemux
			return result
		case Res1080p:
			result.source = SourceBluRay
			result.resolution = Res1080p
			result.modifier = ModRemux
			return result
		}
	}

	// QualityParser.cs:421 — Anime Bluray.
	if matches(animeBlurayRegex, normalizedName) {
		result.source = SourceBluRay
		switch {
		case resolution == Res480p || resolution == Res576p || containsIgnoreCase(normalizedName, "480p"):
			result.source = SourceDVD // Quality.DVD
			result.resolution = ResUnknown
			return result
		case resolution == Res1080p || containsIgnoreCase(normalizedName, "1080p"):
			result.resolution = Res1080p
			if remuxMatch {
				result.modifier = ModRemux
			}
			return result
		case resolution == Res2160p || containsIgnoreCase(normalizedName, "2160p"):
			result.resolution = Res2160p
			if remuxMatch {
				result.modifier = ModRemux
			}
			return result
		default:
			if remuxMatch && resolution != Res720p {
				result.resolution = Res1080p
				result.modifier = ModRemux
				return result
			}
			result.resolution = Res720p
			return result
		}
	}

	// QualityParser.cs:460 — Anime WEB-DL.
	if matches(animeWebDlRegex, normalizedName) {
		result.source = SourceWEBDL
		switch {
		case resolution == Res480p || resolution == Res576p || containsIgnoreCase(normalizedName, "480p"):
			result.resolution = Res480p
			return result
		case resolution == Res1080p || containsIgnoreCase(normalizedName, "1080p"):
			result.resolution = Res1080p
			return result
		case resolution == Res2160p || containsIgnoreCase(normalizedName, "2160p"):
			result.resolution = Res2160p
			return result
		default:
			result.resolution = Res720p
			return result
		}
	}

	// QualityParser.cs:494 — resolution known, source inferred from remux or the
	// file extension; otherwise HDTV (TV) at that resolution / SDTV for SD.
	if resolution != ResUnknown {
		source := SourceUnknown
		modifier := ModNone
		if remuxMatch {
			source = SourceBluRay
			modifier = ModRemux
		} else {
			ext := getQualityForExtension(name)
			if ext.source != SourceUnknown {
				source = ext.source
			}
		}

		switch resolution {
		case Res2160p:
			if source == SourceUnknown {
				result.source = SourceTV
				result.resolution = Res2160p
			} else {
				result.source, result.resolution, result.modifier = findBySourceAndResolution(source, Res2160p, modifier)
			}
			return result
		case Res1080p:
			if source == SourceUnknown {
				result.source = SourceTV
				result.resolution = Res1080p
			} else {
				result.source, result.resolution, result.modifier = findBySourceAndResolution(source, Res1080p, modifier)
			}
			return result
		case Res720p:
			if source == SourceUnknown {
				result.source = SourceTV
				result.resolution = Res720p
			} else {
				result.source, result.resolution, result.modifier = findBySourceAndResolution(source, Res720p, modifier)
			}
			return result
		case Res480p, Res576p:
			if source == SourceUnknown {
				result.source = SourceTV
				result.resolution = Res480p // Quality.SDTV
			} else {
				result.source, result.resolution, result.modifier = findBySourceAndResolution(source, Res480p, modifier)
			}
			return result
		}
	}

	// NTSC/PAL fallback — Sonarr's QualityParser carries these tokens in its dvd
	// source group. We apply them as an explicit fallback only when no stronger
	// source signal fired and no resolution was inferred above, so an "S01E13.
	// NTSC.x264" release routes to DVD/480p rather than the x264 → SDTV default.
	// Placed after the resolution-known fallback so an "NTSC.1080p" release
	// keeps its inferred source rather than being demoted to DVD/480p.
	if matches(ntscPalRegex, normalizedName) {
		result.source = SourceDVD
		result.resolution = Res480p
		return result
	}

	// QualityParser.cs:569 — x264 with nothing else → SDTV.
	if codec.x264 {
		result.source = SourceTV
		result.resolution = Res480p
		return result
	}

	// QualityParser.cs:575 — explicit pixel dimensions.
	if containsIgnoreCase(normalizedName, "848x480") {
		if strings.Contains(normalizedName, "dvd") {
			result.source = SourceDVD
		} else if containsIgnoreCase(normalizedName, "bluray") {
			result.source = SourceBluRay
			result.resolution = Res480p
		} else {
			result.source = SourceTV
			result.resolution = Res480p
		}
		return result
	}

	if containsIgnoreCase(normalizedName, "1280x720") {
		if containsIgnoreCase(normalizedName, "bluray") {
			result.source = SourceBluRay
		} else {
			result.source = SourceTV
		}
		result.resolution = Res720p
		return result
	}

	if containsIgnoreCase(normalizedName, "1920x1080") {
		if containsIgnoreCase(normalizedName, "bluray") {
			result.source = SourceBluRay
		} else {
			result.source = SourceTV
		}
		result.resolution = Res1080p
		return result
	}

	// QualityParser.cs:631 — concatenated bluray + resolution.
	if containsIgnoreCase(normalizedName, "bluray720p") {
		result.source = SourceBluRay
		result.resolution = Res720p
		return result
	}
	if containsIgnoreCase(normalizedName, "bluray1080p") {
		result.source = SourceBluRay
		result.resolution = Res1080p
		return result
	}
	if containsIgnoreCase(normalizedName, "bluray2160p") {
		result.source = SourceBluRay
		result.resolution = Res2160p
		return result
	}

	// QualityParser.cs:658 — HD TV / SD TV with separators.
	if g := findFirst(otherSourceRegex, normalizedName); len(g) > 0 {
		if g["sdtv"] != "" {
			result.source = SourceTV
			result.resolution = Res480p
			return result
		}
		if g["hdtv"] != "" {
			result.source = SourceTV
			result.resolution = Res720p
			return result
		}
	}

	// QualityParser.cs:92 — extension fallback when nothing else matched.
	if result.source == SourceUnknown && result.resolution == ResUnknown && result.modifier == ModNone {
		ext := getQualityForExtension(name)
		if ext.source != SourceUnknown {
			result.source = ext.source
			result.resolution = ext.resolution
		}
	}

	return result
}

// findBySourceAndResolution ports QualityFinder.FindBySourceAndResolution
// (Qualities/QualityFinder.cs:11) for the cases the parser actually reaches:
// the (source, resolution, modifier) requested either names a real Quality or
// falls back to the nearest. In the orthogonal core there is nothing to "find" —
// the triple IS the quality — so this is the identity for every triple Radarr's
// Quality table defines, with DVD/DVDR special-cased the way the table does
// (DVD has resolution 0; DVDR carries REMUX at 480p).
func findBySourceAndResolution(source Source, resolution Resolution, modifier Modifier) (Source, Resolution, Modifier) {
	// DVD source has no per-resolution Bluray-style variants in Radarr's table:
	// the plain DVD quality is resolution-agnostic (Resolution 0). The extension
	// map only yields DVD from disc images, which carry no resolution.
	if source == SourceDVD {
		return SourceDVD, ResUnknown, ModNone
	}
	return source, resolution, modifier
}
