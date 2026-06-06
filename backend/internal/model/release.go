package model

import (
	"time"

	"github.com/kyleaupton/arrflix/internal/parsing"
)

// Release is the artifact half of the Subject: everything that describes the
// release itself, as increasing verified truth — advertised (the indexer
// listing), parsed (claims read from the title), asserted (what the bytes
// actually are). It uses prefixed namespaces:
//   - candidate.* - Torrent/release metadata from the indexer (advertised)
//   - identity.*  - Parsed identity: title, year, edition, season/episode numbering
//   - quality.*   - Parsed quality info
//   - encode.*    - Parsed properties of this particular rip: release group, hardcoded subs
//   - mediainfo.* - Video file analysis (asserted; available only from import)
//
// The parsed namespaces (Identity, Quality, Encode) carry parsing.Field[T] —
// value + confidence + provenance — because a release title is a claim typed
// by an uploader, not a fact. v1 consumers read bare values (Subject.GetField
// and Subject.ToTemplateData unwrap to .Value) and ignore confidence; it stays
// reachable via direct struct access (release.Quality.Resolution.Confidence)
// so confidence-aware selection is additive later. Candidate and MediaInfo are
// authoritative facts (indexer / ffprobe) and stay bare.
type Release struct {
	Candidate CandidateFields  `namespace:"candidate"`
	Identity  IdentityFields   `namespace:"identity"`
	Quality   QualityFields    `namespace:"quality"`
	Encode    EncodeFields     `namespace:"encode"`
	MediaInfo *MediaInfoFields `namespace:"mediainfo"` // nil until import
}

// CandidateFields contains torrent/release metadata from indexers
type CandidateFields struct {
	Size        int64     `path:"candidate.size" label:"Size" type:"number" phase:"search"`
	Title       string    `path:"candidate.title" label:"Candidate Title" type:"text" phase:"search"`
	Indexer     string    `path:"candidate.indexer" label:"Indexer" type:"dynamic" dynamicSource:"/api/v1/indexers/configured" phase:"search"`
	IndexerID   int64     `path:"candidate.indexer_id" label:"Indexer ID" type:"number" phase:"search"`
	Categories  []string  `path:"candidate.categories" label:"Categories" type:"dynamic" phase:"search"`
	Protocol    string    `path:"candidate.protocol" label:"Protocol" type:"enum" enumValues:"torrent,usenet" phase:"search"`
	Seeders     int       `path:"candidate.seeders" label:"Seeders" type:"number" phase:"search"`
	Peers       int       `path:"candidate.peers" label:"Peers" type:"number" phase:"search"`
	Age         int64     `path:"candidate.age" label:"Age (seconds)" type:"number" phase:"search"`
	AgeHours    float64   `path:"candidate.age_hours" label:"Age (hours)" type:"number" phase:"search"`
	Grabs       int       `path:"candidate.grabs" label:"Grabs" type:"number" phase:"search"`
	PublishDate time.Time `path:"candidate.publish_date" label:"Publish Date" type:"text" phase:"search"`
	Link        string    `path:"candidate.link" label:"Link" type:"text" phase:"search"`
	GUID        string    `path:"candidate.guid" label:"GUID" type:"text" phase:"search"`
}

// QualityFields contains the parsed quality core (Source, Resolution, Modifier,
// Revision) plus the rendered per-domain bin (Name / Full) parsing produces
// once the domain is known. Source, Resolution, and Modifier are the orthogonal
// axes; Name is the bin label ("Bluray-1080p Remux" for series, "Remux-1080p"
// for movies — mirrors *arr's Quality.Name API field), and Full is Name plus
// the {Quality Full} revision-render suffix (mirrors *arr's {Quality Full}
// naming token).
type QualityFields struct {
	Name       parsing.Field[string] `path:"quality.name" label:"Quality Name" type:"text" phase:"search"`
	Full       parsing.Field[string] `path:"quality.full" label:"Full Quality" type:"text" phase:"search"`
	Resolution parsing.Field[string] `path:"quality.resolution" label:"Resolution" type:"enum" enumValues:"Unknown,SD,480p,576p,720p,1080p,2160p" phase:"search"`
	Source     parsing.Field[string] `path:"quality.source" label:"Source" type:"enum" enumValues:"Unknown,CAM,Telesync,Telecine,Workprint,DVD,TV,WEBRip,WEB-DL,BluRay" phase:"search"`
	Modifier   parsing.Field[string] `path:"quality.modifier" label:"Modifier" type:"enum" enumValues:"NONE,REMUX,BR-DISK,RAWHD" phase:"search"`
	IsRepack   parsing.Field[bool]   `path:"quality.is_repack" label:"Is Repack" type:"boolean" phase:"search"`
	Version    parsing.Field[int]    `path:"quality.version" label:"Version" type:"number" phase:"search"`
	Real       parsing.Field[int]    `path:"quality.real" label:"Real" type:"number" phase:"search"`
}

// EncodeFields contains parsed properties of this particular rip (not
// quality-related, not identity). Hardcoded-subs is a property of this
// particular encode (like languages / dual-audio), not of which work or cut
// the release is — so it lives here, not under identity.
type EncodeFields struct {
	ReleaseGroup  parsing.Field[string] `path:"encode.release_group" label:"Release Group" type:"text" phase:"search"`
	HardcodedSubs parsing.Field[string] `path:"encode.hardcoded_subs" label:"Hardcoded Subs" type:"text" phase:"search"`
}

// IdentityFields contains parsed identity hints from the release title: the
// work's title/year/edition and (for series) the season/episode numbering.
// These are advertised values from the parse — matching resolves the canonical
// identity into media.*. Edition lives here (mirroring the parse model and the
// *arr stack): like year, it identifies which cut of the work the release is.
type IdentityFields struct {
	Title     parsing.Field[string]   `path:"identity.title" label:"Title" type:"text" phase:"search"`
	Year      parsing.Field[int]      `path:"identity.year" label:"Year" type:"number" phase:"search"`
	TypeHint  parsing.Field[string]   `path:"identity.type_hint" label:"Type Hint" type:"enum" enumValues:"movie,series" phase:"search"`
	Edition   parsing.Field[string]   `path:"identity.edition" label:"Edition" type:"text" phase:"search"`
	AllTitles parsing.Field[[]string] `path:"identity.all_titles" label:"All Titles (AKA)" type:"text" phase:"search"`

	// Numbering (series) — the release's own numbering, flattened into identity.
	Season          parsing.Field[int]    `path:"identity.season" label:"Season" type:"number" phase:"search"`
	EpisodeNumbers  parsing.Field[[]int]  `path:"identity.episode_numbers" label:"Episode Numbers" type:"number" phase:"search"`
	AbsoluteNumbers parsing.Field[[]int]  `path:"identity.absolute_numbers" label:"Absolute Numbers" type:"number" phase:"search"`
	AirDate         parsing.Field[string] `path:"identity.air_date" label:"Air Date" type:"text" phase:"search"`
	FullSeason      parsing.Field[bool]   `path:"identity.full_season" label:"Full Season" type:"boolean" phase:"search"`
	SeasonPart      parsing.Field[int]    `path:"identity.season_part" label:"Season Part" type:"number" phase:"search"`
	IsPartialSeason parsing.Field[bool]   `path:"identity.is_partial_season" label:"Is Partial Season" type:"boolean" phase:"search"`
	IsMultiSeason   parsing.Field[bool]   `path:"identity.is_multi_season" label:"Is Multi Season" type:"boolean" phase:"search"`
	IsMiniSeries    parsing.Field[bool]   `path:"identity.is_mini_series" label:"Is Mini Series" type:"boolean" phase:"search"`
	Special         parsing.Field[bool]   `path:"identity.special" label:"Special" type:"boolean" phase:"search"`
	IsSplitEpisode  parsing.Field[bool]   `path:"identity.is_split_episode" label:"Is Split Episode" type:"boolean" phase:"search"`
	IsSeasonExtra   parsing.Field[bool]   `path:"identity.is_season_extra" label:"Is Season Extra" type:"boolean" phase:"search"`
	DailyPart       parsing.Field[int]    `path:"identity.daily_part" label:"Daily Part" type:"number" phase:"search"`

	// Derived (computed by the parser, mirroring *arr's computed properties).
	IsDaily                  parsing.Field[bool]   `path:"identity.is_daily" label:"Is Daily" type:"boolean" phase:"search"`
	IsAbsoluteNumbering      parsing.Field[bool]   `path:"identity.is_absolute_numbering" label:"Is Absolute Numbering" type:"boolean" phase:"search"`
	IsPossibleSpecialEpisode parsing.Field[bool]   `path:"identity.is_possible_special" label:"Is Possible Special" type:"boolean" phase:"search"`
	ReleaseType              parsing.Field[string] `path:"identity.release_type" label:"Release Type" type:"enum" enumValues:"unknown,singleEpisode,multiEpisode,fullSeason,partialSeason" phase:"search"`
}

// MediaInfoFields contains video file analysis data (populated at import via mediainfo)
type MediaInfoFields struct {
	// Video properties
	VideoCodec    string  `path:"mediainfo.video_codec" label:"Video Codec" type:"enum" enumValues:"Unknown,H.264,H.265,AV1,VP9,MPEG-2" phase:"import"`
	VideoBitDepth int     `path:"mediainfo.video_bit_depth" label:"Video Bit Depth" type:"number" phase:"import"`
	VideoProfile  string  `path:"mediainfo.video_profile" label:"Video Profile" type:"text" phase:"import"`
	Width         int     `path:"mediainfo.width" label:"Width" type:"number" phase:"import"`
	Height        int     `path:"mediainfo.height" label:"Height" type:"number" phase:"import"`
	VideoBitrate  int64   `path:"mediainfo.video_bitrate" label:"Video Bitrate" type:"number" phase:"import"`
	VideoFps      float64 `path:"mediainfo.video_fps" label:"Video FPS" type:"number" phase:"import"`
	ScanType      string  `path:"mediainfo.scan_type" label:"Scan Type" type:"enum" enumValues:"Unknown,Progressive,Interlaced" phase:"import"`
	HDR           string  `path:"mediainfo.hdr" label:"HDR Format" type:"enum" enumValues:"None,HDR10,HDR10+,Dolby Vision,HLG" phase:"import"`

	// Audio properties
	AudioCodec       string   `path:"mediainfo.audio_codec" label:"Audio Codec" type:"enum" enumValues:"Unknown,AAC,AC3,DTS,DTS-HD MA,TrueHD,FLAC,Opus" phase:"import"`
	AudioChannels    string   `path:"mediainfo.audio_channels" label:"Audio Channels" type:"enum" enumValues:"Unknown,2.0,5.1,7.1" phase:"import"`
	AudioProfile     string   `path:"mediainfo.audio_profile" label:"Audio Profile" type:"text" phase:"import"`
	AudioBitrate     int64    `path:"mediainfo.audio_bitrate" label:"Audio Bitrate" type:"number" phase:"import"`
	AudioStreamCount int      `path:"mediainfo.audio_stream_count" label:"Audio Stream Count" type:"number" phase:"import"`
	AudioLanguages   []string `path:"mediainfo.audio_languages" label:"Audio Languages" type:"text" phase:"import"`

	// Container and general properties
	Container           string   `path:"mediainfo.container" label:"Container" type:"enum" enumValues:"Unknown,MKV,MP4,AVI,TS" phase:"import"`
	Duration            int64    `path:"mediainfo.duration" label:"Duration (seconds)" type:"number" phase:"import"`
	FileSize            int64    `path:"mediainfo.file_size" label:"File Size" type:"number" phase:"import"`
	Subtitles           []string `path:"mediainfo.subtitles" label:"Subtitles" type:"text" phase:"import"`
	VideoMultiViewCount int      `path:"mediainfo.video_multi_view_count" label:"Video Multi-View Count" type:"number" phase:"import"`
}
