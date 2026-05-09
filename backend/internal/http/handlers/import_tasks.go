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

type ImportTasks struct{ svc *service.Services }

func NewImportTasks(s *service.Services) *ImportTasks { return &ImportTasks{svc: s} }

// ----- List -----

type ImportTasksListInput struct {
	Status string `query:"status" doc:"Filter by status (e.g. pending, in_progress, completed, failed, cancelled). Empty = no filter."`
	Limit  int32  `query:"limit" minimum:"1" maximum:"500" default:"50" doc:"Page size."`
	Offset int32  `query:"offset" minimum:"0" default:"0" doc:"Page offset."`
}

type ImportTasksListOutput struct {
	Body []model.ImportTask
}

func (h *ImportTasks) List(ctx context.Context, input *ImportTasksListInput) (*ImportTasksListOutput, error) {
	if input.Status != "" {
		out, err := h.svc.ImportTasks.ListByStatus(ctx, input.Status, input.Limit, input.Offset)
		if err != nil {
			return nil, err
		}
		return &ImportTasksListOutput{Body: out}, nil
	}
	out, err := h.svc.ImportTasks.List(ctx, input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	return &ImportTasksListOutput{Body: out}, nil
}

// ----- Counts -----

type ImportTasksCountsInput struct{}

type ImportTasksCountsOutput struct {
	Body model.ImportTaskCounts
}

func (h *ImportTasks) Counts(ctx context.Context, _ *ImportTasksCountsInput) (*ImportTasksCountsOutput, error) {
	out, err := h.svc.ImportTasks.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &ImportTasksCountsOutput{Body: out}, nil
}

// ----- Get -----

type ImportTasksGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

type ImportTasksGetOutput struct {
	Body model.ImportTaskWithDetails
}

func (h *ImportTasks) Get(ctx context.Context, input *ImportTasksGetInput) (*ImportTasksGetOutput, error) {
	out, err := h.svc.ImportTasks.GetWithDetails(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksGetOutput{Body: out}, nil
}

// ----- Timeline -----

type ImportTasksTimelineInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

type ImportTasksTimelineOutput struct {
	Body []model.ImportTaskEvent
}

func (h *ImportTasks) GetTimeline(ctx context.Context, input *ImportTasksTimelineInput) (*ImportTasksTimelineOutput, error) {
	out, err := h.svc.ImportTasks.GetTimeline(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksTimelineOutput{Body: out}, nil
}

// ----- History -----

type ImportTasksHistoryInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

type ImportTasksHistoryOutput struct {
	Body []model.ImportTaskHistoryEntry
}

func (h *ImportTasks) GetHistory(ctx context.Context, input *ImportTasksHistoryInput) (*ImportTasksHistoryOutput, error) {
	out, err := h.svc.ImportTasks.GetHistory(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksHistoryOutput{Body: out}, nil
}

// ----- Reimport -----

type ImportTasksReimportInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

type ImportTasksReimportOutput struct {
	Body model.ImportTask
}

func (h *ImportTasks) Reimport(ctx context.Context, input *ImportTasksReimportInput) (*ImportTasksReimportOutput, error) {
	out, err := h.svc.ImportTasks.Reimport(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksReimportOutput{Body: out}, nil
}

// ----- Cancel -----

type ImportTasksCancelInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

type ImportTasksCancelOutput struct {
	Body model.ImportTask
}

func (h *ImportTasks) Cancel(ctx context.Context, input *ImportTasksCancelInput) (*ImportTasksCancelOutput, error) {
	out, err := h.svc.ImportTasks.Cancel(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksCancelOutput{Body: out}, nil
}

// ----- Register -----

func (h *ImportTasks) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "import-tasks-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/import-tasks",
		Summary:     "List import tasks",
		Description: "Paginated import tasks, optionally filtered by status.",
		Tags:        []string{"import-tasks"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "import-tasks-counts",
		Method:      http.MethodGet,
		Path:        "/api/v1/import-tasks/counts",
		Summary:     "Get import task counts by status",
		Tags:        []string{"import-tasks"},
	}, h.Counts)

	huma.Register(api, huma.Operation{
		OperationID: "import-tasks-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/import-tasks/{id}",
		Summary:     "Get import task with details",
		Tags:        []string{"import-tasks"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "import-tasks-timeline",
		Method:      http.MethodGet,
		Path:        "/api/v1/import-tasks/{id}/timeline",
		Summary:     "Get import task timeline",
		Tags:        []string{"import-tasks"},
		Errors:      errsRead,
	}, h.GetTimeline)

	huma.Register(api, huma.Operation{
		OperationID: "import-tasks-history",
		Method:      http.MethodGet,
		Path:        "/api/v1/import-tasks/{id}/history",
		Summary:     "Get import task reimport history",
		Tags:        []string{"import-tasks"},
		Errors:      errsRead,
	}, h.GetHistory)

	huma.Register(api, huma.Operation{
		OperationID: "import-tasks-reimport",
		Method:      http.MethodPost,
		Path:        "/api/v1/import-tasks/{id}/reimport",
		Summary:     "Reimport import task",
		Description: "Create a new import task linked to this one via previous_task_id. Returns 409 when the source status doesn't allow reimport.",
		Tags:        []string{"import-tasks"},
		Errors:      errs(errsRead, []int{http.StatusConflict}),
	}, h.Reimport)

	huma.Register(api, huma.Operation{
		OperationID: "import-tasks-cancel",
		Method:      http.MethodPost,
		Path:        "/api/v1/import-tasks/{id}/cancel",
		Summary:     "Cancel import task",
		Description: "Cancel a pending import task. Returns 409 when the status doesn't allow cancelling.",
		Tags:        []string{"import-tasks"},
		Errors:      errs(errsRead, []int{http.StatusConflict}),
	}, h.Cancel)
}
