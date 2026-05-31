package sse

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// outboundDepth is the per-session outbound channel buffer. The realtime spec
// guesses 256 as a starting point for the worst case (a large download-job
// storm or a scan emitting per-file progress); this picks that value over the
// old 64 to absorb bursts before the overflow path trips.
const outboundDepth = 256

// Event is a single message published onto the in-process event bus.
// It maps 1:1 to an SSE event on the wire.
type Event struct {
	Type      string          // SSE event name
	Data      json.RawMessage // JSON payload
	ID        string          // optional SSE event id
	At        time.Time       // server timestamp
	Recipient Recipient       // delivery tag; the broker filters by it per session
}

// Session is one connected SSE stream. The broker holds a registry of these
// and filters every published event against each session's recipient
// eligibility and topic filter before delivery.
type Session struct {
	ID     uuid.UUID
	UserID uuid.UUID
	// topics is the connect-time `?type=` filter set. Empty means "all events"
	// — preserving the prior broker semantics where an unfiltered subscriber
	// saw everything.
	topics      map[string]bool
	out         chan Event
	connectedAt time.Time
}

// SubscribeParams carries the per-session attributes the handler resolves at
// connect: the authenticated user and the connect-time topic filter.
type SubscribeParams struct {
	UserID uuid.UUID
	Topics []string
}

type Broker struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*Session
}

func NewBroker() *Broker {
	return &Broker{sessions: make(map[uuid.UUID]*Session)}
}

// Subscribe registers a new session and returns it plus a cancel function that
// removes the session from the registry and closes its outbound channel. The
// cancel func is the only place the channel is closed — Publish never closes
// it, since a send on a closed channel panics under concurrent publishers.
func (b *Broker) Subscribe(params SubscribeParams) (*Session, func()) {
	topics := make(map[string]bool, len(params.Topics))
	for _, t := range params.Topics {
		if t != "" {
			topics[t] = true
		}
	}

	s := &Session{
		ID:          uuid.New(),
		UserID:      params.UserID,
		topics:      topics,
		out:         make(chan Event, outboundDepth),
		connectedAt: time.Now(),
	}

	b.mu.Lock()
	b.sessions[s.ID] = s
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if existing, ok := b.sessions[s.ID]; ok {
			delete(b.sessions, s.ID)
			close(existing.out)
		}
	}

	return s, cancel
}

// Events is the receive-only side of the session's outbound channel.
func (s *Session) Events() <-chan Event { return s.out }

// Publish delivers an event to every eligible session. Delivery to a session
// requires both:
//
//   - recipient match — Broadcast reaches every session; User(id) reaches only
//     sessions whose UserID == id; Admins reaches every session (see the admin
//     eligibility note below).
//   - topic match — the event's Type must pass the session's `?type=` filter
//     (an empty filter matches every type).
//
// The send is non-blocking: a full outbound channel logs the overflow and
// drops the event for that session. The connection stays up — session teardown
// on overflow is deferred to Phase 3, where replay makes a forced reconnect
// lossless.
func (b *Broker) Publish(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.sessions {
		if !recipientMatches(ev.Recipient, s) {
			continue
		}
		if !s.topicAllowed(ev.Type) {
			continue
		}
		select {
		case s.out <- ev:
		default:
			// Backpressure: the consumer can't keep up. Log loudly and drop the
			// event for this session only — a slow consumer must not stall
			// delivery to others. Teardown + lossless reconnect is Phase 3.
			//
			// TODO(phase3): replace the stderr line with the project logger
			// once the broker carries one, and tear the session down here (the
			// replay buffer makes the forced reconnect lossless).
			fmt.Fprintf(os.Stderr,
				"sse: outbound channel full, dropping event session=%s type=%s\n",
				s.ID, ev.Type)
		}
	}
}

// recipientMatches reports whether an event's recipient tag targets the given
// session, using each session's connect-time eligibility.
func recipientMatches(r Recipient, s *Session) bool {
	switch r.Kind {
	case RecipientBroadcast:
		return true
	case RecipientAdmins:
		// Single eligibility tier this phase: every authenticated session is
		// treated as admin-eligible. Real admin resolution (resolved once per
		// session at connect, then filtered by set-membership) lands in a
		// later phase; this is where that check goes.
		return true
	case RecipientUser:
		return s.UserID == r.UserID
	default:
		return false
	}
}

// topicAllowed reports whether the event type passes the session's connect-time
// `?type=` filter. An empty filter matches all types.
func (s *Session) topicAllowed(t string) bool {
	if len(s.topics) == 0 {
		return true
	}
	return s.topics[t]
}
