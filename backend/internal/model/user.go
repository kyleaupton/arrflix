package model

import (
	"time"

	"github.com/google/uuid"
)

// User is the domain shape for an app_user row, with composed roles.
//
// The persistence layer has three sqlc shapes that all collapse here:
//   - dbgen.AppUser       — the bare row, no roles aggregated.
//   - dbgen.GetUserRow    — the row plus a json_agg roles aggregate.
//   - dbgen.ListUsersRow  — same as GetUserRow but for list queries.
//
// All three feed translators in repo/users.go (toModelUser /
// toModelUserFromGetRow / toModelUserFromListRow) that produce this single
// model type.
//
// PasswordHash is intentionally tagged json:"-" so it never crosses the wire,
// even though the service layer needs to read it to verify credentials.
type User struct {
	ID        uuid.UUID `json:"id"`
	Email     *string   `json:"email"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatarUrl,omitempty"`
	IsActive  bool      `json:"isActive"`
	Roles     []Role    `json:"roles"`
	// AutoApprove and Allow4k are the user's request settings, read back from
	// their user-subject grants (see RequestSettings). They have no effect while
	// the user's role already grants everything.
	AutoApprove  bool      `json:"autoApprove"`
	Allow4k      bool      `json:"allow4k"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	PasswordHash *string   `json:"-"`
}

// RequestSettings are the per-user request settings an admin sets. They are
// stored only as user-subject grants: authz.RequestSettingsGrants derives the
// grant set, and the user's DTO reads them back from those grants.
type RequestSettings struct {
	// AutoApprove lets the user's requests skip the approval queue at every
	// tier they may request.
	AutoApprove bool
	// Allow4k lets the user request 4K versions.
	Allow4k bool
}
