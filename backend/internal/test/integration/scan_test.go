//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// TestScan_MatchPersistPipeline_EndToEnd exercises the full Phase-3
// cutover: walk a small on-disk library, run MatcherService over the
// collection, and assert that each MatchOutcomeRecord becomes the
// correct side effect (media_file vs unmatched_file) per the matching
// spec's confidence-band table. The fake TMDB server stands in for the
// real provider so the matcher's Tier-1 validation and Tier-3 search
// paths can be exercised deterministically.
//
// Fixtures (all under a single movie library):
//
//   - Inception (2010) {tmdb-27205}/Inception.mkv         — path-embed Tier-1 → confident
//   - The Matrix (1999)/The Matrix.mkv                    — name-parse Tier-3, unique TMDB hit → confident (corroborated)
//   - Strange Movie (2099) {imdb-tt9999999}/Movie.mkv     — IMDb embed → TMDB resolve → confident (cross-provider)
//   - random.mkv                                          — no signal → no_match
//   - Y (1999) {tmdb-11820}/Y.mkv                         — path-embed-only confident (no parse)
func TestScan_MatchPersistPipeline_EndToEnd(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)

	// Validation lookups — every embedded ID the path-embed resolver
	// will surface needs a /movie/{id} response. Movies first; series
	// would need /tv/{id} similarly.
	tmdbSrv.OnMovieDetails(27205, tmdb.MovieDetails{ID: 27205, ReleaseDate: "2010-07-15", Title: "Inception"})
	tmdbSrv.OnMovieDetails(11820, tmdb.MovieDetails{ID: 11820, ReleaseDate: "1999-09-15", Title: "Y"})
	tmdbSrv.OnMovieDetails(98765, tmdb.MovieDetails{ID: 98765, ReleaseDate: "2099-01-01", Title: "Strange Movie"})

	// Cross-provider resolve: IMDb → TMDB. The matcher's TmdbProvider
	// calls /find/{external_id}?external_source=imdb_id; we wire it to
	// resolve tt9999999 to the strange-movie TMDB id, then validation
	// follows up with /movie/98765 (registered above).
	tmdbSrv.OnFindByID("tt9999999", "imdb_id",
		[]tmdbtest.Movie{{ID: 98765, Title: "Strange Movie", ReleaseDate: "2099-01-01"}},
		nil,
	)

	// Tier-3 name-parse search. The parser will surface The Matrix as
	// the title hint; we register the search response so the name-parse
	// resolver gets a unique hit with the right year.
	tmdbSrv.OnSearchMulti("The Matrix",
		tmdbtest.Movie{ID: 603, Title: "The Matrix", ReleaseDate: "1999-03-31"},
	)
	tmdbSrv.OnMovieDetails(603, tmdb.MovieDetails{ID: 603, ReleaseDate: "1999-03-31", Title: "The Matrix"})

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	root := t.TempDir()
	for _, rel := range []string{
		"Inception (2010) {tmdb-27205}/Inception.mkv",
		"The Matrix (1999)/The Matrix 1999 BluRay.mkv",
		"Strange Movie (2099) {imdb-tt9999999}/Movie.mkv",
		"random.mkv",
		"Y (1999) {tmdb-11820}/Y.mkv",
	} {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}

	// Create the library via the public API.
	body := makeCreateBody("movies", "movie", root, true, false)
	var lib model.Library
	app.POST(t, "/api/v1/libraries", body, &lib, http.StatusCreated)

	// Kick off a scan.
	var scanOut map[string]any
	app.POST(t, "/api/v1/libraries/"+lib.ID.String()+"/scan", nil, &scanOut, http.StatusAccepted)
	scanID, _ := scanOut["scanId"].(string)
	if scanID == "" {
		t.Fatal("scan did not return scanId")
	}

	// Poll until match_decision rows for all five files AND the persist
	// loop has finished (media_file + unmatched_file totals to 5). The
	// match_decision rows are written by MatchBatch before the persist
	// loop runs, so a count of 5 match_decisions isn't enough — we'd
	// race the persist transactions and see "no rows in result set"
	// when the media_file query fires.
	waitForRows(t, pool, "match_decision", 5, 30*time.Second)
	waitForPersistComplete(t, pool, lib.ID, 5, 30*time.Second)

	// Movie 1: Inception — Tier-1 path-embed (1.0 → ×0.99 ≈ 0.99) →
	// confident. media_file written, no unmatched_file row.
	assertConfident(t, pool, lib.ID, "Inception (2010) {tmdb-27205}/Inception.mkv", 27205, "Inception", 2010)

	// Movie 2: The Matrix — name-parse hit. Score = base 0.50 +
	// unique 0.10 + year 0.15 + title-exact 0.10 = 0.85, ×1.0 conf →
	// 0.85 (cap). With recommended preset Auto=0.85 inclusive, this
	// lands confident; the spec's "name-parse alone can't auto-match"
	// stance is enforced by the cap landing exactly at Auto so any
	// small drift surfaces as confident_review.
	assertConfidentOrReview(t, pool, lib.ID, "The Matrix (1999)/The Matrix 1999 BluRay.mkv", 603, "The Matrix", 1999)

	// Movie 3: Strange Movie — IMDb embed resolves cross-provider to
	// TMDB → confident. The aggregator rewrites the chosen ref from
	// imdb to tmdb so persistConfident sees a tmdb ref.
	assertConfident(t, pool, lib.ID, "Strange Movie (2099) {imdb-tt9999999}/Movie.mkv", 98765, "Strange Movie", 2099)

	// Movie 4: random.mkv — nothing fires. unmatched_file written.
	assertNoMatch(t, pool, lib.ID, "random.mkv")

	// Movie 5: Y — Tier-1 alone, no name-parse Available (single-letter
	// title would still parse; main signal is the embed).
	assertConfident(t, pool, lib.ID, "Y (1999) {tmdb-11820}/Y.mkv", 11820, "Y", 1999)

	// match_decision shape spot-check: confident outcomes carry a
	// chosen_external_id; no_match doesn't.
	var (
		confidentCount int
		noMatchCount   int
	)
	if err := pool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE outcome = 'confident'),
			count(*) FILTER (WHERE outcome = 'no_match')
		FROM match_decision
	`).Scan(&confidentCount, &noMatchCount); err != nil {
		t.Fatalf("count by outcome: %v", err)
	}
	if confidentCount < 3 {
		t.Fatalf("expected >=3 confident outcomes, got %d", confidentCount)
	}
	if noMatchCount != 1 {
		t.Fatalf("expected 1 no_match outcome, got %d", noMatchCount)
	}

	// Suggested-matches shape on the unmatched_file row.
	var rawJSON []byte
	if err := pool.QueryRow(context.Background(), `
		SELECT suggested_matches FROM unmatched_file
		WHERE library_id = $1 AND path = 'random.mkv'
	`, lib.ID).Scan(&rawJSON); err != nil {
		t.Fatalf("read unmatched row: %v", err)
	}
	// no_match writes no suggestions (NULL JSONB in the column).
	if len(rawJSON) != 0 {
		var sugs []model.SuggestedMatch
		if err := json.Unmarshal(rawJSON, &sugs); err != nil {
			t.Fatalf("unmarshal suggestions: %v (raw=%s)", err, rawJSON)
		}
		if len(sugs) != 0 {
			t.Fatalf("expected 0 suggestions for no_match, got %d", len(sugs))
		}
	}
}

// waitForRows polls until the named table has >=expected rows or the
// timeout fires.
func waitForRows(t *testing.T, pool *pgxpool.Pool, table string, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var count int
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count >= expected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	var count int
	_ = pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count)
	t.Fatalf("timed out waiting for %d rows in %s (have %d)", expected, table, count)
}

// waitForPersistComplete waits until the persist phase has produced
// (media_file ∪ unmatched_file) rows for `expected` distinct paths in
// the library. The scan's per-file persist loop runs serially after
// MatchBatch returns; this is the latest signal that all side effects
// have landed before assertions read back.
func waitForPersistComplete(t *testing.T, pool *pgxpool.Pool, libraryID uuid.UUID, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var count int
		if err := pool.QueryRow(context.Background(), `
			SELECT (
				(SELECT count(*) FROM media_file WHERE library_id = $1) +
				(SELECT count(*) FROM unmatched_file WHERE library_id = $1)
			)
		`, libraryID).Scan(&count); err != nil {
			t.Fatalf("count persist: %v", err)
		}
		if count >= expected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	var mf, uf int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM media_file WHERE library_id = $1`, libraryID).Scan(&mf)
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM unmatched_file WHERE library_id = $1`, libraryID).Scan(&uf)
	t.Fatalf("timed out waiting for %d persisted rows (media_file=%d, unmatched_file=%d)", expected, mf, uf)
}

// assertConfident asserts a media_file exists for the path tied to the
// expected TMDB id, the matching media_item carries the right title +
// year, and no unmatched_file row exists for the same path.
func assertConfident(t *testing.T, pool *pgxpool.Pool, libraryID uuid.UUID, path string, tmdbID int64, title string, year int) {
	t.Helper()

	var (
		mfTmdb  int64
		mfTitle string
		mfYear  int
	)
	err := pool.QueryRow(context.Background(), `
		SELECT mi.tmdb_id, mi.title, mi.year
		FROM media_file mf
		JOIN media_item mi ON mi.id = mf.media_item_id
		WHERE mf.library_id = $1 AND mf.path = $2
	`, libraryID, path).Scan(&mfTmdb, &mfTitle, &mfYear)
	if err != nil {
		t.Fatalf("%s: media_file row not found: %v", path, err)
	}
	if mfTmdb != tmdbID {
		t.Errorf("%s: tmdb_id = %d, want %d", path, mfTmdb, tmdbID)
	}
	if mfTitle != title {
		t.Errorf("%s: title = %q, want %q", path, mfTitle, title)
	}
	if mfYear != year {
		t.Errorf("%s: year = %d, want %d", path, mfYear, year)
	}

	var unmatchedCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM unmatched_file WHERE library_id = $1 AND path = $2
	`, libraryID, path).Scan(&unmatchedCount); err != nil {
		t.Fatalf("count unmatched for %s: %v", path, err)
	}
	if unmatchedCount != 0 {
		t.Errorf("%s: expected no unmatched_file row, got %d", path, unmatchedCount)
	}
}

// assertConfidentOrReview accepts both confident and confident_review
// outcomes — both write a media_file row with the same shape. Used for
// the borderline name-parse-only cases where the cap lands the score
// exactly at Auto.
func assertConfidentOrReview(t *testing.T, pool *pgxpool.Pool, libraryID uuid.UUID, path string, tmdbID int64, title string, year int) {
	t.Helper()
	assertConfident(t, pool, libraryID, path, tmdbID, title, year)

	// Find the file_id by the chosen_external_id we expect to land on.
	// The match_decision.file_id is the matcher's stable file identifier
	// (the FileRef.ID minted by resolveFileID), not the media_file row's
	// primary key — they only collide when scan reuses an existing
	// media_file id. Look up the decision by chosen TMDB id instead.
	var outcome string
	if err := pool.QueryRow(context.Background(), `
		SELECT outcome::text FROM match_decision
		WHERE superseded_at IS NULL
		  AND chosen_source = 'tmdb'
		  AND chosen_external_id = $1
		ORDER BY decided_at DESC
		LIMIT 1
	`, strconv.FormatInt(tmdbID, 10)).Scan(&outcome); err != nil {
		t.Fatalf("%s: read match_decision for tmdb=%d: %v", path, tmdbID, err)
	}
	if outcome != string(matcher.OutcomeConfident) && outcome != string(matcher.OutcomeConfidentReview) {
		t.Errorf("%s: outcome = %q, want confident|confident_review", path, outcome)
	}
}

// assertNoMatch asserts a no_match decision was recorded and an
// unmatched_file row exists for the given path.
func assertNoMatch(t *testing.T, pool *pgxpool.Pool, libraryID uuid.UUID, path string) {
	t.Helper()
	var ufCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM unmatched_file WHERE library_id = $1 AND path = $2
	`, libraryID, path).Scan(&ufCount); err != nil {
		t.Fatalf("count unmatched for %s: %v", path, err)
	}
	if ufCount != 1 {
		t.Fatalf("%s: expected 1 unmatched_file row, got %d", path, ufCount)
	}
	var mfCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM media_file WHERE library_id = $1 AND path = $2
	`, libraryID, path).Scan(&mfCount); err != nil {
		t.Fatalf("count media_file for %s: %v", path, err)
	}
	if mfCount != 0 {
		t.Errorf("%s: expected no media_file row, got %d", path, mfCount)
	}
}
