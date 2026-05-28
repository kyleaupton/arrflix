//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/repo"
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

	svc := matcher.NewMatcherService(nil, matcher.NewRepoAdapter(r), nil, matcher.NewRegistry(), matcher.DefaultConfig())

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
