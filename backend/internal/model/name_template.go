package model

import (
	"time"

	"github.com/google/uuid"
)

// NameTemplate is the domain shape for a name_template row. It mirrors the
// persistence-layer dbgen.NameTemplate but uses idiomatic Go types
// (uuid.UUID, time.Time) and lives outside the persistence boundary.
type NameTemplate struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	Type                 string    `json:"type"`
	Template             string    `json:"template"`
	MovieDirTemplate     *string   `json:"movieDirTemplate"`
	SeriesShowTemplate   *string   `json:"seriesShowTemplate"`
	SeriesSeasonTemplate *string   `json:"seriesSeasonTemplate"`
	Default              bool      `json:"default"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}
