package model

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/template"
)

// Phase is one of the three moments on the evaluation timeline,
// search < grab < import. Every registered field is tagged with the earliest
// moment it's knowable; evaluating at moment M, a field with phase ≤ M
// resolves normally and a later-phase field is indeterminate. Knowledge only
// accumulates along the timeline — it's a ratchet, never a reset.
type Phase string

const (
	PhaseSearch Phase = "search"
	PhaseGrab   Phase = "grab"
	PhaseImport Phase = "import"
)

// Rank orders the timeline: search(0) < grab(1) < import(2). An absent or
// unknown phase tag ranks as search — the earliest moment, so the field is
// always knowable.
func (p Phase) Rank() int {
	switch p {
	case PhaseGrab:
		return 1
	case PhaseImport:
		return 2
	default:
		return 0
	}
}

// Subject is the unit of evaluation: everything a rule or a name template can
// ask about one prospective acquisition, in one object. It is the situation,
// of which the release is the centerpiece but not the whole:
//   - Release — the artifact under consideration (candidate.* identity.*
//     quality.* encode.* mediainfo.*)
//   - Media   — media.*  what we matched it to (TMDB)
//   - Want    — want.*   why we're acquiring it (tracking); nil until
//     acquisition assembles it
//
// decision.* (what quality concluded; grab phase) is reserved — no struct
// until routing iteration 2.
//
// The Go nesting is structure, not path grammar — field paths stay flat
// (quality.resolution, media.title, want.trigger).
type Subject struct {
	Release Release
	Media   MediaFields `namespace:"media"`
	Want    *WantFields `namespace:"want"` // nil until acquisition assembles it
}

// MediaFields contains TMDB/media metadata
type MediaFields struct {
	Type         string  `path:"media.type" label:"Media Type" type:"enum" enumValues:"movie,series" phase:"search"`
	Title        string  `path:"media.title" label:"Media Title" type:"text" phase:"search"`
	CleanTitle   string  `path:"media.clean_title" label:"Clean Title" type:"text" phase:"search"`
	Year         int     `path:"media.year" label:"Year" type:"number" phase:"search"`
	TmdbID       int64   `path:"media.tmdb_id" label:"TMDB ID" type:"number" phase:"search"`
	Runtime      *int    `path:"media.runtime" label:"Runtime (minutes)" type:"number" phase:"search"`
	Season       *int    `path:"media.season" label:"Season" type:"number" phase:"search"`
	Episode      *int    `path:"media.episode" label:"Episode" type:"number" phase:"search"`
	EpisodeTitle *string `path:"media.episode_title" label:"Episode Title" type:"text" phase:"search"`
}

// WantFields contains the durable acquisition-intent facts from tracking: what
// put this acquisition in motion, who wants it, and under which tier. The
// requester set is tracking-derived, not request-derived — it can be empty
// (an RSS or admin grab), and `contains` over an empty set is definitively
// false, not indeterminate. Requesters carries user ids.
//
// Registered but unassembled: no producer populates Subject.Want until
// acquisition lands, so these fields evaluate as indeterminate (nil Want)
// everywhere today. Dynamic-source values are directional; pin them when
// acquisition wires the namespace.
type WantFields struct {
	Trigger    string   `path:"want.trigger" label:"Trigger" type:"enum" enumValues:"request,rss,upgrade,manual" phase:"search"`
	Requesters []string `path:"want.requesters" label:"Requesters" type:"dynamic" dynamicSource:"/api/v1/users" phase:"search"`
	Tier       string   `path:"want.tier" label:"Tier" type:"dynamic" phase:"search"`
}

// NewSubject creates a Subject from a DownloadCandidate and a parsed release.
// It builds the Release half, filling the parsed namespaces (Identity,
// Quality, Encode) by copying the parse's Field[T] directly — confidence and
// evidence ride along. Media is populated later by matching
// (WithMedia/WithSeriesInfo), MediaInfo by the ffprobe extractor
// (WithMediaInfo), and Want by acquisition (not built yet). The
// Identity/Quality/Encode split mirrors the parse model — notably Edition
// lives under identity.* (it identifies the cut) and hardcoded-subs under
// encode.* (an encode property).
//
// Quality.Name / Quality.Full are taken straight from the parse — Parse
// rendered them in the domain's vocabulary because the caller passed the
// domain to Parse.
func NewSubject(candidate DownloadCandidate, parsed parsing.ParsedRelease) Subject {
	// Parsing's ModNone is the empty string (mirroring Radarr's Modifier.NONE
	// being its zero enum value). Surface it to rules and the routing UI as the
	// canonical "NONE" label so the enum stays closed — normalized on the value
	// only; confidence and evidence stay the parse's own.
	modifier := parsed.Quality.Modifier
	if modifier.Value == "" {
		modifier.Value = "NONE"
	}

	n := parsed.Identity.Numbering
	return Subject{
		Release: Release{
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
			MediaInfo: nil,
		},
		Media: MediaFields{},
		Want:  nil,
	}
}

// WithMedia sets the media fields on the subject. runtime is the matched
// media's runtime in minutes (the movie runtime, or a series' per-episode
// runtime); nil when unknown, in which case size bands are not enforced.
func (s Subject) WithMedia(mediaType MediaType, title string, year int, tmdbID int64, runtime *int) Subject {
	s.Media = MediaFields{
		Type:       string(mediaType),
		Title:      title,
		CleanTitle: template.CleanTitle(title),
		Year:       year,
		TmdbID:     tmdbID,
		Runtime:    runtime,
	}
	return s
}

// WithSeriesInfo sets series-specific media fields
func (s Subject) WithSeriesInfo(season, episode *int, episodeTitle *string) Subject {
	s.Media.Season = season
	s.Media.Episode = episode
	s.Media.EpisodeTitle = episodeTitle
	return s
}

// WithMediaInfo sets the mediainfo fields on the Release half (import time)
func (s Subject) WithMediaInfo(mi *MediaInfoFields) Subject {
	s.Release.MediaInfo = mi
	return s
}

// GetField retrieves a field value by its path (e.g., "candidate.size",
// "quality.resolution", "want.trigger"). Parsed-namespace fields are unwrapped
// to their bare .Value — callers always get plain values regardless of
// namespace.
func (s *Subject) GetField(path string) (any, error) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid field path: %s (expected namespace.field)", path)
	}

	namespace := parts[0]
	fieldPath := parts[1]

	switch namespace {
	case "candidate":
		return getFieldByPath(&s.Release.Candidate, "candidate."+fieldPath)
	case "identity":
		return getFieldByPath(&s.Release.Identity, "identity."+fieldPath)
	case "quality":
		return getFieldByPath(&s.Release.Quality, "quality."+fieldPath)
	case "encode":
		return getFieldByPath(&s.Release.Encode, "encode."+fieldPath)
	case "media":
		return getFieldByPath(&s.Media, "media."+fieldPath)
	case "want":
		if s.Want == nil {
			return nil, fmt.Errorf("want not available (unassembled)")
		}
		return getFieldByPath(s.Want, "want."+fieldPath)
	case "mediainfo":
		if s.Release.MediaInfo == nil {
			return nil, fmt.Errorf("mediainfo not available (pre-import)")
		}
		return getFieldByPath(s.Release.MediaInfo, "mediainfo."+fieldPath)
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

// SubjectFieldInfo represents metadata about a field available on the Subject
type SubjectFieldInfo struct {
	Path          string   `json:"path"`
	Label         string   `json:"label"`
	Type          string   `json:"type"`      // text, number, enum, boolean, dynamic
	ValueType     string   `json:"valueType"` // string, int64, int, float64, bool, []string
	Phase         Phase    `json:"phase"`
	EnumValues    []string `json:"enumValues,omitempty"`
	DynamicSource string   `json:"dynamicSource,omitempty"`
}

// ListSubjectFields returns all available fields with their metadata
func ListSubjectFields() []SubjectFieldInfo {
	var fields []SubjectFieldInfo

	// Collect fields from each namespace struct
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(CandidateFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(IdentityFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(QualityFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(EncodeFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(MediaFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(WantFields{}))...)
	fields = append(fields, extractFieldsFromStruct(reflect.TypeOf(MediaInfoFields{}))...)

	return fields
}

// extractFieldsFromStruct extracts SubjectFieldInfo from struct tags
func extractFieldsFromStruct(t reflect.Type) []SubjectFieldInfo {
	var fields []SubjectFieldInfo

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		path := field.Tag.Get("path")
		if path == "" {
			continue
		}

		info := SubjectFieldInfo{
			Path:          path,
			Label:         field.Tag.Get("label"),
			Type:          field.Tag.Get("type"),
			Phase:         Phase(field.Tag.Get("phase")),
			DynamicSource: field.Tag.Get("dynamicSource"),
		}
		// An absent phase tag means search — the earliest moment.
		if info.Phase == "" {
			info.Phase = PhaseSearch
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

// ToTemplateData converts the subject to a map suitable for Go templates
// This provides namespaced access (e.g., .Candidate.Title, .Media.Title)
func (s *Subject) ToTemplateData() map[string]any {
	// Build a custom map for Media so Season/Episode render as zero-padded strings
	media := map[string]any{
		"Type":       s.Media.Type,
		"Title":      s.Media.Title,
		"CleanTitle": s.Media.CleanTitle,
		"Year":       s.Media.Year,
		"TmdbID":     s.Media.TmdbID,
	}
	if s.Media.Season != nil {
		media["Season"] = fmt.Sprintf("%02d", *s.Media.Season)
	}
	if s.Media.Episode != nil {
		media["Episode"] = fmt.Sprintf("%02d", *s.Media.Episode)
	}
	if s.Media.EpisodeTitle != nil {
		media["EpisodeTitle"] = *s.Media.EpisodeTitle
	}

	data := map[string]any{
		"Candidate": s.Release.Candidate,
		"Identity":  templateMap(s.Release.Identity),
		"Quality":   templateMap(s.Release.Quality),
		"Encode":    templateMap(s.Release.Encode),
		"Media":     media,
	}

	// Always include MediaInfo (empty struct if not available) to avoid <no value> in templates
	if s.Release.MediaInfo != nil {
		data["MediaInfo"] = s.Release.MediaInfo
	} else {
		data["MediaInfo"] = &MediaInfoFields{}
	}

	return data
}
