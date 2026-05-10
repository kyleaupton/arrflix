package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Filesystem struct{ svc *service.Services }

func NewFilesystem(s *service.Services) *Filesystem { return &Filesystem{svc: s} }

// ----- Browse response shape -----

type filesystemDirectoryEntry struct {
	Name string `json:"name" doc:"Display name (basename) of the directory"`
	Path string `json:"path" doc:"Absolute path of the directory"`
}

// filesystemBrowseResponse uses snake_case for `current_path` and `parent`
// because the FE consumes those names today.
type filesystemBrowseResponse struct {
	CurrentPath string                     `json:"current_path" doc:"Resolved absolute path that was browsed"`
	Parent      string                     `json:"parent" doc:"Parent directory path (empty when current_path is /)"`
	Directories []filesystemDirectoryEntry `json:"directories" doc:"Child directories, sorted case-insensitive by name"`
}

// ----- Browse -----

type FilesystemBrowseInput struct {
	Path string `query:"path" doc:"Absolute directory path to browse. Empty defaults to /."`
}

type FilesystemBrowseOutput struct {
	Body filesystemBrowseResponse
}

func (h *Filesystem) Browse(ctx context.Context, input *FilesystemBrowseInput) (*FilesystemBrowseOutput, error) {
	result, err := h.svc.Filesystem.Browse(ctx, input.Path)
	if err != nil {
		return nil, err
	}

	dirs := make([]filesystemDirectoryEntry, len(result.Directories))
	for i, d := range result.Directories {
		dirs[i] = filesystemDirectoryEntry{Name: d.Name, Path: d.Path}
	}

	return &FilesystemBrowseOutput{Body: filesystemBrowseResponse{
		CurrentPath: result.CurrentPath,
		Parent:      result.Parent,
		Directories: dirs,
	}}, nil
}

// ----- Register -----

func (h *Filesystem) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "filesystem-browse",
		Method:      http.MethodGet,
		Path:        "/api/v1/filesystem/browse",
		Summary:     "Browse filesystem directories",
		Description: "Lists the immediate child directories of an absolute path. Used by the FE library-picker. Hides dotfiles and (at the root only) system directories.",
		Tags:        []string{"filesystem"},
		Errors:      errs(errsRead, errsForbidden, []int{http.StatusUnprocessableEntity}),
	}, h.Browse)
}
