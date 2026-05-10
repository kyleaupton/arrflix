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

type IndexersListConfiguredInput struct{}

type IndexersListConfiguredOutput struct {
	Body []*prowlarr.IndexerOutput
}

func (h *Indexers) ListConfigured(ctx context.Context, _ *IndexersListConfiguredInput) (*IndexersListConfiguredOutput, error) {
	out, err := h.svc.Indexer.ListConfiguredIndexers(ctx)
	if err != nil {
		return nil, err
	}
	return &IndexersListConfiguredOutput{Body: out}, nil
}

// ----- GetSchema -----

type IndexersGetSchemaInput struct{}

// IndexersGetSchemaOutput wraps Prowlarr's indexer-schema response. The
// underlying shape is opaque (an array of objects with arbitrary fields),
// so the wire is `[]any` and the spec emits a generic array schema.
type IndexersGetSchemaOutput struct {
	Body []any
}

func (h *Indexers) GetSchema(ctx context.Context, _ *IndexersGetSchemaInput) (*IndexersGetSchemaOutput, error) {
	out, err := h.svc.Indexer.GetSchema(ctx)
	if err != nil {
		return nil, err
	}
	return &IndexersGetSchemaOutput{Body: out}, nil
}

// ----- Get -----

type IndexersGetInput struct {
	ID int64 `path:"id" doc:"Indexer ID (Prowlarr-assigned)"`
}

type IndexersGetOutput struct {
	Body *prowlarr.IndexerOutput
}

func (h *Indexers) Get(ctx context.Context, input *IndexersGetInput) (*IndexersGetOutput, error) {
	out, err := h.svc.Indexer.GetIndexer(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &IndexersGetOutput{Body: out}, nil
}

// ----- Save -----

// IndexersSaveInput: ID == 0 inserts a new indexer; non-zero updates.
type IndexersSaveInput struct {
	Body prowlarr.IndexerInput
}

type IndexersSaveOutput struct {
	Body *prowlarr.IndexerOutput
}

func (h *Indexers) Save(ctx context.Context, input *IndexersSaveInput) (*IndexersSaveOutput, error) {
	out, err := h.svc.Indexer.SaveIndexerConfig(ctx, &input.Body)
	if err != nil {
		return nil, err
	}
	return &IndexersSaveOutput{Body: out}, nil
}

// ----- Delete -----

type IndexersDeleteInput struct {
	ID int64 `path:"id" doc:"Indexer ID"`
}

type IndexersDeleteOutput struct{}

func (h *Indexers) Delete(ctx context.Context, input *IndexersDeleteInput) (*IndexersDeleteOutput, error) {
	if err := h.svc.Indexer.DeleteIndexer(ctx, input.ID); err != nil {
		return nil, err
	}
	return &IndexersDeleteOutput{}, nil
}

// ----- Toggle -----

type IndexersToggleInput struct {
	ID int64 `path:"id" doc:"Indexer ID"`
}

type IndexersToggleOutput struct {
	Body *prowlarr.IndexerOutput
}

func (h *Indexers) Toggle(ctx context.Context, input *IndexersToggleInput) (*IndexersToggleOutput, error) {
	out, err := h.svc.Indexer.ToggleIndexer(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &IndexersToggleOutput{Body: out}, nil
}

// ----- Action -----

// IndexersActionInput is the action-name path param + an opaque body. Body
// is `any` so the FE can pass arbitrary action payloads through to Prowlarr
// unmodified.
type IndexersActionInput struct {
	Name string `path:"name" doc:"Prowlarr action name"`
	Body any
}

type IndexersActionOutput struct {
	Body any
}

func (h *Indexers) Action(ctx context.Context, input *IndexersActionInput) (*IndexersActionOutput, error) {
	out, err := h.svc.Indexer.Action(ctx, input.Name, input.Body)
	if err != nil {
		return nil, err
	}
	return &IndexersActionOutput{Body: out}, nil
}

// ----- TestSaved -----

type IndexersTestSavedInput struct {
	ID int64 `path:"id" doc:"Saved indexer ID"`
}

type IndexersTestSavedOutput struct {
	Body *model.IndexerTestResult
}

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

type IndexersTestUnsavedInput struct {
	Body prowlarr.IndexerInput
}

type IndexersTestUnsavedOutput struct {
	Body *model.IndexerTestResult
}

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

type IndexersTestAllInput struct{}

type IndexersTestAllOutput struct {
	Body []*model.IndexerBatchTestResult
}

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
