package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type BrowseResult struct {
	CurrentPath string           `json:"current_path"`
	Parent      string           `json:"parent"`
	Directories []DirectoryEntry `json:"directories"`
}

type FilesystemService struct{}

func NewFilesystemService() *FilesystemService {
	return &FilesystemService{}
}

// rootSkipDirs are system directories to hide when browsing the root level.
var rootSkipDirs = map[string]bool{
	"proc":  true,
	"sys":   true,
	"dev":   true,
	"run":   true,
	"boot":  true,
	"sbin":  true,
	"bin":   true,
	"lib":   true,
	"lib64": true,
	"usr":   true,
	"var":   true,
}

func (s *FilesystemService) Browse(_ context.Context, path string) (*BrowseResult, error) {
	if path == "" {
		path = "/"
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperrors.NotFoundf("path %q not found", path).
				Op("FilesystemService.Browse")
		}
		if os.IsPermission(err) {
			return nil, apperrors.Forbiddenf("permission denied for %q", path).
				Op("FilesystemService.Browse")
		}
		return nil, apperrors.Internalf("cannot access path %q: %v", path, err).
			Op("FilesystemService.Browse")
	}
	if !info.IsDir() {
		return nil, apperrors.Validation("not a directory",
			apperrors.Field("query.path", fmt.Sprintf("%q is not a directory", path)),
		).Op("FilesystemService.Browse")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsPermission(err) {
			return nil, apperrors.Forbiddenf("permission denied for %q", path).
				Op("FilesystemService.Browse")
		}
		return nil, apperrors.Internalf("cannot read directory %q: %v", path, err).
			Op("FilesystemService.Browse")
	}

	isRoot := path == "/"
	var dirs []DirectoryEntry

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip hidden directories
		if strings.HasPrefix(name, ".") {
			continue
		}
		// Skip system directories at root level
		if isRoot && rootSkipDirs[name] {
			continue
		}
		dirs = append(dirs, DirectoryEntry{
			Name: name,
			Path: filepath.Join(path, name),
		})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	parent := ""
	if path != "/" {
		parent = filepath.Dir(path)
	}

	return &BrowseResult{
		CurrentPath: path,
		Parent:      parent,
		Directories: dirs,
	}, nil
}
