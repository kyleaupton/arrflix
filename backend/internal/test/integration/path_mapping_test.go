//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// fakeQbittorrent serves the slice of qBittorrent's Web API that a
// connection test and a save-path check use, reporting savePath as its
// default save path.
func fakeQbittorrent(t *testing.T, savePath string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "sid"})
		_, _ = w.Write([]byte("Ok."))
	})
	mux.HandleFunc("GET /api/v2/app/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("v5.0.0"))
	})
	mux.HandleFunc("GET /api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("2.11"))
	})
	mux.HandleFunc("GET /api/v2/app/defaultSavePath", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(savePath))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func createQbitAt(t *testing.T, app *testapp.App, name, url string) model.Downloader {
	t.Helper()
	var dl model.Downloader
	app.POST(t, "/api/v1/downloaders", map[string]any{
		"name":     name,
		"type":     "qbittorrent",
		"protocol": "torrent",
		"url":      url,
		"enabled":  true,
		"default":  false,
	}, &dl, http.StatusCreated)
	return dl
}

func pathChecks(t *testing.T, app *testapp.App) map[uuid.UUID]model.DownloaderPathCheck {
	t.Helper()
	var checks []model.DownloaderPathCheck
	app.GET(t, "/api/v1/path-mapping/checks", &checks, http.StatusOK)
	byID := make(map[uuid.UUID]model.DownloaderPathCheck, len(checks))
	for _, c := range checks {
		byID[c.DownloaderID] = c
	}
	return byID
}

// REQ-PATH-003 — An install where every actor sees the same paths must need
// no path configuration.
//
// With no volumes, a downloader whose default save path exists in Arrflix is
// reported as needing no mapping, and its connection test says its paths
// resolve. One whose save path Arrflix can't see is reported as such.
func TestReqPath003_ChecksReportNoMappingNeeded(t *testing.T) {
	t.Parallel()
	app := testapp.New(t, dbtest.New(t))
	shared := t.TempDir()
	sharedDL := createQbitAt(t, app, "shared", fakeQbittorrent(t, shared))
	hiddenDL := createQbitAt(t, app, "hidden", fakeQbittorrent(t, "/downloads/nowhere"))

	checks := pathChecks(t, app)
	if got := checks[sharedDL.ID]; got.Status != model.PathCheckNoMappingNeeded ||
		got.SavePath != shared || got.ResolvedPath != shared || got.VolumeID != nil || got.DownloaderName != "shared" {
		t.Errorf("shared check = %+v, want no mapping needed at %q", got, shared)
	}
	if got := checks[hiddenDL.ID]; got.Status != model.PathCheckNotVisible || got.ResolvedPath != "/downloads/nowhere" {
		t.Errorf("hidden check = %+v, want not visible at /downloads/nowhere", got)
	}

	var ok, bad downloader.TestResult
	app.POST(t, "/api/v1/downloaders/"+sharedDL.ID.String()+"/test", nil, &ok, http.StatusOK)
	if ok.PathsResolve == nil || !*ok.PathsResolve {
		t.Errorf("shared downloader test pathsResolve = %v, want true", ok.PathsResolve)
	}
	app.POST(t, "/api/v1/downloaders/"+hiddenDL.ID.String()+"/test", nil, &bad, http.StatusOK)
	if bad.PathsResolve == nil || *bad.PathsResolve {
		t.Errorf("hidden downloader test pathsResolve = %v, want false", bad.PathsResolve)
	}
}

func TestPathMapping_ChecksAndPreviewFollowVolumes(t *testing.T) {
	t.Parallel()
	app := testapp.New(t, dbtest.New(t))
	dl := createQbitAt(t, app, "qbit", fakeQbittorrent(t, "/downloads"))
	base := t.TempDir()

	if got := pathChecks(t, app)[dl.ID]; got.Status != model.PathCheckNotVisible {
		t.Fatalf("check before mapping = %+v, want not visible", got)
	}

	var preview []model.DownloaderPathCheck
	app.POST(t, "/api/v1/path-mapping/preview", map[string]any{
		"arrflixBase": base,
		"aliases":     []map[string]any{aliasBody(dl.ID, "/downloads")},
	}, &preview, http.StatusOK)
	if len(preview) != 1 || preview[0].Status != model.PathCheckMapped || preview[0].ResolvedPath != base {
		t.Errorf("preview = %+v, want the draft mapping /downloads to %q", preview, base)
	}
	if got := pathChecks(t, app)[dl.ID]; got.Status != model.PathCheckNotVisible {
		t.Errorf("check after preview = %+v, want still not visible: preview persists nothing", got)
	}

	var v model.Volume
	app.POST(t, "/api/v1/volumes", volumeBody("media", base, aliasBody(dl.ID, "/downloads")), &v, http.StatusCreated)
	got := pathChecks(t, app)[dl.ID]
	if got.Status != model.PathCheckMapped || got.VolumeID == nil || *got.VolumeID != v.ID || got.VolumeName != "media" {
		t.Errorf("check after mapping = %+v, want mapped via media", got)
	}

	// Editing the saved volume's alias away in a draft previews it unmapped.
	app.POST(t, "/api/v1/path-mapping/preview", map[string]any{
		"volumeId":    v.ID,
		"arrflixBase": base,
		"aliases":     []map[string]any{},
	}, &preview, http.StatusOK)
	if len(preview) != 1 || preview[0].Status != model.PathCheckNotVisible {
		t.Errorf("preview without the alias = %+v, want not visible", preview)
	}
}

// REQ-PATH-004 — An operator must be able to verify a mapping before relying
// on it, including whether imports will hardlink or copy.
//
// Testing a downloader path mapped through a volume reports the resolved path,
// the volume, and that a library on the same filesystem hardlinks. A path that
// doesn't exist is reported missing with no library verdicts. (The copy
// verdict needs a second filesystem and is covered by the pathmapping unit
// test.)
func TestReqPath004_TestEndpointReportsHardlinkAndNotFound(t *testing.T) {
	t.Parallel()
	app := testapp.New(t, dbtest.New(t))
	dl := createQbitAt(t, app, "qbit", fakeQbittorrent(t, "/downloads"))
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "torrents"), 0o755); err != nil {
		t.Fatal(err)
	}
	libRoot := t.TempDir()
	var lib model.Library
	app.POST(t, "/api/v1/libraries", map[string]any{
		"name": "Movies", "type": "movie", "rootPath": libRoot, "enabled": true, "default": true,
	}, &lib, http.StatusCreated)
	var v model.Volume
	app.POST(t, "/api/v1/volumes", volumeBody("media", base, aliasBody(dl.ID, "/downloads")), &v, http.StatusCreated)

	var got model.PathTestResult
	app.POST(t, "/api/v1/path-mapping/test", map[string]any{
		"downloaderId": dl.ID, "path": "/downloads/torrents",
	}, &got, http.StatusOK)
	if got.ResolvedPath != filepath.Join(base, "torrents") || !got.Exists ||
		got.VolumeID == nil || *got.VolumeID != v.ID || got.VolumeName != "media" {
		t.Fatalf("test = %+v, want %q via media, existing", got, filepath.Join(base, "torrents"))
	}
	if len(got.Libraries) != 1 || got.Libraries[0].LibraryID != lib.ID || got.Libraries[0].Verdict != "hardlink" {
		t.Errorf("library verdicts = %+v, want Movies hardlink", got.Libraries)
	}

	var missing model.PathTestResult
	app.POST(t, "/api/v1/path-mapping/test", map[string]any{
		"downloaderId": dl.ID, "path": "/downloads/nope",
	}, &missing, http.StatusOK)
	if missing.Exists || missing.Libraries == nil || len(missing.Libraries) != 0 {
		t.Errorf("missing = %+v, want not found and an empty library list", missing)
	}

	app.POST(t, "/api/v1/path-mapping/test", map[string]any{
		"downloaderId": dl.ID, "path": "downloads",
	}, nil, http.StatusUnprocessableEntity)
	app.POST(t, "/api/v1/path-mapping/test", map[string]any{
		"downloaderId": uuid.New(), "path": "/downloads",
	}, nil, http.StatusNotFound)
}
