package model

import (
	"time"

	"github.com/google/uuid"
)

type MediaSeason struct {
	ID           uuid.UUID  `json:"id"`
	MediaItemID  uuid.UUID  `json:"mediaItemId"`
	SeasonNumber int32      `json:"seasonNumber"`
	AirDate      *time.Time `json:"airDate,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}
