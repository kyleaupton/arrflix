package model

import (
	"time"

	"github.com/google/uuid"
)

// UserPolicy is the domain shape for a user_policy row — the per-user approval
// seam. Minimal in the movie-only PoC: a single auto-approve flag. Mirrors
// dbgen.UserPolicy.
type UserPolicy struct {
	UserID           uuid.UUID `json:"userId"`
	AutoApproveMovie bool      `json:"autoApproveMovie"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
