package sse

import "github.com/google/uuid"

// RecipientKind enumerates the coarse delivery tags a producer can stamp on an
// event. The broker filters per-session against connect-time eligibility.
type RecipientKind string

const (
	// RecipientBroadcast delivers to every active session.
	RecipientBroadcast RecipientKind = "broadcast"
	// RecipientAdmins delivers to every session belonging to a current admin.
	RecipientAdmins RecipientKind = "admins"
	// RecipientUser delivers only to the named user's sessions.
	RecipientUser RecipientKind = "user"
)

// Recipient is the delivery tag on an event. UserID is meaningful only when
// Kind is RecipientUser. It lives in the sse package (not realtime) because
// it's a routing concept the broker filters against, and the broker can't
// import realtime (realtime imports sse).
type Recipient struct {
	Kind   RecipientKind
	UserID uuid.UUID
}

// Broadcast targets every active session.
var Broadcast = Recipient{Kind: RecipientBroadcast}

// Admins targets every session belonging to a current admin.
var Admins = Recipient{Kind: RecipientAdmins}

// User targets one specific user's sessions.
func User(id uuid.UUID) Recipient {
	return Recipient{Kind: RecipientUser, UserID: id}
}
