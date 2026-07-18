package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EmailProvider is the domain shape for the singleton email_provider row.
//
// Password is present for internal transport-build use only. Unlike Downloader
// (whose password rides the wire), email is write-only: the HTTP response DTO
// omits the secret entirely. The json:"-" tag here is defense-in-depth — the
// handler never serializes model.EmailProvider directly, it maps to a DTO.
type EmailProvider struct {
	ID            uuid.UUID       `json:"id"`
	Provider      string          `json:"provider"`
	FromAddress   string          `json:"fromAddress"`
	FromName      *string         `json:"fromName"`
	ReplyTo       *string         `json:"replyTo"`
	Host          *string         `json:"host"`
	Port          *int            `json:"port"`
	Security      *string         `json:"security"`
	Auth          bool            `json:"auth"`
	Username      *string         `json:"username"`
	Password      *string         `json:"-"`
	SkipTLSVerify bool            `json:"skipTlsVerify"`
	ConfigJSON    json.RawMessage `json:"configJson" swaggertype:"object"`
	Enabled       bool            `json:"enabled"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}
