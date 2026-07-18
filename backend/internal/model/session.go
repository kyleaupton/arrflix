package model

import (
	"time"

	"github.com/google/uuid"
)

// Session is the domain shape for a user_session row — the first-class record of
// an authenticated device/login. The short-lived access JWT is a stateless
// projection of it; this row is the source of truth for authentication.
//
// RefreshHash/PrevRefreshHash are sha256(refresh secret) and never leave the
// server — like VAPIDConfig.PrivateKey they are json:"-" so they cannot reach the
// wire even by accident. The handler-facing sessions view (a later phase) maps to
// a DTO that omits them entirely.
type Session struct {
	ID               uuid.UUID  `json:"id"`
	UserID           uuid.UUID  `json:"userId"`
	RefreshHash      []byte     `json:"-"`
	PrevRefreshHash  []byte     `json:"-"`
	RotatedAt        *time.Time `json:"-"`
	RefreshExpiresAt time.Time  `json:"refreshExpiresAt"`
	CreatedAt        time.Time  `json:"createdAt"`
	LastUsedAt       time.Time  `json:"lastUsedAt"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	UserAgent        *string    `json:"userAgent"`
	IP               *string    `json:"ip,omitempty"`
	Label            *string    `json:"label"`
}
