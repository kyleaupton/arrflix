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

type Libraries struct{ svc *service.Services }

func NewLibraries(s *service.Services) *Libraries { return &Libraries{svc: s} }

// ----- Shared body shape -----

type libraryWriteBody struct {
	Name     string `json:"name" required:"true" minLength:"1" maxLength:"100" doc:"Display name of the library"`
	Type     string `json:"type" required:"true" enum:"movie,series" doc:"Library type"`
	RootPath string `json:"rootPath" required:"true" minLength:"1" doc:"Absolute path to the library root on disk"`
	Enabled  bool   `json:"enabled" doc:"Whether the library accepts new imports"`
	Default  bool   `json:"default" doc:"Whether this is the default library for its type"`
}

// ----- List -----

type LibrariesListInput struct{}

type LibrariesListOutput struct {
	Body []model.Library
}

func (h *Libraries) List(ctx context.Context, _ *LibrariesListInput) (*LibrariesListOutput, error) {
	out, err := h.svc.Libraries.List(ctx)
	if err != nil {
		return nil, err
	}
	return &LibrariesListOutput{Body: out}, nil
}

// ----- Get -----

type LibrariesGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
}

type LibrariesGetOutput struct {
	Body model.Library
}

func (h *Libraries) Get(ctx context.Context, input *LibrariesGetInput) (*LibrariesGetOutput, error) {
	lib, err := h.svc.Libraries.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &LibrariesGetOutput{Body: lib}, nil
}

// ----- Create -----

type LibrariesCreateInput struct {
	Body libraryWriteBody
}

type LibrariesCreateOutput struct {
	Body model.Library
}

func (h *Libraries) Create(ctx context.Context, input *LibrariesCreateInput) (*LibrariesCreateOutput, error) {
	lib, err := h.svc.Libraries.Create(
		ctx,
		input.Body.Name,
		input.Body.Type,
		input.Body.RootPath,
		input.Body.Enabled,
		input.Body.Default,
	)
	if err != nil {
		return nil, err
	}
	return &LibrariesCreateOutput{Body: lib}, nil
}

// ----- Update -----

type LibrariesUpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
	Body libraryWriteBody
}

type LibrariesUpdateOutput struct {
	Body model.Library
}

func (h *Libraries) Update(ctx context.Context, input *LibrariesUpdateInput) (*LibrariesUpdateOutput, error) {
	lib, err := h.svc.Libraries.Update(
		ctx,
		input.ID,
		input.Body.Name,
		input.Body.Type,
		input.Body.RootPath,
		input.Body.Enabled,
		input.Body.Default,
	)
	if err != nil {
		return nil, err
	}
	return &LibrariesUpdateOutput{Body: lib}, nil
}

// ----- Delete -----

type LibrariesDeleteInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
}

type LibrariesDeleteOutput struct{}

func (h *Libraries) Delete(ctx context.Context, input *LibrariesDeleteInput) (*LibrariesDeleteOutput, error) {
	if err := h.svc.Libraries.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &LibrariesDeleteOutput{}, nil
}

// ----- Scan -----

type LibrariesScanInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
}

type ScanResponse struct {
	ScanID string `json:"scanId" doc:"Identifier for the kicked-off scan"`
}

type LibrariesScanOutput struct {
	Body ScanResponse
}

func (h *Libraries) Scan(ctx context.Context, input *LibrariesScanInput) (*LibrariesScanOutput, error) {
	scanID, err := h.svc.Scanner.StartScan(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &LibrariesScanOutput{Body: ScanResponse{ScanID: scanID}}, nil
}

// ----- Register -----

func (h *Libraries) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "libraries-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/libraries",
		Summary:     "List libraries",
		Tags:        []string{"libraries"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "libraries-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/libraries/{id}",
		Summary:     "Get library",
		Tags:        []string{"libraries"},
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID:   "libraries-create",
		Method:        http.MethodPost,
		Path:          "/api/v1/libraries",
		Summary:       "Create library",
		Tags:          []string{"libraries"},
		DefaultStatus: http.StatusCreated,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "libraries-update",
		Method:      http.MethodPut,
		Path:        "/api/v1/libraries/{id}",
		Summary:     "Update library",
		Tags:        []string{"libraries"},
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID:   "libraries-delete",
		Method:        http.MethodDelete,
		Path:          "/api/v1/libraries/{id}",
		Summary:       "Delete library",
		Tags:          []string{"libraries"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsDelete,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID:   "libraries-scan",
		Method:        http.MethodPost,
		Path:          "/api/v1/libraries/{id}/scan",
		Summary:       "Scan library",
		Description:   "Kick off an async filesystem scan for the library. Returns 202 with the scan ID; 409 if a scan is already running.",
		Tags:          []string{"libraries"},
		DefaultStatus: http.StatusAccepted,
	}, h.Scan)
}
