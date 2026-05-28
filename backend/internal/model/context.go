package model

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/quality"
	"github.com/kyleaupton/arrflix/internal/template"
)

// Phase indicates when a field becomes available
type Phase string

const (
	PhasePreDownload  Phase = "pre_download"
	PhasePostDownload Phase = "post_download"
)

// EvaluationContext is the unified context available to both the policy engine
// and name template system. It uses prefixed namespaces:
//   - candidate.* - Torrent/release metadata (available at policy time)
//   - identity.*  - Parsed identity: title, year, edition, season/episode numbering (policy time)
//   - quality.*   - Parsed quality info (available at policy time)
//   - release.*   - Release metadata like group and hardcoded-subs (available at policy time)
//   - media.*     - TMDB/media metadata (available at policy time)
//   - mediainfo.* - Video file analysis (available only post-download)
type EvaluationContext struct {
	Candidate CandidateFields  `namespace:"candidate"`
	Identity  IdentityFields   `namespace:"identity"`
	Quality   QualityFields    `namespace:"quality"`
	Release   ReleaseFields    `namespace:"release"`
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
// Revision) plus Full — the per-domain bin name projected from the core by
// internal/quality at context-build time. Source, Resolution, and Modifier are
// the orthogonal axes parsing emits; Full is the domain-shaped render
// ("Bluray-1080p Remux" for series, "Remux-1080p" for movies) and is the
// user-facing display value rules and templates read.
type QualityFields struct {
	Full       string `path:"quality.full" label:"Full Quality" type:"text" phase:"pre_download"`
	Resolution string `path:"quality.resolution" label:"Resolution" type:"enum" enumValues:"Unknown,SD,480p,576p,720p,1080p,2160p" phase:"pre_download"`
	Source     string `path:"quality.source" label:"Source" type:"enum" enumValues:"Unknown,CAM,Telesync,Telecine,Workprint,DVD,TV,WEBRip,WEB-DL,BluRay" phase:"pre_download"`
	Modifier   string `path:"quality.modifier" label:"Modifier" type:"enum" enumValues:"NONE,REMUX,BR-DISK,RAWHD" phase:"pre_download"`
	IsRepack   bool   `path:"quality.is_repack" label:"Is Repack" type:"boolean" phase:"pre_download"`
	Version    int    `path:"quality.version" label:"Version" type:"number" phase:"pre_download"`
	Real       int    `path:"quality.real" label:"Real" type:"number" phase:"pre_download"`
}

// ReleaseFields contains release metadata (not quality-related, not identity).
// Hardcoded-subs is a property of this particular encode (like languages /
// dual-audio), not of which work or cut the release is — so it lives here, not
// under identity.
type ReleaseFields struct {
	ReleaseGroup  string `path:"release.release_group" label:"Release Group" type:"text" phase:"pre_download"`
	HardcodedSubs string `path:"release.hardcoded_subs" label:"Hardcoded Subs" type:"text" phase:"pre_download"`
}

// IdentityFields contains parsed identity hints from the release title: the
// work's title/year/edition and (for series) the season/episode numbering.
// These are advertised values from the parse — matching resolves the canonical
// identity into media.*. Edition lives here (mirroring the parse model and the
// *arr stack): like year, it identifies which cut of the work the release is.
type IdentityFields struct {
	Title     string   `path:"identity.title" label:"Title" type:"text" phase:"pre_download"`
	Year      int      `path:"identity.year" label:"Year" type:"number" phase:"pre_download"`
	TypeHint  string   `path:"identity.type_hint" label:"Type Hint" type:"enum" enumValues:"movie,series" phase:"pre_download"`
	Edition   string   `path:"identity.edition" label:"Edition" type:"text" phase:"pre_download"`
	AllTitles []string `path:"identity.all_titles" label:"All Titles (AKA)" type:"text" phase:"pre_download"`

	// Numbering (series) — the release's own numbering, flattened into identity.
	Season          int    `path:"identity.season" label:"Season" type:"number" phase:"pre_download"`
	EpisodeNumbers  []int  `path:"identity.episode_numbers" label:"Episode Numbers" type:"number" phase:"pre_download"`
	AbsoluteNumbers []int  `path:"identity.absolute_numbers" label:"Absolute Numbers" type:"number" phase:"pre_download"`
	AirDate         string `path:"identity.air_date" label:"Air Date" type:"text" phase:"pre_download"`
	FullSeason      bool   `path:"identity.full_season" label:"Full Season" type:"boolean" phase:"pre_download"`
	SeasonPart      int    `path:"identity.season_part" label:"Season Part" type:"number" phase:"pre_download"`
	IsPartialSeason bool   `path:"identity.is_partial_season" label:"Is Partial Season" type:"boolean" phase:"pre_download"`
	IsMultiSeason   bool   `path:"identity.is_multi_season" label:"Is Multi Season" type:"boolean" phase:"pre_download"`
	IsMiniSeries    bool   `path:"identity.is_mini_series" label:"Is Mini Series" type:"boolean" phase:"pre_download"`
	Special         bool   `path:"identity.special" label:"Special" type:"boolean" phase:"pre_download"`
	IsSplitEpisode  bool   `path:"identity.is_split_episode" label:"Is Split Episode" type:"boolean" phase:"pre_download"`
	IsSeasonExtra   bool   `path:"identity.is_season_extra" label:"Is Season Extra" type:"boolean" phase:"pre_download"`
	DailyPart       int    `path:"identity.daily_part" label:"Daily Part" type:"number" phase:"pre_download"`

	// Derived (computed by the parser, mirroring *arr's computed properties).
	IsDaily                  bool   `path:"identity.is_daily" label:"Is Daily" type:"boolean" phase:"pre_download"`
	IsAbsoluteNumbering      bool   `path:"identity.is_absolute_numbering" label:"Is Absolute Numbering" type:"boolean" phase:"pre_download"`
	IsPossibleSpecialEpisode bool   `path:"identity.is_possible_special" label:"Is Possible Special" type:"boolean" phase:"pre_download"`
	ReleaseType              string `path:"identity.release_type" label:"Release Type" type:"enum" enumValues:"unknown,singleEpisode,multiEpisode,fullSeason,partialSeason" phase:"pre_download"`
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

// NewEvaluationContext creates an EvaluationContext from a DownloadCandidate, a
// parsed release, and the domain whose vocabulary the quality bin should render
// in. It fills the advertised namespaces (Identity, Quality, Release) from the
// parse via its flat Values() projection; Media is populated later by matching
// (WithMedia/WithSeriesInfo) and MediaInfo by the ffprobe extractor
// (WithMediaInfo). The Identity/Quality/Release split mirrors the parse model
// 1:1 — notably Edition lives under identity.* (it identifies the cut) and
// hardcoded-subs under release.* (an encode property).
//
// Quality.Full is the per-domain bin name projected from the orthogonal core by
// internal/quality. Pass quality.DomainSeries / quality.DomainMovie when the
// caller knows the domain; quality.DomainUnknown yields an empty bin (the same
// "no matching bin" sentinel BinFor produces for an undetected core).
func NewEvaluationContext(candidate DownloadCandidate, parsed parsing.ParsedRelease, domain quality.Domain) EvaluationContext {
	v := parsed.Values()
	bin := quality.BinFor(
		parsing.Source(v.Quality.Source),
		parsing.Resolution(v.Quality.Resolution),
		parsing.Modifier(v.Quality.Modifier),
		domain,
	)
	// Parsing's ModNone is the empty string (mirroring Radarr's Modifier.NONE
	// being its zero enum value). Surface it to rules and the routing UI as the
	// canonical "NONE" label so the enum stays closed.
	modifier := v.Quality.Modifier
	if modifier == "" {
		modifier = "NONE"
	}
	return EvaluationContext{
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
			Title:                    v.Identity.Title,
			Year:                     v.Identity.Year,
			TypeHint:                 v.Identity.TypeHint,
			Edition:                  v.Identity.Edition,
			AllTitles:                v.Identity.AllTitles,
			Season:                   v.Identity.Numbering.Season,
			EpisodeNumbers:           v.Identity.Numbering.EpisodeNumbers,
			AbsoluteNumbers:          v.Identity.Numbering.AbsoluteNumbers,
			AirDate:                  v.Identity.Numbering.AirDate,
			FullSeason:               v.Identity.Numbering.FullSeason,
			SeasonPart:               v.Identity.Numbering.SeasonPart,
			IsPartialSeason:          v.Identity.Numbering.IsPartialSeason,
			IsMultiSeason:            v.Identity.Numbering.IsMultiSeason,
			IsMiniSeries:             v.Identity.Numbering.IsMiniSeries,
			Special:                  v.Identity.Numbering.Special,
			IsSplitEpisode:           v.Identity.Numbering.IsSplitEpisode,
			IsSeasonExtra:            v.Identity.Numbering.IsSeasonExtra,
			DailyPart:                v.Identity.Numbering.DailyPart,
			IsDaily:                  v.Identity.Numbering.IsDaily,
			IsAbsoluteNumbering:      v.Identity.Numbering.IsAbsoluteNumbering,
			IsPossibleSpecialEpisode: v.Identity.Numbering.IsPossibleSpecialEpisode,
			ReleaseType:              v.Identity.Numbering.ReleaseType,
		},
		Quality: QualityFields{
			Full:       bin.Name,
			Resolution: v.Quality.Resolution,
			Source:     v.Quality.Source,
			Modifier:   modifier,
			IsRepack:   v.Quality.IsRepack,
			Version:    v.Quality.Version,
			Real:       v.Quality.Real,
		},
		Release: ReleaseFields{
			ReleaseGroup:  v.Release.ReleaseGroup,
			HardcodedSubs: v.Release.HardcodedSubs,
		},
		Media:     MediaFields{},
		MediaInfo: nil,
	}
}

// WithMedia sets the media fields on the context
func (ctx EvaluationContext) WithMedia(mediaType MediaType, title string, year int, tmdbID int64) EvaluationContext {
	ctx.Media = MediaFields{
		Type:       string(mediaType),
		Title:      title,
		CleanTitle: template.CleanTitle(title),
		Year:       year,
		TmdbID:     tmdbID,
	}
	return ctx
}

// WithSeriesInfo sets series-specific media fields
func (ctx EvaluationContext) WithSeriesInfo(season, episode *int, episodeTitle *string) EvaluationContext {
	ctx.Media.Season = season
	ctx.Media.Episode = episode
	ctx.Media.EpisodeTitle = episodeTitle
	return ctx
}

// WithMediaInfo sets the mediainfo fields (post-download)
func (ctx EvaluationContext) WithMediaInfo(mi *MediaInfoFields) EvaluationContext {
	ctx.MediaInfo = mi
	return ctx
}

// GetField retrieves a field value by its path (e.g., "candidate.size", "quality.resolution")
func (ctx *EvaluationContext) GetField(path string) (any, error) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid field path: %s (expected namespace.field)", path)
	}

	namespace := parts[0]
	fieldPath := parts[1]

	switch namespace {
	case "candidate":
		return getFieldByPath(&ctx.Candidate, "candidate."+fieldPath)
	case "identity":
		return getFieldByPath(&ctx.Identity, "identity."+fieldPath)
	case "quality":
		return getFieldByPath(&ctx.Quality, "quality."+fieldPath)
	case "media":
		return getFieldByPath(&ctx.Media, "media."+fieldPath)
	case "mediainfo":
		if ctx.MediaInfo == nil {
			return nil, fmt.Errorf("mediainfo not available (pre-download phase)")
		}
		return getFieldByPath(ctx.MediaInfo, "mediainfo."+fieldPath)
	default:
		return nil, fmt.Errorf("unknown namespace: %s", namespace)
	}
}

// getFieldByPath uses reflection to find a struct field by its path tag
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
				return fieldVal.Elem().Interface(), nil
			}
			return fieldVal.Interface(), nil
		}
	}

	return nil, fmt.Errorf("unknown field: %s", path)
}

// ContextFieldInfo represents metadata about a field available in EvaluationContext
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
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(ReleaseFields{}))...)
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

// goTypeToValueType converts Go reflect.Type to a string representation
func goTypeToValueType(t reflect.Type) string {
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

// ToTemplateData converts the context to a map suitable for Go templates
// This provides namespaced access (e.g., .Candidate.Title, .Media.Title)
func (ctx *EvaluationContext) ToTemplateData() map[string]any {
	// Build a custom map for Media so Season/Episode render as zero-padded strings
	media := map[string]any{
		"Type":       ctx.Media.Type,
		"Title":      ctx.Media.Title,
		"CleanTitle": ctx.Media.CleanTitle,
		"Year":       ctx.Media.Year,
		"TmdbID":     ctx.Media.TmdbID,
	}
	if ctx.Media.Season != nil {
		media["Season"] = fmt.Sprintf("%02d", *ctx.Media.Season)
	}
	if ctx.Media.Episode != nil {
		media["Episode"] = fmt.Sprintf("%02d", *ctx.Media.Episode)
	}
	if ctx.Media.EpisodeTitle != nil {
		media["EpisodeTitle"] = *ctx.Media.EpisodeTitle
	}

	data := map[string]any{
		"Candidate": ctx.Candidate,
		"Identity":  ctx.Identity,
		"Quality":   ctx.Quality,
		"Release":   ctx.Release,
		"Media":     media,
	}

	// Always include MediaInfo (empty struct if not available) to avoid <no value> in templates
	if ctx.MediaInfo != nil {
		data["MediaInfo"] = ctx.MediaInfo
	} else {
		data["MediaInfo"] = &MediaInfoFields{}
	}

	return data
}
