package handlers

import (
	"net/http"
	"strconv"

	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/labstack/echo/v4"
)

type DownloadCandidates struct{ svc *service.Services }

func NewDownloadCandidates(s *service.Services) *DownloadCandidates {
	return &DownloadCandidates{svc: s}
}

func (h *DownloadCandidates) RegisterProtected(v1 *echo.Group) {
	v1.GET("/movie/:id/candidates", h.GetDownloadCandidates)
	v1.POST("/movie/:id/candidate/preview", h.PreviewCandidate)
	v1.POST("/movie/:id/candidate/download", h.DownloadCandidate)

	v1.GET("/series/:id/candidates", h.GetSeriesDownloadCandidates)
	v1.POST("/series/:id/candidate/preview", h.PreviewSeriesCandidate)
	v1.POST("/series/:id/candidate/download", h.DownloadSeriesCandidate)
}

// GetDownloadCandidates searches for download candidates for a movie
// @Summary Get download candidates for a movie
// @Tags    download-candidates
// @Produce json
// @Param   id path int true "Movie ID (TMDB ID)"
// @Success 200 {array} model.DownloadCandidate
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router  /v1/movie/{id}/candidates [get]
func (h *DownloadCandidates) GetDownloadCandidates(c echo.Context) error {
	movieID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid movie ID"})
	}

	ctx := c.Request().Context()
	candidates, err := h.svc.DownloadCandidates.SearchDownloadCandidates(ctx, movieID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, candidates)
}

// GetSeriesDownloadCandidates searches for download candidates for a series, season, or episode
// @Summary Get download candidates for a series
// @Tags    download-candidates
// @Produce json
// @Param   id path int true "Series ID (TMDB ID)"
// @Param   season query int false "Season number"
// @Param   episode query int false "Episode number"
// @Success 200 {array} model.DownloadCandidate
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router  /v1/series/{id}/candidates [get]
func (h *DownloadCandidates) GetSeriesDownloadCandidates(c echo.Context) error {
	seriesID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid series ID"})
	}

	var season, episode *int
	if s := c.QueryParam("season"); s != "" {
		val, err := strconv.Atoi(s)
		if err == nil {
			season = &val
		}
	}
	if e := c.QueryParam("episode"); e != "" {
		val, err := strconv.Atoi(e)
		if err == nil {
			episode = &val
		}
	}

	ctx := c.Request().Context()
	candidates, err := h.svc.DownloadCandidates.SearchSeriesDownloadCandidates(ctx, seriesID, season, episode)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, candidates)
}

// EnqueueCandidateRequest is the request body for enqueueing a candidate
type EnqueueCandidateRequest struct {
	IndexerID int64  `json:"indexerId"`
	GUID      string `json:"guid"`
	Season    *int   `json:"season,omitempty"`
	Episode   *int   `json:"episode,omitempty"`
}

// PreviewCandidate previews what will happen when a candidate is enqueued
// @Summary Preview policy evaluation for a download candidate
// @Tags    download-candidates
// @Accept  json
// @Produce json
// @Param   id path int true "Movie ID (TMDB ID)"
// @Param   payload body handlers.EnqueueCandidateRequest true "Preview request"
// @Success 200 {object} model.EvaluationTrace
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router  /v1/movie/{id}/candidate/preview [post]
func (h *DownloadCandidates) PreviewCandidate(c echo.Context) error {
	movieID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid movie ID"})
	}

	var req EnqueueCandidateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	if req.IndexerID == 0 || req.GUID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "indexerId and guid are required"})
	}

	ctx := c.Request().Context()
	trace, err := h.svc.DownloadCandidates.EvaluateCandidate(ctx, movieID, req.IndexerID, req.GUID)
	if err != nil {
		if apperrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, trace)
}

// PreviewSeriesCandidate previews what will happen when a series candidate is enqueued
// @Summary Preview policy evaluation for a series download candidate
// @Tags    download-candidates
// @Accept  json
// @Produce json
// @Param   id path int true "Series ID (TMDB ID)"
// @Param   payload body handlers.EnqueueCandidateRequest true "Preview request"
// @Success 200 {object} model.EvaluationTrace
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router  /v1/series/{id}/candidate/preview [post]
func (h *DownloadCandidates) PreviewSeriesCandidate(c echo.Context) error {
	seriesID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid series ID"})
	}

	var req EnqueueCandidateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	if req.IndexerID == 0 || req.GUID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "indexerId and guid are required"})
	}

	ctx := c.Request().Context()
	trace, err := h.svc.DownloadCandidates.EvaluateCandidate(ctx, seriesID, req.IndexerID, req.GUID)
	if err != nil {
		if apperrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, trace)
}

// DownloadCandidate downloads candidate through the policy engine
// @Summary Download a download candidate
// @Tags    download-candidates
// @Accept  json
// @Produce json
// @Param   id path int true "Movie ID (TMDB ID)"
// @Param   payload body handlers.EnqueueCandidateRequest true "Download request"
// @Success 200 {object} handlers.DownloadCandidateResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router  /v1/movie/{id}/candidate/download [post]
func (h *DownloadCandidates) DownloadCandidate(c echo.Context) error {
	movieID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid movie ID"})
	}

	var req EnqueueCandidateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	if req.IndexerID == 0 || req.GUID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "indexerId and guid are required"})
	}

	ctx := c.Request().Context()
	trace, job, err := h.svc.DownloadCandidates.EnqueueCandidate(ctx, movieID, req.IndexerID, req.GUID)
	if err != nil {
		if apperrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, DownloadCandidateResponse{
		Trace: trace,
		Job:   job,
	})
}

// DownloadSeriesCandidate downloads series candidate through the policy engine
// @Summary Download a series download candidate
// @Tags    download-candidates
// @Accept  json
// @Produce json
// @Param   id path int true "Series ID (TMDB ID)"
// @Param   payload body handlers.EnqueueCandidateRequest true "Download request"
// @Success 200 {object} handlers.DownloadCandidateResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router  /v1/series/{id}/candidate/download [post]
func (h *DownloadCandidates) DownloadSeriesCandidate(c echo.Context) error {
	seriesID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid series ID"})
	}

	var req EnqueueCandidateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	if req.IndexerID == 0 || req.GUID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "indexerId and guid are required"})
	}

	ctx := c.Request().Context()
	trace, job, err := h.svc.DownloadCandidates.EnqueueSeriesCandidate(ctx, seriesID, req.IndexerID, req.GUID, req.Season, req.Episode)
	if err != nil {
		if apperrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, DownloadCandidateResponse{
		Trace: trace,
		Job:   job,
	})
}

type DownloadCandidateResponse struct {
	Trace model.EvaluationTrace `json:"trace"`
	Job   dbgen.DownloadJob     `json:"job"`
}
