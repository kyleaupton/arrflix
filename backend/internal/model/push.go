package model

import (
	"time"

	"github.com/google/uuid"
)

// VAPIDConfig is the domain shape for the singleton vapid_config row — the
// server's Web Push signing identity.
//
// PrivateKey is the ES256 signing secret, present for internal request-signing
// only. Like EmailProvider.Password it is write-only on the wire: the json:"-"
// tag is defense-in-depth (handlers map to a DTO that omits it). PublicKey is
// safe to expose — the browser needs it as applicationServerKey to subscribe.
//
// Subject is the VAPID `sub` contact URI (mailto:/https:); see the migration
// comment for why it need not be a working inbox.
type VAPIDConfig struct {
	ID         uuid.UUID `json:"id"`
	PublicKey  string    `json:"publicKey"`
	PrivateKey string    `json:"-"`
	Subject    string    `json:"subject"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// PushSubscription is one browser's Web Push registration for one user. A user
// may hold several (one per device). Endpoint is the push service's delivery URL
// and the natural key; P256dh/Auth are the client's ECDH public key and auth
// secret the adapter encrypts each payload with.
type PushSubscription struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"userId"`
	Endpoint   string    `json:"endpoint"`
	P256dh     string    `json:"p256dh"`
	Auth       string    `json:"auth"`
	UserAgent  *string   `json:"userAgent"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}
