// Package realtime is the typed producer API for ephemeral server→browser
// events delivered over SSE. Each event type has a constructor (grouped by
// producing domain in events_*.go) that returns an Event carrying its wire
// name, recipient tag, and a marshaled typed payload. Emit publishes the
// Event onto the in-process broker; the owned SSE writer in
// internal/http/handlers maps it straight to the wire.
//
// Events are lossy by design — a slow subscriber misses events rather than
// backpressuring the broker. Durable, multi-channel delivery is the
// notifications system's job, not this package's.
package realtime

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// RecipientKind enumerates the coarse delivery tags a producer can stamp on
// an event. The broker filters per-session against connect-time eligibility;
// this phase carries the tag but does not yet filter (every event fans out to
// every subscriber — per-user scoping is a later phase).
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
// Kind is RecipientUser.
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

// Event is a single realtime message. Name is the wire `event:` line;
// Recipient is the delivery tag (carried, not yet filtered); Data is the
// pre-marshaled JSON payload written verbatim as the `data:` line; ID is the
// sortable wire `id:` line used for Last-Event-ID resume.
type Event struct {
	Name      string
	Recipient Recipient
	Data      json.RawMessage
	ID        string
}

// Emit stamps a sortable ID if absent and publishes the event onto the
// broker. The ID is a UUIDv7 — time-ordered and lexicographically sortable,
// so it satisfies the resume contract's "sortable, unique string" without a
// new dependency.
//
// ctx is unused this phase; it is retained for the recipient-resolution path
// a later phase wires in. The Recipient field is likewise carried but not yet
// honored by the broker.
func Emit(_ context.Context, broker *sse.Broker, e Event) {
	if broker == nil {
		return
	}
	if e.ID == "" {
		if v7, err := uuid.NewV7(); err == nil {
			e.ID = v7.String()
		} else {
			// NewV7 only errors if the system RNG fails; fall back to v4 so
			// the event still carries a unique (if non-sortable) id rather
			// than going out with an empty id line.
			e.ID = uuid.NewString()
		}
	}
	broker.Publish(sse.Event{
		Type: e.Name,
		Data: e.Data,
		ID:   e.ID,
	})
}

// mustMarshal marshals a payload that is statically known to be serializable
// (plain structs / model types with no custom MarshalJSON that can fail). A
// marshal failure here is a programmer error in the payload type, not a
// runtime condition; emitting an explicit JSON null keeps the wire valid
// rather than silently dropping the field.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
