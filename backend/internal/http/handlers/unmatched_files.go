package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// UnmatchedFiles exposes the read surface over the matcher inbox — files
// whose current match_decision banded to something other than confident /
// detached. The decision flows (match, un-match, detach, evidence read)
// live on the /files/{fileId}/* endpoints in match_decisions.go.
type UnmatchedFiles struct{ svc *service.Services }

func NewUnmatchedFiles(s *service.Services) *UnmatchedFiles { return &UnmatchedFiles{svc: s} }

// ----- List -----

// UnmatchedFilesListInput uses uuid.Nil as the "no filter" sentinel because
// huma doesn't accept pointer types for query params. Outcome is an
// optional band filter; confident / detached are never valid inbox bands,
// so they're absent from the enum.
type UnmatchedFilesListInput struct {
	LibraryID uuid.UUID `query:"libraryId" format:"uuid" doc:"Optional library filter (uuid.Nil / omitted = no filter)"`
	Outcome   string    `query:"outcome" enum:"confident_review,low_confidence,ambiguous,no_match,partial_series" doc:"Optional outcome-band filter"`
	Page      int       `query:"page" minimum:"1" default:"1" doc:"Page number (1-indexed)"`
	PageSize  int       `query:"pageSize" minimum:"1" maximum:"100" default:"20" doc:"Page size (1-100)"`
}

type UnmatchedFilesListOutput struct {
	Body model.InboxPage
}

func (h *UnmatchedFiles) List(ctx context.Context, input *UnmatchedFilesListInput) (*UnmatchedFilesListOutput, error) {
	params := service.ListParams{
		Page:     input.Page,
		PageSize: input.PageSize,
	}
	if input.LibraryID != uuid.Nil {
		id := input.LibraryID
		params.LibraryID = &id
	}
	if input.Outcome != "" {
		outcome := input.Outcome
		params.Outcome = &outcome
	}
	out, err := h.svc.UnmatchedFiles.List(ctx, params)
	if err != nil {
		return nil, err
	}
	return &UnmatchedFilesListOutput{Body: out}, nil
}

// ----- Get -----

type UnmatchedFilesGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"File ID"`
}

type UnmatchedFilesGetOutput struct {
	Body model.InboxItem
}

func (h *UnmatchedFiles) Get(ctx context.Context, input *UnmatchedFilesGetInput) (*UnmatchedFilesGetOutput, error) {
	out, err := h.svc.UnmatchedFiles.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &UnmatchedFilesGetOutput{Body: out}, nil
}

// ----- Register -----

func (h *UnmatchedFiles) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "unmatched-files-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/unmatched-files",
		Summary:     "List files needing review",
		Description: "Paginated matcher inbox: files whose current match decision banded to something other than confident/detached (includes confident_review and partial_series). Optional libraryId and outcome filters; response carries per-band counts. Match/un-match/detach are on /api/v1/files/{fileId}/*.",
		Tags:        []string{"unmatched-files"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "unmatched-files-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/unmatched-files/{id}",
		Summary:     "Get a file needing review",
		Tags:        []string{"unmatched-files"},
		Errors:      errsRead,
	}, h.Get)
}
