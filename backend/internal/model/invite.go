package model

import (
	"time"

	"github.com/google/uuid"
)

// Invite is the domain shape for a user_invite row. It mirrors the
// persistence-layer dbgen.UserInvite but uses idiomatic Go types
// (uuid.UUID, time.Time, *time.Time for nullable claimed_at).
type Invite struct {
	ID        uuid.UUID  `json:"id"`
	Email     string     `json:"email"`
	InvitedBy uuid.UUID  `json:"invitedBy"`
	CreatedAt time.Time  `json:"createdAt"`
	ClaimedAt *time.Time `json:"claimedAt"`
}
