// unmatched_files.go is the humachi-shaped unmatched-files handler. Five
// operations: list (paginated, optional library filter), get-by-id, manual
// match, dismiss, and refresh-suggestions.
//
// Note: the underlying UnmatchedFilesService still takes pgtype.UUID for ids
// and pgtype.UUID for the optional library filter — handlers normally only
// see uuid.UUID, but this service hasn't been migrated to the model.* shape
// yet. The handler converts at the boundary so the wire shape is the
// idiomatic uuid.UUID. Service-layer cleanup is a follow-up — flagged in the
// migration report.
package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type UnmatchedFiles struct{ svc *service.Services }

func NewUnmatchedFiles(s *service.Services) *UnmatchedFiles { return &UnmatchedFiles{svc: s} }

// pgtypeUUID wraps a uuid.UUID into the pgtype.UUID shape that the
// UnmatchedFilesService still uses. Drop once the service is migrated to
// model.* / uuid.UUID.
func pgtypeUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// ----- List -----

// UnmatchedFilesListInput carries the pagination + optional library filter.
//
// Huma doesn't accept pointer types for query params; LibraryID is bound as
// uuid.UUID and the zero value (uuid.Nil) acts as "no filter". The
// pre-migration Echo handler also let an unparseable libraryId silently fall
// through to "no filter" — this matches.
type UnmatchedFilesListInput struct {
	LibraryID uuid.UUID `query:"libraryId" format:"uuid" doc:"Optional library filter (uuid.Nil / omitted = no filter)"`
	Page      int       `query:"page" minimum:"1" default:"1" doc:"Page number (1-indexed)"`
	PageSize  int       `query:"pageSize" minimum:"1" maximum:"100" default:"20" doc:"Page size (1-100)"`
}

// UnmatchedFilesListOutput is the paginated list shape returned by the
// service as-is — preserves the pre-migration Echo wire envelope.
type UnmatchedFilesListOutput struct {
	Body service.ListResult
}

// List returns a paginated list of unresolved unmatched files.
func (h *UnmatchedFiles) List(ctx context.Context, input *UnmatchedFilesListInput) (*UnmatchedFilesListOutput, error) {
	params := service.ListParams{
		Page:     input.Page,
		PageSize: input.PageSize,
	}
	if input.LibraryID != uuid.Nil {
		pg := pgtypeUUID(input.LibraryID)
		params.LibraryID = &pg
	}
	out, err := h.svc.UnmatchedFiles.List(ctx, params)
	if err != nil {
		return nil, err
	}
	return &UnmatchedFilesListOutput{Body: out}, nil
}

// ----- Get -----

// UnmatchedFilesGetInput holds the path id.
type UnmatchedFilesGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Unmatched file ID"`
}

// UnmatchedFilesGetOutput wraps the response shape.
type UnmatchedFilesGetOutput struct {
	Body service.UnmatchedFileResponse
}

// Get fetches a single unmatched file with its current suggestions.
func (h *UnmatchedFiles) Get(ctx context.Context, input *UnmatchedFilesGetInput) (*UnmatchedFilesGetOutput, error) {
	out, err := h.svc.UnmatchedFiles.Get(ctx, pgtypeUUID(input.ID))
	if err != nil {
		return nil, err
	}
	return &UnmatchedFilesGetOutput{Body: out}, nil
}

// ----- Match -----

// UnmatchedFilesMatchInput is the path id + match request body.
type UnmatchedFilesMatchInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Unmatched file ID"`
	Body struct {
		TmdbID  int64  `json:"tmdbId" required:"true" minimum:"1" doc:"TMDB ID to match against"`
		Type    string `json:"type" required:"true" enum:"movie,series" doc:"Media type"`
		Season  *int   `json:"season,omitempty" doc:"Season number (series only)"`
		Episode *int   `json:"episode,omitempty" doc:"Episode number (series only)"`
	}
}

// UnmatchedFilesMatchResponse is the match response shape — small and
// purpose-built for "what was created"; matches the pre-migration Echo
// `map[string]interface{}` body but as a typed struct.
type UnmatchedFilesMatchResponse struct {
	MediaFileID string `json:"mediaFileId" doc:"Created media file ID (UUID string)"`
	Path        string `json:"path" doc:"Path of the matched file"`
}

// UnmatchedFilesMatchOutput wraps the typed match response.
type UnmatchedFilesMatchOutput struct {
	Body UnmatchedFilesMatchResponse
}

// Match manually matches an unmatched file to a TMDB media item. The service
// returns Conflict if the file is already resolved.
func (h *UnmatchedFiles) Match(ctx context.Context, input *UnmatchedFilesMatchInput) (*UnmatchedFilesMatchOutput, error) {
	mediaFile, err := h.svc.UnmatchedFiles.Match(ctx, pgtypeUUID(input.ID), service.MatchRequest{
		TmdbID:  input.Body.TmdbID,
		Type:    input.Body.Type,
		Season:  input.Body.Season,
		Episode: input.Body.Episode,
	})
	if err != nil {
		return nil, err
	}
	return &UnmatchedFilesMatchOutput{Body: UnmatchedFilesMatchResponse{
		MediaFileID: mediaFile.ID.String(),
		Path:        mediaFile.Path,
	}}, nil
}

// ----- Dismiss -----

// UnmatchedFilesDismissInput holds the path id.
type UnmatchedFilesDismissInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Unmatched file ID"`
}

// UnmatchedFilesDismissOutput is empty — DefaultStatus 204.
type UnmatchedFilesDismissOutput struct{}

// Dismiss marks an unmatched file as dismissed (resolved without a match).
func (h *UnmatchedFiles) Dismiss(ctx context.Context, input *UnmatchedFilesDismissInput) (*UnmatchedFilesDismissOutput, error) {
	if err := h.svc.UnmatchedFiles.Dismiss(ctx, pgtypeUUID(input.ID)); err != nil {
		return nil, err
	}
	return &UnmatchedFilesDismissOutput{}, nil
}

// ----- Refresh -----

// UnmatchedFilesRefreshInput holds the path id.
type UnmatchedFilesRefreshInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Unmatched file ID"`
}

// UnmatchedFilesRefreshOutput wraps the refreshed response shape.
type UnmatchedFilesRefreshOutput struct {
	Body service.UnmatchedFileResponse
}

// Refresh regenerates the suggestion list for an unmatched file by
// re-querying TMDB.
func (h *UnmatchedFiles) Refresh(ctx context.Context, input *UnmatchedFilesRefreshInput) (*UnmatchedFilesRefreshOutput, error) {
	out, err := h.svc.UnmatchedFiles.RefreshSuggestions(ctx, pgtypeUUID(input.ID))
	if err != nil {
		return nil, err
	}
	return &UnmatchedFilesRefreshOutput{Body: out}, nil
}

// ----- Register -----

// RegisterHumachi wires every unmatched-files operation onto the humachi API.
func (h *UnmatchedFiles) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "unmatched-files-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/unmatched-files",
		Summary:     "List unmatched files",
		Description: "Paginated list of files that couldn't be auto-matched to media items. Optional libraryId filter.",
		Tags:        []string{"unmatched-files"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "unmatched-files-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/unmatched-files/{id}",
		Summary:     "Get unmatched file",
		Tags:        []string{"unmatched-files"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "unmatched-files-match",
		Method:      http.MethodPost,
		Path:        "/api/v1/unmatched-files/{id}/match",
		Summary:     "Match unmatched file to media item",
		Description: "Manually matches an unmatched file to a TMDB-identified media item. Returns 409 if the file is already resolved.",
		Tags:        []string{"unmatched-files"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.Match)

	huma.Register(api, huma.Operation{
		OperationID:   "unmatched-files-dismiss",
		Method:        http.MethodPost,
		Path:          "/api/v1/unmatched-files/{id}/dismiss",
		Summary:       "Dismiss unmatched file",
		Description:   "Mark an unmatched file as resolved without matching it.",
		Tags:          []string{"unmatched-files"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsRead,
	}, h.Dismiss)

	huma.Register(api, huma.Operation{
		OperationID: "unmatched-files-refresh",
		Method:      http.MethodPost,
		Path:        "/api/v1/unmatched-files/{id}/refresh",
		Summary:     "Refresh match suggestions",
		Description: "Regenerate the list of suggested matches for an unmatched file.",
		Tags:        []string{"unmatched-files"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.Refresh)
}
