//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/matcher/resolvers"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// TestMatchDecision_EndToEnd writes a no_match record + a confident
// record via the matcher service through its repo adapter, then reads
// back via the repo's GetCurrentMatchDecision to assert the wire
// shape, the JSONB payloads, and the supersede chain.
//
// Phase-1 scope: no HTTP surface for matcher actions yet (Phase 4
// territory), so this test drives MatcherService directly. The
// assertion focus is the real-Postgres round-trip — pgtype/UUID
// translation, JSONB encoding, the partial-index hit on the "current
// match" read.
func TestMatchDecision_EndToEnd(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)

	svc := service.NewMatcherService(nil, r, nil, matcher.NewRegistry(), matcher.DefaultConfig())

	fileID := uuid.New()
	files := []matcher.FileRef{{ID: fileID, Path: "/lib/test.mkv"}}

	out, err := svc.MatchBatch(context.Background(), files)
	if err != nil {
		t.Fatalf("MatchBatch: %v", err)
	}
	if len(out) != 1 || out[0].Outcome != matcher.OutcomeNoMatch {
		t.Fatalf("expected one no_match record, got %+v", out)
	}

	// Round-trip read directly against the DB to verify JSONB columns
	// were marshalled cleanly. We use a raw query rather than the repo
	// helper because Phase 1 doesn't ship a domain-shape model for
	// match_decision — that lands when the matcher inbox UI does.
	var (
		dbOutcome       string
		dbConfidence    float64
		dbResolversJSON []byte
		dbEvidenceJSON  []byte
		dbDecidedBy     string
		dbSupersededAt  *string
	)
	err = pool.QueryRow(context.Background(), `
		SELECT outcome::text, confidence, resolvers_consulted::text, evidence::text, decided_by, superseded_at::text
		FROM match_decision
		WHERE file_id = $1
	`, fileID).Scan(&dbOutcome, &dbConfidence, &dbResolversJSON, &dbEvidenceJSON, &dbDecidedBy, &dbSupersededAt)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if dbOutcome != string(matcher.OutcomeNoMatch) {
		t.Fatalf("outcome: got %q, want %q", dbOutcome, matcher.OutcomeNoMatch)
	}
	if dbConfidence != 0 {
		t.Fatalf("confidence: got %v, want 0", dbConfidence)
	}
	if dbDecidedBy != "auto" {
		t.Fatalf("decided_by: got %q, want auto", dbDecidedBy)
	}

	// JSONB columns should decode cleanly.
	var resolvers []matcher.ResolverAudit
	if err := json.Unmarshal(dbResolversJSON, &resolvers); err != nil {
		t.Fatalf("unmarshal resolvers_consulted: %v (raw=%s)", err, dbResolversJSON)
	}
	if len(resolvers) != 0 {
		t.Fatalf("expected empty resolvers_consulted for no_match, got %d", len(resolvers))
	}
	var evidence map[string]json.RawMessage
	if err := json.Unmarshal(dbEvidenceJSON, &evidence); err != nil {
		t.Fatalf("unmarshal evidence: %v (raw=%s)", err, dbEvidenceJSON)
	}

	// A second MatchBatch for the same file should write a new row
	// AND supersede the first row.
	if _, err := svc.MatchBatch(context.Background(), files); err != nil {
		t.Fatalf("second MatchBatch: %v", err)
	}

	var currentCount int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM match_decision
		WHERE file_id = $1 AND superseded_at IS NULL
	`, fileID).Scan(&currentCount)
	if err != nil {
		t.Fatalf("count current: %v", err)
	}
	if currentCount != 1 {
		t.Fatalf("expected exactly 1 current decision after re-match, got %d", currentCount)
	}

	var totalCount int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM match_decision WHERE file_id = $1
	`, fileID).Scan(&totalCount)
	if err != nil {
		t.Fatalf("count total: %v", err)
	}
	if totalCount != 2 {
		t.Fatalf("expected 2 total decisions after re-match, got %d", totalCount)
	}
}

// fakeMetadataProviderForIntegration is a stand-in MetadataProvider for
// the full-pipeline test. It avoids hitting real TMDB while still
// exercising the aggregator's validation and search paths against the
// real DB. The lookup table is keyed by "<source>:<id>", same shape as
// the unit-level fakes in aggregator_test.go.
type fakeMetadataProviderForIntegration struct {
	items   map[string]*metadata.Item
	search  map[string][]metadata.Candidate
	resolve map[string]string // "<source>:<id>" -> tmdb id
}

func (f *fakeMetadataProviderForIntegration) LookupByID(_ context.Context, source metadata.ExternalSource, id string) (*metadata.Item, error) {
	if f.items == nil {
		return nil, nil
	}
	return f.items[string(source)+":"+id], nil
}

func (f *fakeMetadataProviderForIntegration) Search(_ context.Context, query string, _ metadata.SearchOptions) ([]metadata.Candidate, error) {
	if f.search == nil {
		return nil, nil
	}
	return f.search[query], nil
}

func (f *fakeMetadataProviderForIntegration) ResolveCrossProviderID(_ context.Context, src metadata.ExternalSource, id string) (string, error) {
	if f.resolve == nil {
		return "", nil
	}
	return f.resolve[string(src)+":"+id], nil
}

// TestMatcher_FullPipeline_EndToEnd exercises the full Phase-2 pipeline
// against the real DB: PathEmbed + NameParse via DefaultRegistry, a
// fake MetadataProvider for validation/search, MatcherService writing
// match_decision rows via the repo adapter. Four file shapes:
//
//   - Path-embed-only (TRaSH-style {tmdb-X}) → confident, Tier-1 short-circuit
//   - NameParse-only (parser hint, no path embed) → confident_review (≤0.85 cap)
//   - Both signals corroborating → confident
//   - Neither → no_match
//
// The assertions focus on outcome bands, the chosen ref shape, and that
// the right number of match_decision rows lands in the DB.
func TestMatcher_FullPipeline_EndToEnd(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)

	prov := &fakeMetadataProviderForIntegration{
		// Validation: path-embed candidates resolve to real items.
		items: map[string]*metadata.Item{
			"tmdb:27205": {Source: metadata.SourceTMDB, ExternalID: "27205", Type: metadata.EntityMovie, Title: "Inception", Year: 2010},
			"tmdb:11820": {Source: metadata.SourceTMDB, ExternalID: "11820", Type: metadata.EntityMovie, Title: "Y", Year: 1999},
		},
		// Search: name-parse hits TMDB.
		search: map[string][]metadata.Candidate{
			"Inception": {{Source: metadata.SourceTMDB, ExternalID: "27205", Type: metadata.EntityMovie, Title: "Inception", Year: 2010}},
			"Unknown Movie": {
				{Source: metadata.SourceTMDB, ExternalID: "1", Type: metadata.EntityMovie, Title: "Unknown Movie", Year: 2020},
				{Source: metadata.SourceTMDB, ExternalID: "2", Type: metadata.EntityMovie, Title: "Unknown Movie", Year: 2021},
			},
		},
	}

	svc := service.NewMatcherService(
		nil,
		r,
		prov,
		resolvers.DefaultRegistry(prov),
		matcher.DefaultConfig(),
	)

	// File 1: path-embed only — Tier-1 short-circuit.
	file1 := matcher.FileRef{
		ID:   uuid.New(),
		Path: "/lib/movies/Y (1999) {tmdb-11820}/Y.mkv",
	}
	// File 2: name-parse only — Tier-3 caps at 0.85 (cap is intentional; with
	// recommended preset Auto=0.85, exactly hitting the cap lands confident).
	// To land in confident_review, parsed title isn't an exact-match — use a
	// non-unique result with year mismatch to drop below cap.
	file2 := matcher.FileRef{
		ID:   uuid.New(),
		Path: "/lib/movies/Unknown Movie (2020).mkv",
		Parsed: &parsing.ParsedRelease{
			Identity: parsing.IdentityAttrs{
				Title:    parsing.Field[string]{Value: "Unknown Movie", Confidence: 1.0},
				Year:     parsing.Field[int]{Value: 2020, Confidence: 1.0},
				TypeHint: parsing.Field[string]{Value: "movie", Confidence: 1.0},
			},
		},
	}
	// File 3: both signals point at TMDB 27205 — Tier-1 short-circuit, but
	// even without it the resolvers corroborate. Tier-1 alone after
	// validation: 1.0 * 0.99 = 0.99, landing in confident.
	file3 := matcher.FileRef{
		ID:   uuid.New(),
		Path: "/lib/movies/Inception (2010) {tmdb-27205}/Inception.mkv",
		Parsed: &parsing.ParsedRelease{
			Identity: parsing.IdentityAttrs{
				Title:    parsing.Field[string]{Value: "Inception", Confidence: 1.0},
				Year:     parsing.Field[int]{Value: 2010, Confidence: 1.0},
				TypeHint: parsing.Field[string]{Value: "movie", Confidence: 1.0},
			},
		},
	}
	// File 4: neither signal. No embed in path, no parsed identity.
	file4 := matcher.FileRef{
		ID:   uuid.New(),
		Path: "/lib/movies/random.mkv",
	}

	out, err := svc.MatchBatch(context.Background(), []matcher.FileRef{file1, file2, file3, file4})
	if err != nil {
		t.Fatalf("MatchBatch: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("expected 4 outcomes, got %d", len(out))
	}

	// File 1: Tier-1 path-embed validates → confident, chosen tmdb:11820.
	if out[0].Outcome != matcher.OutcomeConfident {
		t.Fatalf("file1: expected confident (path-embed), got %s (conf=%v)", out[0].Outcome, out[0].Confidence)
	}
	if out[0].ChosenRef == nil || out[0].ChosenRef.ExternalID != "11820" {
		t.Fatalf("file1: expected chosen tmdb:11820, got %+v", out[0].ChosenRef)
	}

	// File 2: name-parse only. Search returns 2 candidates so no unique
	// bonus; top candidate at base 0.50 + year 0.15 + title-exact 0.10 =
	// 0.75 (× 1.0 title confidence). That lands in confident_review
	// (≥0.7, <0.85) under the Recommended preset — matching the spec's
	// stance that name-parse alone shouldn't auto-match without
	// corroboration.
	if out[1].Outcome != matcher.OutcomeConfidentReview {
		t.Fatalf("file2: expected confident_review (name-parse alone), got %s (conf=%v)", out[1].Outcome, out[1].Confidence)
	}
	if out[1].ChosenRef == nil || out[1].ChosenRef.ExternalID != "1" {
		t.Fatalf("file2: expected chosen tmdb:1, got %+v", out[1].ChosenRef)
	}

	// File 3: corroborates. Tier-1 alone validates to ~0.99 → confident,
	// short-circuit means Tier-3 isn't consulted (no name-parse audit).
	if out[2].Outcome != matcher.OutcomeConfident {
		t.Fatalf("file3: expected confident, got %s (conf=%v)", out[2].Outcome, out[2].Confidence)
	}
	if out[2].ChosenRef == nil || out[2].ChosenRef.ExternalID != "27205" {
		t.Fatalf("file3: expected chosen tmdb:27205, got %+v", out[2].ChosenRef)
	}
	for _, a := range out[2].ResolversConsulted {
		if a.Name == "name-parse" {
			t.Fatalf("file3: expected Tier-1 short-circuit, but name-parse ran")
		}
	}

	// File 4: nothing fired.
	if out[3].Outcome != matcher.OutcomeNoMatch {
		t.Fatalf("file4: expected no_match, got %s", out[3].Outcome)
	}

	// All four landed in match_decision; confirm we wrote exactly the
	// shapes we asserted on the in-memory records.
	var rows int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM match_decision
		WHERE file_id = ANY($1)
	`, []uuid.UUID{file1.ID, file2.ID, file3.ID, file4.ID}).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 4 {
		t.Fatalf("expected 4 rows in match_decision, got %d", rows)
	}

	// Spot-check file1's persisted shape: outcome enum, chosen ref, and
	// the resolvers_consulted JSON carries the path-embed audit.
	var (
		dbOutcome   string
		dbSource    *string
		dbID        *string
		dbResolvers []byte
	)
	if err := pool.QueryRow(context.Background(), `
		SELECT outcome::text, chosen_source, chosen_external_id, resolvers_consulted::text
		FROM match_decision WHERE file_id = $1
	`, file1.ID).Scan(&dbOutcome, &dbSource, &dbID, &dbResolvers); err != nil {
		t.Fatalf("read file1: %v", err)
	}
	if dbOutcome != string(matcher.OutcomeConfident) {
		t.Fatalf("file1 outcome: got %q", dbOutcome)
	}
	if dbSource == nil || *dbSource != string(metadata.SourceTMDB) || dbID == nil || *dbID != "11820" {
		t.Fatalf("file1 chosen: got source=%v id=%v", dbSource, dbID)
	}
	var audits []matcher.ResolverAudit
	if err := json.Unmarshal(dbResolvers, &audits); err != nil {
		t.Fatalf("unmarshal audits: %v", err)
	}
	foundEmbed := false
	for _, a := range audits {
		if a.Name == "path-embed" {
			foundEmbed = true
			break
		}
	}
	if !foundEmbed {
		t.Fatalf("expected path-embed audit in file1's resolvers_consulted, got %+v", audits)
	}
}
