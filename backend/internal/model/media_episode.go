package model

import (
	"time"

	"github.com/google/uuid"
)

type MediaEpisode struct {
	ID            uuid.UUID  `json:"id"`
	SeasonID      uuid.UUID  `json:"seasonId"`
	EpisodeNumber int32      `json:"episodeNumber"`
	Title         *string    `json:"title,omitempty"`
	AirDate       *time.Time `json:"airDate,omitempty"`
	TmdbID        *int64     `json:"tmdbId,omitempty"`
	TvdbID        *int64     `json:"tvdbId,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}
