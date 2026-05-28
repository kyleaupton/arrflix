package template

import (
	"testing"

	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/quality"
)

// quality_map mirrors the QualityFields shape EvaluationContext.ToTemplateData
// exposes, projecting the parsed core into the given domain's bin so
// `{{.Quality.Full}}` resolves the same way a real evaluation would. Built here
// (not via model.NewEvaluationContext) to keep this test out of the model →
// template import cycle.
func quality_map(q parsing.QualityValues, domain quality.Domain) map[string]any {
	bin := quality.BinFor(parsing.Source(q.Source), parsing.Resolution(q.Resolution), parsing.Modifier(q.Modifier), domain)
	return map[string]any{
		"Full":       bin.Name,
		"Resolution": q.Resolution,
		"Source":     q.Source,
		"Modifier":   q.Modifier,
		"IsRepack":   q.IsRepack,
		"Version":    q.Version,
		"Real":       q.Real,
	}
}

func TestRender(t *testing.T) {
	// Quality.Full is the per-domain bin name, projected from the parsed core
	// by internal/quality; rendering against the series vocabulary yields
	// "Bluray-2160p Remux" for this 2160p Bluray remux release.
	q := parsing.Parse("21.Jump.Street.2012.2160p.UHD.BluRay.REMUX.HEVC.TrueHD.Atmos-GROUP", parsing.AsSeries()).Values().Quality
	qualityMap := quality_map(q, quality.DomainSeries)

	// Create a namespaced context structure matching EvaluationContext.ToTemplateData()
	media := map[string]any{
		"Title":      "21 Jump Street",
		"CleanTitle": "21 Jump Street",
		"Year":       2012,
	}

	ctx := map[string]any{
		"Media":   media,
		"Quality": qualityMap,
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "Simple title and year",
			template: "{{.Media.Title}} ({{.Media.Year}})",
			want:     "21 Jump Street (2012)",
		},
		{
			name:     "With quality resolution",
			template: "{{.Media.Title}} ({{.Media.Year}}) [{{.Quality.Resolution}}]",
			want:     "21 Jump Street (2012) [2160p]",
		},
		{
			name:     "With clean title",
			template: "{{.Media.CleanTitle}} ({{.Media.Year}})",
			want:     "21 Jump Street (2012)",
		},
		{
			name:     "With clean function for unknown",
			template: "{{.Media.Title}} ({{.Media.Year}}) [{{clean .Quality.Resolution}}]",
			want:     "21 Jump Street (2012) [2160p]",
		},
		{
			name:     "With sanitize function",
			template: "{{sanitize .Media.Title}}",
			want:     "21 Jump Street",
		},
		{
			name:     "Full quality string",
			template: "{{.Media.CleanTitle}} ({{.Media.Year}}) [{{.Quality.Full}}]",
			want:     "21 Jump Street (2012) [Bluray-2160p Remux]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.template, ctx)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Render() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderZeroPaddedSeasonEpisode(t *testing.T) {
	q := parsing.Parse("Breaking.Bad.S01E05.720p.BluRay.x264-GROUP", parsing.AsSeries()).Values().Quality
	qualityMap := quality_map(q, quality.DomainSeries)

	media := map[string]any{
		"Title":        "Breaking Bad",
		"CleanTitle":   "Breaking Bad",
		"Year":         2008,
		"Season":       "01",
		"Episode":      "05",
		"EpisodeTitle": "Gray Matter",
	}

	ctx := map[string]any{
		"Media":   media,
		"Quality": qualityMap,
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "Series with zero-padded season and episode",
			template: "{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}} - {{.Media.EpisodeTitle}}",
			want:     "Breaking Bad - S01E05 - Gray Matter",
		},
		{
			name:     "Season directory template",
			template: "Season {{.Media.Season}}",
			want:     "Season 01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.template, ctx)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Render() got = %v, want %v", got, tt.want)
			}
		})
	}
}
