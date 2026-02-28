package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrPathNotFound     = errors.New("path not found")
	ErrPermissionDenied = errors.New("permission denied")
	ErrNotADirectory    = errors.New("not a directory")
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
			return nil, fmt.Errorf("%w: %s", ErrPathNotFound, path)
		}
		if os.IsPermission(err) {
			return nil, fmt.Errorf("%w: %s", ErrPermissionDenied, path)
		}
		return nil, fmt.Errorf("cannot access path: %s", path)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotADirectory, path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsPermission(err) {
			return nil, fmt.Errorf("%w: %s", ErrPermissionDenied, path)
		}
		return nil, fmt.Errorf("cannot read directory: %s", path)
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
