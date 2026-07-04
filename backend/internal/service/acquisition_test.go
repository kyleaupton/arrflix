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
		{"season extras", "Show.S03.Extras.Behind.the.Scenes.1080p.BluRay-NTb", releaseReject},
		{"season subpack", "Show.S03.SUBPACK.1080p.BluRay-NTb", releaseReject},
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

// TestPackRuntimeMinutes proves a pack sizes against its covered episodes' summed
// runtime: a full-season pack sums the whole season, a range sums only its
// numbers, an episode's missing runtime falls back to the series runtime (then
// 45), and a season with no episode rows yields nil (caller keeps single-episode
// runtime).
func TestPackRuntimeMinutes(t *testing.T) {
	t.Parallel()

	seriesRT := ptr(30)
	eps := []model.MediaEpisode{
		{EpisodeNumber: 1, Runtime: ptr(int32(22))},
		{EpisodeNumber: 2, Runtime: ptr(int32(24))},
		{EpisodeNumber: 3, Runtime: nil}, // falls back to series runtime (30)
	}

	parse := func(title string) parsing.ParsedRelease {
		return parsing.Parse(title, parsing.DomainSeries)
	}

	t.Run("full season sums every episode, series-runtime fallback for missing", func(t *testing.T) {
		t.Parallel()
		got := packRuntimeMinutes(parse("Show.S03.COMPLETE.1080p.BluRay.x264"), eps, seriesRT)
		if got == nil || *got != 22+24+30 {
			t.Fatalf("packRuntimeMinutes = %v, want %d", got, 22+24+30)
		}
	})

	t.Run("range sums only covered numbers", func(t *testing.T) {
		t.Parallel()
		got := packRuntimeMinutes(parse("Show.S03E01-E02.1080p.BluRay.x264"), eps, seriesRT)
		if got == nil || *got != 22+24 {
			t.Fatalf("packRuntimeMinutes = %v, want %d", got, 22+24)
		}
	})

	t.Run("no episode rows yields nil", func(t *testing.T) {
		t.Parallel()
		got := packRuntimeMinutes(parse("Show.S03.COMPLETE.1080p.BluRay.x264"), nil, seriesRT)
		if got != nil {
			t.Fatalf("packRuntimeMinutes = %v, want nil", got)
		}
	})

	t.Run("45-minute fallback when neither episode nor series runtime known", func(t *testing.T) {
		t.Parallel()
		bare := []model.MediaEpisode{{EpisodeNumber: 1}, {EpisodeNumber: 2}}
		got := packRuntimeMinutes(parse("Show.S03.COMPLETE.1080p.BluRay.x264"), bare, nil)
		if got == nil || *got != 45+45 {
			t.Fatalf("packRuntimeMinutes = %v, want %d", got, 90)
		}
	})
}

// TestWantToQuery covers the season-scoped broadening: an episode want with ≥2
// in-flight siblings drops the episode token so season packs enter the pool,
// while a lone episode keeps the precise S%02dE%02d query.
func TestWantToQuery(t *testing.T) {
	t.Parallel()

	s := &AcquisitionService{}
	mi := model.MediaItem{Title: "Rick and Morty"}
	ep := &episodeCtx{season: 5, episode: 1}

	t.Run("episode-scoped keeps the episode token and sets Episode", func(t *testing.T) {
		t.Parallel()
		q := s.wantToQuery(mi, ep, false)
		if q.Query != "Rick and Morty S05E01" {
			t.Errorf("query = %q, want %q", q.Query, "Rick and Morty S05E01")
		}
		if q.Episode == nil || *q.Episode != 1 {
			t.Errorf("Episode = %v, want 1", q.Episode)
		}
	})

	t.Run("season-scoped drops the episode token and leaves Episode nil", func(t *testing.T) {
		t.Parallel()
		q := s.wantToQuery(mi, ep, true)
		if q.Query != "Rick and Morty S05" {
			t.Errorf("query = %q, want %q", q.Query, "Rick and Morty S05")
		}
		if q.Episode != nil {
			t.Errorf("Episode = %v, want nil (season-scoped omits the episode)", *q.Episode)
		}
		if q.Season == nil || *q.Season != 5 {
			t.Errorf("Season = %v, want 5", q.Season)
		}
	})
}

// TestCoveredWantIDs_ExclusionShrinks proves a sibling that has excluded the pack
// release is carved out of its coverage, across full-season and range packs. An
// exclusion keyed on a different release leaves coverage untouched.
func TestCoveredWantIDs_ExclusionShrinks(t *testing.T) {
	t.Parallel()

	const indexerID = int64(7)
	const packGUID = "pack-guid"

	e1, e2, e3 := uuid.New(), uuid.New(), uuid.New()
	siblings := []seasonSibling{
		{want: model.Want{ID: e1}, episode: 1},
		{want: model.Want{ID: e2}, episode: 2},
		{want: model.Want{ID: e3}, episode: 3},
	}
	packEval := func(title string) *qualityprofile.Evaluation {
		cand := model.DownloadCandidate{IndexerID: indexerID, GUID: packGUID, Title: title}
		return &qualityprofile.Evaluation{Subject: model.NewSubject(cand, parsing.Parse(title, parsing.DomainSeries))}
	}
	fullSeason := packEval("Show.S03.COMPLETE.1080p.BluRay.x264")
	rangePack := packEval("Show.S03E01-E02.1080p.BluRay.x264")

	// excludeFor builds an index where each listed want excludes the pack release.
	excludeFor := func(wantIDs ...uuid.UUID) exclusionIndex {
		rows := make([]model.WantReleaseExclusion, 0, len(wantIDs))
		for _, id := range wantIDs {
			rows = append(rows, model.WantReleaseExclusion{WantID: id, IndexerID: indexerID, GUID: packGUID})
		}
		return buildExclusionIndex(rows)
	}

	cases := []struct {
		name string
		pack *qualityprofile.Evaluation
		excl exclusionIndex
		want []uuid.UUID
	}{
		{"full season, no exclusions", fullSeason, nil, []uuid.UUID{e1, e2, e3}},
		{"full season, e2 excludes", fullSeason, excludeFor(e2), []uuid.UUID{e1, e3}},
		{"full season, all exclude → none covered", fullSeason, excludeFor(e1, e2, e3), nil},
		{"range covers only e1,e2", rangePack, nil, []uuid.UUID{e1, e2}},
		{"range, e1 excludes → only e2", rangePack, excludeFor(e1), []uuid.UUID{e2}},
		{
			"exclusion on a different release is ignored",
			fullSeason,
			buildExclusionIndex([]model.WantReleaseExclusion{{WantID: e1, IndexerID: 99, GUID: "other"}}),
			[]uuid.UUID{e1, e2, e3},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := coveredWantIDs(tc.pack, siblings, tc.excl)
			if !uuidSliceEqual(got, tc.want) {
				t.Errorf("coveredWantIDs = %v, want %v", got, tc.want)
			}
		})
	}
}

// uuidSliceEqual reports order-sensitive slice equality (coveredWantIDs preserves
// sibling order), treating nil and empty as equal.
func uuidSliceEqual(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestExclusionIndex covers buildExclusionIndex + excludes: a present (want,
// release) pair matches, the same release under a different want does not, an
// absent release key does not, and a nil index reports false for everything.
func TestExclusionIndex(t *testing.T) {
	t.Parallel()

	w1, w2 := uuid.New(), uuid.New()
	idx := buildExclusionIndex([]model.WantReleaseExclusion{
		{WantID: w1, IndexerID: 7, GUID: "a"},
		{WantID: w2, IndexerID: 7, GUID: "a"},
		{WantID: w1, IndexerID: 7, GUID: "b"},
	})

	if !idx.excludes(w1, 7, "a") {
		t.Errorf("w1 should exclude (7, a)")
	}
	if !idx.excludes(w2, 7, "a") {
		t.Errorf("w2 should exclude (7, a)")
	}
	if !idx.excludes(w1, 7, "b") {
		t.Errorf("w1 should exclude (7, b)")
	}
	if idx.excludes(w2, 7, "b") {
		t.Errorf("w2 should NOT exclude (7, b) — only w1 did")
	}
	if idx.excludes(w1, 7, "zzz") {
		t.Errorf("absent guid should not match")
	}
	if idx.excludes(w1, 99, "a") {
		t.Errorf("absent indexer should not match")
	}

	var nilIdx exclusionIndex
	if nilIdx.excludes(w1, 7, "a") {
		t.Errorf("nil index should exclude nothing")
	}
}
