package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parsed := parsing.Parse(tc.title, parsing.DomainMovie)
			kind, reason := relevanceReason(tc.res, parsed, mi, tc.imdbID, nil)
			if ok := kind != releaseReject; ok != tc.wantOK {
				t.Fatalf("relevanceReason kind = %v, want ok=%v (reason=%q)", kind, tc.wantOK, reason)
			}
		})
	}
}

// TestClassifyEpisodeRelease covers the front-half gate across the five title
// shapes: a single for the wanted episode, a full-season pack, a multi-episode
// range that covers it, and the rejects (wrong episode, wrong season,
// multi-season, partial-season, a range that misses the want).
func TestClassifyEpisodeRelease(t *testing.T) {
	t.Parallel()

	ep := &episodeCtx{season: 3, episode: 5}
	cases := []struct {
		name  string
		title string
		want  releaseKind
	}{
		{"single wanted episode", "Show.S03E05.1080p.BluRay.x264", releaseSingle},
		{"single wrong episode", "Show.S03E06.1080p.BluRay.x264", releaseReject},
		{"full season pack", "Show.S03.COMPLETE.1080p.BluRay.x264", releasePack},
		{"range covering wanted", "Show.S03E01-E06.1080p.BluRay.x264", releasePack},
		{"range missing wanted", "Show.S03E01-E04.1080p.BluRay.x264", releaseReject},
		{"wrong season", "Show.S02E05.1080p.BluRay.x264", releaseReject},
		{"multi-season pack", "Show.S01-S03.1080p.BluRay.x264", releaseReject},
		{"partial-season pack", "Show.S03.Part.2.1080p.BluRay.x264", releaseReject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parsed := parsing.Parse(tc.title, parsing.DomainSeries)
			got, reason := classifyEpisodeRelease(parsed, ep)
			if got != tc.want {
				t.Errorf("classifyEpisodeRelease(%q) = %v (%q), want %v", tc.title, got, reason, tc.want)
			}
		})
	}
}

// TestChoosePick_CoverageGuard proves the coverage-first ≥2 guard: a single beats
// a pack that covers only one want, a pack covering two wants beats the single, a
// lone-coverage pack is the fallback when no single qualifies, and an empty field
// yields no pick.
func TestChoosePick_CoverageGuard(t *testing.T) {
	t.Parallel()

	wantID := uuid.New()
	single := &qualityprofile.Evaluation{}
	pack := &qualityprofile.Evaluation{}

	t.Run("single beats 1-coverage pack", func(t *testing.T) {
		t.Parallel()
		out := choosePick(wantID, single, pack, []uuid.UUID{wantID})
		if out == nil || out.isPack {
			t.Fatalf("choosePick = %+v, want the single (pack covers only 1)", out)
		}
		if len(out.coveredWantIDs) != 1 || out.coveredWantIDs[0] != wantID {
			t.Errorf("single coveredWantIDs = %v, want [%s]", out.coveredWantIDs, wantID)
		}
	})

	t.Run("pack at coverage 2 beats single", func(t *testing.T) {
		t.Parallel()
		covered := []uuid.UUID{wantID, uuid.New()}
		out := choosePick(wantID, single, pack, covered)
		if out == nil || !out.isPack {
			t.Fatalf("choosePick = %+v, want the pack (covers 2)", out)
		}
		if len(out.coveredWantIDs) != 2 {
			t.Errorf("pack coveredWantIDs = %v, want 2", out.coveredWantIDs)
		}
	})

	t.Run("lone-coverage pack is the fallback when no single", func(t *testing.T) {
		t.Parallel()
		out := choosePick(wantID, nil, pack, []uuid.UUID{wantID})
		if out == nil || !out.isPack {
			t.Fatalf("choosePick = %+v, want the lone-coverage pack fallback", out)
		}
	})

	t.Run("no winner in either group", func(t *testing.T) {
		t.Parallel()
		if out := choosePick(wantID, nil, nil, nil); out != nil {
			t.Errorf("choosePick = %+v, want nil", out)
		}
	})
}
