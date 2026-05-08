package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/labstack/echo/v4"
)

type Libraries struct{ svc *service.Services }

func NewLibraries(s *service.Services) *Libraries { return &Libraries{svc: s} }

func (h *Libraries) RegisterProtected(v1 *echo.Group) {
	v1.GET("/libraries", h.List)
	v1.POST("/libraries", h.Create)
	v1.GET("/libraries/:id", h.Get)
	v1.PUT("/libraries/:id", h.Update)
	v1.DELETE("/libraries/:id", h.Delete)

	v1.POST("/libraries/:id/scan", h.Scan)
}

// librarySwagger is a minimal type used only for Swagger schemas.
// It mirrors fields of dbgen.Library without importing it here.
type librarySwagger struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	RootPath  string `json:"root_path"`
	Enabled   bool   `json:"enabled"`
	Default   bool   `json:"default"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// LibraryCreateRequest payload
type LibraryCreateRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	RootPath string `json:"root_path"`
	Enabled  bool   `json:"enabled"`
	Default  bool   `json:"default"`
}

// LibraryUpdateRequest payload
type LibraryUpdateRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	RootPath string `json:"root_path"`
	Enabled  bool   `json:"enabled"`
	Default  bool   `json:"default"`
}

// List libraries
// @Summary List libraries
// @Tags    libraries
// @Produce json
// @Success 200 {array} handlers.librarySwagger
// @Router  /v1/libraries [get]
func (h *Libraries) List(c echo.Context) error {
	ctx := c.Request().Context()
	out, err := h.svc.Libraries.List(ctx)
	if err != nil {
		return RenderError(c, err)
	}
	return c.JSON(http.StatusOK, out)
}

// Create library
// @Summary Create library
// @Tags    libraries
// @Accept  json
// @Produce json
// @Param   payload body handlers.LibraryCreateRequest true "Create library"
// @Success 201 {object} handlers.librarySwagger
// @Failure 422 {object} map[string]string
// @Router  /v1/libraries [post]
func (h *Libraries) Create(c echo.Context) error {
	var req LibraryCreateRequest
	if err := c.Bind(&req); err != nil {
		// Log the underlying parse error; do NOT include it in the wire
		// detail (would leak struct internals / parser text).
		c.Logger().Error(err)
		return RenderError(c, apperrors.Validation("invalid request body").
			Op("LibrariesHandler.Create"))
	}
	ctx := c.Request().Context()
	lib, err := h.svc.Libraries.Create(ctx, req.Name, req.Type, req.RootPath, req.Enabled, req.Default)
	if err != nil {
		return RenderError(c, err)
	}
	return c.JSON(http.StatusCreated, lib)
}

// Get library
// @Summary Get library
// @Tags    libraries
// @Produce json
// @Success 200 {object} handlers.librarySwagger
// @Router  /v1/libraries/{id} [get]
func (h *Libraries) Get(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return RenderError(c, apperrors.Validation("invalid library id",
			apperrors.Field("path.id", "must be a valid UUID")).
			Op("LibrariesHandler.Get"))
	}
	ctx := c.Request().Context()
	lib, err := h.svc.Libraries.Get(ctx, id)
	if err != nil {
		return RenderError(c, err)
	}
	return c.JSON(http.StatusOK, lib)
}

// Update library
// @Summary Update library
// @Tags    libraries
// @Accept  json
// @Produce json
// @Param   id path string true "Library ID"
// @Param   payload body handlers.LibraryUpdateRequest true "Update library"
// @Success 200 {object} handlers.librarySwagger
// @Failure 422 {object} map[string]string
// @Router  /v1/libraries/{id} [put]
func (h *Libraries) Update(c echo.Context) error {
	var req LibraryUpdateRequest
	if err := c.Bind(&req); err != nil {
		c.Logger().Error(err)
		return RenderError(c, apperrors.Validation("invalid request body").
			Op("LibrariesHandler.Update"))
	}
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return RenderError(c, apperrors.Validation("invalid library id",
			apperrors.Field("path.id", "must be a valid UUID")).
			Op("LibrariesHandler.Update"))
	}
	ctx := c.Request().Context()
	lib, err := h.svc.Libraries.Update(ctx, id, req.Name, req.Type, req.RootPath, req.Enabled, req.Default)
	if err != nil {
		return RenderError(c, err)
	}
	return c.JSON(http.StatusOK, lib)
}

// Delete library
// @Summary Delete library
// @Tags    libraries
// @Param   id path string true "Library ID"
// @Success 204 {string} string ""
// @Router  /v1/libraries/{id} [delete]
func (h *Libraries) Delete(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return RenderError(c, apperrors.Validation("invalid library id",
			apperrors.Field("path.id", "must be a valid UUID")).
			Op("LibrariesHandler.Delete"))
	}
	ctx := c.Request().Context()
	if err := h.svc.Libraries.Delete(ctx, id); err != nil {
		return RenderError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Scan library (async)
// @Summary Scan library
// @Tags    libraries
// @Param   id path string true "Library ID"
// @Produce json
// @Success 202 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router  /v1/libraries/{id}/scan [post]
func (h *Libraries) Scan(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return RenderError(c, apperrors.Validation("invalid library id",
			apperrors.Field("path.id", "must be a valid UUID")).
			Op("LibrariesHandler.Scan"))
	}
	ctx := c.Request().Context()
	scanID, err := h.svc.Scanner.StartScan(ctx, id)
	if err != nil {
		// Scan service emits typed Conflict on already-running.
		return RenderError(c, err)
	}
	return c.JSON(http.StatusAccepted, map[string]string{"scanId": scanID})
}
