package repo

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
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
	ResolversConsulted json.RawMessage
	Evidence           json.RawMessage
	EvidenceTruncated  bool
	DecidedBy          string
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
		ResolversConsulted: resolvers,
		Evidence:           evidence,
		EvidenceTruncated:  params.EvidenceTruncated,
		DecidedBy:          params.DecidedBy,
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
