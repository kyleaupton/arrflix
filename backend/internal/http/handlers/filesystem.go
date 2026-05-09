// filesystem.go is the humachi-shaped filesystem handler. The single
// GET /filesystem/browse endpoint is the FE's directory picker: given an
// absolute path, lists the immediate child directories so the user can
// drill in. The service performs the security checks (path validity,
// permission, hides system dirs at root, hides dotfiles) and returns
// typed errors that humaerr renders as RFC 9457 problem-details.
//
// No path-traversal protection lives here at the handler — the service
// rejects non-directories and surfaces filesystem permission errors with
// the proper kind. This matches the pre-migration Echo behavior; the FE
// uses the endpoint to navigate a path the user types, and the OS-level
// permission checks decide what they can see.
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

// filesystemDirectoryEntry is a single child directory.
type filesystemDirectoryEntry struct {
	Name string `json:"name" doc:"Display name (basename) of the directory"`
	Path string `json:"path" doc:"Absolute path of the directory"`
}

// filesystemBrowseResponse is the wire shape returned by GET /filesystem/browse.
// `current_path` and `parent` use snake_case to match the pre-migration Echo
// response exactly; the FE consumes those names today.
type filesystemBrowseResponse struct {
	CurrentPath string                     `json:"current_path" doc:"Resolved absolute path that was browsed"`
	Parent      string                     `json:"parent" doc:"Parent directory path (empty when current_path is /)"`
	Directories []filesystemDirectoryEntry `json:"directories" doc:"Child directories, sorted case-insensitive by name"`
}

// ----- Browse -----

// FilesystemBrowseInput carries the optional `?path=` query. An empty value
// resolves to the filesystem root "/".
type FilesystemBrowseInput struct {
	Path string `query:"path" doc:"Absolute directory path to browse. Empty defaults to /."`
}

// FilesystemBrowseOutput wraps filesystemBrowseResponse.
type FilesystemBrowseOutput struct {
	Body filesystemBrowseResponse
}

// Browse lists immediate child directories of `path`. Errors from the
// service are typed: NotFound (404), Forbidden (403), Validation (422,
// e.g. when the path is a file), or Internal (500).
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

// RegisterHumachi wires the single filesystem operation onto the humachi API.
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
