package service

import (
	"testing"

	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

func ptr[T any](v T) *T { return &v }

// TestRelevanceReason covers the ID-preferred relevance gate: a result carrying
// a stable id is decided by that id alone, and only an id-less result falls
// through to the parsed title+year check.
func TestRelevanceReason(t *testing.T) {
	t.Parallel()

	mi := model.MediaItem{
		Title:  "Fight Club",
		Year:   ptr(int32(1999)),
		TmdbID: ptr(int64(550)),
	}
	wantImdb := ptr("tt0137523") // 137523

	cases := []struct {
		name   string
		res    indexer.SearchResult
		title  string // release title fed to the parser for the fallback path
		imdbID *string
		wantOK bool
	}{
		{
			name:   "matching tmdbId accepts",
			res:    indexer.SearchResult{TmdbID: 550},
			title:  "Some.Unrelated.Name.1999.1080p",
			imdbID: wantImdb,
			wantOK: true,
		},
		{
			name:   "mismatching tmdbId rejects even when title matches",
			res:    indexer.SearchResult{TmdbID: 999},
			title:  "Fight Club 1999 1080p BluRay",
			imdbID: wantImdb,
			wantOK: false,
		},
		{
			name:   "matching imdbId accepts when no tmdbId on result",
			res:    indexer.SearchResult{ImdbID: 137523},
			title:  "Some.Unrelated.Name.1999.1080p",
			imdbID: wantImdb,
			wantOK: true,
		},
		{
			name:   "mismatching imdbId rejects",
			res:    indexer.SearchResult{ImdbID: 111111},
			title:  "Fight Club 1999 1080p BluRay",
			imdbID: wantImdb,
			wantOK: false,
		},
		{
			name:   "no-id result falls back to title+year match",
			res:    indexer.SearchResult{},
			title:  "Fight Club 1999 1080p BluRay x264",
			imdbID: wantImdb,
			wantOK: true,
		},
		{
			name:   "no-id result falls back and rejects wrong title",
			res:    indexer.SearchResult{},
			title:  "Michael 2026 1080p WEB",
			imdbID: wantImdb,
			wantOK: false,
		},
		{
			name:   "imdbId on result but want has none falls back to title",
			res:    indexer.SearchResult{ImdbID: 137523},
			title:  "Fight Club 1999 1080p BluRay",
			imdbID: nil,
			wantOK: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parsed := parsing.Parse(tc.title, parsing.DomainMovie)
			reason, ok := relevanceReason(tc.res, parsed, mi, tc.imdbID)
			if ok != tc.wantOK {
				t.Fatalf("relevanceReason ok = %v, want %v (reason=%q)", ok, tc.wantOK, reason)
			}
		})
	}
}
