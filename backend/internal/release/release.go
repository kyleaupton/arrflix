package release

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Resolution string

const (
	ResUnknown Resolution = "Unknown"
	ResSD      Resolution = "SD"
	Res480p    Resolution = "480p"
	Res576p    Resolution = "576p"
	Res720p    Resolution = "720p"
	Res1080p   Resolution = "1080p"
	Res1440p   Resolution = "1440p"
	Res2160p   Resolution = "2160p"
	Res4320p   Resolution = "4320p"
)

type Source string

const (
	SourceUnknown Source = "Unknown"
	SourceSDTV    Source = "SDTV"
	SourceCAM     Source = "CAM"
	SourceTS      Source = "Telesync"
	SourceTC      Source = "Telecine"
	SourceSCR     Source = "Screener"
	SourceDVD     Source = "DVD"
	SourceDVDRip  Source = "DVD-Rip"
	SourceHDTV    Source = "HDTV"
	SourceWEBRip  Source = "WEBRip"
	SourceWEBDL   Source = "WEB-DL"
	SourceBluRay  Source = "BluRay"
	SourceREMUX   Source = "REMUX"
	SourceRAWHD   Source = "Raw-HD"
)

// Quality represents the Sonarr Quality ID
type Quality int

const (
	Unknown          Quality = 0
	SDTV             Quality = 1
	DVD              Quality = 2
	WEBDL1080p       Quality = 3
	HDTV720p         Quality = 4
	WEBDL720p        Quality = 5
	Bluray720p       Quality = 6
	Bluray1080p      Quality = 7
	WEBDL480p        Quality = 8
	HDTV1080p        Quality = 9
	RAWHD            Quality = 10
	WEBRip480p       Quality = 12
	Bluray480p       Quality = 13
	WEBRip720p       Quality = 14
	WEBRip1080p      Quality = 15
	HDTV2160p        Quality = 16
	WEBRip2160p      Quality = 17
	WEBDL2160p       Quality = 18
	Bluray2160p      Quality = 19
	Bluray1080pRemux Quality = 20
	Bluray2160pRemux Quality = 21
	Bluray576p       Quality = 22
)

func (q Quality) String() string {
	switch q {
	case Unknown:
		return "Unknown"
	case SDTV:
		return "SDTV"
	case DVD:
		return "DVD"
	case WEBDL1080p:
		return "WEBDL-1080p"
	case HDTV720p:
		return "HDTV-720p"
	case WEBDL720p:
		return "WEBDL-720p"
	case Bluray720p:
		return "Bluray-720p"
	case Bluray1080p:
		return "Bluray-1080p"
	case WEBDL480p:
		return "WEBDL-480p"
	case HDTV1080p:
		return "HDTV-1080p"
	case RAWHD:
		return "Raw-HD"
	case WEBRip480p:
		return "WEBRip-480p"
	case Bluray480p:
		return "Bluray-480p"
	case WEBRip720p:
		return "WEBRip-720p"
	case WEBRip1080p:
		return "WEBRip-1080p"
	case HDTV2160p:
		return "HDTV-2160p"
	case WEBRip2160p:
		return "WEBRip-2160p"
	case WEBDL2160p:
		return "WEBDL-2160p"
	case Bluray2160p:
		return "Bluray-2160p"
	case Bluray1080pRemux:
		return "Bluray-1080p Remux"
	case Bluray2160pRemux:
		return "Bluray-2160p Remux"
	case Bluray576p:
		return "Bluray-576p"
	default:
		return "Unknown"
	}
}

func (q Quality) Source() string {
	switch q {
	case SDTV:
		return string(SourceSDTV)
	case DVD:
		return string(SourceDVD)
	case WEBDL1080p, WEBDL720p, WEBDL480p, WEBDL2160p:
		return string(SourceWEBDL)
	case HDTV720p, HDTV1080p, HDTV2160p:
		return string(SourceHDTV)
	case Bluray720p, Bluray1080p, Bluray480p, Bluray2160p, Bluray576p, Bluray1080pRemux, Bluray2160pRemux:
		return string(SourceBluRay)
	case WEBRip480p, WEBRip720p, WEBRip1080p, WEBRip2160p:
		return string(SourceWEBRip)
	case RAWHD:
		return string(SourceRAWHD)
	default:
		return string(SourceUnknown)
	}
}

func (q Quality) Resolution() string {
	switch q {
	case WEBDL1080p, HDTV1080p, Bluray1080p, WEBRip1080p, Bluray1080pRemux:
		return string(Res1080p)
	case HDTV720p, WEBDL720p, Bluray720p, WEBRip720p:
		return string(Res720p)
	case WEBDL480p, Bluray480p, WEBRip480p:
		return string(Res480p)
	case HDTV2160p, WEBRip2160p, WEBDL2160p, Bluray2160p, Bluray2160pRemux:
		return string(Res2160p)
	case Bluray576p:
		return string(Res576p)
	case SDTV, DVD:
		return string(ResSD)
	default:
		return string(ResUnknown)
	}
}

func (q Quality) IsRemux() bool {
	return q == Bluray1080pRemux || q == Bluray2160pRemux
}

type Revision struct {
	Version  int
	Real     int
	IsRepack bool
}

// ParseResult contains all information parsed from a release title.
// It separates quality metrics (resolution, source, codecs) from release metadata
// (release group, edition) for semantic clarity.
type ParseResult struct {
	Quality QualityInfo
	Release ReleaseInfo
}

// QualityInfo represents encoding quality characteristics.
// This includes resolution, source quality, and version information.
type QualityInfo struct {
	Quality  Quality  // The core quality enum (resolution + source combination)
	Revision Revision // Version/repack information
}

// Source returns the source type (BluRay, WEB-DL, HDTV, etc.)
func (qi QualityInfo) Source() string {
	return qi.Quality.Source()
}

// Resolution returns the resolution string (720p, 1080p, 2160p, etc.)
func (qi QualityInfo) Resolution() string {
	return qi.Quality.Resolution()
}

// IsRemux returns true if this is a REMUX quality
func (qi QualityInfo) IsRemux() bool {
	return qi.Quality.IsRemux()
}

// Full returns the Radarr/Sonarr-style quality tag (e.g., "HDTV-720p", "WEBDL-1080p")
func (qi QualityInfo) Full() string {
	return qi.Quality.String()
}

// Version returns the revision version number
func (qi QualityInfo) Version() int {
	return qi.Revision.Version
}

// String returns a human-readable representation including revision info
func (qi QualityInfo) String() string {
	res := qi.Quality.String()
	if qi.Revision.Version > 1 {
		res += fmt.Sprintf(" v%d", qi.Revision.Version)
	}
	if qi.Revision.IsRepack {
		res += " [REPACK]"
	}
	return res
}

// ReleaseInfo represents release metadata (not quality-related).
// This includes information about who released it and what edition it is.
type ReleaseInfo struct {
	ReleaseGroup *string // Release group name (e.g., "DIMENSION", "NTb", "Tigole")
	Edition      *string // Movie edition (e.g., "Director's Cut", "Extended", "IMAX")
}

// GetReleaseGroup returns the release group name, or empty string if not found
func (ri ReleaseInfo) GetReleaseGroup() string {
	if ri.ReleaseGroup == nil {
		return ""
	}
	return *ri.ReleaseGroup
}

// GetEdition returns the edition string, or empty string if not found
func (ri ReleaseInfo) GetEdition() string {
	if ri.Edition == nil {
		return ""
	}
	return *ri.Edition
}

// QualityModel is the legacy struct that combines quality and release info.
// Deprecated: Use ParseResult with separate QualityInfo and ReleaseInfo instead.
type QualityModel struct {
	Quality      Quality
	Revision     Revision
	ReleaseGroup *string // Pointer allows nil (not found) vs "" (empty)
	Edition      *string // Movies only, nil for TV or not found
}

func (qm QualityModel) String() string {
	res := qm.Quality.String()
	if qm.Revision.Version > 1 {
		res += fmt.Sprintf(" v%d", qm.Revision.Version)
	}
	if qm.Revision.IsRepack {
		res += " [REPACK]"
	}
	return res
}

func (qm QualityModel) Source() string {
	return qm.Quality.Source()
}

func (qm QualityModel) Resolution() string {
	return qm.Quality.Resolution()
}

func (qm QualityModel) IsRemux() bool {
	return qm.Quality.IsRemux()
}

// Full returns the Radarr/Sonarr-style quality tag (e.g., "HDTV-720p", "WEBDL-1080p")
// without revision information. This matches what name templates expect: {{.Quality.Full}}
func (qm QualityModel) Full() string {
	return qm.Quality.String()
}

// Version returns the revision version number
func (qm QualityModel) Version() int {
	return qm.Revision.Version
}

// FieldInfo represents metadata about an available quality field
type FieldInfo struct {
	Name        string                 // Field name (e.g., "Full", "Resolution")
	Type        string                 // Field type: "string", "bool", "int"
	Description string                 // Human-readable description
	Accessor    func(QualityModel) any // Function to get the field value
}

// QualityFields is the registry of all available quality fields
var QualityFields = []FieldInfo{
	{
		Name:        "Full",
		Type:        "string",
		Description: "Full quality tag (e.g., HDTV-720p, WEBDL-1080p)",
		Accessor:    func(qm QualityModel) any { return qm.Full() },
	},
	{
		Name:        "Resolution",
		Type:        "string",
		Description: "Resolution value (e.g., 720p, 1080p, 2160p)",
		Accessor:    func(qm QualityModel) any { return qm.Resolution() },
	},
	{
		Name:        "Source",
		Type:        "string",
		Description: "Source type (e.g., HDTV, WEB-DL, BluRay)",
		Accessor:    func(qm QualityModel) any { return qm.Source() },
	},
	{
		Name:        "IsRemux",
		Type:        "bool",
		Description: "Whether the quality is a remux",
		Accessor:    func(qm QualityModel) any { return qm.IsRemux() },
	},
	{
		Name:        "IsRepack",
		Type:        "bool",
		Description: "Whether the release is a repack",
		Accessor:    func(qm QualityModel) any { return qm.Revision.IsRepack },
	},
	{
		Name:        "Version",
		Type:        "int",
		Description: "Revision version number",
		Accessor:    func(qm QualityModel) any { return qm.Version() },
	},
	{
		Name:        "ReleaseGroup",
		Type:        "string",
		Description: "Release group name (e.g., DIMENSION, NTb, Tigole)",
		Accessor: func(qm QualityModel) any {
			if qm.ReleaseGroup == nil {
				return ""
			}
			return *qm.ReleaseGroup
		},
	},
	{
		Name:        "Edition",
		Type:        "string",
		Description: "Movie edition (e.g., Director's Cut, Extended) - movies only",
		Accessor: func(qm QualityModel) any {
			if qm.Edition == nil {
				return ""
			}
			return *qm.Edition
		},
	},
}

// GetField retrieves a field value by name from a QualityModel
func GetField(name string, qm QualityModel) (any, error) {
	for _, field := range QualityFields {
		if field.Name == name {
			return field.Accessor(qm), nil
		}
	}
	return nil, fmt.Errorf("unknown quality field: %s", name)
}

// ListFields returns all available quality fields
func ListFields() []FieldInfo {
	return QualityFields
}

var (
	ResolutionRegex = regexp.MustCompile(`(?i)\b(?:(?P<R360p>360p)|(?P<R480p>480p|480i|640x480|848x480)|(?P<R540p>540p)|(?P<R576p>576p)|(?P<R720p>720p|1280x720|960p)|(?P<R1080p>1080p|1920x1080|1440p|FHD|1080i|4kto1080p)|(?P<R2160p>2160p|3840x2160|4k[-_. ](?:UHD|HEVC|BD|H265)|(?:UHD|HEVC|BD|H265)[-_. ]4k))\b`)

	// Simplified sources for Go
	// BD pattern: match BD followed by anything except end-of-string (Sonarr parity)
	// Handles: BluRay, Blu-Ray, HD-DVD, HDDVD, BDMux, and BD followed by any character
	BlurayRegex = regexp.MustCompile(`(?i)\b(BluRay|Blu-Ray|HD-?DVD|BDMux)\b|(?i)\bBD[^a-z]`)
	WebDlRegex  = regexp.MustCompile(`(?i)\b(WEB[-_. ]DL(?:mux)?|WEBDL|AmazonHD|AmazonSD|iTunesHD|MaxdomeHD|NetflixU?HD|WebHD|HBOMaxHD|DisneyHD|[. ]WEB[. ](?:[xh][ .]?26[45]|AVC|HEVC|DDP?5[. ]1)|[. ]WEB$|(?:720|1080|2160)p[-. ]WEB[-. ]|[-. ]WEB[-. ](?:720|1080|2160)p|\b\s\/\sWEB\s\/\s\b|(?:AMZN|NF|DP)[. -]WEB[. -])`)
	WebRipRegex = regexp.MustCompile(`(?i)\b(WebRip|Web-Rip|WEBMux)\b`)
	HdtvRegex   = regexp.MustCompile(`(?i)\b(HDTV)\b`)
	DvdRegex    = regexp.MustCompile(`(?i)\b(DVD|DVDRip|NTSC|PAL|xvidvd)\b`)

	ProperRegex  = regexp.MustCompile(`(?i)\bproper\b`)
	RepackRegex  = regexp.MustCompile(`(?i)\b(repack\d?|rerip\d?)\b`)
	VersionRegex = regexp.MustCompile(`(?i)\d[-._ ]?v(?P<version>\d)[-._ ]|\[v(?P<version>\d)\]|repack(?P<version>\d)|rerip(?P<version>\d)|(?:480|576|720|1080|2160)p[._ ]v(?P<version>\d)`)

	RemuxRegex = regexp.MustCompile(`(?i)(?:[_. ]|\d{4}p-|\bHybrid-)(?P<remux>(?:(BD|UHD)[-_. ]?)?Remux)\b|(?P<remux>(?:(BD|UHD)[-_. ]?)?Remux[_. ]\d{4}p)`)

	// Raw-HD detection (Sonarr parity)
	RawHDRegex = regexp.MustCompile(`(?i)\b(?:RawHD|Raw[-_. ]HD)\b`)
	MPEG2Regex = regexp.MustCompile(`(?i)\bMPEG[-_. ]?2\b`)

	// Additional source types (Sonarr parity)
	BDRipRegex = regexp.MustCompile(`(?i)\b(?:BDRip|BDLight)\b`)
	BRRipRegex = regexp.MustCompile(`(?i)\bBRRip\b`)
	PDTVRegex  = regexp.MustCompile(`(?i)\bPDTV\b`)
	DSRRegex   = regexp.MustCompile(`(?i)\b(?:WS[-_. ]DSR|DSR)\b`)
	TVRipRegex = regexp.MustCompile(`(?i)\bTVRip\b`)
	SDTVRegex  = regexp.MustCompile(`(?i)\bSDTV\b`)

	// Alternative resolution detection (Sonarr parity)
	// Used when primary resolution regex doesn't match
	AltResolutionRegex = regexp.MustCompile(`(?i)\b(?:UHD)\b|\[4K\]`)

	// Anime-specific patterns (Sonarr parity)
	AnimeBlurayRegex = regexp.MustCompile(`(?i)bd(?:720|1080|2160)|[-_. (\[]bd[-_. )\]]`)
	AnimeWebDlRegex  = regexp.MustCompile(`(?i)\[WEB\]|[\[\(]WEB[ .]`)

	// Codec detection (Sonarr parity)
	CodecRegex = regexp.MustCompile(`(?i)\b(?:(?P<x264>x264)|(?P<h264>h264)|(?P<xvidhd>XvidHD)|(?P<xvid>Xvid)|(?P<divx>divx))\b`)

	// Other source patterns (HD TV with space, SD TV)
	OtherSourceRegex = regexp.MustCompile(`(?i)(?P<hdtv>HD[-_. ]TV)|(?P<sdtv>SD[-_. ]TV)`)

	// High-def PDTV (HR WS = High Resolution Widescreen)
	HighDefPdtvRegex = regexp.MustCompile(`(?i)hr[-_. ]ws`)

	// Release Group Patterns (Radarr + Sonarr unified)
	// Main release group regex - captures groups like -GROUP or [GROUP]
	// Simplified for Go's RE2 engine (no lookahead/lookbehind support)
	// Note: Dotted release groups like YTS.LT, YTS.MX are handled via exception lists
	ReleaseGroupRegex = regexp.MustCompile(`(?i)-([a-z0-9]+(?:-[a-z0-9]+)?)(?:\b|[-._ ]|$)|[-._ ]\[([a-z0-9]+)\]$`)

	// Anime-style release groups: [SubGroup] Title
	// Simplified for Go's RE2 engine
	AnimeReleaseGroupRegex = regexp.MustCompile(`(?i)^\[([^\]]+)\](?:_|-|\s|\.)?`)

	// Invalid release groups to filter (season/episode codes, hex strings)
	InvalidReleaseGroupRegex = regexp.MustCompile(`(?i)^([se]\d+|[0-9a-f]{8})$`)

	// Clean suffixes before parsing (RP, postbot, Rakuv*, etc.)
	CleanReleaseGroupRegex = regexp.MustCompile(`(?i)(-(RP|1|NZBGeek|Obfuscated|Obfuscation|Scrambled|sample|Pre|postbot|xpost|Rakuv[a-z0-9]*|WhiteRev|BUYMORE|AsRequested|AlternativeToRequested|GEROV|Z0iDS3N|Chamele0n|4P|4Planet|AlteZachen|RePACKPOST))+$`)

	// Website prefix removal (www.site.com prefix)
	WebsitePrefixRegex = regexp.MustCompile(`(?i)^(?:(?:\[|\()\s*)?(?:www\.)?[-a-z0-9]{1,256}\.(?:[a-z]{2,6}\.[a-z]{2,6}|[a-z]{2,})(?:\s*(?:\]|\))|[ -]{2,})[ -]*`)

	// Torrent tracker suffix removal (including .com variants)
	CleanTorrentSuffixRegex = regexp.MustCompile(`(?i)\[(?:ettv|rartv|rarbg|cttv|publichd)(?:\.com)?\]$`)

	// Edition pattern from Radarr - simplified for Go's RE2 engine
	// Captures Director's Cut, Extended, IMAX, etc.
	EditionRegex = regexp.MustCompile(`(?i)\(?\b((?:(?:Recut|Extended|Ultimate)[. ])?(?:Director.?s|Collector.?s|Theatrical|Ultimate|Extended|Despecialized|Special|Rouge|Final|Assembly|Imperial|Diamond|Signature|Hunter|Rekall|Uncensored|Remastered|Unrated|Uncut|IMAX|Fan[. ]?Edit|Restored|[23]in1|\d{2,3}(?:th)? Anniversary)[. ]?(?:Cut|Edition|Version)?(?:[. ](?:Extended|Uncensored|Remastered|Unrated|Uncut|Open[. ]?Matte|IMAX|Fan[. ]?Edit))?|(?:Open[. ]?Matte|4in1))\b\)?`)
)

// Exception groups that don't follow standard -GROUP pattern (exact matches)
// Combined from Radarr and Sonarr
var releaseGroupExceptionsExact = map[string]bool{
	// Radarr exceptions
	"KRaLiMaRKo": true, "E.N.D": true, "D-Z0N3": true, "Koten_Gars": true,
	"BluDragon": true, "ZØNEHD": true, "HQMUX": true, "VARYG": true,
	"YIFY": true, "YTS": true, "YTS.MX": true, "YTS.LT": true, "YTS.AG": true,
	"TMd": true, "Eml HDTeam": true, "LMain": true, "DarQ": true,
	"BEN THE MEN": true, "TAoE": true, "QxR": true, "Fight-BB": true,
	"KCRT": true, "Vialle": true, "126811": true,
}

// Exception groups whose releases end with GROUP) or GROUP]
// Combined from Radarr and Sonarr
var releaseGroupExceptionsPattern = map[string]bool{
	// Radarr exceptions
	"Silence": true, "afm72": true, "Panda": true, "Ghost": true,
	"MONOLITH": true, "Tigole": true, "Joy": true, "ImE": true,
	"UTR": true, "t3nzin": true, "Anime Time": true, "Project Angel": true,
	"Hakata Ramen": true, "HONE": true, "GiLG": true, "Vyndros": true,
	"SEV": true, "Garshasp": true, "Kappa": true, "Natty": true,
	"RCVR": true, "SAMPA": true, "YOGI": true, "r00t": true,
	"EDGE2020": true, "RZeroX": true, "FreetheFish": true, "Anna": true,
	"Bandi": true, "Qman": true, "theincognito": true, "HDO": true,
	"DusIctv": true, "DHD": true, "CtrlHD": true, "-ZR-": true,
	"ADC": true, "XZVN": true, "RH": true, "Kametsu": true,
}

// Extension-based quality mapping (Sonarr parity)
var extensionQualityMap = map[string]Quality{
	// SDTV extensions
	".avi":    SDTV,
	".m4v":    SDTV,
	".3gp":    SDTV,
	".nsv":    SDTV,
	".ty":     SDTV,
	".strm":   SDTV,
	".rm":     SDTV,
	".rmvb":   SDTV,
	".m3u":    SDTV,
	".ifo":    SDTV,
	".mov":    SDTV,
	".qt":     SDTV,
	".divx":   SDTV,
	".xvid":   SDTV,
	".bivx":   SDTV,
	".nrg":    SDTV,
	".pva":    SDTV,
	".wmv":    SDTV,
	".asf":    SDTV,
	".asx":    SDTV,
	".ogm":    SDTV,
	".ogv":    SDTV,
	".m2v":    SDTV,
	".bin":    SDTV,
	".dat":    SDTV,
	".dvr-ms": SDTV,
	".mpg":    SDTV,
	".mpeg":   SDTV,
	".mp4":    SDTV,
	".avc":    SDTV,
	".vp3":    SDTV,
	".svq3":   SDTV,
	".nuv":    SDTV,
	".viv":    SDTV,
	".dv":     SDTV,
	".fli":    SDTV,
	".flv":    SDTV,
	".wpl":    SDTV,
	// DVD extensions
	".img": DVD,
	".iso": DVD,
	".vob": DVD,
	// HD extensions
	".mkv": HDTV720p,
	".ts":  HDTV720p,
	".wtv": HDTV720p,
	// Bluray extensions
	".m2ts": Bluray720p,
}

// getQualityForExtension returns quality based on file extension
func getQualityForExtension(name string) Quality {
	// Find extension
	lastDot := strings.LastIndex(name, ".")
	if lastDot == -1 || lastDot == len(name)-1 {
		return Unknown
	}
	ext := strings.ToLower(name[lastDot:])
	if q, ok := extensionQualityMap[ext]; ok {
		return q
	}
	return Unknown
}

// removeFileExtension removes common video file extensions
func removeFileExtension(title string) string {
	videoExts := []string{
		".mkv", ".mp4", ".avi", ".m4v", ".mov", ".wmv", ".flv",
		".ts", ".m2ts", ".vob", ".iso", ".img", ".mpg", ".mpeg",
	}

	titleLower := strings.ToLower(title)
	for _, ext := range videoExts {
		if strings.HasSuffix(titleLower, ext) {
			return title[:len(title)-len(ext)]
		}
	}
	return title
}

// parseReleaseGroup extracts the release group from a title
// Returns pointer to group name, or nil if not found
// Follows Radarr/Sonarr parsing logic with unified exception lists
func parseReleaseGroup(title string) *string {
	title = strings.TrimSpace(title)

	// Remove file extension
	title = removeFileExtension(title)

	// Clean website prefixes and torrent suffixes
	title = WebsitePrefixRegex.ReplaceAllString(title, "")
	title = CleanTorrentSuffixRegex.ReplaceAllString(title, "")

	// Priority 1: Check anime format [SubGroup] - highest priority
	if match := AnimeReleaseGroupRegex.FindStringSubmatch(title); match != nil {
		if len(match) > 1 && match[1] != "" {
			group := strings.TrimSpace(match[1])
			return &group
		}
	}

	// Clean known suffixes
	title = CleanReleaseGroupRegex.ReplaceAllString(title, "")

	// Priority 2: Check exact exceptions (case-insensitive)
	// Sort by length (longest first) to match YTS.LT before YTS, etc.
	titleLower := strings.ToLower(title)
	exceptions := make([]string, 0, len(releaseGroupExceptionsExact))
	for exception := range releaseGroupExceptionsExact {
		exceptions = append(exceptions, exception)
	}
	// Sort by length descending
	for i := 0; i < len(exceptions); i++ {
		for j := i + 1; j < len(exceptions); j++ {
			if len(exceptions[j]) > len(exceptions[i]) {
				exceptions[i], exceptions[j] = exceptions[j], exceptions[i]
			}
		}
	}

	for _, exception := range exceptions {
		exceptionLower := strings.ToLower(exception)
		// Find all occurrences
		if strings.Contains(titleLower, exceptionLower) {
			// Return last match (Radarr behavior)
			lastIdx := strings.LastIndex(titleLower, exceptionLower)
			group := title[lastIdx : lastIdx+len(exception)]
			return &group
		}
	}

	// Priority 3: Check pattern exceptions (groups ending with ) or ])
	for exception := range releaseGroupExceptionsPattern {
		// Look for pattern: [._ []GROUP[)\]]
		pattern := regexp.MustCompile(`(?i)[._ \[]` + regexp.QuoteMeta(exception) + `(?:\)|\])`)
		if matches := pattern.FindAllString(title, -1); len(matches) > 0 {
			// Return last match, extract just the group name
			group := exception
			return &group
		}
	}

	// Priority 4: Main regex pattern
	// Filter list: patterns that look like release groups but aren't
	invalidPatterns := []string{
		"480p", "576p", "720p", "1080p", "1440p", "2160p", "4320p",
		"WEB-DL", "WEBDL", "WEB-Rip", "WEBRip", "Blu-Ray", "BluRay",
		"DTS-HD", "DTS-X", "DTS-MA", "DTS-ES", "DTS",
		"HDTV", "SDTV", "PDTV",
		"DL", "Rip", "HD", "MA", "ES", "X", "bit",
		"REMUX", "AVC", "HEVC", "H264", "H265", "x264", "x265",
		"DD", "DDP", "AAC", "FLAC", "TrueHD", "Atmos",
		"HDR", "HDR10", "DV", "Dolby",
	}

	if matches := ReleaseGroupRegex.FindAllStringSubmatch(title, -1); len(matches) > 0 {
		// Take last match (Radarr behavior)
		lastMatch := matches[len(matches)-1]

		// Check both capture groups (group 1 for -GROUP, group 2 for [GROUP])
		var group string
		var isBracketGroup bool
		if len(lastMatch) > 1 && lastMatch[1] != "" {
			group = lastMatch[1]
			isBracketGroup = false
		} else if len(lastMatch) > 2 && lastMatch[2] != "" {
			group = lastMatch[2]
			isBracketGroup = true
		}

		if group != "" {
			// Filter: reject if all numeric
			if _, err := strconv.Atoi(group); err == nil {
				return nil
			}

			// Filter: reject if invalid pattern (s01, e04, hex hashes)
			if InvalidReleaseGroupRegex.MatchString(group) {
				return nil
			}

			// Filter: reject if it matches known invalid patterns
			groupLower := strings.ToLower(group)
			for _, invalid := range invalidPatterns {
				if groupLower == strings.ToLower(invalid) {
					return nil
				}
			}

			// Filter: reject if it's a date pattern (MM-DD, YYYY-MM, or just MM)
			if matched, _ := regexp.MatchString(`^\d{1,4}-\d{1,2}$`, group); matched {
				return nil
			}
			if matched, _ := regexp.MatchString(`^\d{1,2}$`, group); matched {
				return nil
			}

			// Filter: reject if it's a language code (EN, ES, FR, etc.)
			if matched, _ := regexp.MatchString(`^(?:EN|ES|CAT|ENG|JAP|GER|FRA|FRE|ITA)$`, strings.ToUpper(group)); matched {
				return nil
			}

			// Filter: reject if it's a bit depth (10-bit, 8-bit, etc.)
			if matched, _ := regexp.MatchString(`^\d{1,2}-bit$`, strings.ToLower(group)); matched {
				return nil
			}

			// Filter: reject single or two letter groups (too ambiguous)
			// But allow bracket groups since they're more explicit
			if !isBracketGroup && len(group) <= 2 {
				return nil
			}

			return &group
		}
	}

	return nil
}

// parseEdition extracts movie edition from title
// Returns pointer to edition string, or nil if not found
// Follows Radarr implementation - movies only
func parseEdition(title string) *string {
	matches := EditionRegex.FindAllStringSubmatchIndex(title, -1)
	if len(matches) == 0 {
		return nil
	}

	// Check the last match (Radarr behavior - take last occurrence)
	lastMatch := matches[len(matches)-1]
	if len(lastMatch) > 3 {
		edition := title[lastMatch[2]:lastMatch[3]] // capture group 1

		// Replace dots with spaces and trim (Radarr behavior)
		edition = strings.ReplaceAll(edition, ".", " ")
		edition = strings.TrimSpace(edition)

		// Reject if edition words appear at the very start of title
		// (e.g., "Directors.Cut.German.2006" - Directors Cut is the title, not edition)
		titleStart := strings.TrimSpace(title)
		editionNorm := strings.ToLower(strings.ReplaceAll(edition, " ", ""))
		titleNorm := strings.ToLower(strings.ReplaceAll(titleStart[:min(len(titleStart), len(edition)+5)], ".", ""))
		titleNorm = strings.ReplaceAll(titleNorm, " ", "")

		if strings.HasPrefix(titleNorm, editionNorm) {
			// Edition appears at the start - likely part of title, not edition tag
			return nil
		}

		// Reject if standalone single-word edition (without Cut/Edition/Version) is immediately
		// followed by a year AND preceded by another word connected by a dot
		// e.g., "Movie.Holiday.Special.1978" - "Special" is part of "Holiday Special" title
		// But allow "Movie Title Extended 2012" - space-separated, Extended is edition
		hasEditionSuffix := strings.Contains(strings.ToLower(edition), "cut") ||
			strings.Contains(strings.ToLower(edition), "edition") ||
			strings.Contains(strings.ToLower(edition), "version")

		if !hasEditionSuffix {
			// Check if followed by year
			afterMatch := title[lastMatch[3]:]
			if matched, _ := regexp.MatchString(`^[.\s]*(19|20)\d{2}`, afterMatch); matched {
				// Check if preceded by a word connected by a DOT (not space)
				beforeMatch := ""
				if lastMatch[2] > 0 {
					beforeMatch = title[:lastMatch[2]]
				}
				// Only reject if there's a word.dot pattern immediately before
				// (like "Holiday." in "Holiday.Special")
				if matched, _ := regexp.MatchString(`[a-zA-Z]+\.\s*$`, beforeMatch); matched {
					return nil
				}
			}
		}

		return &edition
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func ParseResolution(name string) int {
	match := ResolutionRegex.FindStringSubmatch(name)
	if match != nil {
		for i, groupName := range ResolutionRegex.SubexpNames() {
			if i != 0 && groupName != "" && match[i] != "" {
				switch groupName {
				case "R360p":
					return 360
				case "R480p":
					return 480
				case "R540p":
					return 540
				case "R576p":
					return 576
				case "R720p":
					return 720
				case "R1080p":
					return 1080
				case "R2160p":
					return 2160
				}
			}
		}
	}

	// Fallback: check alternative resolution patterns (UHD, [4K])
	if AltResolutionRegex.MatchString(name) {
		return 2160
	}

	return 0
}

// Parse extracts quality and release information from a release title.
// It returns a ParseResult containing separated QualityInfo (resolution, source, etc.)
// and ReleaseInfo (release group, edition).
func Parse(name string) ParseResult {
	normalizedName := strings.ReplaceAll(name, "_", " ")
	normalizedName = strings.TrimSpace(normalizedName)

	result := ParseResult{
		Quality: QualityInfo{Quality: Unknown},
	}

	// Parse Revision
	if vMatch := VersionRegex.FindStringSubmatch(normalizedName); vMatch != nil {
		for i, groupName := range VersionRegex.SubexpNames() {
			if groupName == "version" && vMatch[i] != "" {
				v, _ := strconv.Atoi(vMatch[i])
				result.Quality.Revision.Version = v
			}
		}
	}

	if ProperRegex.MatchString(normalizedName) {
		if result.Quality.Revision.Version < 2 {
			result.Quality.Revision.Version = 2
		} else {
			result.Quality.Revision.Version++
		}
	}

	if RepackRegex.MatchString(normalizedName) {
		result.Quality.Revision.IsRepack = true
		if result.Quality.Revision.Version < 2 {
			result.Quality.Revision.Version = 2
		} else {
			result.Quality.Revision.Version++
		}
	}

	// Parse release group (use original name to preserve formatting)
	result.Release.ReleaseGroup = parseReleaseGroup(name)

	// Parse edition (movies only - parsed for all but primarily used for movies)
	result.Release.Edition = parseEdition(normalizedName)

	resolution := ParseResolution(normalizedName)
	remuxMatch := RemuxRegex.MatchString(normalizedName)
	codecMatch := CodecRegex.FindStringSubmatch(normalizedName)

	// Check for xvid/divx codec
	hasXvid := false
	hasDivx := false
	hasX264 := false
	if codecMatch != nil {
		for i, gn := range CodecRegex.SubexpNames() {
			if codecMatch[i] != "" {
				switch gn {
				case "xvid":
					hasXvid = true
				case "divx":
					hasDivx = true
				case "x264":
					hasX264 = true
				}
			}
		}
	}

	// Raw-HD detection (check early, before source detection)
	if RawHDRegex.MatchString(normalizedName) {
		result.Quality.Quality = RAWHD
		return result
	}

	// Source Detection
	if BlurayRegex.MatchString(normalizedName) {
		// XviD/DivX + Bluray = Bluray-480p (Sonarr parity)
		if hasXvid || hasDivx {
			result.Quality.Quality = Bluray480p
			return result
		}
		switch resolution {
		case 2160:
			if remuxMatch {
				result.Quality.Quality = Bluray2160pRemux
			} else {
				result.Quality.Quality = Bluray2160p
			}
		case 1080:
			if remuxMatch {
				result.Quality.Quality = Bluray1080pRemux
			} else {
				result.Quality.Quality = Bluray1080p
			}
		case 720:
			result.Quality.Quality = Bluray720p
		case 576:
			result.Quality.Quality = Bluray576p
		case 480, 360, 540:
			result.Quality.Quality = Bluray480p
		default:
			// Treat a remux without resolution as 1080p, not 720p
			// 720p remux should fallback as 720p BluRay
			if remuxMatch {
				result.Quality.Quality = Bluray1080pRemux
			} else {
				result.Quality.Quality = Bluray720p
			}
		}
		return result
	}

	if WebDlRegex.MatchString(normalizedName) && !WebRipRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			result.Quality.Quality = WEBDL2160p
		case 1080:
			result.Quality.Quality = WEBDL1080p
		case 720:
			result.Quality.Quality = WEBDL720p
		default:
			// [WEBDL] bracket without resolution defaults to 720p
			if strings.Contains(name, "[WEBDL]") {
				result.Quality.Quality = WEBDL720p
			} else {
				result.Quality.Quality = WEBDL480p
			}
		}
		return result
	}

	if WebRipRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			result.Quality.Quality = WEBRip2160p
		case 1080:
			result.Quality.Quality = WEBRip1080p
		case 720:
			result.Quality.Quality = WEBRip720p
		default:
			result.Quality.Quality = WEBRip480p
		}
		return result
	}

	if HdtvRegex.MatchString(normalizedName) {
		// HDTV + MPEG2 = Raw-HD (uncompressed HDTV capture)
		if MPEG2Regex.MatchString(normalizedName) {
			result.Quality.Quality = RAWHD
			return result
		}
		switch resolution {
		case 2160:
			result.Quality.Quality = HDTV2160p
		case 1080:
			result.Quality.Quality = HDTV1080p
		case 720:
			result.Quality.Quality = HDTV720p
		default:
			// [HDTV] bracket without resolution defaults to 720p
			if strings.Contains(name, "[HDTV]") {
				result.Quality.Quality = HDTV720p
			} else {
				result.Quality.Quality = SDTV
			}
		}
		return result
	}

	if DvdRegex.MatchString(normalizedName) {
		result.Quality.Quality = DVD
		return result
	}

	// BDRip/BRRip → Bluray (Sonarr parity)
	if BDRipRegex.MatchString(normalizedName) || BRRipRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			result.Quality.Quality = Bluray2160p
		case 1080:
			result.Quality.Quality = Bluray1080p
		case 720:
			result.Quality.Quality = Bluray720p
		default:
			result.Quality.Quality = Bluray480p
		}
		return result
	}

	// PDTV/SDTV/DSR/TVRip → SDTV or HDTV by resolution (Sonarr parity)
	if PDTVRegex.MatchString(normalizedName) || SDTVRegex.MatchString(normalizedName) ||
		DSRRegex.MatchString(normalizedName) || TVRipRegex.MatchString(normalizedName) {
		switch resolution {
		case 1080:
			result.Quality.Quality = HDTV1080p
		case 720:
			result.Quality.Quality = HDTV720p
		default:
			// HR.WS (High Resolution Widescreen) PDTV = 720p
			if HighDefPdtvRegex.MatchString(normalizedName) {
				result.Quality.Quality = HDTV720p
			} else {
				result.Quality.Quality = SDTV
			}
		}
		return result
	}

	// Remux without source detection (Sonarr parity)
	// When remux is detected but no source, infer from resolution
	if remuxMatch && resolution != 0 {
		switch resolution {
		case 480:
			result.Quality.Quality = Bluray480p
		case 720:
			result.Quality.Quality = Bluray720p
		case 1080:
			result.Quality.Quality = Bluray1080pRemux
		case 2160:
			result.Quality.Quality = Bluray2160pRemux
		}
		if result.Quality.Quality != Unknown {
			return result
		}
	}

	// Anime BluRay pattern (e.g., [Group] Title - 01 [BD 1080p])
	if AnimeBlurayRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			if remuxMatch {
				result.Quality.Quality = Bluray2160pRemux
			} else {
				result.Quality.Quality = Bluray2160p
			}
		case 1080:
			if remuxMatch {
				result.Quality.Quality = Bluray1080pRemux
			} else {
				result.Quality.Quality = Bluray1080p
			}
		case 720:
			result.Quality.Quality = Bluray720p
		case 360, 480, 540, 576:
			result.Quality.Quality = DVD
		default:
			if remuxMatch {
				result.Quality.Quality = Bluray1080pRemux
			} else {
				result.Quality.Quality = Bluray720p
			}
		}
		return result
	}

	// Anime WEB-DL pattern (e.g., [Group] Title - 01 [WEB 1080p])
	if AnimeWebDlRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			result.Quality.Quality = WEBDL2160p
		case 1080:
			result.Quality.Quality = WEBDL1080p
		case 720:
			result.Quality.Quality = WEBDL720p
		case 360, 480, 540, 576:
			result.Quality.Quality = WEBDL480p
		default:
			result.Quality.Quality = WEBDL720p
		}
		return result
	}

	// Resolution-only fallback with extension-based source detection
	if resolution != 0 {
		// 540p without a recognized source returns Unknown (Sonarr parity)
		// Extension fallback should NOT apply to 540p
		if resolution == 540 {
			// Leave as Unknown - Sonarr does not assign quality to 540p without source
			return result
		}

		// Get source from extension
		extQuality := getQualityForExtension(name)
		switch resolution {
		case 2160:
			if extQuality == Bluray720p {
				if remuxMatch {
					result.Quality.Quality = Bluray2160pRemux
				} else {
					result.Quality.Quality = Bluray2160p
				}
			} else {
				result.Quality.Quality = HDTV2160p
			}
		case 1080:
			if extQuality == Bluray720p {
				if remuxMatch {
					result.Quality.Quality = Bluray1080pRemux
				} else {
					result.Quality.Quality = Bluray1080p
				}
			} else {
				result.Quality.Quality = HDTV1080p
			}
		case 720:
			if extQuality == Bluray720p {
				result.Quality.Quality = Bluray720p
			} else {
				result.Quality.Quality = HDTV720p
			}
		case 360, 480, 576:
			if extQuality == Bluray720p {
				result.Quality.Quality = Bluray480p
			} else {
				result.Quality.Quality = SDTV
			}
		}
		if result.Quality.Quality != Unknown {
			return result
		}
	}

	// x264 codec fallback → SDTV (Sonarr parity)
	if hasX264 {
		result.Quality.Quality = SDTV
		return result
	}

	// Concatenated bluray patterns (bluray720p, bluray1080p, bluray2160p)
	normalizedLower := strings.ToLower(normalizedName)
	if strings.Contains(normalizedLower, "bluray720p") {
		result.Quality.Quality = Bluray720p
		return result
	}
	if strings.Contains(normalizedLower, "bluray1080p") {
		result.Quality.Quality = Bluray1080p
		return result
	}
	if strings.Contains(normalizedLower, "bluray2160p") {
		result.Quality.Quality = Bluray2160p
		return result
	}

	// HD TV / SD TV patterns (with space/separator)
	otherMatch := OtherSourceRegex.FindStringSubmatch(normalizedName)
	if otherMatch != nil {
		for i, gn := range OtherSourceRegex.SubexpNames() {
			if otherMatch[i] != "" {
				switch gn {
				case "hdtv":
					result.Quality.Quality = HDTV720p
					return result
				case "sdtv":
					result.Quality.Quality = SDTV
					return result
				}
			}
		}
	}

	// Extension-based fallback (Sonarr parity)
	// If we still have Unknown quality, try to determine from extension
	if result.Quality.Quality == Unknown {
		extQuality := getQualityForExtension(name)
		if extQuality != Unknown {
			result.Quality.Quality = extQuality
		}
	}

	return result
}
