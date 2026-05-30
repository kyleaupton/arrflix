package model

import (
	"time"

	"github.com/google/uuid"
)

// FileState is the domain shape for a file_state row — the 1:1 sidecar of
// filesystem facts (presence, size, content hash, verified-at) kept apart
// from the cold identity row because it's rewritten on every scan/verify.
type FileState struct {
	FileID         uuid.UUID `json:"fileId"`
	Exists         bool      `json:"exists"`
	SizeBytes      *int64    `json:"sizeBytes,omitempty"`
	OsdbHash       *string   `json:"osdbHash,omitempty"`
	LastVerifiedAt time.Time `json:"lastVerifiedAt"`
}
