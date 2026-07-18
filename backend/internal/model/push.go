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
// comment for why it need not be a working inbox. It must name a routable
// domain even so: Apple 403s a JWT whose sub is something like admin@host.local,
// taking down iOS push while Chrome — which does not validate sub — stays green.
type VAPIDConfig struct {
	ID         uuid.UUID `json:"id"`
	PublicKey  string    `json:"publicKey"`
	PrivateKey string    `json:"-"`
	Subject    string    `json:"subject"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// PushSubscription is one browser's Web Push registration, bound to the session
// (login) it was created under — a session holds at most one. Owner is derived via
// session_id -> user_session.user_id. Endpoint is the push service's delivery URL
// and the natural key; P256dh/Auth are the client's ECDH public key and auth
// secret the adapter encrypts each payload with.
// LastUsedAt is when the browser last (re)subscribed/refreshed this registration;
// LastNotifiedAt is when the push adapter last successfully delivered to it, nil
// until the first push. The devices UI shows "last notified" from the latter — the
// former overstated activity for a device that had only ever subscribed.
type PushSubscription struct {
	ID             uuid.UUID  `json:"id"`
	SessionID      uuid.UUID  `json:"sessionId"`
	Endpoint       string     `json:"endpoint"`
	P256dh         string     `json:"p256dh"`
	Auth           string     `json:"auth"`
	UserAgent      *string    `json:"userAgent"`
	CreatedAt      time.Time  `json:"createdAt"`
	LastUsedAt     time.Time  `json:"lastUsedAt"`
	LastNotifiedAt *time.Time `json:"lastNotifiedAt"`
}
