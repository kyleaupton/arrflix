package model

import (
	"time"

	"github.com/google/uuid"
)

// FileOrigin is the domain shape for a file_origin row — the cold, insert-once
// provenance sidecar of `file` (1:1 on FileID, mirroring FileState). It captures,
// at the moment a file's bytes first enter the library, the source name its
// quality/release tokens derive from, plus a materialized parse for querying and
// audit — independent of download_job lifecycle.
//
// Origin discriminates how the bytes entered ('grab', 'scan', 'manual'), which
// is what tells a consumer what kind of SourceTitle (if any) exists. SourceTitle
// is the source of truth for re-rendering under a changed name template
// (re-parsing picks up parser improvements); the bin_*/Parsed cache is for
// querying and parser-drift audit, never trusted by the renderer. Nullable
// fields are absent for the legacy/unknown case. MediaInfo is intentionally
// excluded (re-gatherable). See [FileState] for the hot-facts counterpart.
type FileOrigin struct {
	FileID        uuid.UUID  `json:"fileId"`
	Origin        string     `json:"origin"`
	SourceTitle   *string    `json:"sourceTitle,omitempty"`
	BinSource     *string    `json:"binSource,omitempty"`
	BinResolution *string    `json:"binResolution,omitempty"`
	BinModifier   *string    `json:"binModifier,omitempty"`
	ReleaseGroup  *string    `json:"releaseGroup,omitempty"`
	Edition       *string    `json:"edition,omitempty"`
	Parsed        []byte     `json:"parsed,omitempty" swaggertype:"object"`
	IndexerID     *int64     `json:"indexerId,omitempty"`
	Guid          *string    `json:"guid,omitempty"`
	DownloadJobID *uuid.UUID `json:"downloadJobId,omitempty"`
	CapturedAt    time.Time  `json:"capturedAt"`
}
