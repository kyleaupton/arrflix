package model

import (
	"time"

	"github.com/google/uuid"
)

// MediaItem is the domain shape for a media item row. It mirrors the
// persistence-layer dbgen.MediaItem but uses idiomatic Go types
// (uuid.UUID, time.Time, *T for nullable scalars) and lives outside the
// persistence boundary.
type MediaItem struct {
	ID                uuid.UUID  `json:"id"`
	Type              string     `json:"type"`
	Title             string     `json:"title"`
	Year              *int32     `json:"year,omitempty"`
	TmdbID            *int64     `json:"tmdbId,omitempty"`
	PosterPath        *string    `json:"posterPath,omitempty"`
	BackdropPath      *string    `json:"backdropPath,omitempty"`
	Overview          *string    `json:"overview,omitempty"`
	VoteAverage       *float64   `json:"voteAverage,omitempty"`
	VoteCount         *int32     `json:"voteCount,omitempty"`
	Runtime           *int32     `json:"runtime,omitempty"`
	Status            *string    `json:"status,omitempty"`
	Certification     *string    `json:"certification,omitempty"`
	Genres            []Genre    `json:"genres,omitempty"`
	ReleaseDate       *time.Time `json:"releaseDate,omitempty"`
	LastAirDate       *time.Time `json:"lastAirDate,omitempty"`
	InProduction      *bool      `json:"inProduction,omitempty"`
	ImdbID            *string    `json:"imdbId,omitempty"`
	MetadataUpdatedAt *time.Time `json:"metadataUpdatedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}
