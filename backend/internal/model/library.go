package model

import (
	"time"

	"github.com/google/uuid"
)

// Library is the domain shape for a media library row. It mirrors the
// persistence-layer dbgen.Library but uses idiomatic Go types
// (uuid.UUID, time.Time) and lives outside the persistence boundary.
type Library struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	RootPath  string    `json:"rootPath"`
	Enabled   bool      `json:"enabled"`
	Default   bool      `json:"default"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
