package model

import (
	"time"

	"github.com/google/uuid"
)

// FileState is the domain shape for a file_state row — the 1:1 sidecar of
// filesystem facts (presence, size, content hash, link facts, verified-at)
// kept apart from the cold identity row because it's rewritten on every
// scan/verify. Device, Inode and LinkCount are nil on a row no stat has
// filled them in for yet.
type FileState struct {
	FileID         uuid.UUID `json:"fileId"`
	Exists         bool      `json:"exists"`
	SizeBytes      *int64    `json:"sizeBytes,omitempty"`
	OsdbHash       *string   `json:"osdbHash,omitempty"`
	Device         *int64    `json:"device,omitempty"`
	Inode          *int64    `json:"inode,omitempty"`
	LinkCount      *int64    `json:"linkCount,omitempty"`
	LastVerifiedAt time.Time `json:"lastVerifiedAt"`
}

// LinkFacts are a file's st_dev, st_ino and st_nlink from one stat(). The
// kernel's unsigned values are carried bit-for-bit as int64 (the Postgres
// BIGINT width), so equality between two LinkFacts is exact.
type LinkFacts struct {
	Device    int64
	Inode     int64
	LinkCount int64
}
