//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// matchDecisionWire mirrors handlers.MatchDecisionResponse for
// decoding. We could import the handler type, but per CLAUDE.md tests
// decode into a local mirror only when the production type is genuinely
// internal; here the wire shape is the contract, so we shadow it
// directly and any field rename will break the test. Reasonable
// tradeoff for a fresh endpoint surface.
type matchDecisionWire struct {
	ID                 int64           `json:"id"`
	FileID             string          `json:"fileId"`
	Outcome            string          `json:"outcome"`
	ChosenRef          *externalRefDTO `json:"chosenRef,omitempty"`
	ChosenItem         *metadataItem   `json:"chosenItem,omitempty"`
	ChosenEpisode      *episodeRefDTO  `json:"chosenEpisode,omitempty"`
	ChosenEdition      *string         `json:"chosenEdition,omitempty"`
	Confidence         float64         `json:"confidence"`
	ResolversConsulted json.RawMessage `json:"resolversConsulted"`
	Evidence           json.RawMessage `json:"evidence"`
	EvidenceTruncated  bool            `json:"evidenceTruncated"`
	DecidedBy          string          `json:"decidedBy"`
	DecidedAt          string          `json:"decidedAt"`
	SupersededAt       *string         `json:"supersededAt,omitempty"`
	SupersededBy       *int64          `json:"supersededBy,omitempty"`
}

type externalRefDTO struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
}

type episodeRefDTO struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

type metadataItem struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
	Type       string `json:"type,omitempty"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	Redirected bool   `json:"redirected,omitempty"`
}

type matchDecisionView struct {
	Current matchDecisionWire   `json:"current"`
	History []matchDecisionWire `json:"history,omitempty"`
}

type detachResponse struct {
	Decision        matchDecisionWire `json:"decision"`
	QuarantinedPath *string           `json:"quarantinedPath,omitempty"`
}

// seedUnmatchedFile creates a live file row with NULL identity (the inbox
// state) plus its file_state via raw SQL, so the test doesn't have to
// drive the whole scan loop. This is a per-CLAUDE.md backdoor; the
// alternative (running scan with files on disk) is heavyweight when the
// assertion is about the action endpoints. The id matches
// matcher.FileRef.ID so /files/{id} resolves.
func seedUnmatchedFile(t *testing.T, app *testapp.App, libraryID uuid.UUID, path string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := app.Pool.QueryRow(context.Background(),
		`INSERT INTO file (library_id, path)
		 VALUES ($1, $2)
		 RETURNING id`,
		libraryID, path,
	).Scan(&id); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := app.Pool.Exec(context.Background(),
		`INSERT INTO file_state (file_id, exists, size_bytes)
		 VALUES ($1, true, $2)`,
		id, int64(1024),
	); err != nil {
		t.Fatalf("seed file_state: %v", err)
	}
	return id
}

// seedMediaFile creates a media_item + identified file + file_state triple
// via raw SQL. Used by the re-match / unmatch / detach tests to drive a
// "file already identified" precondition without running the scan loop.
func seedMediaFile(t *testing.T, app *testapp.App, libraryID uuid.UUID, tmdbID int64, title string, year int, path string) uuid.UUID {
	t.Helper()
	var mediaItemID uuid.UUID
	if err := app.Pool.QueryRow(context.Background(),
		`INSERT INTO media_item (type, title, year, tmdb_id)
		 VALUES ('movie', $1, $2, $3)
		 RETURNING id`,
		title, year, tmdbID,
	).Scan(&mediaItemID); err != nil {
		t.Fatalf("seed media_item: %v", err)
	}
	var fileID uuid.UUID
	if err := app.Pool.QueryRow(context.Background(),
		`INSERT INTO file (library_id, media_item_id, path)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		libraryID, mediaItemID, path,
	).Scan(&fileID); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := app.Pool.Exec(context.Background(),
		`INSERT INTO file_state (file_id, exists, size_bytes)
		 VALUES ($1, true, $2)`,
		fileID, int64(1024),
	); err != nil {
		t.Fatalf("seed file_state: %v", err)
	}
	return fileID
}

// seedLibrary builds a movie library with a real on-disk root, returns
// the library uuid + root path. Used by every test that needs the
// /files endpoints — the handlers join library.RootPath to file.Path
// before passing to commitMatch.
func seedLibrary(t *testing.T, app *testapp.App, name string) (uuid.UUID, string) {
	t.Helper()
	root := t.TempDir()
	body := makeCreateBody(name, "movie", root, true, false)
	var lib struct {
		ID uuid.UUID `json:"id"`
	}
	app.POST(t, "/api/v1/libraries", body, &lib, http.StatusCreated)
	return lib.ID, root
}

// countMatchDecisions returns the (total, current) pair for a file id.
func countMatchDecisions(t *testing.T, app *testapp.App, fileID uuid.UUID) (int, int) {
	t.Helper()
	var total, current int
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM match_decision WHERE file_id = $1`, fileID,
	).Scan(&total); err != nil {
		t.Fatalf("count total: %v", err)
	}
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM match_decision WHERE file_id = $1 AND superseded_at IS NULL`, fileID,
	).Scan(&current); err != nil {
		t.Fatalf("count current: %v", err)
	}
	return total, current
}

// TestMatchDecisions_MatchByID_FromInbox: unmatched_file → media_file
// via POST /files/{id}/match.
func TestMatchDecisions_MatchByID_FromInbox(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(603, tmdb.MovieDetails{ID: 603, ReleaseDate: "1999-03-31", Title: "The Matrix"})

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	libID, _ := seedLibrary(t, app, "movies")

	fileID := seedUnmatchedFile(t, app, libID, "The Matrix/The Matrix.mkv")

	body := map[string]any{
		"external": map[string]any{
			"source":     "tmdb",
			"externalId": "603",
		},
	}
	var out matchDecisionWire
	app.POST(t, "/api/v1/files/"+fileID.String()+"/match", body, &out, http.StatusOK)

	if out.Outcome != "confident" {
		t.Fatalf("outcome: got %q, want confident", out.Outcome)
	}
	if out.Confidence != 1.0 {
		t.Fatalf("confidence: got %v, want 1.0", out.Confidence)
	}
	if out.ChosenRef == nil || out.ChosenRef.Source != "tmdb" || out.ChosenRef.ExternalID != "603" {
		t.Fatalf("chosenRef: got %+v", out.ChosenRef)
	}
	if out.ChosenItem == nil || out.ChosenItem.Title != "The Matrix" {
		t.Fatalf("chosenItem: got %+v", out.ChosenItem)
	}

	// The same file row should survive (id unchanged), now carrying the
	// resolved identity. "Matched" is file.media_item_id IS NOT NULL.
	var identifiedCount int
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM file WHERE id = $1 AND media_item_id IS NOT NULL AND deleted_at IS NULL`,
		fileID,
	).Scan(&identifiedCount); err != nil {
		t.Fatalf("count identified file: %v", err)
	}
	if identifiedCount != 1 {
		t.Fatalf("expected file %s to be identified in place, got %d", fileID, identifiedCount)
	}

	total, current := countMatchDecisions(t, app, fileID)
	if total != 1 {
		t.Fatalf("expected 1 match_decision, got %d", total)
	}
	if current != 1 {
		t.Fatalf("expected 1 current decision, got %d", current)
	}
}

// TestMatchDecisions_MatchByID_BadID returns NotFound when the
// metadata provider's lookup returns 404, without writing a decision.
func TestMatchDecisions_MatchByID_BadID(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetailsNotFound(999999999)
	tmdbSrv.OnTVDetailsNotFound(999999999)

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	libID, _ := seedLibrary(t, app, "movies")

	fileID := seedUnmatchedFile(t, app, libID, "weird.mkv")
	totalBefore, _ := countMatchDecisions(t, app, fileID)

	body := map[string]any{
		"external": map[string]any{
			"source":     "tmdb",
			"externalId": "999999999",
		},
	}
	app.POST(t, "/api/v1/files/"+fileID.String()+"/match", body, nil, http.StatusNotFound)

	totalAfter, _ := countMatchDecisions(t, app, fileID)
	if totalAfter != totalBefore {
		t.Fatalf("expected no decision write on bad id, total before=%d after=%d", totalBefore, totalAfter)
	}
}

// TestMatchDecisions_Unmatch: media_file → unmatched_file via POST
// /files/{id}/unmatch.
func TestMatchDecisions_Unmatch(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	_, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	libID, _ := seedLibrary(t, app, "movies")
	fileID := seedMediaFile(t, app, libID, 27205, "Inception", 2010, "Inception (2010)/Inception.mkv")

	var out matchDecisionWire
	app.POST(t, "/api/v1/files/"+fileID.String()+"/unmatch", nil, &out, http.StatusOK)

	if out.Outcome != "no_match" {
		t.Fatalf("outcome: got %q, want no_match", out.Outcome)
	}
	if out.Confidence != 0.0 {
		t.Fatalf("confidence: got %v, want 0.0", out.Confidence)
	}

	// The file row should survive in place with identity cleared
	// (media_item_id NULL) — un-match is an UPDATE, not a row swap.
	var unidentifiedCount int
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM file WHERE id = $1 AND media_item_id IS NULL AND deleted_at IS NULL`,
		fileID,
	).Scan(&unidentifiedCount); err != nil {
		t.Fatalf("count unidentified file: %v", err)
	}
	if unidentifiedCount != 1 {
		t.Fatalf("expected file %s to have identity cleared in place, got %d", fileID, unidentifiedCount)
	}
}

// TestMatchDecisions_Rematch_InPlace re-matches a file that's already in
// media_file to a different identity and asserts the transition is
// in-place: the same media_file.id survives (so the match_decision chain
// keyed on it stays consistent), the media_file_state snapshot is
// preserved, and the media_file_import history row stays attached. This
// is the regression guard for the delete-and-recreate bug — a failed
// commit must never orphan the file, and a successful one must not
// fragment its state/import history.
func TestMatchDecisions_Rematch_InPlace(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(603, tmdb.MovieDetails{ID: 603, ReleaseDate: "1999-03-31", Title: "The Matrix"})
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	libID, _ := seedLibrary(t, app, "movies")
	fileID := seedMediaFile(t, app, libID, 27205, "Inception", 2010, "Inception (2010)/Inception.mkv")

	// Seed an import-history row so we can assert it stays attached to the
	// same file across the re-match.
	if _, err := app.Pool.Exec(context.Background(),
		`INSERT INTO file_import (file_id, method, dest_path, success)
		 VALUES ($1, 'scan', $2, true)`,
		fileID, "Inception (2010)/Inception.mkv",
	); err != nil {
		t.Fatalf("seed file_import: %v", err)
	}

	// Re-match the media-resident file to a new identity (Matrix, 603).
	var out matchDecisionWire
	app.POST(t, "/api/v1/files/"+fileID.String()+"/match",
		map[string]any{"external": map[string]any{"source": "tmdb", "externalId": "603"}},
		&out, http.StatusOK,
	)
	if out.ChosenRef == nil || out.ChosenRef.ExternalID != "603" {
		t.Fatalf("current chosenRef: got %+v, want tmdb:603", out.ChosenRef)
	}

	// The file row must still exist under the SAME id, now pointing at the
	// Matrix media_item.
	var (
		fCount      int
		newTmdbID   int64
		stateSize   *int64
		stateExists bool
	)
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM file WHERE id = $1 AND deleted_at IS NULL`, fileID,
	).Scan(&fCount); err != nil {
		t.Fatalf("count file: %v", err)
	}
	if fCount != 1 {
		t.Fatalf("expected file %s to survive re-match in place, got %d rows", fileID, fCount)
	}
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT mi.tmdb_id FROM file f JOIN media_item mi ON mi.id = f.media_item_id WHERE f.id = $1`,
		fileID,
	).Scan(&newTmdbID); err != nil {
		t.Fatalf("read re-pointed identity: %v", err)
	}
	if newTmdbID != 603 {
		t.Fatalf("file identity: got tmdb %d, want 603", newTmdbID)
	}

	// file_state must be preserved (same row, original size).
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT exists, size_bytes FROM file_state WHERE file_id = $1`, fileID,
	).Scan(&stateExists, &stateSize); err != nil {
		t.Fatalf("read file_state: %v (expected preserved across re-match)", err)
	}
	if !stateExists || stateSize == nil || *stateSize != 1024 {
		t.Fatalf("file_state not preserved: exists=%v size=%v", stateExists, stateSize)
	}

	// The seeded import-history row must still be attached (file_id not
	// nulled by a cascade) — a delete-and-recreate would have detached it.
	// The in-place re-match appends its own import row, so the count is 2:
	// the original 'scan' attempt plus the 'manual_match' re-match.
	var importCount, seededCount int
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM file_import WHERE file_id = $1`, fileID,
	).Scan(&importCount); err != nil {
		t.Fatalf("count file_import: %v", err)
	}
	if importCount != 2 {
		t.Fatalf("expected 2 import rows (seeded scan + re-match), got %d", importCount)
	}
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM file_import WHERE file_id = $1 AND method = 'scan'`, fileID,
	).Scan(&seededCount); err != nil {
		t.Fatalf("count seeded import: %v", err)
	}
	if seededCount != 1 {
		t.Fatalf("expected the seeded scan import row to survive re-match, got %d", seededCount)
	}
}

// TestMatchDecisions_Detach: media_file → gone, with the detached
// outcome row preserving the audit trail.
func TestMatchDecisions_Detach(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	_, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	libID, root := seedLibrary(t, app, "movies")
	relPath := "Garbage (2010)/Garbage.mkv"
	abs := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fileID := seedMediaFile(t, app, libID, 999, "Garbage", 2010, relPath)

	body := map[string]any{"quarantine": false}
	var out detachResponse
	app.POST(t, "/api/v1/files/"+fileID.String()+"/detach", body, &out, http.StatusOK)

	if out.Decision.Outcome != "detached" {
		t.Fatalf("outcome: got %q, want detached", out.Decision.Outcome)
	}
	if out.QuarantinedPath != nil {
		t.Fatalf("expected nil quarantined path when quarantine=false, got %v", *out.QuarantinedPath)
	}

	// File still on disk (no quarantine path configured).
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("expected file %s to still exist, got %v", abs, err)
	}

	// file soft-deleted (excluded from live reads) but the row survives as
	// the audit anchor; match_decision row preserved.
	var liveCount, mdCount int
	_ = app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM file WHERE id = $1 AND deleted_at IS NULL`, fileID,
	).Scan(&liveCount)
	if liveCount != 0 {
		t.Fatalf("expected file %s to be soft-deleted, got %d live rows", fileID, liveCount)
	}
	var totalCount int
	_ = app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM file WHERE id = $1`, fileID,
	).Scan(&totalCount)
	if totalCount != 1 {
		t.Fatalf("expected detached file row to survive as audit anchor, got %d", totalCount)
	}
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM match_decision WHERE file_id = $1 AND outcome = 'detached'`,
		fileID,
	).Scan(&mdCount); err != nil {
		t.Fatalf("count md: %v", err)
	}
	if mdCount != 1 {
		t.Fatalf("expected 1 detached decision, got %d", mdCount)
	}
}

// TestMatchDecisions_Get_NoHistory returns just the current decision.
// TestMatchDecisions_Get_WithHistory returns the supersede chain too.
func TestMatchDecisions_Get_NoHistory(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(603, tmdb.MovieDetails{ID: 603, ReleaseDate: "1999-03-31", Title: "The Matrix"})

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	libID, _ := seedLibrary(t, app, "movies")
	fileID := seedUnmatchedFile(t, app, libID, "The Matrix/The Matrix.mkv")

	// Match it first so there's a decision to read.
	app.POST(t, "/api/v1/files/"+fileID.String()+"/match",
		map[string]any{"external": map[string]any{"source": "tmdb", "externalId": "603"}},
		nil, http.StatusOK,
	)

	var view matchDecisionView
	app.GET(t, "/api/v1/files/"+fileID.String()+"/match-decision", &view, http.StatusOK)

	if view.Current.Outcome != "confident" {
		t.Fatalf("current outcome: got %q, want confident", view.Current.Outcome)
	}
	if view.Current.ChosenItem == nil || view.Current.ChosenItem.Title != "The Matrix" {
		t.Fatalf("chosenItem on current: got %+v", view.Current.ChosenItem)
	}
	if len(view.History) != 0 {
		t.Fatalf("expected empty history without ?includeHistory=true, got %d entries", len(view.History))
	}
}

func TestMatchDecisions_Get_WithHistory(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(603, tmdb.MovieDetails{ID: 603, ReleaseDate: "1999-03-31", Title: "The Matrix"})
	tmdbSrv.OnMovieDetails(27205, tmdb.MovieDetails{ID: 27205, ReleaseDate: "2010-07-15", Title: "Inception"})

	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	libID, _ := seedLibrary(t, app, "movies")
	fileID := seedUnmatchedFile(t, app, libID, "ambiguous.mkv")

	// Decision 1: match-by-id 603 (Matrix).
	app.POST(t, "/api/v1/files/"+fileID.String()+"/match",
		map[string]any{"external": map[string]any{"source": "tmdb", "externalId": "603"}},
		nil, http.StatusOK,
	)
	// Decision 2: re-match to 27205 (Inception).
	app.POST(t, "/api/v1/files/"+fileID.String()+"/match",
		map[string]any{"external": map[string]any{"source": "tmdb", "externalId": "27205"}},
		nil, http.StatusOK,
	)

	var view matchDecisionView
	app.GET(t, "/api/v1/files/"+fileID.String()+"/match-decision?includeHistory=true", &view, http.StatusOK)

	if view.Current.ChosenRef == nil || view.Current.ChosenRef.ExternalID != "27205" {
		t.Fatalf("current chosenRef: got %+v, want tmdb:27205", view.Current.ChosenRef)
	}
	if len(view.History) != 1 {
		t.Fatalf("expected 1 historical decision, got %d", len(view.History))
	}
	if view.History[0].ChosenRef == nil || view.History[0].ChosenRef.ExternalID != "603" {
		t.Fatalf("history[0] chosenRef: got %+v, want tmdb:603", view.History[0].ChosenRef)
	}
	if view.History[0].SupersededAt == nil {
		t.Fatalf("expected historical row to have non-nil supersededAt")
	}
}
