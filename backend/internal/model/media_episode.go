package model

import (
	"time"

	"github.com/google/uuid"
)

type MediaEpisode struct {
	ID             uuid.UUID  `json:"id"`
	SeasonID       uuid.UUID  `json:"seasonId"`
	EpisodeNumber  int32      `json:"episodeNumber"`
	Title          *string    `json:"title,omitempty"`
	AirDate        *time.Time `json:"airDate,omitempty"`
	Overview       *string    `json:"overview,omitempty"`
	StillPath      *string    `json:"stillPath,omitempty"`
	VoteAverage    *float64   `json:"voteAverage,omitempty"`
	Runtime        *int32     `json:"runtime,omitempty"`
	AbsoluteNumber *int32     `json:"absoluteNumber,omitempty"`
	Deprecated     bool       `json:"deprecated"`
	TmdbID         *int64     `json:"tmdbId,omitempty"`
	TvdbID         *int64     `json:"tvdbId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}
