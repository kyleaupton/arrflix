package sse

import "time"

// ring is a per-session replay buffer of recent Events, bounded by both a
// maximum length and a maximum age. It backs SSE Last-Event-ID resume: a
// reconnecting client replays the events it missed, or is told to refetch when
// its resume point has aged out.
//
// Events are stored in append order, which is also chronological order: publish
// order is monotonic and IDs are UUIDv7 (time-ordered), so a lexicographic
// compare of Event.ID matches chronological order.
//
// ring is not safe for concurrent use. The owning session holds its own lock
// across calls to append/since; ring carries no mutex of its own.
type ring struct {
	events []Event
	maxLen int
	maxAge time.Duration
	now    func() time.Time
}

// newRing builds an empty ring capped by maxLen entries and maxAge of
// retention. now supplies the reference time for age eviction; if nil it
// defaults to time.Now.
func newRing(maxLen int, maxAge time.Duration, now func() time.Time) *ring {
	if now == nil {
		now = time.Now
	}
	return &ring{
		maxLen: maxLen,
		maxAge: maxAge,
		now:    now,
	}
}

// append adds ev to the buffer and evicts entries that fall outside the age
// window or the length cap. A just-appended event is never evicted by age: a
// zero ev.At is treated as "now".
func (r *ring) append(ev Event) {
	r.events = append(r.events, ev)
	r.evict()
}

// evict drops entries older than maxAge relative to now(), then trims the
// oldest entries beyond maxLen. Eviction is from the front (oldest) since the
// buffer is append-ordered.
func (r *ring) evict() {
	if r.maxAge > 0 {
		cutoff := r.now().Add(-r.maxAge)
		drop := 0
		for _, ev := range r.events {
			// A zero At means "just added" — count it as current, never stale.
			if !ev.At.IsZero() && ev.At.Before(cutoff) {
				drop++
				continue
			}
			break
		}
		if drop > 0 {
			r.events = r.events[drop:]
		}
	}

	if r.maxLen > 0 && len(r.events) > r.maxLen {
		r.events = r.events[len(r.events)-r.maxLen:]
	}
}

// since returns the buffered events the client hasn't seen given its last
// received id, and whether a gap was detected (the resume point has been
// evicted, so the client must refetch rather than replay).
//
// Semantics:
//   - lastID == "" → (nil, false): no resume point, caller starts fresh.
//   - buffer empty → (nil, false): no evidence of loss.
//   - lastID older than the oldest retained id → (nil, true): resume point
//     evicted, signal a gap.
//   - otherwise → events with ID > lastID in order, false (an empty slice when
//     the client is already caught up).
func (r *ring) since(lastID string) (events []Event, gapped bool) {
	if lastID == "" || len(r.events) == 0 {
		return nil, false
	}

	if lastID < r.events[0].ID {
		return nil, true
	}

	for i := range r.events {
		if r.events[i].ID > lastID {
			return r.events[i:], false
		}
	}
	return nil, false
}

// len reports the number of buffered events.
func (r *ring) len() int {
	return len(r.events)
}

// snapshot returns a copy of the current buffered events in order. Exposed for
// tests; callers must not mutate the result's elements.
func (r *ring) snapshot() []Event {
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}
