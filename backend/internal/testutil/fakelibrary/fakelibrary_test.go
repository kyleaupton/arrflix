package fakelibrary

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreate(t *testing.T) {
	dir := t.TempDir()
	if err := Create(dir, SceneMovies); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Spot-check a media file exists.
	p := filepath.Join(dir, "The.Matrix.1999.1080p.BluRay.x264-FGT/The.Matrix.1999.1080p.BluRay.x264-FGT.mkv")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	// Spot-check a noise file exists.
	nfo := filepath.Join(dir, "The.Matrix.1999.1080p.BluRay.x264-FGT/The.Matrix.1999.1080p.BluRay.x264-FGT.nfo")
	if _, err := os.Stat(nfo); err != nil {
		t.Fatalf("expected nfo to exist: %v", err)
	}

	// Verify nested subs dir was created.
	srt := filepath.Join(dir, "Se7en.1995.REMASTERED.1080p.BluRay.x264-AMIABLE/subs/Se7en.srt")
	if _, err := os.Stat(srt); err != nil {
		t.Fatalf("expected srt to exist: %v", err)
	}
}

func TestCreateTemp(t *testing.T) {
	dir := CreateTemp(t, CleanSeries)

	ep := filepath.Join(dir, "Breaking Bad/Season 01/Breaking Bad S01E01.mkv")
	if _, err := os.Stat(ep); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestCreateAll(t *testing.T) {
	for _, lib := range All {
		dir := CreateTemp(t, lib)

		// Every declared file must exist.
		for _, rel := range lib.Files {
			abs := filepath.Join(dir, rel)
			if _, err := os.Stat(abs); err != nil {
				t.Errorf("[%s] missing file %s: %v", lib.Name, rel, err)
			}
		}
	}
}

func TestLookup(t *testing.T) {
	lib, err := Lookup("scene-movies")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if lib.Type != "movie" {
		t.Fatalf("expected movie, got %s", lib.Type)
	}

	_, err = Lookup("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown fixture")
	}
}

func TestByType(t *testing.T) {
	movies := ByType("movie")
	if len(movies) != 3 {
		t.Fatalf("expected 3 movie fixtures, got %d", len(movies))
	}
	for _, m := range movies {
		if m.Type != "movie" {
			t.Errorf("expected type movie, got %s", m.Type)
		}
	}

	series := ByType("series")
	if len(series) != 3 {
		t.Fatalf("expected 3 series fixtures, got %d", len(series))
	}
}

func TestByTypeAndStyle(t *testing.T) {
	lib, err := ByTypeAndStyle("movie", "scene")
	if err != nil {
		t.Fatalf("ByTypeAndStyle: %v", err)
	}
	if lib.Name != "scene-movies" {
		t.Fatalf("expected scene-movies, got %s", lib.Name)
	}

	lib, err = ByTypeAndStyle("series", "embedded-id")
	if err != nil {
		t.Fatalf("ByTypeAndStyle: %v", err)
	}
	if lib.Name != "embedded-id-series" {
		t.Fatalf("expected embedded-id-series, got %s", lib.Name)
	}
}
