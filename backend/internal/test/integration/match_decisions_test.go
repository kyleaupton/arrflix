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

// seedUnmatchedFile creates an unmatched_file row directly via raw SQL
// so the test doesn't have to drive the whole scan loop. This is a
// per-CLAUDE.md backdoor; the alternative (running scan with files on
// disk) is heavyweight when the assertion is about the action
// endpoints, not the scan loop. The fileID matches matcher.FileRef.ID
// so /files/{id} resolves.
func seedUnmatchedFile(t *testing.T, app *testapp.App, libraryID uuid.UUID, path string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := app.Pool.QueryRow(context.Background(),
		`INSERT INTO unmatched_file (library_id, path, file_size, suggested_matches, partial_series)
		 VALUES ($1, $2, $3, $4, false)
		 RETURNING id`,
		libraryID, path, int64(1024), []byte(`[]`),
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed unmatched_file: %v", err)
	}
	return id
}

// seedMediaFile creates a media_item + media_file + media_file_state
// triple via raw SQL. Used by the re-match / unmatch / detach tests to
// drive a "file currently in media_file" precondition without running
// the scan loop.
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
	var mediaFileID uuid.UUID
	if err := app.Pool.QueryRow(context.Background(),
		`INSERT INTO media_file (library_id, media_item_id, path)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		libraryID, mediaItemID, path,
	).Scan(&mediaFileID); err != nil {
		t.Fatalf("seed media_file: %v", err)
	}
	if _, err := app.Pool.Exec(context.Background(),
		`INSERT INTO media_file_state (media_file_id, file_exists, file_size)
		 VALUES ($1, true, $2)`,
		mediaFileID, int64(1024),
	); err != nil {
		t.Fatalf("seed media_file_state: %v", err)
	}
	return mediaFileID
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

	// unmatched_file row should be gone; media_file row should exist.
	var ufCount, mfCount int
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM unmatched_file WHERE id = $1`, fileID,
	).Scan(&ufCount); err != nil {
		t.Fatalf("count uf: %v", err)
	}
	if ufCount != 0 {
		t.Fatalf("expected 0 unmatched_file rows, got %d", ufCount)
	}
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM media_file WHERE library_id = $1 AND path = $2`,
		libID, "The Matrix/The Matrix.mkv",
	).Scan(&mfCount); err != nil {
		t.Fatalf("count mf: %v", err)
	}
	if mfCount != 1 {
		t.Fatalf("expected 1 media_file row, got %d", mfCount)
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

	// media_file row should be gone; unmatched_file row should be
	// present at the same path.
	var mfCount, ufCount int
	_ = app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM media_file WHERE id = $1`, fileID,
	).Scan(&mfCount)
	if mfCount != 0 {
		t.Fatalf("expected 0 media_file rows, got %d", mfCount)
	}
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM unmatched_file WHERE library_id = $1 AND path = $2`,
		libID, "Inception (2010)/Inception.mkv",
	).Scan(&ufCount); err != nil {
		t.Fatalf("count uf: %v", err)
	}
	if ufCount != 1 {
		t.Fatalf("expected 1 unmatched_file row, got %d", ufCount)
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
	// same media_file across the re-match.
	if _, err := app.Pool.Exec(context.Background(),
		`INSERT INTO media_file_import (media_file_id, method, dest_path, success)
		 VALUES ($1, 'scan', $2, true)`,
		fileID, "Inception (2010)/Inception.mkv",
	); err != nil {
		t.Fatalf("seed media_file_import: %v", err)
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

	// The media_file row must still exist under the SAME id, now pointing
	// at the Matrix media_item.
	var (
		mfCount     int
		newTmdbID   int64
		stateSize   *int64
		stateExists bool
	)
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM media_file WHERE id = $1`, fileID,
	).Scan(&mfCount); err != nil {
		t.Fatalf("count media_file: %v", err)
	}
	if mfCount != 1 {
		t.Fatalf("expected media_file %s to survive re-match in place, got %d rows", fileID, mfCount)
	}
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT mi.tmdb_id FROM media_file mf JOIN media_item mi ON mi.id = mf.media_item_id WHERE mf.id = $1`,
		fileID,
	).Scan(&newTmdbID); err != nil {
		t.Fatalf("read re-pointed identity: %v", err)
	}
	if newTmdbID != 603 {
		t.Fatalf("media_file identity: got tmdb %d, want 603", newTmdbID)
	}

	// media_file_state must be preserved (same row, original file_size).
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT file_exists, file_size FROM media_file_state WHERE media_file_id = $1`, fileID,
	).Scan(&stateExists, &stateSize); err != nil {
		t.Fatalf("read media_file_state: %v (expected preserved across re-match)", err)
	}
	if !stateExists || stateSize == nil || *stateSize != 1024 {
		t.Fatalf("media_file_state not preserved: exists=%v size=%v", stateExists, stateSize)
	}

	// The import-history row must still be attached (media_file_id not
	// nulled by a cascade) — delete-and-recreate would have detached it.
	var importCount int
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM media_file_import WHERE media_file_id = $1`, fileID,
	).Scan(&importCount); err != nil {
		t.Fatalf("count media_file_import: %v", err)
	}
	if importCount != 1 {
		t.Fatalf("expected import-history row to stay attached, got %d", importCount)
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

	// media_file row gone; match_decision row preserved.
	var mfCount, mdCount int
	_ = app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM media_file WHERE id = $1`, fileID,
	).Scan(&mfCount)
	if mfCount != 0 {
		t.Fatalf("expected 0 media_file rows after detach, got %d", mfCount)
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
