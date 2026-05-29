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

	// Poll until the persist loop has finished. All five file rows are
	// created up-front (at discovery, before matching) so counting file
	// rows no longer signals completion — instead wait for the four
	// confident files to have their identity set (random.mkv stays NULL).
	// The persist loop runs serially after MatchBatch returns; this is the
	// latest signal that all side effects have landed before reading back.
	waitForRows(t, pool, "match_decision", 5, 30*time.Second)
	waitForIdentified(t, pool, lib.ID, 4, 30*time.Second)

	// Movie 1: Inception — Tier-1 path-embed (1.0 → ×0.99 ≈ 0.99) →
	// confident. file row identified.
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

	// Movie 4: random.mkv — nothing fires. file row left unidentified.
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

	// Ranked-candidates shape on the no_match file's current decision.
	var rawJSON []byte
	if err := pool.QueryRow(context.Background(), `
		SELECT md.ranked_candidates
		FROM match_decision md
		JOIN file f ON f.id = md.file_id
		WHERE f.library_id = $1 AND f.path = 'random.mkv' AND md.superseded_at IS NULL
	`, lib.ID).Scan(&rawJSON); err != nil {
		t.Fatalf("read decision row: %v", err)
	}
	// no_match writes no candidates (NULL JSONB in the column).
	if len(rawJSON) != 0 {
		var sugs []model.SuggestedMatch
		if err := json.Unmarshal(rawJSON, &sugs); err != nil {
			t.Fatalf("unmarshal ranked_candidates: %v (raw=%s)", err, rawJSON)
		}
		if len(sugs) != 0 {
			t.Fatalf("expected 0 candidates for no_match, got %d", len(sugs))
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

// waitForIdentified waits until `expected` files in the library have had
// their identity set (media_item_id NOT NULL) by the persist loop. File
// rows are created at discovery before matching, so identity-set is the
// signal that the confident-band persist transactions have landed.
func waitForIdentified(t *testing.T, pool *pgxpool.Pool, libraryID uuid.UUID, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var count int
		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM file WHERE library_id = $1 AND media_item_id IS NOT NULL AND deleted_at IS NULL`,
			libraryID,
		).Scan(&count); err != nil {
			t.Fatalf("count identified: %v", err)
		}
		if count >= expected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	var f int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM file WHERE library_id = $1 AND media_item_id IS NOT NULL AND deleted_at IS NULL`, libraryID).Scan(&f)
	t.Fatalf("timed out waiting for %d identified files (have %d)", expected, f)
}

// assertConfident asserts an identified file exists for the path tied to
// the expected TMDB id and the matching media_item carries the right
// title + year.
func assertConfident(t *testing.T, pool *pgxpool.Pool, libraryID uuid.UUID, path string, tmdbID int64, title string, year int) {
	t.Helper()

	var (
		fTmdb  int64
		fTitle string
		fYear  int
	)
	err := pool.QueryRow(context.Background(), `
		SELECT mi.tmdb_id, mi.title, mi.year
		FROM file f
		JOIN media_item mi ON mi.id = f.media_item_id
		WHERE f.library_id = $1 AND f.path = $2 AND f.deleted_at IS NULL
	`, libraryID, path).Scan(&fTmdb, &fTitle, &fYear)
	if err != nil {
		t.Fatalf("%s: identified file row not found: %v", path, err)
	}
	if fTmdb != tmdbID {
		t.Errorf("%s: tmdb_id = %d, want %d", path, fTmdb, tmdbID)
	}
	if fTitle != title {
		t.Errorf("%s: title = %q, want %q", path, fTitle, title)
	}
	if fYear != year {
		t.Errorf("%s: year = %d, want %d", path, fYear, year)
	}
}

// assertConfidentOrReview accepts both confident and confident_review
// outcomes — both identify the file with the same shape. Used for the
// borderline name-parse-only cases where the cap lands the score exactly
// at Auto.
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

// assertNoMatch asserts a no_match decision left an unidentified file row
// (media_item_id NULL) for the given path.
func assertNoMatch(t *testing.T, pool *pgxpool.Pool, libraryID uuid.UUID, path string) {
	t.Helper()
	var unidentifiedCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM file
		WHERE library_id = $1 AND path = $2 AND media_item_id IS NULL AND deleted_at IS NULL
	`, libraryID, path).Scan(&unidentifiedCount); err != nil {
		t.Fatalf("count unidentified file for %s: %v", path, err)
	}
	if unidentifiedCount != 1 {
		t.Fatalf("%s: expected 1 unidentified file row, got %d", path, unidentifiedCount)
	}
}
