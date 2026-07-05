package model

import (
	"time"

	"github.com/google/uuid"
)

// SeriesEpisodeAvailability is the persistence-shape composite returned by
// repo.ListEpisodeAvailabilityForSeries. Distinct from the wire-facing
// model.EpisodeAvailability (in media_detail.go); services translate this
// into the wire shape.
type SeriesEpisodeAvailability struct {
	SeasonNumber  int32      `json:"seasonNumber"`
	EpisodeNumber int32      `json:"episodeNumber"`
	EpisodeID     uuid.UUID  `json:"episodeId"`
	Title         *string    `json:"title,omitempty"`
	AirDate       *time.Time `json:"airDate,omitempty"`
	Deprecated    bool       `json:"deprecated"`
	FileID        *uuid.UUID `json:"fileId,omitempty"`
	LibraryID     *uuid.UUID `json:"libraryId,omitempty"`
	FileExists    *bool      `json:"fileExists,omitempty"`
}
