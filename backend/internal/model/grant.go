package model

import (
	"time"

	"github.com/google/uuid"
)

// GrantSubject names what a permission grant is attached to — a role (grant
// applies to every member) or a single user. Mirrors the grant_subject enum in
// the database; the persistence-layer equivalent is dbgen.GrantSubject, which
// the repo casts across at the boundary.
type GrantSubject string

// GrantEffect is a grant's polarity. Deny wins over allow during resolution.
// Mirrors the grant_effect enum; dbgen.GrantEffect is the persistence twin.
type GrantEffect string

const (
	GrantSubjectRole GrantSubject = "role"
	GrantSubjectUser GrantSubject = "user"
	GrantEffectAllow GrantEffect  = "allow"
	GrantEffectDeny  GrantEffect  = "deny"
)

// Grant is the domain shape for a permission_grant row. It mirrors the
// persistence-layer dbgen.PermissionGrant but uses idiomatic Go types
// (uuid.UUID, time.Time) and pointers for the nullable resource scope. A grant
// with nil ResourceType/ResourceID is global (matches any resource); a scoped
// grant matches only its exact (type, id).
type Grant struct {
	ID            uuid.UUID    `json:"id"`
	SubjectType   GrantSubject `json:"subjectType"`
	SubjectID     uuid.UUID    `json:"subjectId"`
	PermissionKey string       `json:"permissionKey"`
	ResourceType  *string      `json:"resourceType"`
	ResourceID    *uuid.UUID   `json:"resourceId"`
	Effect        GrantEffect  `json:"effect"`
	CreatedAt     time.Time    `json:"createdAt"`
}
