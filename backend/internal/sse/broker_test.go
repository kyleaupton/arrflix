package sse

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// These tests drive the broker through an injected clock (newBroker, not
// NewBroker — no sweeper goroutine) so reattach/replay/eviction are
// deterministic. Event ids are synthetic but lexicographically ordered, the
// same property real UUIDv7 ids have.

func mkEvent(id, typ string, r Recipient, at time.Time) Event {
	return Event{Type: typ, ID: id, At: at, Recipient: r, Data: json.RawMessage(`{}`)}
}

// drain reads everything currently buffered on a channel without blocking.
func drain(ch <-chan Event) []Event {
	var out []Event
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestAttachFreshAllocatesSession(t *testing.T) {
	t.Parallel()

	b := newBroker((&fakeClock{t: time.Unix(0, 0)}).now)
	user := uuid.New()

	att := b.Attach(AttachParams{UserID: user})
	defer att.Cancel()

	if att.Session.ID == uuid.Nil {
		t.Fatal("fresh attach must allocate a session id")
	}
	if att.Session.UserID != user {
		t.Fatalf("UserID = %v, want %v", att.Session.UserID, user)
	}
	if att.Replay != nil || att.Gapped {
		t.Fatalf("fresh attach must not replay or gap: replay=%v gapped=%v", att.Replay, att.Gapped)
	}
}

func TestReattachReplaysOnlyAfterLastEventID(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()

	att := b.Attach(AttachParams{UserID: user})
	sid := att.Session.ID

	for _, id := range []string{"id-001", "id-002", "id-003"} {
		b.Publish(mkEvent(id, "scan_progress", Broadcast, clock.now()))
	}
	att.Cancel() // detach; ring retained

	re := b.Attach(AttachParams{SessionID: sid, UserID: user, LastEventID: "id-001"})
	defer re.Cancel()

	if re.Session.ID != sid {
		t.Fatalf("reattach got a different session: %v != %v", re.Session.ID, sid)
	}
	if re.Gapped {
		t.Fatal("gapped = true, want false (id-001 still in ring)")
	}
	if got := ids(re.Replay); !equalIDs(got, "id-002", "id-003") {
		t.Fatalf("replay = %v, want id-002,id-003", got)
	}
}

func TestReattachAcrossUsersIsRejected(t *testing.T) {
	t.Parallel()

	b := newBroker((&fakeClock{t: time.Unix(0, 0)}).now)
	owner := uuid.New()
	att := b.Attach(AttachParams{UserID: owner})
	sid := att.Session.ID
	att.Cancel()

	// A different user presenting the same session id must NOT reattach — they
	// get a fresh, distinct session.
	other := uuid.New()
	re := b.Attach(AttachParams{SessionID: sid, UserID: other})
	defer re.Cancel()

	if re.Session.ID == sid {
		t.Fatal("cross-user reattach must not reuse the session")
	}
	if re.Session.UserID != other {
		t.Fatalf("UserID = %v, want %v", re.Session.UserID, other)
	}
}

func TestPublishWhileDetachedLandsInRing(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()

	att := b.Attach(AttachParams{UserID: user})
	sid := att.Session.ID
	att.Cancel() // detached

	// Events published while detached must still accumulate in the ring.
	b.Publish(mkEvent("id-001", "scan_progress", Broadcast, clock.now()))
	b.Publish(mkEvent("id-002", "scan_progress", Broadcast, clock.now()))

	re := b.Attach(AttachParams{SessionID: sid, UserID: user, LastEventID: ""})
	defer re.Cancel()

	// LastEventID "" yields no replay slice, but the events must have landed in
	// the ring while detached so a resume point within the window replays them.
	if got := ids(re.Session.replay.snapshot()); !equalIDs(got, "id-001", "id-002") {
		t.Fatalf("ring contents = %v, want events published while detached", got)
	}
}

func TestReattachGapWhenResumePointEvicted(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()

	att := b.Attach(AttachParams{UserID: user})
	sid := att.Session.ID
	for _, id := range []string{"id-003", "id-004"} {
		b.Publish(mkEvent(id, "scan_progress", Broadcast, clock.now()))
	}
	att.Cancel()

	// Resume point predates the oldest retained event → gap, empty replay.
	re := b.Attach(AttachParams{SessionID: sid, UserID: user, LastEventID: "id-001"})
	defer re.Cancel()

	if !re.Gapped {
		t.Fatal("gapped = false, want true")
	}
	if re.Replay != nil {
		t.Fatalf("replay = %v, want nil on gap", ids(re.Replay))
	}
}

func TestPublishOverflowKicksWithoutDropOrPanic(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()

	att := b.Attach(AttachParams{UserID: user})
	defer att.Cancel()

	// Fill the outbound channel past capacity without anyone draining it.
	total := outboundDepth + 10
	for i := 0; i < total; i++ {
		b.Publish(mkEvent(syntheticID(i), "scan_progress", Broadcast, clock.now()))
	}

	// The handler is signalled to tear down.
	select {
	case <-att.Kick:
	default:
		t.Fatal("expected a kick after overflow")
	}

	// Every event is still in the ring (capped at replayMaxLen), so nothing was
	// lost to the drop path — the client will replay on reconnect.
	if got := att.Session.replay.len(); got != replayMaxLen {
		t.Fatalf("ring len = %d, want %d (events retained, not dropped)", got, replayMaxLen)
	}
}

func TestRecipientAndTopicFilterGovernSendAndRing(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()
	other := uuid.New()

	// Session filtered to only "scan_progress".
	att := b.Attach(AttachParams{UserID: user, Topics: []string{"scan_progress"}})
	defer att.Cancel()

	b.Publish(mkEvent("id-001", "scan_progress", Broadcast, clock.now()))   // delivered
	b.Publish(mkEvent("id-002", "download_job_updated", Broadcast, clock.now())) // filtered by topic
	b.Publish(mkEvent("id-003", "scan_progress", User(other), clock.now()))  // filtered by recipient

	got := ids(drain(att.Out))
	if !equalIDs(got, "id-001") {
		t.Fatalf("delivered = %v, want only id-001", got)
	}

	// The ring mirrors the send filter: only eligible events buffered.
	if got := ids(att.Session.replay.snapshot()); !equalIDs(got, "id-001") {
		t.Fatalf("ring = %v, want only id-001", got)
	}
}

func TestUserScopedDeliveryReachesOnlyOwner(t *testing.T) {
	t.Parallel()

	b := newBroker((&fakeClock{t: time.Unix(0, 0)}).now)
	alice := uuid.New()
	bob := uuid.New()

	aAtt := b.Attach(AttachParams{UserID: alice})
	defer aAtt.Cancel()
	bAtt := b.Attach(AttachParams{UserID: bob})
	defer bAtt.Cancel()

	b.Publish(mkEvent("id-001", "notification_delivered", User(alice), time.Unix(1, 0)))

	if got := ids(drain(aAtt.Out)); !equalIDs(got, "id-001") {
		t.Fatalf("alice got %v, want id-001", got)
	}
	if got := drain(bAtt.Out); len(got) != 0 {
		t.Fatalf("bob got %v, want nothing (event was user-scoped to alice)", ids(got))
	}
}

func TestSweepEvictsDetachedPastTTL(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()

	att := b.Attach(AttachParams{UserID: user})
	sid := att.Session.ID
	att.Cancel() // detachedAt = now

	// Before the TTL elapses the session is retained (reattach still possible).
	clock.advance(detachTTL - time.Second)
	b.sweep()
	if _, ok := b.sessions[sid]; !ok {
		t.Fatal("session evicted before TTL elapsed")
	}

	// Past the TTL it is swept.
	clock.advance(2 * time.Second)
	b.sweep()
	if _, ok := b.sessions[sid]; ok {
		t.Fatal("session survived past detach TTL")
	}
}

func TestSweepKeepsAttachedSessions(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)

	att := b.Attach(AttachParams{UserID: uuid.New()})
	defer att.Cancel()

	clock.advance(detachTTL * 10)
	b.sweep()

	if _, ok := b.sessions[att.Session.ID]; !ok {
		t.Fatal("an attached session must never be swept")
	}
}

func TestStaleCancelDoesNotDetachReattachedSession(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()

	first := b.Attach(AttachParams{UserID: user})
	sid := first.Session.ID
	first.Cancel() // detach

	second := b.Attach(AttachParams{SessionID: sid, UserID: user})

	// The first attachment's (now stale) cancel must be a no-op: it must not
	// detach the live second attachment.
	first.Cancel()

	second.Session.mu.Lock()
	live := second.Session.out != nil && second.Session.detachedAt == nil
	second.Session.mu.Unlock()
	if !live {
		t.Fatal("stale cancel tore down the reattached session")
	}
	second.Cancel()
}

func TestTopicMutationRoundTrip(t *testing.T) {
	t.Parallel()

	b := newBroker((&fakeClock{t: time.Unix(0, 0)}).now)
	user := uuid.New()
	att := b.Attach(AttachParams{UserID: user})
	defer att.Cancel()
	sid := att.Session.ID

	if topics, ok := b.Topics(sid, user); !ok || len(topics) != 0 {
		t.Fatalf("fresh session topics = %v ok=%v, want empty set", topics, ok)
	}

	if !b.AddTopics(sid, user, []string{"scan_progress", "download_job_updated", ""}) {
		t.Fatal("AddTopics returned false for the owner")
	}
	got, ok := b.Topics(sid, user)
	if !ok || !equalIDs(got, "download_job_updated", "scan_progress") {
		t.Fatalf("topics after add = %v, want the two non-empty topics sorted", got)
	}

	if !b.RemoveTopic(sid, user, "scan_progress") {
		t.Fatal("RemoveTopic returned false for the owner")
	}
	// Removing a topic never held is a no-op success.
	if !b.RemoveTopic(sid, user, "never_subscribed") {
		t.Fatal("RemoveTopic of an absent topic should still succeed")
	}
	got, _ = b.Topics(sid, user)
	if !equalIDs(got, "download_job_updated") {
		t.Fatalf("topics after remove = %v, want only download_job_updated", got)
	}
}

func TestTopicMutationRejectsForeignAndMissing(t *testing.T) {
	t.Parallel()

	b := newBroker((&fakeClock{t: time.Unix(0, 0)}).now)
	owner := uuid.New()
	intruder := uuid.New()
	att := b.Attach(AttachParams{UserID: owner})
	defer att.Cancel()
	sid := att.Session.ID

	// Foreign user must not see or mutate the session — existence never leaks.
	if _, ok := b.Topics(sid, intruder); ok {
		t.Fatal("Topics leaked a foreign session")
	}
	if b.AddTopics(sid, intruder, []string{"scan_progress"}) {
		t.Fatal("AddTopics mutated a foreign session")
	}
	if b.RemoveTopic(sid, intruder, "scan_progress") {
		t.Fatal("RemoveTopic mutated a foreign session")
	}

	// The intruder's rejected add must not have leaked into the owner's set.
	if got, _ := b.Topics(sid, owner); len(got) != 0 {
		t.Fatalf("owner topics = %v, want untouched", got)
	}

	// Unknown session id → not found for everyone.
	if _, ok := b.Topics(uuid.New(), owner); ok {
		t.Fatal("Topics returned ok for an unknown session")
	}
}

// syntheticID renders a zero-padded, lexicographically sortable id for bulk
// publishes.
func syntheticID(i int) string {
	const digits = "0123456789"
	b := []byte("id-0000")
	for p := len(b) - 1; p >= 3 && i > 0; p-- {
		b[p] = digits[i%10]
		i /= 10
	}
	return string(b)
}
