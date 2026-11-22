package model

import (
	"time"

	"github.com/google/uuid"
)

// Volume is one storage location — one filesystem, one hardlink boundary —
// that Arrflix and its downloaders may each reach at a different path.
// ArrflixBase is the canonical absolute path as Arrflix sees it.
type Volume struct {
	ID          uuid.UUID     `json:"id"`
	Name        string        `json:"name"`
	ArrflixBase string        `json:"arrflixBase"`
	Aliases     []VolumeAlias `json:"aliases"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

// VolumeAlias is the path at which one downloader sees a volume's storage.
// Flavor is reserved for non-POSIX path semantics and is always "posix".
type VolumeAlias struct {
	DownloaderID uuid.UUID `json:"downloaderId"`
	Base         string    `json:"base"`
	Flavor       string    `json:"flavor"`
}
