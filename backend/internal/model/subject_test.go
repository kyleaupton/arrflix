package model

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/template"
)

// parsedFixture builds a ParsedRelease with hand-picked values, confidences,
// and evidence so tests can assert the constructor carries all three through.
func parsedFixture() parsing.ParsedRelease {
	return parsing.ParsedRelease{
		Input: "The.Office.S03E05.1080p.BluRay.x264-GROUP",
		Identity: parsing.IdentityAttrs{
			Title: parsing.Field[string]{Value: "The Office", Confidence: 0.9, Evidence: "title"},
			Year:  parsing.Field[int]{Value: 2005, Confidence: 0.9, Evidence: "year token"},
			Numbering: parsing.Numbering{
				Season:         parsing.Field[int]{Value: 3, Confidence: 0.9, Evidence: "S03E05"},
				EpisodeNumbers: parsing.Field[[]int]{Value: []int{5}, Confidence: 0.9, Evidence: "S03E05"},
				Kind:           parsing.NumberingSeasonEpisode,
			},
		},
		Quality: parsing.QualityAttrs{
			Full:       parsing.Field[string]{Value: "Bluray-1080p", Confidence: 0.9, Evidence: "bin"},
			Resolution: parsing.Field[string]{Value: "1080p", Confidence: 0.9, Evidence: "1080p token"},
			Source:     parsing.Field[string]{Value: "BluRay", Confidence: 0.9, Evidence: "BluRay token"},
			// Modifier deliberately absent (zero value) — exercises the "" → "NONE"
			// normalization while the parse's own confidence/evidence are kept.
			Modifier: parsing.Field[string]{Value: "", Confidence: 0.5, Evidence: "no modifier token"},
		},
		Release: parsing.ReleaseAttrs{
			ReleaseGroup:  parsing.Field[string]{Value: "GROUP", Confidence: 0.9, Evidence: "trailing group"},
			HardcodedSubs: parsing.Field[string]{Value: "Generic Hardcoded Subs", Confidence: 0.9, Evidence: "HC token"},
		},
	}
}

func TestNewSubjectCarriesFields(t *testing.T) {
	t.Parallel()

	s := NewSubject(DownloadCandidate{Title: "The.Office.S03E05.1080p.BluRay.x264-GROUP", Size: 1234}, parsedFixture())
	r := s.Release

	// Parsed namespaces preserve value + confidence + evidence.
	if r.Identity.Title.Value != "The Office" || r.Identity.Title.Confidence != 0.9 || r.Identity.Title.Evidence != "title" {
		t.Errorf("Identity.Title not carried: %+v", r.Identity.Title)
	}
	if r.Identity.Season.Value != 3 || r.Identity.Season.Confidence != 0.9 {
		t.Errorf("flattened numbering not carried: %+v", r.Identity.Season)
	}
	if got := r.Identity.EpisodeNumbers.Value; len(got) != 1 || got[0] != 5 {
		t.Errorf("Identity.EpisodeNumbers.Value = %v, want [5]", got)
	}
	if r.Quality.Resolution.Value != "1080p" || r.Quality.Resolution.Confidence != 0.9 || r.Quality.Resolution.Evidence != "1080p token" {
		t.Errorf("Quality.Resolution not carried: %+v", r.Quality.Resolution)
	}
	if r.Encode.ReleaseGroup.Value != "GROUP" || r.Encode.ReleaseGroup.Confidence != 0.9 || r.Encode.ReleaseGroup.Evidence != "trailing group" {
		t.Errorf("Encode.ReleaseGroup not carried: %+v", r.Encode.ReleaseGroup)
	}

	// Modifier "" normalizes to "NONE" on the value only.
	if r.Quality.Modifier.Value != "NONE" {
		t.Errorf("Quality.Modifier.Value = %q, want NONE", r.Quality.Modifier.Value)
	}
	if r.Quality.Modifier.Confidence != 0.5 || r.Quality.Modifier.Evidence != "no modifier token" {
		t.Errorf("modifier normalization clobbered confidence/evidence: %+v", r.Quality.Modifier)
	}

	// Derived numbering flags are computed from the parse.
	if r.Identity.ReleaseType.Value != "singleEpisode" {
		t.Errorf("Identity.ReleaseType.Value = %q, want singleEpisode", r.Identity.ReleaseType.Value)
	}

	// Candidate stays bare and authoritative.
	if r.Candidate.Size != 1234 {
		t.Errorf("Candidate.Size = %d, want 1234", r.Candidate.Size)
	}

	// Want is nil until acquisition assembles it.
	if s.Want != nil {
		t.Errorf("Want = %+v, want nil (unassembled)", s.Want)
	}
}

func TestGetFieldUnwraps(t *testing.T) {
	t.Parallel()

	s := NewSubject(DownloadCandidate{Size: 4096}, parsedFixture())

	// Parsed-namespace lookups return the bare value, not a Field wrapper.
	got, err := s.GetField("quality.resolution")
	if err != nil {
		t.Fatalf("GetField(quality.resolution): %v", err)
	}
	if v, ok := got.(string); !ok || v != "1080p" {
		t.Errorf("GetField(quality.resolution) = %#v, want bare string %q", got, "1080p")
	}

	got, err = s.GetField("encode.release_group")
	if err != nil {
		t.Fatalf("GetField(encode.release_group): %v", err)
	}
	if v, ok := got.(string); !ok || v != "GROUP" {
		t.Errorf("GetField(encode.release_group) = %#v, want bare string %q", got, "GROUP")
	}

	// Bare namespaces are returned directly.
	got, err = s.GetField("candidate.size")
	if err != nil {
		t.Fatalf("GetField(candidate.size): %v", err)
	}
	if n, ok := got.(int64); !ok || n != 4096 {
		t.Errorf("GetField(candidate.size) = %#v, want int64 4096", got)
	}

	// MediaInfo is unavailable before import.
	if _, err := s.GetField("mediainfo.video_codec"); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("GetField(mediainfo.video_codec) pre-import err = %v, want 'not available'", err)
	}

	// Want is unavailable while unassembled.
	if _, err := s.GetField("want.trigger"); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("GetField(want.trigger) unassembled err = %v, want 'not available'", err)
	}

	// A populated Want resolves to bare values.
	s.Want = &WantFields{Trigger: "rss"}
	got, err = s.GetField("want.trigger")
	if err != nil {
		t.Fatalf("GetField(want.trigger): %v", err)
	}
	if v, ok := got.(string); !ok || v != "rss" {
		t.Errorf("GetField(want.trigger) = %#v, want bare string %q", got, "rss")
	}
}

func TestListSubjectFieldsContract(t *testing.T) {
	t.Parallel()

	byPath := map[string]SubjectFieldInfo{}
	for _, f := range ListSubjectFields() {
		byPath[f.Path] = f
	}

	cases := []struct {
		path      string
		typ       string
		valueType string
		phase     Phase
		enum      []string
	}{
		{"candidate.size", "number", "int64", PhaseSearch, nil},
		{"identity.year", "number", "int", PhaseSearch, nil},
		{"identity.all_titles", "text", "[]string", PhaseSearch, nil},
		{"identity.episode_numbers", "number", "[]any", PhaseSearch, nil},
		{"quality.resolution", "enum", "string", PhaseSearch, []string{"Unknown", "SD", "480p", "576p", "720p", "1080p", "2160p"}},
		{"quality.is_repack", "boolean", "bool", PhaseSearch, nil},
		{"quality.version", "number", "int", PhaseSearch, nil},
		{"encode.release_group", "text", "string", PhaseSearch, nil},
		{"encode.hardcoded_subs", "text", "string", PhaseSearch, nil},
		{"media.tmdb_id", "number", "int64", PhaseSearch, nil},
		{"media.runtime", "number", "int", PhaseSearch, nil},
		{"want.trigger", "enum", "string", PhaseSearch, []string{"request", "rss", "upgrade", "manual"}},
		{"want.requesters", "dynamic", "[]string", PhaseSearch, nil},
		{"want.tier", "dynamic", "string", PhaseSearch, nil},
		{"mediainfo.video_codec", "enum", "string", PhaseImport, []string{"Unknown", "H.264", "H.265", "AV1", "VP9", "MPEG-2"}},
	}
	for _, tc := range cases {
		f, ok := byPath[tc.path]
		if !ok {
			t.Errorf("field %s missing from catalog", tc.path)
			continue
		}
		if f.Type != tc.typ {
			t.Errorf("%s type = %q, want %q", tc.path, f.Type, tc.typ)
		}
		if f.ValueType != tc.valueType {
			t.Errorf("%s valueType = %q, want %q", tc.path, f.ValueType, tc.valueType)
		}
		if f.Phase != tc.phase {
			t.Errorf("%s phase = %q, want %q", tc.path, f.Phase, tc.phase)
		}
		if tc.enum != nil && !reflect.DeepEqual(f.EnumValues, tc.enum) {
			t.Errorf("%s enumValues = %v, want %v", tc.path, f.EnumValues, tc.enum)
		}
	}

	// Every catalog entry carries a timeline phase; stale namespaces are gone.
	for path, f := range byPath {
		if f.Phase != PhaseSearch && f.Phase != PhaseGrab && f.Phase != PhaseImport {
			t.Errorf("%s has unknown phase %q", path, f.Phase)
		}
		if strings.HasPrefix(path, "release.") {
			t.Errorf("stale release.* path in catalog: %s", path)
		}
	}
}

func TestPhaseRank(t *testing.T) {
	t.Parallel()

	if PhaseSearch.Rank() >= PhaseGrab.Rank() || PhaseGrab.Rank() >= PhaseImport.Rank() {
		t.Errorf("timeline order broken: search=%d grab=%d import=%d",
			PhaseSearch.Rank(), PhaseGrab.Rank(), PhaseImport.Rank())
	}
	// Absent/unknown phase ranks as search, the always-knowable default.
	if Phase("").Rank() != PhaseSearch.Rank() {
		t.Errorf(`Phase("").Rank() = %d, want %d`, Phase("").Rank(), PhaseSearch.Rank())
	}
}

func TestToTemplateDataRendering(t *testing.T) {
	t.Parallel()

	season, episode := 3, 5
	epTitle := "Initiation"
	s := NewSubject(DownloadCandidate{Title: "raw title"}, parsedFixture()).
		WithMedia(MediaTypeSeries, "The Office", 2005, 2316, nil).
		WithSeriesInfo(&season, &episode, &epTitle)

	data := s.ToTemplateData()

	cases := []struct {
		name string
		tmpl string
		want string
	}{
		{"quality and candidate", "{{.Quality.Full}} {{.Candidate.Title}}", "Bluray-1080p raw title"},
		{"zero-padded media numbering", "S{{.Media.Season}}E{{.Media.Episode}}", "S03E05"},
		{"encode namespace", "{{.Encode.ReleaseGroup}}", "GROUP"},
		{"identity namespace", "{{.Identity.Title}} ({{.Identity.Year}})", "The Office (2005)"},
		{"mediainfo empty pre-import", "[{{.MediaInfo.VideoCodec}}]", "[]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := template.Render(tc.tmpl, data)
			if err != nil {
				t.Fatalf("Render(%q): %v", tc.tmpl, err)
			}
			if got != tc.want {
				t.Errorf("Render(%q) = %q, want %q", tc.tmpl, got, tc.want)
			}
		})
	}
}
