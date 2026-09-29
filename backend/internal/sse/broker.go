package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// outboundDepth is the per-attachment outbound channel buffer. 256 absorbs
	// bursts (a download-job storm, per-file scan progress) before the overflow
	// path trips a lossless reconnect.
	outboundDepth = 256

	// replayMaxLen / replayMaxAge bound each session's replay ring. A
	// reconnecting client resumes from its Last-Event-ID within this window;
	// beyond it the broker signals a gap and the client refetches.
	replayMaxLen = 200
	replayMaxAge = 5 * time.Minute

	// detachTTL is how long a detached session is kept (with its ring) so a
	// reconnecting client can reattach and replay. sweepInterval is how often
	// the sweeper looks for sessions past the TTL.
	detachTTL     = 5 * time.Minute
	sweepInterval = 1 * time.Minute
)

// Event is a single message published onto the in-process event bus.
// It maps 1:1 to an SSE event on the wire.
type Event struct {
	Type      string          // SSE event name
	Data      json.RawMessage // JSON payload
	ID        string          // sortable SSE event id (UUIDv7); drives Last-Event-ID resume
	At        time.Time       // server timestamp; used for ring age eviction
	Recipient Recipient       // delivery tag; the broker filters by it per session

	// Topic, when non-empty, makes the event opt-in: only sessions that have
	// subscribed to this exact topic receive it. An empty Topic means the event
	// is delivered to every eligible session, whatever it has subscribed to.
	//
	// This is what keeps subscribing safe. A session's topic set is purely an
	// opt-in list for page-scoped, high-cardinality streams; it can never
	// blackhole the events a session gets by default.
	Topic string
}

// Session is one logical SSE subscription. It outlives a single TCP connection:
// when a connection drops the session is detached (its outbound channel closed)
// but retained — with its replay ring — so a reconnecting client can reattach
// via ?session=<id> and replay the events it missed. The sweeper evicts
// sessions that stay detached past detachTTL.
type Session struct {
	ID     uuid.UUID
	UserID uuid.UUID

	// mu guards every mutable field below. Lock order is broker.mu before
	// session.mu, never the reverse.
	mu sync.Mutex
	// topics is the session's opt-in subscription set, consulted only for
	// events that carry a Topic. It never restricts default-delivery events.
	topics map[string]bool
	// capabilities is the permission keyspace this session is eligible for,
	// resolved once per attach. Refreshed on reattach so a grant change lands
	// at the next reconnect rather than never.
	capabilities map[string]bool
	replay       *ring
	// out is the current attachment's outbound channel; nil while detached.
	out chan Event
	// kick signals the attached handler to tear down (overflow → lossless
	// reconnect). Buffered size 1; a non-blocking send coalesces repeats.
	kick chan struct{}
	// detachedAt is nil while attached, else the time the connection dropped.
	detachedAt *time.Time
	// epoch increments on every (re)attach. The cancel func captures its
	// epoch; Detach is a no-op if the session has since been reattached, so a
	// stale handler's teardown can't kill a fresh connection.
	epoch       uint64
	connectedAt time.Time
}

// AttachParams carries what the handler resolves before attaching: an optional
// prior session to reattach to, the authenticated user, the client's resume
// point, any opt-in topics, and the user's resolved capability set.
type AttachParams struct {
	// SessionID, when non-zero, requests reattach to an existing session.
	SessionID uuid.UUID
	UserID    uuid.UUID
	// LastEventID is the client's resume point within the reattached session's
	// ring. Ignored on a fresh attach.
	LastEventID string
	Topics      []string
	// Capabilities is the permission keyspace the handler resolved for this
	// user. Nil means no capability-targeted event reaches this session.
	Capabilities []string
}

// Attachment is what Attach hands back to the handler. Out and Kick are the
// channels for this specific attachment, captured here so the handler never
// re-reads them off the Session (which a concurrent reattach could swap).
type Attachment struct {
	Session *Session
	Out     <-chan Event
	Kick    <-chan struct{}
	// Replay is the ordered set of buffered events with id > LastEventID, to be
	// sent before going live. Nil on a fresh attach or when Gapped.
	Replay []Event
	// Gapped is true when LastEventID predates the ring's oldest entry: the
	// resume point aged out, so the client must refetch rather than replay.
	Gapped bool
	// Cancel detaches the session (keeping it for reattach); the handler defers
	// it.
	Cancel func()
}

type Broker struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*Session
	now      func() time.Time
}

// NewBroker builds a broker and starts its sweeper goroutine, which evicts
// detached sessions past their TTL until ctx is cancelled.
func NewBroker(ctx context.Context) *Broker {
	b := newBroker(time.Now)
	go b.runSweeper(ctx)
	return b
}

// newBroker builds a broker without starting the sweeper. Tests use it to drive
// the clock and call sweep() deterministically.
func newBroker(now func() time.Time) *Broker {
	if now == nil {
		now = time.Now
	}
	return &Broker{
		sessions: make(map[uuid.UUID]*Session),
		now:      now,
	}
}

// Attach either reattaches to an existing session (replaying missed events) or
// allocates a fresh one. The returned Cancel func detaches the session; the
// handler defers it.
func (b *Broker) Attach(params AttachParams) Attachment {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Reattach: a known session id, owned by the same user. A user mismatch is
	// treated as "not found" — never reattach across users.
	if params.SessionID != uuid.Nil {
		if s, ok := b.sessions[params.SessionID]; ok && s.UserID == params.UserID {
			return b.reattach(s, params)
		}
	}

	return b.fresh(params)
}

// reattach revives a retained session for a new connection: a fresh outbound
// channel + kick, detachedAt cleared, replay computed from the ring. The caller
// holds broker.mu.
func (b *Broker) reattach(s *Session, params AttachParams) Attachment {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If somehow still attached (a second concurrent connection for the same
	// session id), close the prior outbound channel so its handler exits. That
	// handler's deferred Detach is a no-op once we bump the epoch below.
	if s.out != nil {
		close(s.out)
	}

	s.epoch++
	s.out = make(chan Event, outboundDepth)
	s.kick = make(chan struct{}, 1)
	s.detachedAt = nil
	// Re-resolved by the handler on every connect, so a promotion or revocation
	// takes effect here rather than persisting for the session's whole life.
	s.capabilities = stringSet(params.Capabilities)

	replay, gapped := s.replay.since(params.LastEventID)

	return Attachment{
		Session: s,
		Out:     s.out,
		Kick:    s.kick,
		Replay:  replay,
		Gapped:  gapped,
		Cancel:  b.cancelFunc(s, s.epoch),
	}
}

// fresh allocates a new session. The caller holds broker.mu.
func (b *Broker) fresh(params AttachParams) Attachment {
	s := &Session{
		ID:           uuid.New(),
		UserID:       params.UserID,
		topics:       stringSet(params.Topics),
		capabilities: stringSet(params.Capabilities),
		replay:       newRing(replayMaxLen, replayMaxAge, b.now),
		out:          make(chan Event, outboundDepth),
		kick:         make(chan struct{}, 1),
		epoch:        1,
		connectedAt:  b.now(),
	}
	b.sessions[s.ID] = s

	return Attachment{
		Session: s,
		Out:     s.out,
		Kick:    s.kick,
		Cancel:  b.cancelFunc(s, s.epoch),
	}
}

// cancelFunc returns the teardown closure for one attachment. It detaches only
// if the session is still on the epoch it was attached at — a stale handler
// (whose connection was already replaced by a reattach) tears down nothing.
func (b *Broker) cancelFunc(s *Session, epoch uint64) func() {
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.epoch != epoch {
			return
		}
		if s.out != nil {
			close(s.out)
			s.out = nil
		}
		t := b.now()
		s.detachedAt = &t
	}
}

// Publish delivers an event to every session matching its recipient and topic
// filter. For each, the event is appended to the replay ring unconditionally —
// even while detached, so a briefly-gone client accumulates events to replay on
// reconnect — then, if attached, sent non-blocking. A full channel kicks the
// handler to tear down (events are safe in the ring) rather than dropping or
// blocking.
func (b *Broker) Publish(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.sessions {
		s.mu.Lock()
		if !s.eligible(ev) {
			s.mu.Unlock()
			continue
		}

		s.replay.append(ev)

		if s.out != nil {
			select {
			case s.out <- ev:
			default:
				// Backpressure: the consumer can't keep up. The event is already
				// in the ring, so signal the handler to disconnect — the client
				// reconnects and replays. Never drop, never block, never close
				// the channel here (a concurrent Publish could send on it).
				select {
				case s.kick <- struct{}{}:
				default:
				}
				fmt.Fprintf(os.Stderr,
					"sse: outbound channel full, kicking session=%s type=%s\n",
					s.ID, ev.Type)
			}
		}
		s.mu.Unlock()
	}
}

// sweep evicts sessions that have stayed detached past detachTTL. Called on a
// ticker by runSweeper and directly by tests.
func (b *Broker) sweep() {
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := b.now().Add(-detachTTL)
	for id, s := range b.sessions {
		s.mu.Lock()
		stale := s.detachedAt != nil && s.detachedAt.Before(cutoff)
		s.mu.Unlock()
		if stale {
			delete(b.sessions, id)
		}
	}
}

func (b *Broker) runSweeper(ctx context.Context) {
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.sweep()
		}
	}
}

// ownedSession returns the session by id only if it exists AND belongs to
// userID. A missing or foreign session yields (nil, false) — callers map both
// to a 404 so session existence never leaks across users.
func (b *Broker) ownedSession(sessionID, userID uuid.UUID) (*Session, bool) {
	b.mu.RLock()
	s, ok := b.sessions[sessionID]
	b.mu.RUnlock()
	if !ok || s.UserID != userID {
		return nil, false
	}
	return s, true
}

// Topics returns the session's current opt-in subscriptions (sorted) if the
// session exists and is owned by userID. The bool is false for a
// missing/foreign session. An empty slice means no opt-in streams are
// subscribed — not that nothing is delivered.
func (b *Broker) Topics(sessionID, userID uuid.UUID) ([]string, bool) {
	s, ok := b.ownedSession(sessionID, userID)
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.topics))
	for t := range s.topics {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, true
}

// AddTopics adds topics to the session's opt-in set. Returns false for a
// missing/foreign session.
//
// Subscribing is purely additive: it admits events that carry a matching
// Event.Topic and has no effect on the default-delivery events the session
// already receives. A page can subscribe to its own scoped stream on mount and
// drop it on navigate without disturbing anything else on the connection.
func (b *Broker) AddTopics(sessionID, userID uuid.UUID, topics []string) bool {
	s, ok := b.ownedSession(sessionID, userID)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range topics {
		if t != "" {
			s.topics[t] = true
		}
	}
	return true
}

// RemoveTopic drops a topic from the session's filter. Removing a topic the
// session never held is a no-op. Returns false for a missing/foreign session.
func (b *Broker) RemoveTopic(sessionID, userID uuid.UUID, topic string) bool {
	s, ok := b.ownedSession(sessionID, userID)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.topics, topic)
	return true
}

// eligible reports whether this session should receive the event: the recipient
// tag must target it, and an opt-in event must have been subscribed to. Caller
// holds session.mu.
func (s *Session) eligible(ev Event) bool {
	if !s.recipientMatches(ev.Recipient) {
		return false
	}
	if ev.Topic == "" {
		return true
	}
	return s.topics[ev.Topic]
}

// recipientMatches reports whether an event's recipient tag targets this
// session, against its connect-time eligibility. Caller holds session.mu.
//
// Unrecognized kinds — including the zero Recipient — match nothing. An event
// that forgot its recipient reaches no one, which surfaces as a missing update
// rather than as a silent disclosure.
func (s *Session) recipientMatches(r Recipient) bool {
	switch r.Kind {
	case RecipientBroadcast:
		return true
	case RecipientCapability:
		return s.capabilities[r.Capability]
	case RecipientUser:
		return s.UserID == r.UserID
	default:
		return false
	}
}

// stringSet builds a lookup set, dropping empties.
func stringSet(vals []string) map[string]bool {
	set := make(map[string]bool, len(vals))
	for _, v := range vals {
		if v != "" {
			set[v] = true
		}
	}
	return set
}
