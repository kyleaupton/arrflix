package handlers

import (
	"net/http"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/labstack/echo/v4"
)

type Setup struct {
	svc *service.Services
}

func NewSetup(s *service.Services) *Setup {
	return &Setup{svc: s}
}

// RegisterPublic registers public setup routes (no auth required).
// The caller is expected to apply a setup-only guard middleware to the group.
func (h *Setup) RegisterPublic(g *echo.Group) {
	g.GET("/status", h.GetStatus)
	g.POST("/initialize", h.Initialize)
	g.POST("/tmdb", h.SetTmdbKey)
}

type SetupStatusResponse struct {
	Initialized bool               `json:"initialized"`
	Steps       *service.SetupSteps `json:"steps,omitempty"`
}

type SetupInitializeRequest struct {
	Email    string `json:"email" validate:"required"`
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type SetupInitializeResponse struct {
	Success bool `json:"success"`
}

type SetupTmdbRequest struct {
	ApiKey string `json:"api_key" validate:"required"`
}

type SetupTmdbResponse struct {
	Success bool `json:"success"`
}

// GetStatus checks if the system has been initialized
// @Summary Get setup status
// @Tags    setup
// @Produce json
// @Success 200 {object} handlers.SetupStatusResponse
// @Router  /v1/setup/status [get]
func (h *Setup) GetStatus(c echo.Context) error {
	ctx := c.Request().Context()
	initialized, err := h.svc.Setup.IsInitialized(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to check status"})
	}

	var steps *service.SetupSteps
	if !initialized {
		s, err := h.svc.Setup.GetSetupSteps(ctx)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get setup steps"})
		}
		steps = &s
	}

	return c.JSON(http.StatusOK, SetupStatusResponse{
		Initialized: initialized,
		Steps:       steps,
	})
}

// Initialize performs the one-time setup
// @Summary Initialize system
// @Tags    setup
// @Accept  json
// @Produce json
// @Param   payload body handlers.SetupInitializeRequest true "Setup request"
// @Success 200 {object} handlers.SetupInitializeResponse
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string "Already initialized"
// @Router  /v1/setup/initialize [post]
func (h *Setup) Initialize(c echo.Context) error {
	var req SetupInitializeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	ctx := c.Request().Context()
	err := h.svc.Setup.Initialize(ctx, req.Email, req.Username, req.Password)
	if err != nil {
		if apperrors.IsConflict(err) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "system already initialized"})
		}
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, SetupInitializeResponse{Success: true})
}

// SetTmdbKey validates and saves the TMDB API key during setup
// @Summary Set TMDB API key
// @Tags    setup
// @Accept  json
// @Produce json
// @Param   payload body handlers.SetupTmdbRequest true "TMDB key request"
// @Success 200 {object} handlers.SetupTmdbResponse
// @Failure 400 {object} map[string]string
// @Router  /v1/setup/tmdb [post]
func (h *Setup) SetTmdbKey(c echo.Context) error {
	var req SetupTmdbRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if req.ApiKey == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "api_key required"})
	}

	ctx := c.Request().Context()
	if err := h.svc.Setup.SetTmdbKey(ctx, req.ApiKey); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, SetupTmdbResponse{Success: true})
}
