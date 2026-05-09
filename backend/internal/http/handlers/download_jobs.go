// download_jobs.go is the humachi-shaped download-jobs handler. It exposes
// the full pre-migration Echo surface 1:1: list / get / timeline /
// list-import-tasks / reimport / retry / history / cancel, plus the two
// per-media list endpoints (`/movie/{id}/download-jobs`,
// `/series/{id}/download-jobs`).
//
// The two per-media list endpoints take a TMDB id (int64), not a UUID.
// Every other addressable path uses uuid.UUID and binds directly via huma's
// TextUnmarshaler routing — see handlers/CLAUDE.md for the convention.
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

type DownloadJobs struct{ svc *service.Services }

func NewDownloadJobs(s *service.Services) *DownloadJobs { return &DownloadJobs{svc: s} }

// ----- List -----

// DownloadJobsListInput is the empty parameter envelope for
// GET /download-jobs (no pagination, no filtering).
type DownloadJobsListInput struct{}

// DownloadJobsListOutput is the bare slice — matches the pre-migration wire
// shape. ListWithImportSummary returns each job enriched with the matched
// media item, season/episode numbers, and per-status import counts.
type DownloadJobsListOutput struct {
	Body []model.DownloadJobWithSummary
}

// List returns every download job with its computed import-status summary.
func (h *DownloadJobs) List(ctx context.Context, _ *DownloadJobsListInput) (*DownloadJobsListOutput, error) {
	out, err := h.svc.DownloadJobs.ListWithImportSummary(ctx)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsListOutput{Body: out}, nil
}

// ----- Get -----

// DownloadJobsGetInput holds the path id for GET /download-jobs/{id}.
type DownloadJobsGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

// DownloadJobsGetOutput wraps the enriched single job.
type DownloadJobsGetOutput struct {
	Body model.DownloadJobWithSummary
}

// Get fetches a single download job (with import summary) by id.
func (h *DownloadJobs) Get(ctx context.Context, input *DownloadJobsGetInput) (*DownloadJobsGetOutput, error) {
	out, err := h.svc.DownloadJobs.GetWithImportSummary(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsGetOutput{Body: out}, nil
}

// ----- Timeline -----

// DownloadJobsTimelineInput holds the path id for
// GET /download-jobs/{id}/timeline.
type DownloadJobsTimelineInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

// DownloadJobsTimelineOutput is the combined download/import event log.
type DownloadJobsTimelineOutput struct {
	Body []model.DownloadJobTimelineEvent
}

// GetTimeline returns the merged event log: download events plus all import
// events for tasks linked to this job.
func (h *DownloadJobs) GetTimeline(ctx context.Context, input *DownloadJobsTimelineInput) (*DownloadJobsTimelineOutput, error) {
	out, err := h.svc.DownloadJobs.GetTimeline(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsTimelineOutput{Body: out}, nil
}

// ----- List Import Tasks -----

// DownloadJobsListImportTasksInput holds the path id for
// GET /download-jobs/{id}/import-tasks.
type DownloadJobsListImportTasksInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

// DownloadJobsListImportTasksOutput is the flat list of import tasks for the
// job (every status, every reimport-chain entry).
type DownloadJobsListImportTasksOutput struct {
	Body []model.ImportTask
}

// ListImportTasks returns every import task linked to the download job.
func (h *DownloadJobs) ListImportTasks(ctx context.Context, input *DownloadJobsListImportTasksInput) (*DownloadJobsListImportTasksOutput, error) {
	out, err := h.svc.DownloadJobs.ListImportTasks(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsListImportTasksOutput{Body: out}, nil
}

// ----- Reimport -----

// DownloadJobsReimportInput combines the path id with an `?all=true` flag
// for POST /download-jobs/{id}/reimport. Without the flag, only failed root
// import tasks are reimported; with it, every terminal root task is.
type DownloadJobsReimportInput struct {
	ID  uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
	All bool      `query:"all" doc:"Reimport all terminal tasks (completed, failed, cancelled), not just failed ones."`
}

// DownloadJobsReimportOutput is the bespoke result type from the service —
// a list of newly-created tasks plus a count of skipped non-terminal ones.
type DownloadJobsReimportOutput struct {
	Body service.ReimportResult
}

// Reimport creates new import tasks for failed (or all terminal) tasks of a
// download job, linking each new task back to its predecessor via
// `previous_task_id`.
func (h *DownloadJobs) Reimport(ctx context.Context, input *DownloadJobsReimportInput) (*DownloadJobsReimportOutput, error) {
	out, err := h.svc.DownloadJobs.ReimportFailed(ctx, input.ID, input.All)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsReimportOutput{Body: out}, nil
}

// ----- Retry Download -----

// DownloadJobsRetryInput holds the path id for
// POST /download-jobs/{id}/retry.
type DownloadJobsRetryInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

// DownloadJobsRetryOutput wraps the new (retry) job, returned with its
// import summary so callers don't need a follow-up fetch.
type DownloadJobsRetryOutput struct {
	Body model.DownloadJobWithSummary
}

// RetryDownload creates a new download job linked to the failed one via
// `previous_job_id`. The service emits a Conflict (409) when the source
// job's status doesn't allow retrying.
func (h *DownloadJobs) RetryDownload(ctx context.Context, input *DownloadJobsRetryInput) (*DownloadJobsRetryOutput, error) {
	out, err := h.svc.DownloadJobs.RetryDownload(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsRetryOutput{Body: out}, nil
}

// ----- History -----

// DownloadJobsHistoryInput holds the path id for
// GET /download-jobs/{id}/history.
type DownloadJobsHistoryInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

// DownloadJobsHistoryOutput is the retry chain — each entry is a job plus
// its depth in the chain (0 for the root).
type DownloadJobsHistoryOutput struct {
	Body []model.DownloadJobHistoryEntry
}

// GetHistory returns the recursive retry chain for this download job.
func (h *DownloadJobs) GetHistory(ctx context.Context, input *DownloadJobsHistoryInput) (*DownloadJobsHistoryOutput, error) {
	out, err := h.svc.DownloadJobs.GetHistory(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsHistoryOutput{Body: out}, nil
}

// ----- Cancel -----

// DownloadJobsCancelInput holds the path id for DELETE /download-jobs/{id}.
type DownloadJobsCancelInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

// DownloadJobsCancelOutput wraps the cancelled job. Pre-migration the Echo
// handler returned the job rather than 204, so we stick with 200 + body.
type DownloadJobsCancelOutput struct {
	Body model.DownloadJob
}

// Cancel cancels the download and any pending import tasks linked to it.
func (h *DownloadJobs) Cancel(ctx context.Context, input *DownloadJobsCancelInput) (*DownloadJobsCancelOutput, error) {
	out, err := h.svc.DownloadJobs.Cancel(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsCancelOutput{Body: out}, nil
}

// ----- List for Movie -----

// DownloadJobsListForMovieInput holds the TMDB movie id for
// GET /movie/{id}/download-jobs. The id is the TMDB int64, not a UUID.
type DownloadJobsListForMovieInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB movie id"`
}

// DownloadJobsListForMovieOutput is a flat list of all download jobs
// matching the TMDB id.
type DownloadJobsListForMovieOutput struct {
	Body []model.DownloadJob
}

// ListForMovie returns every download job tied to the given TMDB movie id.
func (h *DownloadJobs) ListForMovie(ctx context.Context, input *DownloadJobsListForMovieInput) (*DownloadJobsListForMovieOutput, error) {
	out, err := h.svc.DownloadJobs.ListByMovie(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsListForMovieOutput{Body: out}, nil
}

// ----- List for Series -----

// DownloadJobsListForSeriesInput holds the TMDB series id for
// GET /series/{id}/download-jobs.
type DownloadJobsListForSeriesInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB series id"`
}

// DownloadJobsListForSeriesOutput is a flat list of jobs enriched with
// season/episode numbers from the joined media tables.
type DownloadJobsListForSeriesOutput struct {
	Body []model.DownloadJobBySeriesEntry
}

// ListForSeries returns every download job tied to the given TMDB series id.
func (h *DownloadJobs) ListForSeries(ctx context.Context, input *DownloadJobsListForSeriesInput) (*DownloadJobsListForSeriesOutput, error) {
	out, err := h.svc.DownloadJobs.ListBySeries(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsListForSeriesOutput{Body: out}, nil
}

// ----- Register -----

// RegisterHumachi wires the download-jobs operations onto the supplied
// humachi API. JWT auth runs at the chi layer (see internal/http/http.go),
// so every operation registered here is implicitly protected.
func (h *DownloadJobs) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/download-jobs",
		Summary:     "List download jobs",
		Description: "List every download job with computed import-status summary.",
		Tags:        []string{"download-jobs"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/download-jobs/{id}",
		Summary:     "Get download job",
		Tags:        []string{"download-jobs"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-timeline",
		Method:      http.MethodGet,
		Path:        "/api/v1/download-jobs/{id}/timeline",
		Summary:     "Get download job timeline",
		Description: "Combined event log: download-job events plus all import-task events linked to this job.",
		Tags:        []string{"download-jobs"},
		Errors:      errsRead,
	}, h.GetTimeline)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-list-import-tasks",
		Method:      http.MethodGet,
		Path:        "/api/v1/download-jobs/{id}/import-tasks",
		Summary:     "List import tasks for download job",
		Tags:        []string{"download-jobs"},
		Errors:      errsRead,
	}, h.ListImportTasks)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-reimport",
		Method:      http.MethodPost,
		Path:        "/api/v1/download-jobs/{id}/reimport",
		Summary:     "Reimport failed import tasks",
		Description: "Create new import tasks for failed (or all terminal) tasks of this download job.",
		Tags:        []string{"download-jobs"},
		Errors:      errsRead,
	}, h.Reimport)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-retry",
		Method:      http.MethodPost,
		Path:        "/api/v1/download-jobs/{id}/retry",
		Summary:     "Retry failed download job",
		Description: "Create a new download job linked to the failed one via previous_job_id. Returns 409 when the source status doesn't allow retrying.",
		Tags:        []string{"download-jobs"},
		Errors:      errs(errsRead, []int{http.StatusConflict}),
	}, h.RetryDownload)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-history",
		Method:      http.MethodGet,
		Path:        "/api/v1/download-jobs/{id}/history",
		Summary:     "Get download job retry history",
		Tags:        []string{"download-jobs"},
		Errors:      errsRead,
	}, h.GetHistory)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-cancel",
		Method:      http.MethodDelete,
		Path:        "/api/v1/download-jobs/{id}",
		Summary:     "Cancel download job",
		Description: "Cancel the download and any pending import tasks linked to it. Returns the cancelled job (200), not 204.",
		Tags:        []string{"download-jobs"},
		Errors:      errsRead,
	}, h.Cancel)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-list-for-movie",
		Method:      http.MethodGet,
		Path:        "/api/v1/movie/{id}/download-jobs",
		Summary:     "List download jobs for movie",
		Description: "List every download job tied to the given TMDB movie id.",
		Tags:        []string{"download-jobs"},
	}, h.ListForMovie)

	huma.Register(api, huma.Operation{
		OperationID: "download-jobs-list-for-series",
		Method:      http.MethodGet,
		Path:        "/api/v1/series/{id}/download-jobs",
		Summary:     "List download jobs for series",
		Description: "List every download job tied to the given TMDB series id, enriched with season/episode numbers.",
		Tags:        []string{"download-jobs"},
	}, h.ListForSeries)
}
