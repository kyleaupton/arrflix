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
	ID               uuid.UUID        `json:"id"`
	LibraryID        uuid.UUID        `json:"libraryId"`
	Path             string           `json:"path"`
	FileSize         *int64           `json:"fileSize,omitempty"`
	DiscoveredAt     time.Time        `json:"discoveredAt"`
	SuggestedMatches []SuggestedMatch `json:"suggestedMatches,omitempty"`
	// PartialSeries flags rows produced by the matcher's partial_series
	// outcome — the series identity resolved but the episode didn't, so
	// the matcher inbox needs to surface an episode picker. Default false
	// for the regular unmatched cases (low_confidence / ambiguous /
	// no_match).
	PartialSeries       bool       `json:"partialSeries,omitempty"`
	ResolvedAt          *time.Time `json:"resolvedAt,omitempty"`
	ResolvedMediaFileID *uuid.UUID `json:"resolvedMediaFileId,omitempty"`
}
