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

type DownloadJobsListInput struct{}

type DownloadJobsListOutput struct {
	Body []model.DownloadJobWithSummary
}

func (h *DownloadJobs) List(ctx context.Context, _ *DownloadJobsListInput) (*DownloadJobsListOutput, error) {
	out, err := h.svc.DownloadJobs.ListWithImportSummary(ctx)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsListOutput{Body: out}, nil
}

// ----- Get -----

type DownloadJobsGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

type DownloadJobsGetOutput struct {
	Body model.DownloadJobWithSummary
}

func (h *DownloadJobs) Get(ctx context.Context, input *DownloadJobsGetInput) (*DownloadJobsGetOutput, error) {
	out, err := h.svc.DownloadJobs.GetWithImportSummary(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsGetOutput{Body: out}, nil
}

// ----- Timeline -----

type DownloadJobsTimelineInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

type DownloadJobsTimelineOutput struct {
	Body []model.DownloadJobTimelineEvent
}

func (h *DownloadJobs) GetTimeline(ctx context.Context, input *DownloadJobsTimelineInput) (*DownloadJobsTimelineOutput, error) {
	out, err := h.svc.DownloadJobs.GetTimeline(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsTimelineOutput{Body: out}, nil
}

// ----- List Import Tasks -----

type DownloadJobsListImportTasksInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

type DownloadJobsListImportTasksOutput struct {
	Body []model.ImportTask
}

func (h *DownloadJobs) ListImportTasks(ctx context.Context, input *DownloadJobsListImportTasksInput) (*DownloadJobsListImportTasksOutput, error) {
	out, err := h.svc.DownloadJobs.ListImportTasks(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsListImportTasksOutput{Body: out}, nil
}

// ----- Reimport -----

type DownloadJobsReimportInput struct {
	ID  uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
	All bool      `query:"all" doc:"Reimport all terminal tasks (completed, failed, cancelled), not just failed ones."`
}

type DownloadJobsReimportOutput struct {
	Body service.ReimportResult
}

func (h *DownloadJobs) Reimport(ctx context.Context, input *DownloadJobsReimportInput) (*DownloadJobsReimportOutput, error) {
	out, err := h.svc.DownloadJobs.ReimportFailed(ctx, input.ID, input.All)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsReimportOutput{Body: out}, nil
}

// ----- Retry Download -----

type DownloadJobsRetryInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

type DownloadJobsRetryOutput struct {
	Body model.DownloadJobWithSummary
}

func (h *DownloadJobs) RetryDownload(ctx context.Context, input *DownloadJobsRetryInput) (*DownloadJobsRetryOutput, error) {
	out, err := h.svc.DownloadJobs.RetryDownload(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsRetryOutput{Body: out}, nil
}

// ----- History -----

type DownloadJobsHistoryInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

type DownloadJobsHistoryOutput struct {
	Body []model.DownloadJobHistoryEntry
}

func (h *DownloadJobs) GetHistory(ctx context.Context, input *DownloadJobsHistoryInput) (*DownloadJobsHistoryOutput, error) {
	out, err := h.svc.DownloadJobs.GetHistory(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsHistoryOutput{Body: out}, nil
}

// ----- Cancel -----

type DownloadJobsCancelInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Download job ID"`
}

// Cancel returns the cancelled job (200 with body) rather than 204 — the FE
// uses the returned status fields to drive its UI without a follow-up fetch.
type DownloadJobsCancelOutput struct {
	Body model.DownloadJob
}

func (h *DownloadJobs) Cancel(ctx context.Context, input *DownloadJobsCancelInput) (*DownloadJobsCancelOutput, error) {
	out, err := h.svc.DownloadJobs.Cancel(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsCancelOutput{Body: out}, nil
}

// ----- List for Movie -----

type DownloadJobsListForMovieInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB movie id"`
}

type DownloadJobsListForMovieOutput struct {
	Body []model.DownloadJob
}

func (h *DownloadJobs) ListForMovie(ctx context.Context, input *DownloadJobsListForMovieInput) (*DownloadJobsListForMovieOutput, error) {
	out, err := h.svc.DownloadJobs.ListByMovie(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsListForMovieOutput{Body: out}, nil
}

// ----- List for Series -----

type DownloadJobsListForSeriesInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB series id"`
}

type DownloadJobsListForSeriesOutput struct {
	Body []model.DownloadJobBySeriesEntry
}

func (h *DownloadJobs) ListForSeries(ctx context.Context, input *DownloadJobsListForSeriesInput) (*DownloadJobsListForSeriesOutput, error) {
	out, err := h.svc.DownloadJobs.ListBySeries(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadJobsListForSeriesOutput{Body: out}, nil
}

// ----- Register -----

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
