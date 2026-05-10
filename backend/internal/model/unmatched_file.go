package model

import (
	"time"

	"github.com/google/uuid"
)

// UnmatchedFile is the domain shape for an unmatched_file row. It mirrors the
// persistence-layer dbgen.UnmatchedFile but uses idiomatic Go types
// (uuid.UUID, time.Time, *T for nullable scalars) and lives outside the
// persistence boundary.
type UnmatchedFile struct {
	ID                  uuid.UUID        `json:"id"`
	LibraryID           uuid.UUID        `json:"libraryId"`
	Path                string           `json:"path"`
	FileSize            *int64           `json:"fileSize,omitempty"`
	DiscoveredAt        time.Time        `json:"discoveredAt"`
	SuggestedMatches    []SuggestedMatch `json:"suggestedMatches,omitempty"`
	ResolvedAt          *time.Time       `json:"resolvedAt,omitempty"`
	ResolvedMediaFileID *uuid.UUID       `json:"resolvedMediaFileId,omitempty"`
}
