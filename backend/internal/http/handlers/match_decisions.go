package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// MatchDecisions exposes the file-action endpoints:
//   - POST   /api/v1/files/{fileId}/match            — re-match + match-by-ID
//   - POST   /api/v1/files/{fileId}/unmatch          — back to inbox
//   - POST   /api/v1/files/{fileId}/detach           — out of library
//   - GET    /api/v1/files/{fileId}/match-decision   — evidence read
//
// The {fileId} path component is the stable file id surfaced by
// matcher.FileRef.ID — a lookup against the file table.
type MatchDecisions struct{ svc *service.Services }

func NewMatchDecisions(s *service.Services) *MatchDecisions { return &MatchDecisions{svc: s} }

// ----- Wire shapes -----

// MatchDecisionResponse mirrors model.MatchDecision but uses
// JSON-string timestamps (RFC3339) and a flattened chosenRef shape so
// the FE doesn't have to know about pointer-shaped optional fields.
type MatchDecisionResponse struct {
	ID                 int64                  `json:"id" doc:"Decision id (BIGSERIAL)"`
	FileID             string                 `json:"fileId" doc:"Stable file id (UUID)"`
	Outcome            string                 `json:"outcome" enum:"confident,confident_review,low_confidence,ambiguous,no_match,partial_series,detached"`
	ChosenRef          *ExternalRefDTO        `json:"chosenRef,omitempty"`
	ChosenItem         *MetadataItem          `json:"chosenItem,omitempty"`
	ChosenEpisode      *EpisodeRefDTO         `json:"chosenEpisode,omitempty"`
	ChosenEdition      *string                `json:"chosenEdition,omitempty"`
	Confidence         float64                `json:"confidence"`
	ResolversConsulted json.RawMessage        `json:"resolversConsulted" doc:"Per-resolver audit array"`
	Evidence           json.RawMessage        `json:"evidence" doc:"Per-resolver raw evidence (capped at 8KB)"`
	EvidenceTruncated  bool                   `json:"evidenceTruncated"`
	RankedCandidates   []model.SuggestedMatch `json:"rankedCandidates,omitempty" doc:"Ranked alternative identities the matcher surfaced; the decide-pane suggestion set"`
	ParsedSnapshot     *ParsedSnapshotDTO     `json:"parsedSnapshot,omitempty" doc:"What the parser saw at decision time"`
	DecidedWith        *DecidedWithDTO        `json:"decidedWith,omitempty" doc:"The threshold band this decision ran under"`
	DecidedBy          string                 `json:"decidedBy" doc:"'auto' | 'user:<uuid>' | 'rule:<uuid>'"`
	DecidedAt          string                 `json:"decidedAt" format:"date-time"`
	SupersededAt       *string                `json:"supersededAt,omitempty" format:"date-time"`
	SupersededBy       *int64                 `json:"supersededBy,omitempty"`
}

// ParsedSnapshotDTO is the wire shape for the parser's view at decision
// time. Keys mirror matcher.ParsedSnapshot so the stored JSONB blob
// unmarshals directly.
type ParsedSnapshotDTO struct {
	Title   string `json:"title" doc:"Parsed title"`
	Year    int    `json:"year,omitempty" doc:"Parsed release year"`
	Type    string `json:"type,omitempty" doc:"Parsed media type ('movie' or 'series')"`
	Season  *int   `json:"season,omitempty" doc:"Parsed season number (series only)"`
	Episode *int   `json:"episode,omitempty" doc:"Parsed episode number (series only)"`
}

// DecidedWithDTO is the wire shape for the threshold band a decision ran
// under. Keys mirror the service's decided_with JSONB write
// ({preset, thresholds:{auto, reviewLow, lowMin}}).
type DecidedWithDTO struct {
	Preset     string `json:"preset" doc:"Preset the thresholds matched: 'strict' | 'recommended' | 'relaxed' | 'custom'"`
	Thresholds struct {
		Auto      float64 `json:"auto" doc:"Auto-match confidence floor"`
		ReviewLow float64 `json:"reviewLow" doc:"Confident-but-review floor"`
		LowMin    float64 `json:"lowMin" doc:"Low-confidence floor"`
	} `json:"thresholds" doc:"Confidence band thresholds"`
}

// ExternalRefDTO is the wire shape for a (source, externalId) pair.
type ExternalRefDTO struct {
	Source     string `json:"source" enum:"tmdb,imdb,tvdb"`
	ExternalID string `json:"externalId"`
}

// EpisodeRefDTO is the wire shape for a series episode pointer.
type EpisodeRefDTO struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

// MetadataItem is the slice of metadata.Item the FE needs for display.
type MetadataItem struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
	Type       string `json:"type,omitempty"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	Redirected bool   `json:"redirected,omitempty"`
}

func decisionToResponse(d model.MatchDecision, item *metadata.Item) MatchDecisionResponse {
	resp := MatchDecisionResponse{
		ID:                 d.ID,
		FileID:             d.FileID.String(),
		Outcome:            d.Outcome,
		Confidence:         d.Confidence,
		ResolversConsulted: d.ResolversConsulted,
		Evidence:           d.Evidence,
		EvidenceTruncated:  d.EvidenceTruncated,
		DecidedBy:          d.DecidedBy,
		DecidedAt:          d.DecidedAt.UTC().Format(time.RFC3339),
		SupersededBy:       d.SupersededBy,
		ChosenEdition:      d.ChosenEdition,
	}
	if d.ChosenSource != nil && d.ChosenExternalID != nil {
		resp.ChosenRef = &ExternalRefDTO{
			Source:     *d.ChosenSource,
			ExternalID: *d.ChosenExternalID,
		}
	}
	if d.ChosenSeason != nil && d.ChosenEpisode != nil {
		resp.ChosenEpisode = &EpisodeRefDTO{
			Season:  int(*d.ChosenSeason),
			Episode: int(*d.ChosenEpisode),
		}
	}
	if d.SupersededAt != nil {
		s := d.SupersededAt.UTC().Format(time.RFC3339)
		resp.SupersededAt = &s
	}
	if item != nil {
		resp.ChosenItem = &MetadataItem{
			Source:     string(item.Source),
			ExternalID: item.ExternalID,
			Type:       string(item.Type),
			Title:      item.Title,
			Year:       item.Year,
			Redirected: item.Redirected,
		}
	}
	// These JSONB blobs are our own writes — a malformed one is a persist
	// bug, not user input — so unmarshal best-effort and leave the field
	// absent on error.
	if len(d.RankedCandidates) > 0 {
		var ranked []model.SuggestedMatch
		if err := json.Unmarshal(d.RankedCandidates, &ranked); err == nil {
			resp.RankedCandidates = ranked
		}
	}
	if len(d.ParsedSnapshot) > 0 {
		var parsed ParsedSnapshotDTO
		if err := json.Unmarshal(d.ParsedSnapshot, &parsed); err == nil {
			resp.ParsedSnapshot = &parsed
		}
	}
	if len(d.DecidedWith) > 0 {
		var decided DecidedWithDTO
		if err := json.Unmarshal(d.DecidedWith, &decided); err == nil {
			resp.DecidedWith = &decided
		}
	}
	return resp
}

// userIDFromContext pulls the JWT subject out of the request context.
// All match-decision endpoints require an authenticated user — the
// match_decision row's decided_by column carries the user id.
func userIDFromContext(ctx context.Context, op string) (uuid.UUID, error) {
	claims, ok := middlewares.ClaimsFromContext(ctx)
	if !ok {
		return uuid.Nil, apperrors.Unauthenticatedf("missing credentials").Op(op)
	}
	sub, ok := claims["sub"].(string)
	if !ok {
		return uuid.Nil, apperrors.Unauthenticatedf("invalid token subject").Op(op)
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, apperrors.Unauthenticatedf("invalid token").Op(op)
	}
	return id, nil
}

// ----- Match -----

// FileMatchBody is the unified request body for /match. The endpoint
// serves both spec actions — match-by-ID (from the inbox) and re-match
// (changing identity on a file).
type FileMatchBody struct {
	External struct {
		Source     string `json:"source" required:"true" enum:"tmdb,imdb,tvdb" doc:"External provider"`
		ExternalID string `json:"externalId" required:"true" minLength:"1" doc:"Provider-specific identifier"`
	} `json:"external"`
	Episode *EpisodeRefDTO `json:"episode,omitempty" doc:"Series episode (season + episode); omit for movies"`
	Edition *string        `json:"edition,omitempty" doc:"Movie edition tag (e.g. 'directors_cut', 'extended')"`
}

type MatchDecisionsMatchInput struct {
	FileID uuid.UUID `path:"fileId" format:"uuid" doc:"Stable file id (file row)"`
	Body   FileMatchBody
}

type MatchDecisionsMatchOutput struct {
	Body MatchDecisionResponse
}

func (h *MatchDecisions) Match(ctx context.Context, input *MatchDecisionsMatchInput) (*MatchDecisionsMatchOutput, error) {
	userID, err := userIDFromContext(ctx, "MatchDecisionsHandler.Match")
	if err != nil {
		return nil, err
	}

	req := service.FileMatchRequest{
		Source:     input.Body.External.Source,
		ExternalID: input.Body.External.ExternalID,
		Edition:    input.Body.Edition,
	}
	if input.Body.Episode != nil {
		s := input.Body.Episode.Season
		e := input.Body.Episode.Episode
		req.Season = &s
		req.Episode = &e
	}

	decision, err := h.svc.MatchDecisions.MatchByID(ctx, input.FileID, userID, req)
	if err != nil {
		return nil, err
	}

	// Resolve the item for display so the FE doesn't double-call.
	view, gerr := h.svc.MatchDecisions.GetForFile(ctx, input.FileID, false)
	var item *metadata.Item
	if gerr == nil {
		item = view.ChosenItem
	}

	return &MatchDecisionsMatchOutput{Body: decisionToResponse(decision, item)}, nil
}

// ----- Unmatch -----

type MatchDecisionsUnmatchInput struct {
	FileID uuid.UUID `path:"fileId" format:"uuid" doc:"Stable file id"`
}

type MatchDecisionsUnmatchOutput struct {
	Body MatchDecisionResponse
}

func (h *MatchDecisions) Unmatch(ctx context.Context, input *MatchDecisionsUnmatchInput) (*MatchDecisionsUnmatchOutput, error) {
	userID, err := userIDFromContext(ctx, "MatchDecisionsHandler.Unmatch")
	if err != nil {
		return nil, err
	}

	decision, err := h.svc.MatchDecisions.Unmatch(ctx, input.FileID, userID)
	if err != nil {
		return nil, err
	}
	// no_match outcome carries no chosen item; pass nil.
	return &MatchDecisionsUnmatchOutput{Body: decisionToResponse(decision, nil)}, nil
}

// ----- Detach -----

type MatchDecisionsDetachInput struct {
	FileID uuid.UUID `path:"fileId" format:"uuid" doc:"Stable file id"`
	Body   struct {
		Quarantine bool `json:"quarantine,omitempty" doc:"Move the file to the configured quarantine path"`
	}
}

// MatchDecisionsDetachResponse pairs the new decision with the
// quarantine destination (when the move succeeded). The path is the
// absolute on-disk location; nil when no move happened.
type MatchDecisionsDetachResponse struct {
	Decision        MatchDecisionResponse `json:"decision"`
	QuarantinedPath *string               `json:"quarantinedPath,omitempty" doc:"Destination path when quarantine moved the file; null otherwise"`
}

type MatchDecisionsDetachOutput struct {
	Body MatchDecisionsDetachResponse
}

func (h *MatchDecisions) Detach(ctx context.Context, input *MatchDecisionsDetachInput) (*MatchDecisionsDetachOutput, error) {
	userID, err := userIDFromContext(ctx, "MatchDecisionsHandler.Detach")
	if err != nil {
		return nil, err
	}

	result, err := h.svc.MatchDecisions.Detach(ctx, input.FileID, userID, service.DetachRequest{
		Quarantine: input.Body.Quarantine,
	})
	if err != nil {
		return nil, err
	}
	return &MatchDecisionsDetachOutput{Body: MatchDecisionsDetachResponse{
		Decision:        decisionToResponse(result.Decision, nil),
		QuarantinedPath: result.QuarantinedPath,
	}}, nil
}

// ----- Get -----

type MatchDecisionsGetInput struct {
	FileID         uuid.UUID `path:"fileId" format:"uuid" doc:"Stable file id"`
	IncludeHistory bool      `query:"includeHistory" doc:"Include the supersede chain (prior decisions, newest first)"`
}

// MatchDecisionsGetResponse is the evidence-read response. Current is
// the latest non-superseded decision; History is empty unless
// includeHistory=true, in which case it carries every prior decision
// in newest-first order.
type MatchDecisionsGetResponse struct {
	Current MatchDecisionResponse   `json:"current"`
	History []MatchDecisionResponse `json:"history,omitempty"`
}

type MatchDecisionsGetOutput struct {
	Body MatchDecisionsGetResponse
}

func (h *MatchDecisions) Get(ctx context.Context, input *MatchDecisionsGetInput) (*MatchDecisionsGetOutput, error) {
	view, err := h.svc.MatchDecisions.GetForFile(ctx, input.FileID, input.IncludeHistory)
	if err != nil {
		return nil, err
	}
	resp := MatchDecisionsGetResponse{
		Current: decisionToResponse(view.Current, view.ChosenItem),
	}
	if len(view.History) > 0 {
		resp.History = make([]MatchDecisionResponse, 0, len(view.History))
		for _, h := range view.History {
			resp.History = append(resp.History, decisionToResponse(h, nil))
		}
	}
	return &MatchDecisionsGetOutput{Body: resp}, nil
}

// ----- Register -----

func (h *MatchDecisions) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "files-match",
		Method:      http.MethodPost,
		Path:        "/api/v1/files/{fileId}/match",
		Summary:     "Match a file to a media identity",
		Description: "Unified endpoint for match-by-ID (file currently in the inbox) and re-match (changing identity on an existing file). Validates the supplied (source, externalId) against the metadata provider; on a 404 returns NotFound and writes no decision row. On success, writes a confident match_decision row, supersedes the prior, and sets the file's identity.",
		Tags:        []string{"files"},
		Errors:      errs(errsRead, errsWrite, errsUpstream),
	}, h.Match)

	huma.Register(api, huma.Operation{
		OperationID: "files-unmatch",
		Method:      http.MethodPost,
		Path:        "/api/v1/files/{fileId}/unmatch",
		Summary:     "Un-match a file (back to the inbox)",
		Description: "Writes a no_match decision and clears the file's identity, leaving no suggestions. File stays on disk. Idempotent: returns the current decision unchanged when the file is already unmatched.",
		Tags:        []string{"files"},
		Errors:      errsRead,
	}, h.Unmatch)

	huma.Register(api, huma.Operation{
		OperationID: "files-detach",
		Method:      http.MethodPost,
		Path:        "/api/v1/files/{fileId}/detach",
		Summary:     "Detach a file from the library",
		Description: "Writes a detached decision and removes the file from arrflix's tracking tables. When quarantine=true and a quarantine path is configured in settings, the file is moved on disk; otherwise it stays in place. The match_decision row preserves the audit trail.",
		Tags:        []string{"files"},
		Errors:      errsRead,
	}, h.Detach)

	huma.Register(api, huma.Operation{
		OperationID: "files-match-decision",
		Method:      http.MethodGet,
		Path:        "/api/v1/files/{fileId}/match-decision",
		Summary:     "Read the current match decision (and optional history)",
		Description: "Returns the file's current (non-superseded) match decision with the validated metadata.Item attached for display. When includeHistory=true, also returns the supersede chain (prior decisions, newest first).",
		Tags:        []string{"files"},
		Errors:      errsRead,
	}, h.Get)
}
