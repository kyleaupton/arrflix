package model

import (
	"time"

	"github.com/google/uuid"
)

// MediaFile is the domain shape for a media_file row. It mirrors the
// persistence-layer dbgen.MediaFile but uses idiomatic Go types
// (uuid.UUID, time.Time, *T for nullable scalars) and lives outside the
// persistence boundary.
type MediaFile struct {
	ID          uuid.UUID  `json:"id"`
	LibraryID   uuid.UUID  `json:"libraryId"`
	MediaItemID uuid.UUID  `json:"mediaItemId"`
	EpisodeID   *uuid.UUID `json:"episodeId,omitempty"`
	Path        string     `json:"path"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// MediaFileWithState is the composite shape returned by repo queries that
// join media_files with media_file_state (and optionally season/episode
// context). Each query populates the subset of nullable fields its SQL
// produces; callers know which fields their query covers.
type MediaFileWithState struct {
	MediaFile
	// State fields — nil when the state row is absent or the query didn't select them.
	FileExists     *bool      `json:"fileExists,omitempty"`
	FileSize       *int64     `json:"fileSize,omitempty"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`
	// Episode context — nil for movie-type files or queries that don't join the season table.
	SeasonID      *uuid.UUID `json:"seasonId,omitempty"`
	SeasonNumber  *int32     `json:"seasonNumber,omitempty"`
	EpisodeNumber *int32     `json:"episodeNumber,omitempty"`
}
