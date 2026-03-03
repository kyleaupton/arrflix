// Package fakelibrary provides reusable test fixture definitions for media
// libraries with realistic file naming patterns. Use Create or CreateTemp to
// materialise fixtures on disk for scanner integration tests.
package fakelibrary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Library describes a set of files to materialise under a single root.
type Library struct {
	Name  string   // human-readable label
	Type  string   // "movie" or "series"
	Files []string // relative paths — intermediate dirs are created automatically
}

// ---------------------------------------------------------------------------
// Movie fixtures
// ---------------------------------------------------------------------------

// SceneMovies contains scene/P2P release naming — no embedded IDs, no clean
// renaming. This is the primary pattern for guessit-based title resolution.
var SceneMovies = Library{
	Name: "scene-movies",
	Type: "movie",
	Files: []string{
		// Standard scene naming: release dir + matching filename
		"The.Matrix.1999.1080p.BluRay.x264-FGT/The.Matrix.1999.1080p.BluRay.x264-FGT.mkv",
		"The.Matrix.1999.1080p.BluRay.x264-FGT/The.Matrix.1999.1080p.BluRay.x264-FGT.nfo",

		// Generic inner filename
		"No.Country.for.Old.Men.2007.720p.BluRay.x264-DON/movie.mkv",

		// Foreign language tag
		"Parasite.2019.KOREAN.1080p.BluRay.x265-VXT/Parasite.2019.KOREAN.1080p.BluRay.x265-VXT.mkv",

		// 4K WEB-DL with Atmos audio
		"Dune.Part.Two.2024.2160p.AMZN.WEB-DL.DDP5.1.Atmos.H.265-FLUX/movie.mkv",

		// Remastered tag, trimmed inner filename
		"Se7en.1995.REMASTERED.1080p.BluRay.x264-AMIABLE/Se7en.1995.REMASTERED.mkv",

		// Extra noise files that the scanner should skip
		"Se7en.1995.REMASTERED.1080p.BluRay.x264-AMIABLE/Se7en.1995.REMASTERED.nfo",
		"Se7en.1995.REMASTERED.1080p.BluRay.x264-AMIABLE/cover.jpg",
		"Se7en.1995.REMASTERED.1080p.BluRay.x264-AMIABLE/subs/Se7en.srt",
	},
}

// CleanMovies contains human-organised naming with year in parentheses —
// the typical Radarr/renamed layout.
var CleanMovies = Library{
	Name: "clean-movies",
	Type: "movie",
	Files: []string{
		"The Matrix (1999)/The Matrix.mkv",
		"No Country for Old Men (2007)/No Country for Old Men.mkv",
		"Parasite (2019)/Parasite.mkv",
		"Dune Part Two (2024)/Dune Part Two.mkv",
		"Se7en (1995)/Se7en.mkv",
	},
}

// EmbeddedIDMovies contains the existing {tmdb-XXX} naming convention used
// by Arrflix's own import system. Kept for regression coverage.
var EmbeddedIDMovies = Library{
	Name: "embedded-id-movies",
	Type: "movie",
	Files: []string{
		"The Matrix (1999) {tmdb-603}/The Matrix.mkv",
		"No Country for Old Men (2007) {tmdb-6977}/No Country for Old Men.mkv",
		"Parasite (2019) {tmdb-496243}/Parasite.mkv",
		"Dune Part Two (2024) {tmdb-693134}/Dune Part Two.mkv",
		"Se7en (1995) {tmdb-807}/Se7en.mkv",
	},
}

// ---------------------------------------------------------------------------
// Series fixtures
// ---------------------------------------------------------------------------

// SceneSeries contains scene-named episodes — flat files, show-only dirs,
// and show+season dirs.
var SceneSeries = Library{
	Name: "scene-series",
	Type: "series",
	Files: []string{
		// Flat file, no show directory at all
		"Breaking.Bad.S01E01.720p.BluRay.x264-DEMAND.mkv",

		// Show dir, episodes directly inside (no season folder)
		"The.Office.US/The.Office.US.S03E12.1080p.WEB-DL.DD5.1.mkv",
		"The.Office.US/The.Office.US.S03E13.1080p.WEB-DL.DD5.1.mkv",

		// Show dir + season dir
		"Game.of.Thrones/Season.01/Game.of.Thrones.S01E01.1080p.BluRay.x264-DEMAND.mkv",
		"Game.of.Thrones/Season.01/Game.of.Thrones.S01E02.1080p.BluRay.x264-DEMAND.mkv",
		"Game.of.Thrones/Season.02/Game.of.Thrones.S02E01.720p.BluRay.x264-DEMAND.mkv",

		// Noise
		"Game.of.Thrones/Season.01/Game.of.Thrones.S01E01.nfo",
		"Game.of.Thrones/Season.01/poster.jpg",
	},
}

// CleanSeries contains human-organised naming (Sonarr-style renaming).
var CleanSeries = Library{
	Name: "clean-series",
	Type: "series",
	Files: []string{
		"Breaking Bad/Season 01/Breaking Bad S01E01.mkv",
		"Breaking Bad/Season 01/Breaking Bad S01E02.mkv",
		"The Office (US)/Season 03/The Office S03E12.mkv",
		"Game of Thrones/Season 01/Game of Thrones S01E01.mkv",
		"Game of Thrones/Season 01/Game of Thrones S01E02.mkv",
		"Game of Thrones/Season 02/Game of Thrones S02E01.mkv",
	},
}

// EmbeddedIDSeries contains the existing {tmdb-XXX} naming convention for
// series. Kept for regression coverage.
var EmbeddedIDSeries = Library{
	Name: "embedded-id-series",
	Type: "series",
	Files: []string{
		"Breaking Bad {tmdb-1396}/Season 01/Breaking Bad S01E01.mkv",
		"Breaking Bad {tmdb-1396}/Season 01/Breaking Bad S01E02.mkv",
		"The Office (US) {tmdb-2316}/Season 03/The Office S03E12.mkv",
		"Game of Thrones {tmdb-1399}/Season 01/Game of Thrones S01E01.mkv",
		"Game of Thrones {tmdb-1399}/Season 01/Game of Thrones S01E02.mkv",
		"Game of Thrones {tmdb-1399}/Season 02/Game of Thrones S02E01.mkv",
	},
}

// All is every predefined fixture library.
var All = append([]Library{
	SceneMovies,
	CleanMovies,
	EmbeddedIDMovies,
	SceneSeries,
	CleanSeries,
	EmbeddedIDSeries,
}, allRealWorld...)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Create materialises lib's files as empty files under dir. Intermediate
// directories are created automatically.
func Create(dir string, lib Library) error {
	for _, rel := range lib.Files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(abs), err)
		}
		if err := os.WriteFile(abs, nil, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", abs, err)
		}
	}
	return nil
}

// CreateTemp creates a temp directory via t.TempDir(), materialises lib's
// files inside it, and returns the path.
func CreateTemp(t *testing.T, lib Library) string {
	t.Helper()
	dir := t.TempDir()
	if err := Create(dir, lib); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Lookup returns the first Library whose Name matches name, or an error.
func Lookup(name string) (Library, error) {
	for _, lib := range All {
		if lib.Name == name {
			return lib, nil
		}
	}
	return Library{}, fmt.Errorf("unknown fixture library %q", name)
}

// ByType returns all predefined fixtures matching the given type
// ("movie" or "series").
func ByType(typ string) []Library {
	var out []Library
	for _, lib := range All {
		if lib.Type == typ {
			out = append(out, lib)
		}
	}
	return out
}

// LoadJSON reads a JSON file produced by cmd/dumplib and returns a Library.
// This lets you turn a real user's library dump into a test fixture.
func LoadJSON(path string) (Library, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Library{}, fmt.Errorf("read %s: %w", path, err)
	}
	var lib Library
	if err := json.Unmarshal(data, &lib); err != nil {
		return Library{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return lib, nil
}

// LoadJSONTemp reads a JSON fixture, materialises it in a temp directory,
// and returns (dir, Library). Handy one-liner for tests.
func LoadJSONTemp(t *testing.T, path string) (string, Library) {
	t.Helper()
	lib, err := LoadJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := CreateTemp(t, lib)
	return dir, lib
}

// ByTypeAndStyle returns the fixture matching both type and style suffix.
// Style is one of "scene", "clean", "embedded-id".
func ByTypeAndStyle(typ, style string) (Library, error) {
	want := style + "-" + typ
	if typ == "movie" {
		want = style + "-movies"
	} else if typ == "series" {
		want = style + "-series"
	}
	return Lookup(want)
}
