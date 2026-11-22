//go:build integration

package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

func createTestDownloader(t *testing.T, app *testapp.App, name string) model.Downloader {
	t.Helper()
	var dl model.Downloader
	app.POST(t, "/api/v1/downloaders", map[string]any{
		"name":     name,
		"type":     "qbittorrent",
		"protocol": "torrent",
		"url":      "http://localhost:9999",
		"enabled":  true,
		"default":  false,
	}, &dl, http.StatusCreated)
	return dl
}

func volumeBody(name, arrflixBase string, aliases ...map[string]any) map[string]any {
	if aliases == nil {
		aliases = []map[string]any{}
	}
	return map[string]any{"name": name, "arrflixBase": arrflixBase, "aliases": aliases}
}

func aliasBody(downloaderID uuid.UUID, base string) map[string]any {
	return map[string]any{"downloaderId": downloaderID, "base": base}
}

func TestVolumes_CRUD(t *testing.T) {
	t.Parallel()
	app := testapp.New(t, dbtest.New(t))
	dl := createTestDownloader(t, app, "qbit")
	base := t.TempDir()

	var created model.Volume
	app.POST(t, "/api/v1/volumes", volumeBody("media", base+"/", aliasBody(dl.ID, "/downloads/")), &created, http.StatusCreated)
	if created.ID == uuid.Nil || created.Name != "media" || created.ArrflixBase != base {
		t.Fatalf("created = %+v, want media at cleaned base %q", created, base)
	}
	if len(created.Aliases) != 1 || created.Aliases[0].DownloaderID != dl.ID ||
		created.Aliases[0].Base != "/downloads" || created.Aliases[0].Flavor != "posix" {
		t.Fatalf("created aliases = %+v, want one posix alias /downloads for %s", created.Aliases, dl.ID)
	}

	var got model.Volume
	app.GET(t, "/api/v1/volumes/"+created.ID.String(), &got, http.StatusOK)
	if got.ID != created.ID || len(got.Aliases) != 1 {
		t.Errorf("get = %+v, want %+v", got, created)
	}

	newBase := t.TempDir()
	var updated model.Volume
	app.PUT(t, "/api/v1/volumes/"+created.ID.String(), volumeBody("Media", newBase), &updated, http.StatusOK)
	if updated.Name != "Media" || updated.ArrflixBase != newBase || len(updated.Aliases) != 0 {
		t.Errorf("updated = %+v, want renamed, rebased, no aliases", updated)
	}

	var list []model.Volume
	app.GET(t, "/api/v1/volumes", &list, http.StatusOK)
	if len(list) != 1 || list[0].ID != created.ID || list[0].Aliases == nil {
		t.Errorf("list = %+v, want the one volume with an empty alias list", list)
	}

	app.DELETE(t, "/api/v1/volumes/"+created.ID.String(), http.StatusNoContent)
	app.GET(t, "/api/v1/volumes/"+created.ID.String(), nil, http.StatusNotFound)
	app.DELETE(t, "/api/v1/volumes/"+created.ID.String(), http.StatusNotFound)
}

func TestVolumes_ValidationCollectsEveryFieldError(t *testing.T) {
	t.Parallel()
	app := testapp.New(t, dbtest.New(t))
	dl := createTestDownloader(t, app, "qbit")

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		body   map[string]any
		fields []string
	}{
		{"relative base and relative alias", volumeBody("v", "data", aliasBody(dl.ID, "downloads")),
			[]string{"body.arrflixBase", "body.aliases[0].base"}},
		{"missing base", volumeBody("v", filepath.Join(t.TempDir(), "missing")),
			[]string{"body.arrflixBase"}},
		{"base is a file", volumeBody("v", file),
			[]string{"body.arrflixBase"}},
		{"unknown downloader and duplicate downloader", volumeBody("v", t.TempDir(),
			aliasBody(uuid.New(), "/a"), aliasBody(dl.ID, "/b"), aliasBody(dl.ID, "/c")),
			[]string{"body.aliases[0].downloaderId", "body.aliases[2].downloaderId"}},
	}
	for _, tc := range cases {
		var pd apperrors.ProblemDetails
		app.POST(t, "/api/v1/volumes", tc.body, &pd, http.StatusUnprocessableEntity)
		for _, loc := range tc.fields {
			findFieldError(t, pd, loc)
		}
		if len(pd.Errors) != len(tc.fields) {
			t.Errorf("%s: field errors = %+v, want exactly %v", tc.name, pd.Errors, tc.fields)
		}
	}

	var list []model.Volume
	app.GET(t, "/api/v1/volumes", &list, http.StatusOK)
	if len(list) != 0 {
		t.Errorf("volumes after rejected creates = %d, want 0", len(list))
	}
}

func TestVolumes_Conflicts(t *testing.T) {
	t.Parallel()
	app := testapp.New(t, dbtest.New(t))
	base := t.TempDir()

	var first model.Volume
	app.POST(t, "/api/v1/volumes", volumeBody("media", base), &first, http.StatusCreated)

	app.POST(t, "/api/v1/volumes", volumeBody("MEDIA", t.TempDir()), nil, http.StatusConflict)
	app.POST(t, "/api/v1/volumes", volumeBody("other", base+"/."), nil, http.StatusConflict)

	// A nested base is an overlap, not a duplicate.
	nested := filepath.Join(base, "tv")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	var second model.Volume
	app.POST(t, "/api/v1/volumes", volumeBody("tv", nested), &second, http.StatusCreated)

	app.PUT(t, "/api/v1/volumes/"+second.ID.String(), volumeBody("Media", nested), nil, http.StatusConflict)
	app.PUT(t, "/api/v1/volumes/"+second.ID.String(), volumeBody("tv", base), nil, http.StatusConflict)
	// Saving a volume unchanged doesn't conflict with itself.
	app.PUT(t, "/api/v1/volumes/"+first.ID.String(), volumeBody("media", base), nil, http.StatusOK)
}

func TestVolumes_AliasCascadesWithDownloader(t *testing.T) {
	t.Parallel()
	app := testapp.New(t, dbtest.New(t))
	keep := createTestDownloader(t, app, "keep")
	gone := createTestDownloader(t, app, "gone")

	var v model.Volume
	app.POST(t, "/api/v1/volumes", volumeBody("media", t.TempDir(),
		aliasBody(keep.ID, "/keep"), aliasBody(gone.ID, "/gone")), &v, http.StatusCreated)

	app.DELETE(t, "/api/v1/downloaders/"+gone.ID.String(), http.StatusNoContent)

	var got model.Volume
	app.GET(t, "/api/v1/volumes/"+v.ID.String(), &got, http.StatusOK)
	if len(got.Aliases) != 1 || got.Aliases[0].DownloaderID != keep.ID {
		t.Errorf("aliases after deleting a downloader = %+v, want only %s's", got.Aliases, keep.ID)
	}
}
