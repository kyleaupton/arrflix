package model

import (
	"time"

	"github.com/google/uuid"
)

// File is the domain shape for a file row — one physical media file under
// a library root. Identity is nullable: MediaItemID is the title (movie or
// series), EpisodeID the episode leaf when resolved, both nil meaning "not
// identified yet". Files soft-delete (DeletedAt); a nil DeletedAt is live.
//
// See specs/modules/files/README.md.
type File struct {
	ID          uuid.UUID  `json:"id"`
	LibraryID   uuid.UUID  `json:"libraryId"`
	Path        string     `json:"path"`
	MediaItemID *uuid.UUID `json:"mediaItemId,omitempty"`
	EpisodeID   *uuid.UUID `json:"episodeId,omitempty"`
	Edition     *string    `json:"edition,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
}

// FileWithState is the composite shape returned by repo queries that join
// file with file_state (and optionally season/episode context). Each query
// populates the subset of nullable fields its SQL produces; callers know
// which fields their query covers.
type FileWithState struct {
	File
	// State fields — nil when the state row is absent or the query didn't select them.
	Exists         *bool      `json:"exists,omitempty"`
	SizeBytes      *int64     `json:"sizeBytes,omitempty"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`
	// Episode context — nil for movie-type files or queries that don't join the season table.
	SeasonID      *uuid.UUID `json:"seasonId,omitempty"`
	SeasonNumber  *int32     `json:"seasonNumber,omitempty"`
	EpisodeNumber *int32     `json:"episodeNumber,omitempty"`
}
