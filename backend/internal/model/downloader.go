package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Downloader is the domain shape for a downloader row. It mirrors the
// persistence-layer dbgen.Downloader but uses idiomatic Go types
// (uuid.UUID, time.Time, json.RawMessage in place of the raw []byte
// config_json column).
//
// Password redaction note: the prior wire shape was an ad-hoc map produced
// by downloaderToMap which included the raw password field on every
// response. We preserve that wire shape exactly (no redaction change) so
// this migration is byte-identical at the JSON layer; tightening the
// redaction is a separate concern tracked outside the model migration.
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

// DownloaderWithStatus is the list-endpoint shape: a Downloader plus a
// runtime Initialized flag derived from the downloader Manager. The flag
// reports whether the manager currently has an initialized client for this
// downloader (and the downloader is enabled). Embedding Downloader keeps
// the wire shape flat — `initialized` sits alongside the other fields,
// matching the prior downloaderToMap output exactly.
type DownloaderWithStatus struct {
	Downloader
	Initialized bool `json:"initialized"`
}
