package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Downloader is the domain shape for a downloader row.
//
// Password is on the wire (not redacted) — the FE settings UI re-displays
// it on edit. Tightening this would need a parallel "set new password"
// flow on the FE before the field can be redacted on read.
type Downloader struct {
	ID         uuid.UUID       `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Protocol   string          `json:"protocol"`
	URL        string          `json:"url"`
	Username   *string         `json:"username"`
	Password   *string         `json:"password"`
	ConfigJSON json.RawMessage `json:"configJson" swaggertype:"object"`
	Enabled    bool            `json:"enabled"`
	Default    bool            `json:"default"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// DownloaderWithStatus is the list-endpoint shape: Downloader embedded
// alongside a runtime Initialized flag indicating whether the manager
// currently has an initialized client for this downloader.
type DownloaderWithStatus struct {
	Downloader
	Initialized bool `json:"initialized"`
}
