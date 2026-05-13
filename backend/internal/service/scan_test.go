package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/kyleaupton/arrflix/internal/guessit"
)

// makeSearchMulti builds a tmdb.SearchMulti via JSON roundtrip to avoid
// dealing with the anonymous struct and embedded VoteMetrics types.
func makeSearchMulti(t *testing.T, entries []map[string]any) tmdb.SearchMulti {
	t.Helper()
	payload := map[string]any{
		"page":          1,
		"total_pages":   1,
		"total_results": len(entries),
		"results":       entries,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal search multi: %v", err)
	}
	var result tmdb.SearchMulti
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal search multi: %v", err)
	}
	return result
}

func intPtr(n int) *int { return &n }

func TestIsMediaFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{"movie.mkv", true},
		{"movie.mp4", true},
		{"movie.avi", true},
		{"movie.mov", true},
		{"movie.wmv", true},
		{"movie.flv", true},
		{"movie.m4v", true},
		{"movie.webm", true},
		{"movie.MKV", true},
		{"movie.Mp4", true},
		{"movie.AVI", true},
		{"movie.txt", false},
		{"movie.srt", false},
		{"movie.nfo", false},
		{"", false},
		{"no_extension", false},
		{".mkv", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := isMediaFile(tt.path); got != tt.want {
				t.Errorf("isMediaFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestGuessitInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		libType string
		relPath string
		want    string
	}{
		{"series", filepath.Join("South Park", "Season 01", "South Park S01E01.mkv"), "South Park S01E01.mkv"},
		{"movie", filepath.Join("The Matrix (1999)", "The Matrix.mkv"), "The Matrix (1999)"},
		{"movie", "movie.mkv", "movie.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			t.Parallel()
			got := guessitInput(tt.libType, tt.relPath)
			if got != tt.want {
				t.Errorf("guessitInput(%q, %q) = %q, want %q", tt.libType, tt.relPath, got, tt.want)
			}
		})
	}
}

func TestBuildSearchKey(t *testing.T) {
	t.Parallel()

	title := "South Park"
	year := 1999
	movieTitle := "The Matrix"

	t.Run("series groups by top dir", func(t *testing.T) {
		t.Parallel()
		r1 := guessit.ParseResult{Title: &title, Season: intPtr(1), Episode: intPtr(1)}
		r2 := guessit.ParseResult{Title: &title, Season: intPtr(1), Episode: intPtr(2)}
		k1 := buildSearchKey("series", filepath.Join("South Park", "Season 01", "S01E01.mkv"), r1)
		k2 := buildSearchKey("series", filepath.Join("South Park", "Season 01", "S01E02.mkv"), r2)
		if k1.String() != k2.String() {
			t.Errorf("expected same key, got %q and %q", k1.String(), k2.String())
		}
	})

	t.Run("movie groups by title+year", func(t *testing.T) {
		t.Parallel()
		r := guessit.ParseResult{Title: &movieTitle, Year: &year}
		k := buildSearchKey("movie", filepath.Join("The Matrix (1999)", "The Matrix.mkv"), r)
		if k.Query != "The Matrix" {
			t.Errorf("expected query 'The Matrix', got %q", k.Query)
		}
		if k.Year == nil || *k.Year != 1999 {
			t.Errorf("expected year 1999, got %v", k.Year)
		}
	})
}

func TestEvaluateSearchResults(t *testing.T) {
	t.Parallel()

	t.Run("single match auto-matches", func(t *testing.T) {
		t.Parallel()
		sr := makeSearchMulti(t, []map[string]any{
			{"id": 603, "title": "The Matrix", "media_type": "movie", "release_date": "1999-03-31"},
		})
		year := 1999
		key := tmdbSearchKey{Query: "The Matrix", Year: &year, Type: "movie"}
		match, suggestions := evaluateSearchResults("movie", key, sr)
		if match == nil {
			t.Fatal("expected auto-match")
		}
		if match.tmdbID != 603 {
			t.Errorf("expected tmdbID 603, got %d", match.tmdbID)
		}
		if len(suggestions) != 0 {
			t.Errorf("expected 0 suggestions, got %d", len(suggestions))
		}
	})

	t.Run("multiple results returns suggestions", func(t *testing.T) {
		t.Parallel()
		sr := makeSearchMulti(t, []map[string]any{
			{"id": 19995, "title": "Avatar", "media_type": "movie", "release_date": "2009-12-18"},
			{"id": 76600, "title": "Avatar 2", "media_type": "movie", "release_date": "2022-12-16"},
		})
		key := tmdbSearchKey{Query: "Avatar", Type: "movie"}
		match, suggestions := evaluateSearchResults("movie", key, sr)
		if match != nil {
			t.Fatal("expected no auto-match for ambiguous results")
		}
		if len(suggestions) != 2 {
			t.Fatalf("expected 2 suggestions, got %d", len(suggestions))
		}
	})

	t.Run("filters by media type", func(t *testing.T) {
		t.Parallel()
		sr := makeSearchMulti(t, []map[string]any{
			{"id": 123, "name": "Test Show", "media_type": "tv", "first_air_date": "2020-01-01"},
			{"id": 456, "name": "Test Person", "media_type": "person"},
		})
		key := tmdbSearchKey{Query: "Test Show", Type: "series"}
		match, _ := evaluateSearchResults("series", key, sr)
		if match == nil {
			t.Fatal("expected auto-match (only 1 tv result after filtering)")
		}
		if match.tmdbID != 123 {
			t.Errorf("expected tmdbID 123, got %d", match.tmdbID)
		}
	})
}
