package sse

import (
	"testing"
	"time"
)

// Tests use synthetic, lexicographically-ordered ids ("id-001", "id-002", …).
// Real UUIDv7s sort the same way (time-ordered), so synthetic ids exercise the
// exact string-compare the resume query relies on while keeping assertions
// readable.

// fakeClock is a mutable time source for deterministic age-eviction tests.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func ids(events []Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.ID
	}
	return out
}

func equalIDs(a []string, b ...string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRingAppendCountEviction(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(3, 0, clock.now)

	for _, id := range []string{"id-001", "id-002", "id-003", "id-004"} {
		r.append(Event{ID: id, At: clock.now()})
	}

	if r.len() != 3 {
		t.Fatalf("len = %d, want 3", r.len())
	}
	if got := ids(r.snapshot()); !equalIDs(got, "id-002", "id-003", "id-004") {
		t.Fatalf("contents = %v, want oldest dropped", got)
	}
}

func TestRingAppendAgeEviction(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(100, 5*time.Minute, clock.now)

	// Two stale events at t0.
	r.append(Event{ID: "id-001", At: clock.now()})
	r.append(Event{ID: "id-002", At: clock.now()})

	// Advance past the age window, then append a fresh event. The next append
	// is what triggers eviction of the now-stale entries.
	clock.advance(6 * time.Minute)
	r.append(Event{ID: "id-003", At: clock.now()})

	if got := ids(r.snapshot()); !equalIDs(got, "id-003") {
		t.Fatalf("contents = %v, want only fresh id-003", got)
	}

	// A second fresh event within the window survives alongside id-003.
	clock.advance(time.Minute)
	r.append(Event{ID: "id-004", At: clock.now()})
	if got := ids(r.snapshot()); !equalIDs(got, "id-003", "id-004") {
		t.Fatalf("contents = %v, want both fresh entries", got)
	}
}

func TestRingAppendZeroAtNotEvicted(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(100, 5*time.Minute, clock.now)

	// Event with a zero At must be treated as "now" and never age-evicted on
	// the append that added it.
	r.append(Event{ID: "id-001"})
	if got := ids(r.snapshot()); !equalIDs(got, "id-001") {
		t.Fatalf("contents = %v, want id-001 retained", got)
	}
}

func TestRingSinceCaughtUp(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(100, 0, clock.now)
	for _, id := range []string{"id-001", "id-002", "id-003"} {
		r.append(Event{ID: id, At: clock.now()})
	}

	events, gapped := r.since("id-003")
	if gapped {
		t.Fatalf("gapped = true, want false")
	}
	if len(events) != 0 {
		t.Fatalf("events = %v, want empty (caught up)", ids(events))
	}
}

func TestRingSincePartialReplay(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(100, 0, clock.now)
	for _, id := range []string{"id-001", "id-002", "id-003", "id-004"} {
		r.append(Event{ID: id, At: clock.now()})
	}

	events, gapped := r.since("id-002")
	if gapped {
		t.Fatalf("gapped = true, want false")
	}
	if got := ids(events); !equalIDs(got, "id-003", "id-004") {
		t.Fatalf("events = %v, want only ids after id-002", got)
	}
}

func TestRingSinceGap(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(100, 0, clock.now)
	for _, id := range []string{"id-003", "id-004", "id-005"} {
		r.append(Event{ID: id, At: clock.now()})
	}

	// Resume point predates the oldest retained entry → gap.
	events, gapped := r.since("id-001")
	if !gapped {
		t.Fatalf("gapped = false, want true")
	}
	if events != nil {
		t.Fatalf("events = %v, want nil on gap", ids(events))
	}
}

func TestRingSinceEmptyLastID(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(100, 0, clock.now)
	r.append(Event{ID: "id-001", At: clock.now()})

	events, gapped := r.since("")
	if gapped {
		t.Fatalf("gapped = true, want false")
	}
	if events != nil {
		t.Fatalf("events = %v, want nil for empty lastID", ids(events))
	}
}

func TestRingSinceEmptyBuffer(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(100, 0, clock.now)

	events, gapped := r.since("id-001")
	if gapped {
		t.Fatalf("gapped = true, want false")
	}
	if events != nil {
		t.Fatalf("events = %v, want nil for empty buffer", ids(events))
	}
}

func TestRingSinceOrdering(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRing(100, 0, clock.now)
	for _, id := range []string{"id-001", "id-002", "id-003", "id-004", "id-005"} {
		r.append(Event{ID: id, At: clock.now()})
	}

	events, _ := r.since("id-001")
	got := ids(events)
	if !equalIDs(got, "id-002", "id-003", "id-004", "id-005") {
		t.Fatalf("events = %v, want ascending id order", got)
	}
}

func TestNewRingNilClockDefaults(t *testing.T) {
	t.Parallel()

	// A nil clock must fall back to time.Now rather than panic on append.
	r := newRing(10, time.Minute, nil)
	r.append(Event{ID: "id-001", At: time.Now()})
	if r.len() != 1 {
		t.Fatalf("len = %d, want 1", r.len())
	}
}
