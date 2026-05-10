package model

import (
	"time"

	"github.com/google/uuid"
)

// MediaFileState is the domain shape for a media_file_state row. It mirrors
// the persistence-layer dbgen.MediaFileState but uses idiomatic Go types.
type MediaFileState struct {
	MediaFileID    uuid.UUID `json:"mediaFileId"`
	FileExists     bool      `json:"fileExists"`
	FileSize       *int64    `json:"fileSize,omitempty"`
	LastVerifiedAt time.Time `json:"lastVerifiedAt"`
}
