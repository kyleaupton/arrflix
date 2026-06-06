// Package parsing turns a string — an indexer release title or an on-disk
// filename — into a structured set of advertised attributes (identity hints,
// quality, release metadata), each carried with a confidence and provenance.
//
// Parsing is advertised-only: it reports what the string *claims*, never what
// the bytes *are*. The asserted counterpart (ffprobe/mediainfo) is a separate
// extractor that fills the MediaInfo namespace; parsing fills the advertised
// Quality/Release namespaces and supplies Identity *hints* that matching
// resolves. Parsing a file path is still advertised — it just sources the claim
// from a filename rather than a release title.
//
// Parse (parsing.go) produces this model from the ported Sonarr/Radarr parsers;
// this file defines the data model consumers read. Internally every field is a
// Field[T] (value + confidence + provenance); consumers read the flat Values()
// projection and, when they care, the parallel Provenance() map (provenance.go).
package parsing

// Field wraps a parsed value with how confident the parser is and why. It is
// the internal carrier for every attribute; most consumers never see it
// directly (they read Values()), but matching, the parity harness, and the
// Parse Inspector consume the confidence and evidence.
type Field[T any] struct {
	// Value is the parsed attribute. The zero value means "not detected"
	// (e.g. an empty source string, a nil slice of episode numbers).
	Value T
	// Confidence is the parser's certainty in Value, 0..1. An undetected field
	// has confidence 0.
	Confidence float64
	// Evidence is human-readable provenance. Coarse for v1 (which sub-parser
	// fired, and the matched token); token-index granularity is a follow-up.
	Evidence string
}

// NumberingKind names which numbering namespace a release uses. A release
// numbers its episodes in exactly one of these conventions, and parsing never
// coerces one into another — reconciling a release's numbering against the
// provider's canonical episode identity is matching's job. This is the
// load-bearing seam for anime, where release numbering (absolute) routinely
// disagrees with provider numbering (season/episode).
type NumberingKind string

const (
	// NumberingNone means no episode numbering was detected (e.g. a movie, or
	// an unparseable title).
	NumberingNone NumberingKind = ""
	// NumberingSeasonEpisode is Western S03E05-style numbering.
	NumberingSeasonEpisode NumberingKind = "season_episode"
	// NumberingAbsolute is anime-style absolute numbering ("One Piece - 1071").
	// The field exists in v1; the sub-parser that populates it is deferred.
	NumberingAbsolute NumberingKind = "absolute"
	// NumberingDaily is air-date numbering ("Show.2016-03-14").
	NumberingDaily NumberingKind = "daily"
)

// Numbering preserves the episode numbering exactly as the release expresses
// it, tagged with which namespace it is in. At most one namespace is
// authoritative (see Kind); the other slices stay empty rather than being
// derived. This mirrors how Sonarr models the same data (separate
// episodeNumbers / absoluteEpisodeNumbers arrays plus an isAbsoluteNumbering
// discriminant).
type Numbering struct {
	// Season is the season number in the season_episode namespace.
	Season Field[int]
	// EpisodeNumbers are the episode(s) in the season_episode namespace. A
	// multi-episode release expands to the full list (S03E01-06 → 1..6),
	// matching the oracle.
	EpisodeNumbers Field[[]int]
	// AbsoluteNumbers are the episode(s) in the absolute (anime) namespace.
	AbsoluteNumbers Field[[]int]
	// AirDate is the date in the daily namespace, "YYYY-MM-DD".
	AirDate Field[string]
	// FullSeason marks a whole-season pack — the importer's closed-world
	// assignment depends on it.
	FullSeason Field[bool]
	// SeasonPart is the partial-season part number (the "Part 2" of a season
	// split into multiple packs). Populated alongside IsPartialSeason.
	SeasonPart Field[int]
	// IsPartialSeason marks a partial-season pack (a season released in parts).
	IsPartialSeason Field[bool]
	// IsMultiSeason marks a release spanning more than one distinct season.
	IsMultiSeason Field[bool]
	// IsMiniSeries marks a mini-series (episodes present, no season parsed —
	// upstream defaults SeasonNumber to 1 in that case).
	IsMiniSeries Field[bool]
	// Special marks a special episode.
	Special Field[bool]
	// IsSplitEpisode marks a split episode (e.g. S01E05a / S01E05b).
	IsSplitEpisode Field[bool]
	// IsSeasonExtra marks a season extra (EXTRAS / SUBPACK).
	IsSeasonExtra Field[bool]
	// DailyPart is the daily multi-part number for a daily release.
	DailyPart Field[int]
	// SpecialAbsolute are fractional absolute specials (the anime recap-special
	// drawer, e.g. 12.5). Populated best-effort by the absolute interpreter;
	// reported, not enforced.
	SpecialAbsolute Field[[]float64]
	// Kind names the authoritative namespace for this release.
	Kind NumberingKind
}

// IdentityAttrs are the advertised identity hints. They are claims the string
// makes about *which work* this is, not a resolution — matching turns them into
// a real Media entity. Edition lives here (not in Release): like year, it
// disambiguates which artifact of a work this is (Director's Cut vs Theatrical),
// and arr models it alongside title/year, not inside the quality model.
type IdentityAttrs struct {
	// Title is the parsed work title ("The Office").
	Title Field[string]
	// Year is the disambiguating year, 0 if absent.
	Year Field[int]
	// TypeHint is the parser's guess at the domain: "movie" or "series".
	TypeHint Field[string]
	// Edition is the movie cut ("Director's Cut", "Extended", "IMAX"). Movie
	// domain only; empty for series.
	Edition Field[string]
	// AllTitles are the alternate / AKA titles (paren, pipe, or "AKA" variants).
	// Mirrors upstream SeriesTitleInfo.AllTitles / ParsedMovieInfo.MovieTitles.
	AllTitles Field[[]string]
	// Numbering is the episode numbering, preserved in its own namespace.
	Numbering Numbering
}

// QualityAttrs are the advertised quality claims — the orthogonal attribute
// core (Source, Resolution, Modifier) plus the revision and the per-domain
// rendered bin (Name / Full). Parse populates the bin from BinFor(core,
// domain) at parse time; the domain is the required Parse argument.
type QualityAttrs struct {
	// Name is the rendered per-domain bin label (Sonarr's "Bluray-1080p Remux"
	// / Radarr's "Remux-1080p"). Mirrors *arr's Quality.Name API field. Empty
	// when the core didn't fire or the triple has no bin in the chosen
	// vocabulary.
	Name Field[string]
	// Full is Name plus the {Quality Full} revision-render suffix (Radarr
	// Organizer/FileNameBuilder.cs:359): "Proper" when version > 1, "Repack"
	// when isRepack on an original revision, and "REAL" repeated `real` times
	// for the REAL counter. Trailing whitespace is trimmed so a release with
	// no proper/real renders identically to Name.
	Full Field[string]
	// Resolution is the advertised resolution ("1080p", "2160p", "SD").
	Resolution Field[string]
	// Source is the advertised source — the medium axis ("BluRay", "WEB-DL",
	// "HDTV", "TV", "DVD", …). Orthogonal: remux/raw-ness lives on Modifier.
	Source Field[string]
	// Modifier is the orthogonal modifier axis ("REMUX", "BR-DISK", "RAWHD", "").
	// The canonical carrier of remux/full-disc/raw-ness.
	Modifier Field[string]
	// IsRepack marks a repack.
	IsRepack Field[bool]
	// Version is the proper/revision version (1 = original).
	Version Field[int]
	// Real is the REAL-proper counter (a second-order proper on top of version).
	Real Field[int]
}

// ReleaseAttrs are the advertised release metadata. Some of these (codec, audio,
// HDR, dual-audio, languages) have an asserted counterpart ffprobe can later
// confirm; the rest (release group) are name-derived only. The codec/audio/HDR/
// dual-audio fields exist now but stay zero-valued until the extend in a later
// step, and they have no parse-oracle so they are excluded from parity %.
type ReleaseAttrs struct {
	// ReleaseGroup is the scene/fansub group ("DIMENSION", "Hatsuyuki").
	ReleaseGroup Field[string]
	// Codec is the advertised video codec ("x264", "x265", "HEVC"). EXTEND.
	Codec Field[string]
	// AudioFormat is the advertised audio format ("DTS-HD MA", "TrueHD"). EXTEND.
	AudioFormat Field[string]
	// AudioChannels is the advertised channel layout ("5.1", "7.1", "2.0"). EXTEND.
	AudioChannels Field[string]
	// HDR is the advertised HDR claim ("HDR10", "DV", ""). EXTEND.
	HDR Field[string]
	// DualAudio marks an advertised dual-audio (sub/dub) claim — reserved as a
	// first-class anime axis. EXTEND.
	DualAudio Field[bool]
	// Languages are the advertised language(s).
	Languages Field[[]string]
	// ReleaseHash is the anime CRC32 hash from "[ADE35E38]"-style names.
	ReleaseHash Field[string]
	// HardcodedSubs is the hardcoded-subs marker ("Generic Hardcoded Subs" or a
	// language-qualified value). Movie domain only (Radarr ParseHardcodeSubs).
	HardcodedSubs Field[string]
}

// ParsedRelease is the canonical advertised attribute model produced by Parse.
// It is the contract every downstream consumer reads: matching (identity hints
// + confidence), quality profiles (Quality/Release), name templates (rendering),
// routing, and the importer.
type ParsedRelease struct {
	// Input is the raw source string — a release title or an on-disk path.
	Input string
	// IsPath reports whether Input was parsed in path flavor (filename + folder
	// context) rather than as a release title.
	IsPath bool

	// Identity holds the advertised identity hints.
	Identity IdentityAttrs
	// Quality holds the advertised quality claims.
	Quality QualityAttrs
	// Release holds the advertised release metadata.
	Release ReleaseAttrs
}

// Release-type names mirror upstream's ReleaseType enum (Sonarr
// Model/ReleaseType.cs), extended with the partial-season case the task
// surfaces. These are reported for routing/rules, never enforced by parsing.
const (
	releaseTypeUnknown       = "unknown"
	releaseTypeSingleEpisode = "singleEpisode"
	releaseTypeMultiEpisode  = "multiEpisode"
	releaseTypeFullSeason    = "fullSeason"
	releaseTypePartialSeason = "partialSeason"
)

// IsDaily reports whether this is a daily-numbered release (an air date is
// present). Mirrors ParsedEpisodeInfo.IsDaily.
func (n Numbering) IsDaily() bool {
	return n.AirDate.Value != ""
}

// IsAbsoluteNumbering reports whether absolute (anime) numbering is present.
// Mirrors ParsedEpisodeInfo.IsAbsoluteNumbering, widened to also consider the
// fractional special-absolute drawer.
func (n Numbering) IsAbsoluteNumbering() bool {
	return len(n.AbsoluteNumbers.Value) > 0 || len(n.SpecialAbsolute.Value) > 0
}

// IsPossibleSpecialEpisode mirrors ParsedEpisodeInfo.IsPossibleSpecialEpisode
// (Sonarr Model/ParsedEpisodeInfo.cs:67). It uses the parsed title and numbering
// to flag a likely special: no air date + no title + (no episodes or season 0),
// OR (a title plus the Special flag), OR a lone episode 0.
func (i IdentityAttrs) IsPossibleSpecialEpisode() bool {
	n := i.Numbering
	title := i.Title.Value
	eps := n.EpisodeNumbers.Value

	noDate := n.AirDate.Value == ""
	noTitle := title == ""
	noEpisodesOrSeasonZero := len(eps) == 0 || n.Season.Value == 0

	if (noDate && noTitle && noEpisodesOrSeasonZero) || (!noTitle && n.Special.Value) {
		return true
	}
	return len(eps) == 1 && eps[0] == 0
}

// ReleaseType mirrors ParsedEpisodeInfo.ReleaseType (Sonarr
// Model/ParsedEpisodeInfo.cs:94): multi if >1 episode/absolute, single if
// exactly 1, season-pack if FullSeason. Extended with partialSeason for the
// IsPartialSeason case (which upstream's enum lacks but the routing surface
// wants to gate on).
func (n Numbering) ReleaseType() string {
	if len(n.EpisodeNumbers.Value) > 1 || len(n.AbsoluteNumbers.Value) > 1 {
		return releaseTypeMultiEpisode
	}
	if len(n.EpisodeNumbers.Value) == 1 || len(n.AbsoluteNumbers.Value) == 1 {
		return releaseTypeSingleEpisode
	}
	if n.FullSeason.Value {
		return releaseTypeFullSeason
	}
	if n.IsPartialSeason.Value {
		return releaseTypePartialSeason
	}
	return releaseTypeUnknown
}

// Values is the flat, plain-value projection of a ParsedRelease — the same
// fields with confidence and provenance stripped. The parity harness compares
// this projection against the oracle goldens; model.Release consumes the full
// Field[T] model directly. Confidence/evidence are available separately via
// Provenance().
type Values struct {
	Identity IdentityValues
	Quality  QualityValues
	Release  ReleaseValues
}

// IdentityValues is the plain-value view of IdentityAttrs.
type IdentityValues struct {
	Title     string
	Year      int
	TypeHint  string
	Edition   string
	AllTitles []string
	Numbering NumberingValues
}

// NumberingValues is the plain-value view of Numbering, plus the derived
// getters' results (IsDaily/IsAbsoluteNumbering/IsPossibleSpecialEpisode/
// ReleaseType) so rules can read them without recomputing.
type NumberingValues struct {
	Season          int
	EpisodeNumbers  []int
	AbsoluteNumbers []int
	AirDate         string
	FullSeason      bool
	SeasonPart      int
	IsPartialSeason bool
	IsMultiSeason   bool
	IsMiniSeries    bool
	Special         bool
	IsSplitEpisode  bool
	IsSeasonExtra   bool
	DailyPart       int
	SpecialAbsolute []float64
	Kind            NumberingKind

	// Derived (computed from the fields above, mirroring upstream's computed
	// properties on ParsedEpisodeInfo).
	IsDaily                  bool
	IsAbsoluteNumbering      bool
	IsPossibleSpecialEpisode bool
	ReleaseType              string
}

// QualityValues is the plain-value view of QualityAttrs.
type QualityValues struct {
	Name       string
	Full       string
	Resolution string
	Source     string
	Modifier   string
	IsRepack   bool
	Version    int
	Real       int
}

// ReleaseValues is the plain-value view of ReleaseAttrs.
type ReleaseValues struct {
	ReleaseGroup  string
	Codec         string
	AudioFormat   string
	AudioChannels string
	HDR           string
	DualAudio     bool
	Languages     []string
	ReleaseHash   string
	HardcodedSubs string
}

// Values returns the flat, confidence-stripped projection consumers read.
func (p ParsedRelease) Values() Values {
	return Values{
		Identity: IdentityValues{
			Title:     p.Identity.Title.Value,
			Year:      p.Identity.Year.Value,
			TypeHint:  p.Identity.TypeHint.Value,
			Edition:   p.Identity.Edition.Value,
			AllTitles: p.Identity.AllTitles.Value,
			Numbering: NumberingValues{
				Season:          p.Identity.Numbering.Season.Value,
				EpisodeNumbers:  p.Identity.Numbering.EpisodeNumbers.Value,
				AbsoluteNumbers: p.Identity.Numbering.AbsoluteNumbers.Value,
				AirDate:         p.Identity.Numbering.AirDate.Value,
				FullSeason:      p.Identity.Numbering.FullSeason.Value,
				SeasonPart:      p.Identity.Numbering.SeasonPart.Value,
				IsPartialSeason: p.Identity.Numbering.IsPartialSeason.Value,
				IsMultiSeason:   p.Identity.Numbering.IsMultiSeason.Value,
				IsMiniSeries:    p.Identity.Numbering.IsMiniSeries.Value,
				Special:         p.Identity.Numbering.Special.Value,
				IsSplitEpisode:  p.Identity.Numbering.IsSplitEpisode.Value,
				IsSeasonExtra:   p.Identity.Numbering.IsSeasonExtra.Value,
				DailyPart:       p.Identity.Numbering.DailyPart.Value,
				SpecialAbsolute: p.Identity.Numbering.SpecialAbsolute.Value,
				Kind:            p.Identity.Numbering.Kind,

				IsDaily:                  p.Identity.Numbering.IsDaily(),
				IsAbsoluteNumbering:      p.Identity.Numbering.IsAbsoluteNumbering(),
				IsPossibleSpecialEpisode: p.Identity.IsPossibleSpecialEpisode(),
				ReleaseType:              p.Identity.Numbering.ReleaseType(),
			},
		},
		Quality: QualityValues{
			Name:       p.Quality.Name.Value,
			Full:       p.Quality.Full.Value,
			Resolution: p.Quality.Resolution.Value,
			Source:     p.Quality.Source.Value,
			Modifier:   p.Quality.Modifier.Value,
			IsRepack:   p.Quality.IsRepack.Value,
			Version:    p.Quality.Version.Value,
			Real:       p.Quality.Real.Value,
		},
		Release: ReleaseValues{
			ReleaseGroup:  p.Release.ReleaseGroup.Value,
			Codec:         p.Release.Codec.Value,
			AudioFormat:   p.Release.AudioFormat.Value,
			AudioChannels: p.Release.AudioChannels.Value,
			HDR:           p.Release.HDR.Value,
			DualAudio:     p.Release.DualAudio.Value,
			Languages:     p.Release.Languages.Value,
			ReleaseHash:   p.Release.ReleaseHash.Value,
			HardcodedSubs: p.Release.HardcodedSubs.Value,
		},
	}
}

// Provenance() lives in provenance.go to keep the data model here uncluttered
// by the mechanical key→Field[any] mapping.
