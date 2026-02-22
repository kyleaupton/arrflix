package handlers

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/labstack/echo/v4"
)

type DownloadJobs struct{ svc *service.Services }

func NewDownloadJobs(s *service.Services) *DownloadJobs { return &DownloadJobs{svc: s} }

func (h *DownloadJobs) RegisterProtected(v1 *echo.Group) {
	v1.GET("/download-jobs", h.List)
	v1.GET("/download-jobs/:id", h.Get)
	v1.GET("/download-jobs/:id/timeline", h.GetTimeline)
	v1.GET("/download-jobs/:id/import-tasks", h.ListImportTasks)
	v1.POST("/download-jobs/:id/reimport", h.Reimport)
	v1.POST("/download-jobs/:id/retry", h.RetryDownload)
	v1.GET("/download-jobs/:id/history", h.GetHistory)
	v1.DELETE("/download-jobs/:id", h.Cancel)

	v1.GET("/movie/:id/download-jobs", h.ListForMovie)
	v1.GET("/series/:id/download-jobs", h.ListForSeries)
}

// List download jobs with import summary
// @Summary List download jobs with import status summary
// @Tags    download-jobs
// @Produce json
// @Success 200 {array} dbgen.ListDownloadJobsWithImportSummaryRow
// @Router  /v1/download-jobs [get]
func (h *DownloadJobs) List(c echo.Context) error {
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.ListWithImportSummary(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list"})
	}
	return c.JSON(http.StatusOK, out)
}

// Get download job with import summary
// @Summary Get download job with import status summary
// @Tags    download-jobs
// @Produce json
// @Param   id path string true "Job ID (uuid)"
// @Success 200 {object} dbgen.GetDownloadJobWithImportSummaryRow
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router  /v1/download-jobs/{id} [get]
func (h *DownloadJobs) Get(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.GetWithImportSummary(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.JSON(http.StatusOK, out)
}

// GetTimeline returns combined download and import event log
// @Summary Get download job timeline
// @Tags    download-jobs
// @Produce json
// @Param   id path string true "Job ID (uuid)"
// @Success 200 {array} dbgen.GetDownloadJobTimelineRow
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router  /v1/download-jobs/{id}/timeline [get]
func (h *DownloadJobs) GetTimeline(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.GetTimeline(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.JSON(http.StatusOK, out)
}

// ListImportTasks returns import tasks for a download job
// @Summary List import tasks for download job
// @Tags    download-jobs
// @Produce json
// @Param   id path string true "Job ID (uuid)"
// @Success 200 {array} dbgen.ImportTask
// @Failure 400 {object} map[string]string
// @Router  /v1/download-jobs/{id}/import-tasks [get]
func (h *DownloadJobs) ListImportTasks(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.ListImportTasks(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list"})
	}
	return c.JSON(http.StatusOK, out)
}

// Reimport creates new import tasks for failed (or all) import tasks of a download job
// @Summary Reimport failed import tasks
// @Tags    download-jobs
// @Produce json
// @Param   id path string true "Job ID (uuid)"
// @Param   all query bool false "Reimport all tasks, not just failed ones"
// @Success 200 {object} service.ReimportResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router  /v1/download-jobs/{id}/reimport [post]
func (h *DownloadJobs) Reimport(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	all := c.QueryParam("all") == "true"

	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.ReimportFailed(ctx, id, all)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, out)
}

// Retry a failed download job
// @Summary Retry failed download job
// @Tags    download-jobs
// @Produce json
// @Param   id path string true "Job ID (uuid)"
// @Success 200 {object} dbgen.GetDownloadJobWithImportSummaryRow
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router  /v1/download-jobs/{id}/retry [post]
func (h *DownloadJobs) RetryDownload(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.RetryDownload(ctx, id)
	if err != nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, out)
}

// Get download job retry history
// @Summary Get download job retry history
// @Tags    download-jobs
// @Produce json
// @Param   id path string true "Job ID (uuid)"
// @Success 200 {array} dbgen.GetDownloadJobHistoryRow
// @Failure 400 {object} map[string]string
// @Router  /v1/download-jobs/{id}/history [get]
func (h *DownloadJobs) GetHistory(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.GetHistory(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.JSON(http.StatusOK, out)
}

// Cancel download job
// @Summary Cancel download job
// @Tags    download-jobs
// @Produce json
// @Param   id path string true "Job ID (uuid)"
// @Success 200 {object} dbgen.DownloadJob
// @Failure 400 {object} map[string]string
// @Router  /v1/download-jobs/{id} [delete]
func (h *DownloadJobs) Cancel(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.Cancel(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, out)
}

// List download jobs for a movie (TMDB ID)
// @Summary List movie download jobs
// @Tags    download-jobs
// @Produce json
// @Param   id path int true "Movie ID (TMDB ID)"
// @Success 200 {array} dbgen.DownloadJob
// @Failure 400 {object} map[string]string
// @Router  /v1/movie/{id}/download-jobs [get]
func (h *DownloadJobs) ListForMovie(c echo.Context) error {
	movieID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid movie ID"})
	}
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.ListByMovie(ctx, movieID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list"})
	}
	return c.JSON(http.StatusOK, out)
}

// List download jobs for a series (TMDB ID)
// @Summary List series download jobs
// @Tags    download-jobs
// @Produce json
// @Param   id path int true "Series ID (TMDB ID)"
// @Success 200 {array} dbgen.ListDownloadJobsByTmdbSeriesIDRow
// @Failure 400 {object} map[string]string
// @Router  /v1/series/{id}/download-jobs [get]
func (h *DownloadJobs) ListForSeries(c echo.Context) error {
	seriesID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid series ID"})
	}
	ctx := c.Request().Context()
	out, err := h.svc.DownloadJobs.ListBySeries(ctx, seriesID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list"})
	}
	return c.JSON(http.StatusOK, out)
}
