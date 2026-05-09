// indexers.go is the humachi-shaped indexers handler. Every operation talks
// to Prowlarr via the IndexerService — list, get, create/update (POST), delete,
// toggle, action, test (saved/unsaved/all). Service errors are typed
// (BadGateway for upstream Prowlarr failures); humaerr renders them.
//
// Note: the wire body / response shapes use the third-party
// `prowlarr.IndexerInput` / `prowlarr.IndexerOutput` types directly, matching
// the pre-migration Echo behavior. Huma generates schemas for these via
// reflection, so the OpenAPI spec captures whatever JSON tags those types
// expose.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
	"golift.io/starr/prowlarr"
)

// ----- Handler -----

type Indexers struct{ svc *service.Services }

func NewIndexers(s *service.Services) *Indexers { return &Indexers{svc: s} }

// ----- ListConfigured -----

// IndexersListConfiguredInput is empty.
type IndexersListConfiguredInput struct{}

// IndexersListConfiguredOutput wraps the flat list of configured indexers
// (as Prowlarr returns them).
type IndexersListConfiguredOutput struct {
	Body []*prowlarr.IndexerOutput
}

// ListConfigured returns every indexer currently configured in Prowlarr.
func (h *Indexers) ListConfigured(ctx context.Context, _ *IndexersListConfiguredInput) (*IndexersListConfiguredOutput, error) {
	out, err := h.svc.Indexer.ListConfiguredIndexers(ctx)
	if err != nil {
		return nil, err
	}
	return &IndexersListConfiguredOutput{Body: out}, nil
}

// ----- GetSchema -----

// IndexersGetSchemaInput is empty.
type IndexersGetSchemaInput struct{}

// IndexersGetSchemaOutput wraps Prowlarr's indexer-schema response. The
// underlying shape is opaque (an array of objects with arbitrary fields);
// the wire is `[]any` so the spec emits a generic array schema.
type IndexersGetSchemaOutput struct {
	Body []any
}

// GetSchema returns the indexer-definition schema (the catalog of indexer
// types Prowlarr knows how to set up).
func (h *Indexers) GetSchema(ctx context.Context, _ *IndexersGetSchemaInput) (*IndexersGetSchemaOutput, error) {
	out, err := h.svc.Indexer.GetSchema(ctx)
	if err != nil {
		return nil, err
	}
	return &IndexersGetSchemaOutput{Body: out}, nil
}

// ----- Get -----

// IndexersGetInput holds the path id.
type IndexersGetInput struct {
	ID int64 `path:"id" doc:"Indexer ID (Prowlarr-assigned)"`
}

// IndexersGetOutput wraps a single indexer.
type IndexersGetOutput struct {
	Body *prowlarr.IndexerOutput
}

// Get fetches a single indexer by Prowlarr id.
func (h *Indexers) Get(ctx context.Context, input *IndexersGetInput) (*IndexersGetOutput, error) {
	out, err := h.svc.Indexer.GetIndexer(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &IndexersGetOutput{Body: out}, nil
}

// ----- Save -----

// IndexersSaveInput carries an IndexerInput. ID == 0 inserts a new indexer;
// non-zero updates the existing one.
type IndexersSaveInput struct {
	Body prowlarr.IndexerInput
}

// IndexersSaveOutput is the saved indexer envelope.
type IndexersSaveOutput struct {
	Body *prowlarr.IndexerOutput
}

// Save creates or updates an indexer. The service decides which based on
// whether ID is set.
func (h *Indexers) Save(ctx context.Context, input *IndexersSaveInput) (*IndexersSaveOutput, error) {
	out, err := h.svc.Indexer.SaveIndexerConfig(ctx, &input.Body)
	if err != nil {
		return nil, err
	}
	return &IndexersSaveOutput{Body: out}, nil
}

// ----- Delete -----

// IndexersDeleteInput holds the path id.
type IndexersDeleteInput struct {
	ID int64 `path:"id" doc:"Indexer ID"`
}

// IndexersDeleteOutput is empty (DefaultStatus 204).
type IndexersDeleteOutput struct{}

// Delete removes an indexer by id.
func (h *Indexers) Delete(ctx context.Context, input *IndexersDeleteInput) (*IndexersDeleteOutput, error) {
	if err := h.svc.Indexer.DeleteIndexer(ctx, input.ID); err != nil {
		return nil, err
	}
	return &IndexersDeleteOutput{}, nil
}

// ----- Toggle -----

// IndexersToggleInput holds the path id.
type IndexersToggleInput struct {
	ID int64 `path:"id" doc:"Indexer ID"`
}

// IndexersToggleOutput is the indexer post-toggle.
type IndexersToggleOutput struct {
	Body *prowlarr.IndexerOutput
}

// Toggle flips an indexer's `enable` flag and returns the result.
func (h *Indexers) Toggle(ctx context.Context, input *IndexersToggleInput) (*IndexersToggleOutput, error) {
	out, err := h.svc.Indexer.ToggleIndexer(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &IndexersToggleOutput{Body: out}, nil
}

// ----- Action -----

// IndexersActionInput is the action-name path param + a free-form body.
// Pre-migration the body was bound as `interface{}`; we keep that here so
// the FE can pass arbitrary action payloads through to Prowlarr unmodified.
type IndexersActionInput struct {
	Name string `path:"name" doc:"Prowlarr action name"`
	Body any
}

// IndexersActionOutput wraps Prowlarr's opaque action response.
type IndexersActionOutput struct {
	Body any
}

// Action proxies an arbitrary Prowlarr indexer action call.
func (h *Indexers) Action(ctx context.Context, input *IndexersActionInput) (*IndexersActionOutput, error) {
	out, err := h.svc.Indexer.Action(ctx, input.Name, input.Body)
	if err != nil {
		return nil, err
	}
	return &IndexersActionOutput{Body: out}, nil
}

// ----- TestSaved -----

// IndexersTestSavedInput holds the path id.
type IndexersTestSavedInput struct {
	ID int64 `path:"id" doc:"Saved indexer ID"`
}

// IndexersTestSavedOutput wraps the test result.
type IndexersTestSavedOutput struct {
	Body *model.IndexerTestResult
}

// TestSaved tests a saved indexer config. The service applies a 15-second
// timeout context around the Prowlarr call.
func (h *Indexers) TestSaved(ctx context.Context, input *IndexersTestSavedInput) (*IndexersTestSavedOutput, error) {
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := h.svc.Indexer.TestIndexerByID(tctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &IndexersTestSavedOutput{Body: out}, nil
}

// ----- TestUnsaved -----

// IndexersTestUnsavedInput carries an unsaved IndexerInput.
type IndexersTestUnsavedInput struct {
	Body prowlarr.IndexerInput
}

// IndexersTestUnsavedOutput wraps the test result.
type IndexersTestUnsavedOutput struct {
	Body *model.IndexerTestResult
}

// TestUnsaved tests an unsaved indexer config. Used by the FE config-form
// to validate a config before save.
func (h *Indexers) TestUnsaved(ctx context.Context, input *IndexersTestUnsavedInput) (*IndexersTestUnsavedOutput, error) {
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := h.svc.Indexer.TestIndexer(tctx, &input.Body)
	if err != nil {
		return nil, err
	}
	return &IndexersTestUnsavedOutput{Body: out}, nil
}

// ----- TestAll -----

// IndexersTestAllInput is empty.
type IndexersTestAllInput struct{}

// IndexersTestAllOutput wraps the batch test results.
type IndexersTestAllOutput struct {
	Body []*model.IndexerBatchTestResult
}

// TestAll tests every configured indexer. The service applies a 45-second
// timeout context around the Prowlarr testall call.
func (h *Indexers) TestAll(ctx context.Context, _ *IndexersTestAllInput) (*IndexersTestAllOutput, error) {
	tctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	out, err := h.svc.Indexer.TestAllIndexers(tctx)
	if err != nil {
		return nil, err
	}
	return &IndexersTestAllOutput{Body: out}, nil
}

// ----- Register -----

// RegisterHumachi wires every indexers operation onto the humachi API. All
// operations that touch Prowlarr enumerate 502 (errsUpstream) so the FE can
// render an "indexer service unreachable" affordance per-call.
func (h *Indexers) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "indexers-list-configured",
		Method:      http.MethodGet,
		Path:        "/api/v1/indexers/configured",
		Summary:     "List configured indexers",
		Tags:        []string{"indexers"},
		Errors:      errsUpstream,
	}, h.ListConfigured)

	huma.Register(api, huma.Operation{
		OperationID: "indexers-get-schema",
		Method:      http.MethodGet,
		Path:        "/api/v1/indexers/schema",
		Summary:     "Get indexer schema",
		Description: "Returns the catalog of indexer definitions Prowlarr knows how to configure.",
		Tags:        []string{"indexers"},
		Errors:      errsUpstream,
	}, h.GetSchema)

	huma.Register(api, huma.Operation{
		OperationID: "indexers-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/indexer/{id}",
		Summary:     "Get indexer by ID",
		Tags:        []string{"indexers"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "indexers-save",
		Method:      http.MethodPost,
		Path:        "/api/v1/indexer",
		Summary:     "Create or update indexer",
		Description: "Saves an indexer configuration. ID == 0 in the body inserts a new indexer; non-zero updates the existing one.",
		Tags:        []string{"indexers"},
		Errors:      errs(errsWrite, errsUpstream),
	}, h.Save)

	huma.Register(api, huma.Operation{
		OperationID:   "indexers-delete",
		Method:        http.MethodDelete,
		Path:          "/api/v1/indexer/{id}",
		Summary:       "Delete indexer",
		Tags:          []string{"indexers"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errs(errsDelete, errsUpstream),
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "indexers-toggle",
		Method:      http.MethodPut,
		Path:        "/api/v1/indexer/{id}/toggle",
		Summary:     "Toggle indexer enable state",
		Tags:        []string{"indexers"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.Toggle)

	huma.Register(api, huma.Operation{
		OperationID: "indexers-action",
		Method:      http.MethodPost,
		Path:        "/api/v1/indexer/action/{name}",
		Summary:     "Perform an indexer action",
		Description: "Proxies an arbitrary Prowlarr indexer-action call. Body and response shapes are opaque.",
		Tags:        []string{"indexers"},
		Errors:      errs(errsWrite, errsUpstream),
	}, h.Action)

	huma.Register(api, huma.Operation{
		OperationID: "indexers-test-saved",
		Method:      http.MethodPost,
		Path:        "/api/v1/indexer/{id}/test",
		Summary:     "Test saved indexer",
		Tags:        []string{"indexers"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.TestSaved)

	huma.Register(api, huma.Operation{
		OperationID: "indexers-test-unsaved",
		Method:      http.MethodPost,
		Path:        "/api/v1/indexer/test",
		Summary:     "Test unsaved indexer configuration",
		Tags:        []string{"indexers"},
		Errors:      errs(errsWrite, errsUpstream),
	}, h.TestUnsaved)

	huma.Register(api, huma.Operation{
		OperationID: "indexers-test-all",
		Method:      http.MethodPost,
		Path:        "/api/v1/indexers/testall",
		Summary:     "Test all configured indexers",
		Tags:        []string{"indexers"},
		Errors:      errsUpstream,
	}, h.TestAll)
}
