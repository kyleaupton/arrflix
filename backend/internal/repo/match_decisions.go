package repo

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// InsertMatchDecisionParams is the domain-shaped input for
// InsertMatchDecision. Mirrors the writeable subset of match_decision
// (omits server-managed id, decided_at, superseded_*). Outcomes and
// sources are strings here — the repo layer doesn't depend on the
// matcher's enums, by design (see CLAUDE.md: repo doesn't import
// upward into service/matcher packages).
type InsertMatchDecisionParams struct {
	FileID             uuid.UUID
	Outcome            string
	ChosenSource       *string
	ChosenExternalID   *string
	ChosenSeason       *int32
	ChosenEpisode      *int32
	ChosenEdition      *string
	Confidence         float64
	RankedCandidates   json.RawMessage
	ResolversConsulted json.RawMessage
	Evidence           json.RawMessage
	EvidenceTruncated  bool
	DecidedBy          string
	ParsedSnapshot     json.RawMessage
	DecidedWith        json.RawMessage
}

// InsertMatchDecision writes a match_decision row and returns the
// assigned id. The matcher's MatchOutcomeRecord → this params struct
// translation lives in the matcher's service layer.
func (r *Repository) InsertMatchDecision(ctx context.Context, params InsertMatchDecisionParams) (int64, error) {
	evidence := params.Evidence
	if len(evidence) == 0 {
		evidence = json.RawMessage("{}")
	}
	resolvers := params.ResolversConsulted
	if len(resolvers) == 0 {
		resolvers = json.RawMessage("[]")
	}

	id, err := r.Q.InsertMatchDecision(ctx, dbgen.InsertMatchDecisionParams{
		FileID:             pgtypeFromUUID(params.FileID),
		Outcome:            dbgen.MatchOutcome(params.Outcome),
		ChosenSource:       params.ChosenSource,
		ChosenExternalID:   params.ChosenExternalID,
		ChosenSeason:       params.ChosenSeason,
		ChosenEpisode:      params.ChosenEpisode,
		ChosenEdition:      params.ChosenEdition,
		Confidence:         params.Confidence,
		RankedCandidates:   params.RankedCandidates,
		ResolversConsulted: resolvers,
		Evidence:           evidence,
		EvidenceTruncated:  params.EvidenceTruncated,
		DecidedBy:          params.DecidedBy,
		ParsedSnapshot:     params.ParsedSnapshot,
		DecidedWith:        params.DecidedWith,
	})
	if err != nil {
		return 0, apperrors.FromPg(err, "create match decision for file %s", params.FileID)
	}
	return id, nil
}

// SupersedeMatchDecision flips superseded_at/superseded_by on whatever
// prior current row exists for the given file, pointing at the new
// decision. No-op when there's no prior current row (first-ever match).
func (r *Repository) SupersedeMatchDecision(ctx context.Context, fileID uuid.UUID, supersedingID int64) error {
	return apperrors.FromPg(r.Q.SupersedeMatchDecision(ctx, dbgen.SupersedeMatchDecisionParams{
		SupersedingID: &supersedingID,
		FileID:        pgtypeFromUUID(fileID),
	}), "supersede match decision for file %s", fileID)
}

// toModelMatchDecision translates the persistence-shaped
// dbgen.MatchDecision into the domain-shaped model.MatchDecision. The
// outcome enum stringifies; JSONB blobs are wrapped as json.RawMessage
// so callers can pass them straight through to the API surface.
func toModelMatchDecision(row dbgen.MatchDecision) model.MatchDecision {
	md := model.MatchDecision{
		ID:                 row.ID,
		FileID:             uuidFromPgtype(row.FileID),
		Outcome:            string(row.Outcome),
		ChosenSource:       row.ChosenSource,
		ChosenExternalID:   row.ChosenExternalID,
		ChosenSeason:       row.ChosenSeason,
		ChosenEpisode:      row.ChosenEpisode,
		ChosenEdition:      row.ChosenEdition,
		Confidence:         row.Confidence,
		RankedCandidates:   json.RawMessage(row.RankedCandidates),
		ResolversConsulted: json.RawMessage(row.ResolversConsulted),
		Evidence:           json.RawMessage(row.Evidence),
		EvidenceTruncated:  row.EvidenceTruncated,
		DecidedBy:          row.DecidedBy,
		DecidedAt:          row.DecidedAt,
		SupersededBy:       row.SupersededBy,
		ParsedSnapshot:     json.RawMessage(row.ParsedSnapshot),
		DecidedWith:        json.RawMessage(row.DecidedWith),
	}
	if row.SupersededAt.Valid {
		t := row.SupersededAt.Time
		md.SupersededAt = &t
	}
	return md
}

// GetCurrentMatchDecisionForFile returns the current (non-superseded)
// decision for a file, or a NotFound error when none exists. This is the
// hot-path read behind every endpoint that needs to know "what does the
// matcher think this file is right now."
func (r *Repository) GetCurrentMatchDecisionForFile(ctx context.Context, fileID uuid.UUID) (model.MatchDecision, error) {
	row, err := r.Q.GetCurrentMatchDecision(ctx, pgtypeFromUUID(fileID))
	if err != nil {
		return model.MatchDecision{}, apperrors.FromPg(err, "current match decision for file %s", fileID)
	}
	return toModelMatchDecision(row), nil
}

// ListMatchDecisionHistoryForFile returns every decision row for a file
// in newest-first order. Powers the evidence-read endpoint's
// ?include_history=true path: the supersede chain plus the current row.
func (r *Repository) ListMatchDecisionHistoryForFile(ctx context.Context, fileID uuid.UUID) ([]model.MatchDecision, error) {
	rows, err := r.Q.ListMatchDecisionHistoryForFile(ctx, pgtypeFromUUID(fileID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list match decision history for file %s", fileID)
	}
	out := make([]model.MatchDecision, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMatchDecision(row))
	}
	return out, nil
}
