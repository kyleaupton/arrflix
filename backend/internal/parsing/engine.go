package parsing

import (
	"regexp"
	"strconv"
	"strings"
)

// rawParse is the engine's internal result: the ported quality half (quality
// bin + revision). Parse wraps it into the ParsedRelease model.
type rawParse struct {
	quality  Quality
	revision Revision
}

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
)

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

// parseRaw extracts quality, revision, release group, and edition from a
// release title or filename. It is the ported Sonarr/Radarr quality engine;
// Parse wraps its output into the ParsedRelease model.
func parseRaw(name string) rawParse {
	normalizedName := strings.ReplaceAll(name, "_", " ")
	normalizedName = strings.TrimSpace(normalizedName)

	result := rawParse{quality: Unknown}

	// Parse Revision
	if vMatch := VersionRegex.FindStringSubmatch(normalizedName); vMatch != nil {
		for i, groupName := range VersionRegex.SubexpNames() {
			if groupName == "version" && vMatch[i] != "" {
				v, _ := strconv.Atoi(vMatch[i])
				result.revision.Version = v
			}
		}
	}

	if ProperRegex.MatchString(normalizedName) {
		if result.revision.Version < 2 {
			result.revision.Version = 2
		} else {
			result.revision.Version++
		}
	}

	if RepackRegex.MatchString(normalizedName) {
		result.revision.IsRepack = true
		if result.revision.Version < 2 {
			result.revision.Version = 2
		} else {
			result.revision.Version++
		}
	}

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
		result.quality = RAWHD
		return result
	}

	// Source Detection
	if BlurayRegex.MatchString(normalizedName) {
		// XviD/DivX + Bluray = Bluray-480p (Sonarr parity)
		if hasXvid || hasDivx {
			result.quality = Bluray480p
			return result
		}
		switch resolution {
		case 2160:
			if remuxMatch {
				result.quality = Bluray2160pRemux
			} else {
				result.quality = Bluray2160p
			}
		case 1080:
			if remuxMatch {
				result.quality = Bluray1080pRemux
			} else {
				result.quality = Bluray1080p
			}
		case 720:
			result.quality = Bluray720p
		case 576:
			result.quality = Bluray576p
		case 480, 360, 540:
			result.quality = Bluray480p
		default:
			// Treat a remux without resolution as 1080p, not 720p
			// 720p remux should fallback as 720p BluRay
			if remuxMatch {
				result.quality = Bluray1080pRemux
			} else {
				result.quality = Bluray720p
			}
		}
		return result
	}

	if WebDlRegex.MatchString(normalizedName) && !WebRipRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			result.quality = WEBDL2160p
		case 1080:
			result.quality = WEBDL1080p
		case 720:
			result.quality = WEBDL720p
		default:
			// [WEBDL] bracket without resolution defaults to 720p
			if strings.Contains(name, "[WEBDL]") {
				result.quality = WEBDL720p
			} else {
				result.quality = WEBDL480p
			}
		}
		return result
	}

	if WebRipRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			result.quality = WEBRip2160p
		case 1080:
			result.quality = WEBRip1080p
		case 720:
			result.quality = WEBRip720p
		default:
			result.quality = WEBRip480p
		}
		return result
	}

	if HdtvRegex.MatchString(normalizedName) {
		// HDTV + MPEG2 = Raw-HD (uncompressed HDTV capture)
		if MPEG2Regex.MatchString(normalizedName) {
			result.quality = RAWHD
			return result
		}
		switch resolution {
		case 2160:
			result.quality = HDTV2160p
		case 1080:
			result.quality = HDTV1080p
		case 720:
			result.quality = HDTV720p
		default:
			// [HDTV] bracket without resolution defaults to 720p
			if strings.Contains(name, "[HDTV]") {
				result.quality = HDTV720p
			} else {
				result.quality = SDTV
			}
		}
		return result
	}

	if DvdRegex.MatchString(normalizedName) {
		result.quality = DVD
		return result
	}

	// BDRip/BRRip → Bluray (Sonarr parity)
	if BDRipRegex.MatchString(normalizedName) || BRRipRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			result.quality = Bluray2160p
		case 1080:
			result.quality = Bluray1080p
		case 720:
			result.quality = Bluray720p
		default:
			result.quality = Bluray480p
		}
		return result
	}

	// PDTV/SDTV/DSR/TVRip → SDTV or HDTV by resolution (Sonarr parity)
	if PDTVRegex.MatchString(normalizedName) || SDTVRegex.MatchString(normalizedName) ||
		DSRRegex.MatchString(normalizedName) || TVRipRegex.MatchString(normalizedName) {
		switch resolution {
		case 1080:
			result.quality = HDTV1080p
		case 720:
			result.quality = HDTV720p
		default:
			// HR.WS (High Resolution Widescreen) PDTV = 720p
			if HighDefPdtvRegex.MatchString(normalizedName) {
				result.quality = HDTV720p
			} else {
				result.quality = SDTV
			}
		}
		return result
	}

	// Remux without source detection (Sonarr parity)
	// When remux is detected but no source, infer from resolution
	if remuxMatch && resolution != 0 {
		switch resolution {
		case 480:
			result.quality = Bluray480p
		case 720:
			result.quality = Bluray720p
		case 1080:
			result.quality = Bluray1080pRemux
		case 2160:
			result.quality = Bluray2160pRemux
		}
		if result.quality != Unknown {
			return result
		}
	}

	// Anime BluRay pattern (e.g., [Group] Title - 01 [BD 1080p])
	if AnimeBlurayRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			if remuxMatch {
				result.quality = Bluray2160pRemux
			} else {
				result.quality = Bluray2160p
			}
		case 1080:
			if remuxMatch {
				result.quality = Bluray1080pRemux
			} else {
				result.quality = Bluray1080p
			}
		case 720:
			result.quality = Bluray720p
		case 360, 480, 540, 576:
			result.quality = DVD
		default:
			if remuxMatch {
				result.quality = Bluray1080pRemux
			} else {
				result.quality = Bluray720p
			}
		}
		return result
	}

	// Anime WEB-DL pattern (e.g., [Group] Title - 01 [WEB 1080p])
	if AnimeWebDlRegex.MatchString(normalizedName) {
		switch resolution {
		case 2160:
			result.quality = WEBDL2160p
		case 1080:
			result.quality = WEBDL1080p
		case 720:
			result.quality = WEBDL720p
		case 360, 480, 540, 576:
			result.quality = WEBDL480p
		default:
			result.quality = WEBDL720p
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
					result.quality = Bluray2160pRemux
				} else {
					result.quality = Bluray2160p
				}
			} else {
				result.quality = HDTV2160p
			}
		case 1080:
			if extQuality == Bluray720p {
				if remuxMatch {
					result.quality = Bluray1080pRemux
				} else {
					result.quality = Bluray1080p
				}
			} else {
				result.quality = HDTV1080p
			}
		case 720:
			if extQuality == Bluray720p {
				result.quality = Bluray720p
			} else {
				result.quality = HDTV720p
			}
		case 360, 480, 576:
			if extQuality == Bluray720p {
				result.quality = Bluray480p
			} else {
				result.quality = SDTV
			}
		}
		if result.quality != Unknown {
			return result
		}
	}

	// x264 codec fallback → SDTV (Sonarr parity)
	if hasX264 {
		result.quality = SDTV
		return result
	}

	// Concatenated bluray patterns (bluray720p, bluray1080p, bluray2160p)
	normalizedLower := strings.ToLower(normalizedName)
	if strings.Contains(normalizedLower, "bluray720p") {
		result.quality = Bluray720p
		return result
	}
	if strings.Contains(normalizedLower, "bluray1080p") {
		result.quality = Bluray1080p
		return result
	}
	if strings.Contains(normalizedLower, "bluray2160p") {
		result.quality = Bluray2160p
		return result
	}

	// HD TV / SD TV patterns (with space/separator)
	otherMatch := OtherSourceRegex.FindStringSubmatch(normalizedName)
	if otherMatch != nil {
		for i, gn := range OtherSourceRegex.SubexpNames() {
			if otherMatch[i] != "" {
				switch gn {
				case "hdtv":
					result.quality = HDTV720p
					return result
				case "sdtv":
					result.quality = SDTV
					return result
				}
			}
		}
	}

	// Extension-based fallback (Sonarr parity)
	// If we still have Unknown quality, try to determine from extension
	if result.quality == Unknown {
		extQuality := getQualityForExtension(name)
		if extQuality != Unknown {
			result.quality = extQuality
		}
	}

	return result
}
