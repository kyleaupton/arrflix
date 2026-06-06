package model

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/template"
)

// Phase indicates when a field becomes available
type Phase string

const (
	PhasePreDownload  Phase = "pre_download"
	PhasePostDownload Phase = "post_download"
)

// Release is the unit of evaluation: everything a rule or a name template can
// ask about one candidate release, in one object. It uses prefixed namespaces:
//   - candidate.* - Torrent/release metadata from the indexer (policy time)
//   - identity.*  - Parsed identity: title, year, edition, season/episode numbering (policy time)
//   - quality.*   - Parsed quality info (policy time)
//   - encode.*    - Parsed properties of this particular rip: release group, hardcoded subs (policy time)
//   - media.*     - TMDB/media metadata (policy time)
//   - mediainfo.* - Video file analysis (available only post-download)
//
// The parsed namespaces (Identity, Quality, Encode) carry parsing.Field[T] —
// value + confidence + provenance — because a release title is a claim typed
// by an uploader, not a fact. v1 consumers read bare values (GetField and
// ToTemplateData unwrap to .Value) and ignore confidence; it stays reachable
// via direct struct access (release.Quality.Resolution.Confidence) so
// confidence-aware selection is additive later. Candidate, Media, and
// MediaInfo are authoritative facts (indexer / TMDB / ffprobe) and stay bare.
type Release struct {
	Candidate CandidateFields  `namespace:"candidate"`
	Identity  IdentityFields   `namespace:"identity"`
	Quality   QualityFields    `namespace:"quality"`
	Encode    EncodeFields     `namespace:"encode"`
	Media     MediaFields      `namespace:"media"`
	MediaInfo *MediaInfoFields `namespace:"mediainfo"` // nil until post-download
}

// CandidateFields contains torrent/release metadata from indexers
type CandidateFields struct {
	Size        int64     `path:"candidate.size" label:"Size" type:"number" phase:"pre_download"`
	Title       string    `path:"candidate.title" label:"Candidate Title" type:"text" phase:"pre_download"`
	Indexer     string    `path:"candidate.indexer" label:"Indexer" type:"dynamic" dynamicSource:"/api/v1/indexers/configured" phase:"pre_download"`
	IndexerID   int64     `path:"candidate.indexer_id" label:"Indexer ID" type:"number" phase:"pre_download"`
	Categories  []string  `path:"candidate.categories" label:"Categories" type:"dynamic" phase:"pre_download"`
	Protocol    string    `path:"candidate.protocol" label:"Protocol" type:"enum" enumValues:"torrent,usenet" phase:"pre_download"`
	Seeders     int       `path:"candidate.seeders" label:"Seeders" type:"number" phase:"pre_download"`
	Peers       int       `path:"candidate.peers" label:"Peers" type:"number" phase:"pre_download"`
	Age         int64     `path:"candidate.age" label:"Age (seconds)" type:"number" phase:"pre_download"`
	AgeHours    float64   `path:"candidate.age_hours" label:"Age (hours)" type:"number" phase:"pre_download"`
	Grabs       int       `path:"candidate.grabs" label:"Grabs" type:"number" phase:"pre_download"`
	PublishDate time.Time `path:"candidate.publish_date" label:"Publish Date" type:"text" phase:"pre_download"`
	Link        string    `path:"candidate.link" label:"Link" type:"text" phase:"pre_download"`
	GUID        string    `path:"candidate.guid" label:"GUID" type:"text" phase:"pre_download"`
}

// QualityFields contains the parsed quality core (Source, Resolution, Modifier,
// Revision) plus the rendered per-domain bin (Name / Full) parsing produces
// once the domain is known. Source, Resolution, and Modifier are the orthogonal
// axes; Name is the bin label ("Bluray-1080p Remux" for series, "Remux-1080p"
// for movies — mirrors *arr's Quality.Name API field), and Full is Name plus
// the {Quality Full} revision-render suffix (mirrors *arr's {Quality Full}
// naming token).
type QualityFields struct {
	Name       parsing.Field[string] `path:"quality.name" label:"Quality Name" type:"text" phase:"pre_download"`
	Full       parsing.Field[string] `path:"quality.full" label:"Full Quality" type:"text" phase:"pre_download"`
	Resolution parsing.Field[string] `path:"quality.resolution" label:"Resolution" type:"enum" enumValues:"Unknown,SD,480p,576p,720p,1080p,2160p" phase:"pre_download"`
	Source     parsing.Field[string] `path:"quality.source" label:"Source" type:"enum" enumValues:"Unknown,CAM,Telesync,Telecine,Workprint,DVD,TV,WEBRip,WEB-DL,BluRay" phase:"pre_download"`
	Modifier   parsing.Field[string] `path:"quality.modifier" label:"Modifier" type:"enum" enumValues:"NONE,REMUX,BR-DISK,RAWHD" phase:"pre_download"`
	IsRepack   parsing.Field[bool]   `path:"quality.is_repack" label:"Is Repack" type:"boolean" phase:"pre_download"`
	Version    parsing.Field[int]    `path:"quality.version" label:"Version" type:"number" phase:"pre_download"`
	Real       parsing.Field[int]    `path:"quality.real" label:"Real" type:"number" phase:"pre_download"`
}

// EncodeFields contains parsed properties of this particular rip (not
// quality-related, not identity). Hardcoded-subs is a property of this
// particular encode (like languages / dual-audio), not of which work or cut
// the release is — so it lives here, not under identity.
type EncodeFields struct {
	ReleaseGroup  parsing.Field[string] `path:"encode.release_group" label:"Release Group" type:"text" phase:"pre_download"`
	HardcodedSubs parsing.Field[string] `path:"encode.hardcoded_subs" label:"Hardcoded Subs" type:"text" phase:"pre_download"`
}

// IdentityFields contains parsed identity hints from the release title: the
// work's title/year/edition and (for series) the season/episode numbering.
// These are advertised values from the parse — matching resolves the canonical
// identity into media.*. Edition lives here (mirroring the parse model and the
// *arr stack): like year, it identifies which cut of the work the release is.
type IdentityFields struct {
	Title     parsing.Field[string]   `path:"identity.title" label:"Title" type:"text" phase:"pre_download"`
	Year      parsing.Field[int]      `path:"identity.year" label:"Year" type:"number" phase:"pre_download"`
	TypeHint  parsing.Field[string]   `path:"identity.type_hint" label:"Type Hint" type:"enum" enumValues:"movie,series" phase:"pre_download"`
	Edition   parsing.Field[string]   `path:"identity.edition" label:"Edition" type:"text" phase:"pre_download"`
	AllTitles parsing.Field[[]string] `path:"identity.all_titles" label:"All Titles (AKA)" type:"text" phase:"pre_download"`

	// Numbering (series) — the release's own numbering, flattened into identity.
	Season          parsing.Field[int]    `path:"identity.season" label:"Season" type:"number" phase:"pre_download"`
	EpisodeNumbers  parsing.Field[[]int]  `path:"identity.episode_numbers" label:"Episode Numbers" type:"number" phase:"pre_download"`
	AbsoluteNumbers parsing.Field[[]int]  `path:"identity.absolute_numbers" label:"Absolute Numbers" type:"number" phase:"pre_download"`
	AirDate         parsing.Field[string] `path:"identity.air_date" label:"Air Date" type:"text" phase:"pre_download"`
	FullSeason      parsing.Field[bool]   `path:"identity.full_season" label:"Full Season" type:"boolean" phase:"pre_download"`
	SeasonPart      parsing.Field[int]    `path:"identity.season_part" label:"Season Part" type:"number" phase:"pre_download"`
	IsPartialSeason parsing.Field[bool]   `path:"identity.is_partial_season" label:"Is Partial Season" type:"boolean" phase:"pre_download"`
	IsMultiSeason   parsing.Field[bool]   `path:"identity.is_multi_season" label:"Is Multi Season" type:"boolean" phase:"pre_download"`
	IsMiniSeries    parsing.Field[bool]   `path:"identity.is_mini_series" label:"Is Mini Series" type:"boolean" phase:"pre_download"`
	Special         parsing.Field[bool]   `path:"identity.special" label:"Special" type:"boolean" phase:"pre_download"`
	IsSplitEpisode  parsing.Field[bool]   `path:"identity.is_split_episode" label:"Is Split Episode" type:"boolean" phase:"pre_download"`
	IsSeasonExtra   parsing.Field[bool]   `path:"identity.is_season_extra" label:"Is Season Extra" type:"boolean" phase:"pre_download"`
	DailyPart       parsing.Field[int]    `path:"identity.daily_part" label:"Daily Part" type:"number" phase:"pre_download"`

	// Derived (computed by the parser, mirroring *arr's computed properties).
	IsDaily                  parsing.Field[bool]   `path:"identity.is_daily" label:"Is Daily" type:"boolean" phase:"pre_download"`
	IsAbsoluteNumbering      parsing.Field[bool]   `path:"identity.is_absolute_numbering" label:"Is Absolute Numbering" type:"boolean" phase:"pre_download"`
	IsPossibleSpecialEpisode parsing.Field[bool]   `path:"identity.is_possible_special" label:"Is Possible Special" type:"boolean" phase:"pre_download"`
	ReleaseType              parsing.Field[string] `path:"identity.release_type" label:"Release Type" type:"enum" enumValues:"unknown,singleEpisode,multiEpisode,fullSeason,partialSeason" phase:"pre_download"`
}

// MediaFields contains TMDB/media metadata
type MediaFields struct {
	Type         string  `path:"media.type" label:"Media Type" type:"enum" enumValues:"movie,series" phase:"pre_download"`
	Title        string  `path:"media.title" label:"Media Title" type:"text" phase:"pre_download"`
	CleanTitle   string  `path:"media.clean_title" label:"Clean Title" type:"text" phase:"pre_download"`
	Year         int     `path:"media.year" label:"Year" type:"number" phase:"pre_download"`
	TmdbID       int64   `path:"media.tmdb_id" label:"TMDB ID" type:"number" phase:"pre_download"`
	Season       *int    `path:"media.season" label:"Season" type:"number" phase:"pre_download"`
	Episode      *int    `path:"media.episode" label:"Episode" type:"number" phase:"pre_download"`
	EpisodeTitle *string `path:"media.episode_title" label:"Episode Title" type:"text" phase:"pre_download"`
}

// MediaInfoFields contains video file analysis data (populated post-download via mediainfo)
type MediaInfoFields struct {
	// Video properties
	VideoCodec    string  `path:"mediainfo.video_codec" label:"Video Codec" type:"enum" enumValues:"Unknown,H.264,H.265,AV1,VP9,MPEG-2" phase:"post_download"`
	VideoBitDepth int     `path:"mediainfo.video_bit_depth" label:"Video Bit Depth" type:"number" phase:"post_download"`
	VideoProfile  string  `path:"mediainfo.video_profile" label:"Video Profile" type:"text" phase:"post_download"`
	Width         int     `path:"mediainfo.width" label:"Width" type:"number" phase:"post_download"`
	Height        int     `path:"mediainfo.height" label:"Height" type:"number" phase:"post_download"`
	VideoBitrate  int64   `path:"mediainfo.video_bitrate" label:"Video Bitrate" type:"number" phase:"post_download"`
	VideoFps      float64 `path:"mediainfo.video_fps" label:"Video FPS" type:"number" phase:"post_download"`
	ScanType      string  `path:"mediainfo.scan_type" label:"Scan Type" type:"enum" enumValues:"Unknown,Progressive,Interlaced" phase:"post_download"`
	HDR           string  `path:"mediainfo.hdr" label:"HDR Format" type:"enum" enumValues:"None,HDR10,HDR10+,Dolby Vision,HLG" phase:"post_download"`

	// Audio properties
	AudioCodec       string   `path:"mediainfo.audio_codec" label:"Audio Codec" type:"enum" enumValues:"Unknown,AAC,AC3,DTS,DTS-HD MA,TrueHD,FLAC,Opus" phase:"post_download"`
	AudioChannels    string   `path:"mediainfo.audio_channels" label:"Audio Channels" type:"enum" enumValues:"Unknown,2.0,5.1,7.1" phase:"post_download"`
	AudioProfile     string   `path:"mediainfo.audio_profile" label:"Audio Profile" type:"text" phase:"post_download"`
	AudioBitrate     int64    `path:"mediainfo.audio_bitrate" label:"Audio Bitrate" type:"number" phase:"post_download"`
	AudioStreamCount int      `path:"mediainfo.audio_stream_count" label:"Audio Stream Count" type:"number" phase:"post_download"`
	AudioLanguages   []string `path:"mediainfo.audio_languages" label:"Audio Languages" type:"text" phase:"post_download"`

	// Container and general properties
	Container           string   `path:"mediainfo.container" label:"Container" type:"enum" enumValues:"Unknown,MKV,MP4,AVI,TS" phase:"post_download"`
	Duration            int64    `path:"mediainfo.duration" label:"Duration (seconds)" type:"number" phase:"post_download"`
	FileSize            int64    `path:"mediainfo.file_size" label:"File Size" type:"number" phase:"post_download"`
	Subtitles           []string `path:"mediainfo.subtitles" label:"Subtitles" type:"text" phase:"post_download"`
	VideoMultiViewCount int      `path:"mediainfo.video_multi_view_count" label:"Video Multi-View Count" type:"number" phase:"post_download"`
}

// NewRelease creates a Release from a DownloadCandidate and a parsed release.
// It fills the parsed namespaces (Identity, Quality, Encode) by copying the
// parse's Field[T] directly — confidence and evidence ride along; Media is
// populated later by matching (WithMedia/WithSeriesInfo) and MediaInfo by the
// ffprobe extractor (WithMediaInfo). The Identity/Quality/Encode split mirrors
// the parse model — notably Edition lives under identity.* (it identifies the
// cut) and hardcoded-subs under encode.* (an encode property).
//
// Quality.Name / Quality.Full are taken straight from the parse — Parse
// rendered them in the domain's vocabulary because the caller passed the
// domain to Parse.
func NewRelease(candidate DownloadCandidate, parsed parsing.ParsedRelease) Release {
	// Parsing's ModNone is the empty string (mirroring Radarr's Modifier.NONE
	// being its zero enum value). Surface it to rules and the routing UI as the
	// canonical "NONE" label so the enum stays closed — normalized on the value
	// only; confidence and evidence stay the parse's own.
	modifier := parsed.Quality.Modifier
	if modifier.Value == "" {
		modifier.Value = "NONE"
	}

	n := parsed.Identity.Numbering
	return Release{
		Candidate: CandidateFields{
			Size:        candidate.Size,
			Title:       candidate.Title,
			Indexer:     candidate.Indexer,
			IndexerID:   candidate.IndexerID,
			Categories:  candidate.Categories,
			Protocol:    candidate.Protocol,
			Seeders:     candidate.Seeders,
			Peers:       candidate.Peers,
			Age:         candidate.Age,
			AgeHours:    candidate.AgeHours,
			Grabs:       candidate.Grabs,
			PublishDate: candidate.PublishDate,
			Link:        candidate.Link,
			GUID:        candidate.GUID,
		},
		Identity: IdentityFields{
			Title:           parsed.Identity.Title,
			Year:            parsed.Identity.Year,
			TypeHint:        parsed.Identity.TypeHint,
			Edition:         parsed.Identity.Edition,
			AllTitles:       parsed.Identity.AllTitles,
			Season:          n.Season,
			EpisodeNumbers:  n.EpisodeNumbers,
			AbsoluteNumbers: n.AbsoluteNumbers,
			AirDate:         n.AirDate,
			FullSeason:      n.FullSeason,
			SeasonPart:      n.SeasonPart,
			IsPartialSeason: n.IsPartialSeason,
			IsMultiSeason:   n.IsMultiSeason,
			IsMiniSeries:    n.IsMiniSeries,
			Special:         n.Special,
			IsSplitEpisode:  n.IsSplitEpisode,
			IsSeasonExtra:   n.IsSeasonExtra,
			DailyPart:       n.DailyPart,
			// Derived flags are computed from the numbering rather than parsed
			// from a token — they carry the computed value with no
			// confidence/evidence of their own.
			IsDaily:                  parsing.Field[bool]{Value: n.IsDaily()},
			IsAbsoluteNumbering:      parsing.Field[bool]{Value: n.IsAbsoluteNumbering()},
			IsPossibleSpecialEpisode: parsing.Field[bool]{Value: parsed.Identity.IsPossibleSpecialEpisode()},
			ReleaseType:              parsing.Field[string]{Value: n.ReleaseType()},
		},
		Quality: QualityFields{
			Name:       parsed.Quality.Name,
			Full:       parsed.Quality.Full,
			Resolution: parsed.Quality.Resolution,
			Source:     parsed.Quality.Source,
			Modifier:   modifier,
			IsRepack:   parsed.Quality.IsRepack,
			Version:    parsed.Quality.Version,
			Real:       parsed.Quality.Real,
		},
		Encode: EncodeFields{
			ReleaseGroup:  parsed.Release.ReleaseGroup,
			HardcodedSubs: parsed.Release.HardcodedSubs,
		},
		Media:     MediaFields{},
		MediaInfo: nil,
	}
}

// WithMedia sets the media fields on the release
func (r Release) WithMedia(mediaType MediaType, title string, year int, tmdbID int64) Release {
	r.Media = MediaFields{
		Type:       string(mediaType),
		Title:      title,
		CleanTitle: template.CleanTitle(title),
		Year:       year,
		TmdbID:     tmdbID,
	}
	return r
}

// WithSeriesInfo sets series-specific media fields
func (r Release) WithSeriesInfo(season, episode *int, episodeTitle *string) Release {
	r.Media.Season = season
	r.Media.Episode = episode
	r.Media.EpisodeTitle = episodeTitle
	return r
}

// WithMediaInfo sets the mediainfo fields (post-download)
func (r Release) WithMediaInfo(mi *MediaInfoFields) Release {
	r.MediaInfo = mi
	return r
}

// GetField retrieves a field value by its path (e.g., "candidate.size",
// "quality.resolution"). Parsed-namespace fields are unwrapped to their bare
// .Value — callers always get plain values regardless of namespace.
func (r *Release) GetField(path string) (any, error) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid field path: %s (expected namespace.field)", path)
	}

	namespace := parts[0]
	fieldPath := parts[1]

	switch namespace {
	case "candidate":
		return getFieldByPath(&r.Candidate, "candidate."+fieldPath)
	case "identity":
		return getFieldByPath(&r.Identity, "identity."+fieldPath)
	case "quality":
		return getFieldByPath(&r.Quality, "quality."+fieldPath)
	case "encode":
		return getFieldByPath(&r.Encode, "encode."+fieldPath)
	case "media":
		return getFieldByPath(&r.Media, "media."+fieldPath)
	case "mediainfo":
		if r.MediaInfo == nil {
			return nil, fmt.Errorf("mediainfo not available (pre-download phase)")
		}
		return getFieldByPath(r.MediaInfo, "mediainfo."+fieldPath)
	default:
		return nil, fmt.Errorf("unknown namespace: %s", namespace)
	}
}

// isFieldWrapper reports whether t is a parsing.Field[T]-shaped wrapper — a
// struct exposing exactly Value (any T), Confidence (float64), and Evidence
// (string). Detection is structural rather than by type name so the reflection
// helpers stay agnostic to which package declares the wrapper.
func isFieldWrapper(t reflect.Type) bool {
	if t.Kind() != reflect.Struct || t.NumField() != 3 {
		return false
	}
	if _, ok := t.FieldByName("Value"); !ok {
		return false
	}
	conf, ok := t.FieldByName("Confidence")
	if !ok || conf.Type.Kind() != reflect.Float64 {
		return false
	}
	ev, ok := t.FieldByName("Evidence")
	return ok && ev.Type.Kind() == reflect.String
}

// getFieldByPath uses reflection to find a struct field by its path tag.
// Field[T]-wrapped fields yield their inner Value; bare fields are returned
// directly.
func getFieldByPath(obj any, path string) (any, error) {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", v.Kind())
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if tag := field.Tag.Get("path"); tag == path {
			fieldVal := v.Field(i)
			// Handle pointer fields
			if fieldVal.Kind() == reflect.Pointer {
				if fieldVal.IsNil() {
					return nil, nil
				}
				fieldVal = fieldVal.Elem()
			}
			if isFieldWrapper(fieldVal.Type()) {
				return fieldVal.FieldByName("Value").Interface(), nil
			}
			return fieldVal.Interface(), nil
		}
	}

	return nil, fmt.Errorf("unknown field: %s", path)
}

// ContextFieldInfo represents metadata about a field available on Release
type ContextFieldInfo struct {
	Path          string   `json:"path"`
	Label         string   `json:"label"`
	Type          string   `json:"type"`      // text, number, enum, boolean, dynamic
	ValueType     string   `json:"valueType"` // string, int64, int, float64, bool, []string
	Phase         Phase    `json:"phase"`
	EnumValues    []string `json:"enumValues,omitempty"`
	DynamicSource string   `json:"dynamicSource,omitempty"`
}

// ListContextFields returns all available fields with their metadata
func ListContextFields() []ContextFieldInfo {
	var fields []ContextFieldInfo

	// Collect fields from each namespace struct
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(CandidateFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(IdentityFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(QualityFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(EncodeFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(MediaFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(MediaInfoFields{}))...)

	return fields
}

// extractFieldsFromStruct extracts ContextFieldInfo from struct tags
func extractFieldsFromStruct(t reflect.Type) []ContextFieldInfo {
	var fields []ContextFieldInfo

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		path := field.Tag.Get("path")
		if path == "" {
			continue
		}

		info := ContextFieldInfo{
			Path:          path,
			Label:         field.Tag.Get("label"),
			Type:          field.Tag.Get("type"),
			Phase:         Phase(field.Tag.Get("phase")),
			DynamicSource: field.Tag.Get("dynamicSource"),
		}

		// Determine value type from Go type
		info.ValueType = goTypeToValueType(field.Type)

		// Parse enum values if present
		if enumStr := field.Tag.Get("enumValues"); enumStr != "" {
			info.EnumValues = strings.Split(enumStr, ",")
		}

		fields = append(fields, info)
	}

	return fields
}

// goTypeToValueType converts Go reflect.Type to a string representation.
// Field[T] wrappers are unwrapped first so the emitted valueType is T's
// (e.g. "string"), never the wrapper's.
func goTypeToValueType(t reflect.Type) string {
	if isFieldWrapper(t) {
		inner, _ := t.FieldByName("Value")
		t = inner.Type
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int32:
		return "int"
	case reflect.Int64:
		return "int64"
	case reflect.Float64:
		return "float64"
	case reflect.Bool:
		return "bool"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return "[]string"
		}
		return "[]any"
	case reflect.Pointer:
		return goTypeToValueType(t.Elem())
	default:
		return "any"
	}
}

// templateMap flattens a parsed-namespace struct into a map keyed by Go field
// name, unwrapping each Field[T] to its bare Value — so templates address
// {{.Quality.Full}}, not {{.Quality.Full.Value}}.
func templateMap(obj any) map[string]any {
	v := reflect.ValueOf(obj)
	t := v.Type()
	m := make(map[string]any, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		fieldVal := v.Field(i)
		if isFieldWrapper(fieldVal.Type()) {
			fieldVal = fieldVal.FieldByName("Value")
		}
		m[t.Field(i).Name] = fieldVal.Interface()
	}
	return m
}

// ToTemplateData converts the release to a map suitable for Go templates
// This provides namespaced access (e.g., .Candidate.Title, .Media.Title)
func (r *Release) ToTemplateData() map[string]any {
	// Build a custom map for Media so Season/Episode render as zero-padded strings
	media := map[string]any{
		"Type":       r.Media.Type,
		"Title":      r.Media.Title,
		"CleanTitle": r.Media.CleanTitle,
		"Year":       r.Media.Year,
		"TmdbID":     r.Media.TmdbID,
	}
	if r.Media.Season != nil {
		media["Season"] = fmt.Sprintf("%02d", *r.Media.Season)
	}
	if r.Media.Episode != nil {
		media["Episode"] = fmt.Sprintf("%02d", *r.Media.Episode)
	}
	if r.Media.EpisodeTitle != nil {
		media["EpisodeTitle"] = *r.Media.EpisodeTitle
	}

	data := map[string]any{
		"Candidate": r.Candidate,
		"Identity":  templateMap(r.Identity),
		"Quality":   templateMap(r.Quality),
		"Encode":    templateMap(r.Encode),
		"Media":     media,
	}

	// Always include MediaInfo (empty struct if not available) to avoid <no value> in templates
	if r.MediaInfo != nil {
		data["MediaInfo"] = r.MediaInfo
	} else {
		data["MediaInfo"] = &MediaInfoFields{}
	}

	return data
}
