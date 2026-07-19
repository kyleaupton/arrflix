package model

import (
	"time"

	"github.com/google/uuid"
)

// Invite is the domain shape for a user_invite row. It mirrors the
// persistence-layer dbgen.UserInvite but uses idiomatic Go types
// (uuid.UUID, time.Time, *time.Time for nullable claimed_at/expires_at).
//
// The token hash is deliberately absent: it's a secret that never leaves the
// database, and the raw token is returned once (out of band) at creation time.
type Invite struct {
	ID        uuid.UUID  `json:"id"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	InvitedBy uuid.UUID  `json:"invitedBy"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt"`
	ClaimedAt *time.Time `json:"claimedAt"`
}
