// import_tasks.go is the humachi-shaped import-tasks handler. It mirrors
// the pre-migration Echo surface 1:1: list (paginated + status filter),
// counts-by-status, get-with-details, timeline, history (reimport chain),
// reimport, and cancel.
//
// The list endpoint preserves the Echo behaviour where `?status=...` swaps
// the underlying repo call: no status -> ListImportTasks; status set ->
// ListImportTasksByStatus. Pagination defaults (limit=50, offset=0) come
// from huma's `default:` tag and parse as int32 directly.
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

// ImportTasksListInput carries pagination + an optional status filter.
type ImportTasksListInput struct {
	Status string `query:"status" doc:"Filter by status (e.g. pending, in_progress, completed, failed, cancelled). Empty = no filter."`
	Limit  int32  `query:"limit" minimum:"1" maximum:"500" default:"50" doc:"Page size."`
	Offset int32  `query:"offset" minimum:"0" default:"0" doc:"Page offset."`
}

// ImportTasksListOutput is the flat page of tasks (no pagination envelope —
// matches the Echo wire shape).
type ImportTasksListOutput struct {
	Body []model.ImportTask
}

// List returns paginated import tasks, optionally filtered by status. The
// service routes to either ListImportTasksByStatus or the unfiltered
// ListImportTasks based on whether `status` is set.
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

// ImportTasksCountsInput is the empty parameter envelope for
// GET /import-tasks/counts.
type ImportTasksCountsInput struct{}

// ImportTasksCountsOutput wraps the per-status aggregate counts.
type ImportTasksCountsOutput struct {
	Body model.ImportTaskCounts
}

// Counts returns import-task counts grouped by status. Used by the FE to
// render badge counts on the imports list.
func (h *ImportTasks) Counts(ctx context.Context, _ *ImportTasksCountsInput) (*ImportTasksCountsOutput, error) {
	out, err := h.svc.ImportTasks.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &ImportTasksCountsOutput{Body: out}, nil
}

// ----- Get -----

// ImportTasksGetInput holds the path id for GET /import-tasks/{id}.
type ImportTasksGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

// ImportTasksGetOutput wraps the task with joined media, library, template,
// and download-job context.
type ImportTasksGetOutput struct {
	Body model.ImportTaskWithDetails
}

// Get fetches a single import task, joined with related media / library /
// template rows for display.
func (h *ImportTasks) Get(ctx context.Context, input *ImportTasksGetInput) (*ImportTasksGetOutput, error) {
	out, err := h.svc.ImportTasks.GetWithDetails(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksGetOutput{Body: out}, nil
}

// ----- Timeline -----

// ImportTasksTimelineInput holds the path id for
// GET /import-tasks/{id}/timeline.
type ImportTasksTimelineInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

// ImportTasksTimelineOutput is the flat event log for the task.
type ImportTasksTimelineOutput struct {
	Body []model.ImportTaskEvent
}

// GetTimeline returns the event log (state transitions + structured
// messages) for the import task.
func (h *ImportTasks) GetTimeline(ctx context.Context, input *ImportTasksTimelineInput) (*ImportTasksTimelineOutput, error) {
	out, err := h.svc.ImportTasks.GetTimeline(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksTimelineOutput{Body: out}, nil
}

// ----- History -----

// ImportTasksHistoryInput holds the path id for
// GET /import-tasks/{id}/history.
type ImportTasksHistoryInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

// ImportTasksHistoryOutput is the reimport chain — each entry is a task
// plus its depth in the chain (0 for the root).
type ImportTasksHistoryOutput struct {
	Body []model.ImportTaskHistoryEntry
}

// GetHistory returns the recursive reimport chain for the import task.
func (h *ImportTasks) GetHistory(ctx context.Context, input *ImportTasksHistoryInput) (*ImportTasksHistoryOutput, error) {
	out, err := h.svc.ImportTasks.GetHistory(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksHistoryOutput{Body: out}, nil
}

// ----- Reimport -----

// ImportTasksReimportInput holds the path id for
// POST /import-tasks/{id}/reimport.
type ImportTasksReimportInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

// ImportTasksReimportOutput wraps the new task created by the reimport.
type ImportTasksReimportOutput struct {
	Body model.ImportTask
}

// Reimport creates a new import task linked to the existing one via
// previous_task_id. The service emits Conflict (409) when the source
// status doesn't allow reimport.
func (h *ImportTasks) Reimport(ctx context.Context, input *ImportTasksReimportInput) (*ImportTasksReimportOutput, error) {
	out, err := h.svc.ImportTasks.Reimport(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksReimportOutput{Body: out}, nil
}

// ----- Cancel -----

// ImportTasksCancelInput holds the path id for
// POST /import-tasks/{id}/cancel. (Note: cancel is POST here, not DELETE,
// matching the pre-migration shape.)
type ImportTasksCancelInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Import task ID"`
}

// ImportTasksCancelOutput wraps the cancelled task.
type ImportTasksCancelOutput struct {
	Body model.ImportTask
}

// Cancel cancels a pending import task. The service emits Conflict (409)
// when the source status doesn't allow cancelling.
func (h *ImportTasks) Cancel(ctx context.Context, input *ImportTasksCancelInput) (*ImportTasksCancelOutput, error) {
	out, err := h.svc.ImportTasks.Cancel(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ImportTasksCancelOutput{Body: out}, nil
}

// ----- Register -----

// RegisterHumachi wires the import-tasks operations onto the supplied
// humachi API. JWT auth runs at the chi layer; every operation here is
// implicitly protected.
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
