package model

import (
	"time"

	"github.com/google/uuid"
)

// ExclusionReason is the closed vocabulary of why a release is excluded for a
// want. The DB stores it as TEXT + CHECK; these typed consts give services and
// tests safe names. Only download-failure recovery produces exclusions today;
// 'declined' (propose-decline) and 'regate_failed' (re-gate) join later.
type ExclusionReason string

const (
	ExclusionDownloadFailed ExclusionReason = "download_failed"
)

// WantReleaseExclusion is the domain shape for a want_release_exclusion row —
// one release the acquisition front-half must not re-pick for a want. A release
// is identified by (IndexerID, GUID), the canonical release identity upstream.
// No JSON tags: this carries no API surface. Mirrors dbgen.WantReleaseExclusion.
type WantReleaseExclusion struct {
	WantID    uuid.UUID
	IndexerID int64
	GUID      string
	Reason    string
	Detail    *string
	CreatedAt time.Time
}
