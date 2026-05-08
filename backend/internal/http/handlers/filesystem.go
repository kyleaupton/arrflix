package handlers

import (
	"net/http"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/labstack/echo/v4"
)

type Filesystem struct{ svc *service.Services }

func NewFilesystem(s *service.Services) *Filesystem { return &Filesystem{svc: s} }

func (h *Filesystem) RegisterProtected(v1 *echo.Group) {
	v1.GET("/filesystem/browse", h.Browse)
}

// filesystemDirectoryEntry represents a single directory entry.
type filesystemDirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// filesystemBrowseResponse is the response for the browse endpoint.
type filesystemBrowseResponse struct {
	CurrentPath string                     `json:"current_path"`
	Parent      string                     `json:"parent"`
	Directories []filesystemDirectoryEntry `json:"directories"`
}

// Browse filesystem directories
// @Summary Browse filesystem directories
// @Tags    filesystem
// @Produce json
// @Param   path query string false "Directory path to browse (defaults to /)"
// @Success 200 {object} handlers.filesystemBrowseResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router  /v1/filesystem/browse [get]
func (h *Filesystem) Browse(c echo.Context) error {
	path := c.QueryParam("path")
	ctx := c.Request().Context()

	result, err := h.svc.Filesystem.Browse(ctx, path)
	if err != nil {
		switch {
		case apperrors.IsNotFound(err):
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		case apperrors.IsForbidden(err):
			return c.JSON(http.StatusForbidden, map[string]string{"error": err.Error()})
		case apperrors.IsValidation(err):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}

	dirs := make([]filesystemDirectoryEntry, len(result.Directories))
	for i, d := range result.Directories {
		dirs[i] = filesystemDirectoryEntry{Name: d.Name, Path: d.Path}
	}

	return c.JSON(http.StatusOK, filesystemBrowseResponse{
		CurrentPath: result.CurrentPath,
		Parent:      result.Parent,
		Directories: dirs,
	})
}
