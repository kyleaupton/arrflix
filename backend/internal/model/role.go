package model

import (
	"time"

	"github.com/google/uuid"
)

// Role is the domain shape for a role row. It mirrors the persistence-layer
// dbgen.Role but uses idiomatic Go types (uuid.UUID, time.Time) and lives
// outside the persistence boundary.
type Role struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	BuiltIn     bool      `json:"builtIn"`
	CreatedAt   time.Time `json:"createdAt"`
}

// The roles a user may be invited with, created with, or assigned. The seeded
// co_admin and viewer roles still exist in the catalog but are never offered.
const (
	RoleRequester = "requester"
	RoleAdmin     = "admin"
)

// IsAssignableRole reports whether name is a role a user may hold.
func IsAssignableRole(name string) bool {
	return name == RoleRequester || name == RoleAdmin
}
