package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AuthProvider names the third-party provider that backs a user_identity
// row. Mirrors the auth_provider enum in the database; the persistence-layer
// equivalent is dbgen.AuthProvider, which the repo translates at the
// boundary so services and handlers stay free of dbgen.
type AuthProvider string

const (
	AuthProviderLocal AuthProvider = "local"
	AuthProviderPlex  AuthProvider = "plex"
)

// Identity is the domain shape for a user_identity row (third-party SSO link).
// It mirrors the persistence-layer dbgen.UserIdentity but uses idiomatic Go
// types (uuid.UUID, time.Time, json.RawMessage in place of []byte, *time.Time
// for the nullable token_expires_at column).
//
// Identity is consumed internally by AuthService for the Plex SSO flow; it
// isn't currently serialized to the wire, so JSON tags follow the same
// convention as the rest of the migrated entities.
type Identity struct {
	ID             uuid.UUID       `json:"id"`
	UserID         uuid.UUID       `json:"userId"`
	Provider       AuthProvider    `json:"provider"`
	Subject        string          `json:"subject"`
	Username       *string         `json:"username"`
	AccessToken    *string         `json:"accessToken"`
	RefreshToken   *string         `json:"refreshToken"`
	TokenExpiresAt *time.Time      `json:"tokenExpiresAt"`
	Raw            json.RawMessage `json:"raw"`
	CreatedAt      time.Time       `json:"createdAt"`
}
