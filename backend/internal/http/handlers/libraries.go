// libraries.go is the humachi reference vertical for arrflix's HTTP layer.
// Phase 2 of the humachi migration converted the libraries handler from Echo
// to humachi end-to-end. The pattern established here is the template every
// other handler will follow in phase 3:
//
//   - One file per entity, sectioned by operation with `// ----- <Op> -----`
//     dividers. Shared types at the top, register at the bottom.
//   - Operation registration via huma.Register on huma.API.
//   - Validation split: tag-level for per-field rules, service-level for
//     stateful checks (filesystem, etc.). Resolve() only when a tag-level
//     rule cannot express what's needed (none here).
//   - UUID path params bound directly as `uuid.UUID` — huma calls
//     `UnmarshalText` automatically and emits `path.id`-located validation
//     errors on parse failure.
//   - Auth claims via middlewares.ClaimsFromContext(ctx).
//   - Errors returned directly from the service; humaerr renders RFC 9457.
//
// Echo registration for libraries is gone — chi+humachi serves these routes
// now. Every other handler still lives on Echo (chi.NotFound fallthrough)
// and migrates in phase 3.
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

// libraryWriteBody is the shared shape for Create and Update. They take the
// same fields and apply the same per-field validation; service methods diverge
// only on whether an id is required (Update) or a row is inserted (Create).
type libraryWriteBody struct {
	Name     string `json:"name" required:"true" minLength:"1" maxLength:"100" doc:"Display name of the library"`
	Type     string `json:"type" required:"true" enum:"movie,series" doc:"Library type"`
	RootPath string `json:"rootPath" required:"true" minLength:"1" doc:"Absolute path to the library root on disk"`
	Enabled  bool   `json:"enabled" doc:"Whether the library accepts new imports"`
	Default  bool   `json:"default" doc:"Whether this is the default library for its type"`
}

// ----- List -----

// LibrariesListInput is the empty parameter envelope for GET /libraries.
// Even with no params, declaring a typed Input keeps the handler signature
// uniform with the rest of the operations.
type LibrariesListInput struct{}

// LibrariesListOutput returns every library as a flat slice. Matches the
// pre-migration Echo wire shape (no pagination envelope). When libraries
// grows past the "show them all" point, switch to model.Page[model.Library].
type LibrariesListOutput struct {
	Body []model.Library
}

// List returns every library. The response shape is the bare slice (no
// pagination envelope) — this preserves the Echo handler's prior wire shape.
// If/when libraries grows large, switch the service to a paginated query
// and the output to *LibrariesListOutput{ Body model.Page[model.Library] }.
func (h *Libraries) List(ctx context.Context, _ *LibrariesListInput) (*LibrariesListOutput, error) {
	out, err := h.svc.Libraries.List(ctx)
	if err != nil {
		return nil, err
	}
	return &LibrariesListOutput{Body: out}, nil
}

// ----- Get -----

// LibrariesGetInput holds the path id for GET /libraries/{id}. The id binds
// directly into a uuid.UUID — huma routes path params through
// encoding.TextUnmarshaler, which uuid.UUID implements, so a malformed value
// produces an RFC-9457 validation error located at `path.id` without any
// Resolve method.
type LibrariesGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
}

// LibrariesGetOutput is the single-library response envelope.
type LibrariesGetOutput struct {
	Body model.Library
}

// Get fetches a single library by id. Path id is parsed and validated by
// huma during request binding; the handler reads input.ID directly.
func (h *Libraries) Get(ctx context.Context, input *LibrariesGetInput) (*LibrariesGetOutput, error) {
	lib, err := h.svc.Libraries.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &LibrariesGetOutput{Body: lib}, nil
}

// ----- Create -----

// LibrariesCreateInput carries the body for POST /libraries.
type LibrariesCreateInput struct {
	Body libraryWriteBody
}

// LibrariesCreateOutput is the created-library response envelope. The
// operation is registered with DefaultStatus 201 so huma emits the right
// status without a Status field on the output.
type LibrariesCreateOutput struct {
	Body model.Library
}

// Create persists a new library. Per-field validation (required, enum,
// length) runs at the tag layer before this handler is invoked; the
// stateful os.Stat(rootPath) check stays in the service layer because it
// reads the filesystem and shouldn't run during request-binding.
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

// LibrariesUpdateInput combines a path id with the write body for
// PUT /libraries/{id}.
type LibrariesUpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
	Body libraryWriteBody
}

// LibrariesUpdateOutput is the updated-library response envelope.
type LibrariesUpdateOutput struct {
	Body model.Library
}

// Update mutates an existing library by id. Same validation split as Create.
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

// LibrariesDeleteInput holds the path id for DELETE /libraries/{id}.
type LibrariesDeleteInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
}

// LibrariesDeleteOutput is intentionally empty. The operation is registered
// with DefaultStatus 204 so huma writes no body.
type LibrariesDeleteOutput struct{}

// Delete removes a library by id. The DefaultStatus on registration is 204
// and the output struct is empty, so huma writes no body.
func (h *Libraries) Delete(ctx context.Context, input *LibrariesDeleteInput) (*LibrariesDeleteOutput, error) {
	if err := h.svc.Libraries.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &LibrariesDeleteOutput{}, nil
}

// ----- Scan -----

// LibrariesScanInput holds the path id for POST /libraries/{id}/scan.
type LibrariesScanInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
}

// ScanResponse is the bespoke wire shape for the scan-kickoff endpoint. The
// underlying service returns a generated scan ID; the wire envelope keeps
// the camelCase JSON convention used everywhere else.
type ScanResponse struct {
	ScanID string `json:"scanId" doc:"Identifier for the kicked-off scan"`
}

// LibrariesScanOutput wraps ScanResponse. The operation is registered with
// DefaultStatus 202 (Accepted) — the scan runs asynchronously.
type LibrariesScanOutput struct {
	Body ScanResponse
}

// Scan kicks off an async filesystem scan. The service emits a typed
// Conflict error (409) when a scan is already running for the library;
// humaerr renders that as RFC 9457 problem-details automatically.
func (h *Libraries) Scan(ctx context.Context, input *LibrariesScanInput) (*LibrariesScanOutput, error) {
	scanID, err := h.svc.Scanner.StartScan(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &LibrariesScanOutput{Body: ScanResponse{ScanID: scanID}}, nil
}

// ----- Register -----

// RegisterHumachi wires the libraries operations onto the supplied humachi
// API. This is the humachi-shaped sibling of the old RegisterProtected; the
// Echo registration has been removed because chi+humachi now serves these
// routes. JWT and setup-mode middleware run at the chi layer (see
// internal/http/http.go), so every operation registered here is implicitly
// protected.
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
