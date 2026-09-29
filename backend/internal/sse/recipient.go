package sse

import "github.com/google/uuid"

// RecipientKind enumerates the coarse delivery tags a producer can stamp on an
// event. The broker filters per-session against connect-time eligibility.
type RecipientKind string

const (
	// RecipientBroadcast delivers to every active session.
	RecipientBroadcast RecipientKind = "broadcast"
	// RecipientCapability delivers to sessions whose user holds a permission
	// key, resolved once when the session attaches.
	RecipientCapability RecipientKind = "capability"
	// RecipientUser delivers only to the named user's sessions.
	RecipientUser RecipientKind = "user"
)

// Recipient is the delivery tag on an event. UserID is meaningful only when
// Kind is RecipientUser, Capability only when Kind is RecipientCapability. It
// lives in the sse package (not realtime) because it's a routing concept the
// broker filters against, and the broker can't import realtime (realtime
// imports sse).
//
// The zero value targets nobody. Delivery fails closed: an event whose
// recipient was never set reaches no session rather than every session.
type Recipient struct {
	Kind       RecipientKind
	UserID     uuid.UUID
	Capability string
}

// Broadcast targets every active session.
var Broadcast = Recipient{Kind: RecipientBroadcast}

// User targets one specific user's sessions.
func User(id uuid.UUID) Recipient {
	return Recipient{Kind: RecipientUser, UserID: id}
}

// Capability targets sessions whose user holds the given permission key (an
// authz key such as "jobs.read"). The key is matched against the session's
// connect-time capability set, so the broker never touches the permission
// system on the emit path — the cost is bounded staleness, since a grant
// change reaches a session only on its next attach.
//
// Capability keys are matched literally, so this suits the flat keyspace.
// Resource-scoped eligibility ("library.read on library 42") needs the scope
// qualifier on the topic, not the recipient.
func Capability(key string) Recipient {
	return Recipient{Kind: RecipientCapability, Capability: key}
}
