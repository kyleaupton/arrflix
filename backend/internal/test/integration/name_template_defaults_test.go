//go:build integration

package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/routing"
	"github.com/kyleaupton/arrflix/internal/template"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

func evaluateRouting(t *testing.T, app *testapp.App, mediaType string) routing.Evaluation {
	t.Helper()
	var eval routing.Evaluation
	app.POST(t, "/api/v1/routing/evaluate", map[string]any{
		"mediaType": mediaType,
		"candidate": map[string]any{
			"title": "Some.Title.2024.1080p", "filename": "", "link": "", "guid": "g",
			"indexer": "i", "indexerId": 1, "indexerFlags": []string{},
			"categories": []string{"Movies"}, "protocol": "torrent",
			"publishDate": "2024-01-01T00:00:00Z", "size": 1, "seeders": 1,
			"peers": 0, "age": 0, "ageHours": 0, "grabs": 0,
		},
	}, &eval, http.StatusOK)
	return eval
}

// REQ-HARD-001 — A fresh install without the dev seed must be able to
// complete a grab: with no seed and no hand-made templates, adding a library
// and a downloader is enough to route both a movie and a series.
func TestReqHard001_FreshInstallRoutesMovieAndSeries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	app := testapp.New(t, dbtest.New(t))

	// Startup seeding, as cmd/api runs it.
	if err := app.Services.NameTemplates.SeedDefaults(ctx); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	app.POST(t, "/api/v1/downloaders", map[string]any{
		"name": "qBit", "type": "qbittorrent", "protocol": "torrent",
		"url": "http://localhost:9999", "enabled": true, "default": true,
	}, &model.Downloader{}, http.StatusCreated)
	for _, typ := range []string{"movie", "series"} {
		app.POST(t, "/api/v1/libraries", map[string]any{
			"name": typ, "type": typ, "rootPath": t.TempDir(), "enabled": true, "default": true,
		}, &model.Library{}, http.StatusCreated)
	}

	for _, typ := range []string{"movie", "series"} {
		eval := evaluateRouting(t, app, typ)
		if eval.Final.DownloaderID == nil || eval.Final.LibraryID == nil || eval.Final.NameTemplateID == nil {
			t.Errorf("%s: dispatch left a slot unresolved: %+v", typ, eval.Final)
		}
	}
}

// REQ-HARD-001 — Seeding is idempotent and leaves exactly one default
// template per type, whether the install has none or already has its own.
func TestReqHard001_SeedKeepsExactlyOneDefaultPerType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	app := testapp.New(t, dbtest.New(t))

	// An existing install with a user-made default movie template.
	var custom model.NameTemplate
	app.POST(t, "/api/v1/name-templates", map[string]any{
		"name": "Mine", "type": "movie", "template": "{{.Media.Title}}",
		"movieDirTemplate": "{{.Media.Title}}", "default": true,
	}, &custom, http.StatusCreated)

	for range 2 {
		if err := app.Services.NameTemplates.SeedDefaults(ctx); err != nil {
			t.Fatalf("SeedDefaults: %v", err)
		}
	}

	var all []model.NameTemplate
	app.GET(t, "/api/v1/name-templates", &all, http.StatusOK)
	defaults := map[string][]model.NameTemplate{}
	for _, nt := range all {
		if nt.Default {
			defaults[nt.Type] = append(defaults[nt.Type], nt)
		}
	}
	if len(defaults["movie"]) != 1 || defaults["movie"][0].ID != custom.ID {
		t.Errorf("movie defaults = %+v, want only the user's template", defaults["movie"])
	}
	if len(defaults["series"]) != 1 {
		t.Errorf("series defaults = %d, want 1", len(defaults["series"]))
	}
}

// REQ-HARD-001 — The seeded default templates render without double spaces
// when optional parts such as edition are missing.
func TestReqHard001_DefaultTemplatesRenderWithoutDoubleSpaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	app := testapp.New(t, dbtest.New(t))
	if err := app.Services.NameTemplates.SeedDefaults(ctx); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	data := map[string]any{
		"Media": map[string]any{
			"CleanTitle": "Heat", "Title": "Heat", "Year": 1995, "TmdbID": 949,
			"Season": 1, "Episode": 2, "EpisodeTitle": "Pilot",
		},
		"Identity":  map[string]any{},
		"Quality":   map[string]any{"Full": "Bluray-1080p"},
		"MediaInfo": map[string]any{},
		"Encode":    map[string]any{},
	}
	for _, typ := range []string{"movie", "series"} {
		nt, err := app.Services.NameTemplates.GetDefault(ctx, typ)
		if err != nil {
			t.Fatalf("GetDefault(%s): %v", typ, err)
		}
		parts := []string{nt.Template}
		if nt.MovieDirTemplate != nil {
			parts = append(parts, *nt.MovieDirTemplate)
		}
		if nt.SeriesShowTemplate != nil {
			parts = append(parts, *nt.SeriesShowTemplate)
		}
		if nt.SeriesSeasonTemplate != nil {
			parts = append(parts, *nt.SeriesSeasonTemplate)
		}
		for _, p := range parts {
			out, err := template.Render(p, data)
			if err != nil {
				t.Fatalf("render %q: %v", p, err)
			}
			if strings.Contains(out, "  ") || strings.TrimSpace(out) != out || out == "" {
				t.Errorf("%s: rendered %q has stray whitespace", typ, out)
			}
		}
	}
}
