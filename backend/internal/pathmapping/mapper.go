// Package pathmapping provides path translation between downloader and Arrflix views.
package pathmapping

import (
	"context"

	"github.com/google/uuid"
)

// Mapper translates paths from the downloader's filesystem view to Arrflix's view.
type Mapper struct{}

// New creates a new path mapper.
func New() *Mapper {
	return &Mapper{}
}

// Apply translates a path from downloader's view to Arrflix's view.
// Currently a no-op stub - returns path unchanged.
// TODO: Implement remote_path_mapping table lookup using downloaderID.
func (m *Mapper) Apply(ctx context.Context, downloaderID uuid.UUID, path string) string {
	return path
}
