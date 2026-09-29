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

// Subscribing to a scoped topic must not blackhole the events a session
// receives by default. The topic set is an opt-in list, never a filter.
func TestSubscribingDoesNotRestrictDefaultDelivery(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()

	att := b.Attach(AttachParams{UserID: user, Topics: []string{"title.status:movie:42"}})
	defer att.Cancel()

	b.Publish(mkEvent("id-001", "download_job_updated", Broadcast, clock.now()))
	b.Publish(mkEvent("id-002", "want_updated", Broadcast, clock.now()))

	if got := ids(drain(att.Out)); !equalIDs(got, "id-001", "id-002") {
		t.Fatalf("delivered = %v, want both default events despite an active subscription", got)
	}
}

// An event carrying a Topic is opt-in: only sessions subscribed to that exact
// topic receive it, and it never lands in an unsubscribed session's ring.
func TestScopedEventsReachOnlySubscribers(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)

	sub := b.Attach(AttachParams{UserID: uuid.New(), Topics: []string{"title.status:movie:42"}})
	defer sub.Cancel()
	bystander := b.Attach(AttachParams{UserID: uuid.New()})
	defer bystander.Cancel()

	ev := mkEvent("id-001", "title_status", Broadcast, clock.now())
	ev.Topic = "title.status:movie:42"
	b.Publish(ev)

	other := mkEvent("id-002", "title_status", Broadcast, clock.now())
	other.Topic = "title.status:movie:99"
	b.Publish(other)

	if got := ids(drain(sub.Out)); !equalIDs(got, "id-001") {
		t.Fatalf("subscriber received %v, want only its own topic", got)
	}
	if got := ids(drain(bystander.Out)); len(got) != 0 {
		t.Fatalf("unsubscribed session received %v, want nothing", got)
	}
	if got := ids(bystander.Session.replay.snapshot()); len(got) != 0 {
		t.Fatalf("unsubscribed ring holds %v, want nothing", got)
	}
}

// A capability-targeted event reaches only sessions whose resolved key set
// holds that key. This is the filter that keeps operator payloads off a
// requester's connection.
func TestCapabilityDeliveryRequiresTheGrant(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)

	operator := b.Attach(AttachParams{UserID: uuid.New(), Capabilities: []string{"jobs.read", "library.read"}})
	defer operator.Cancel()
	requester := b.Attach(AttachParams{UserID: uuid.New(), Capabilities: []string{"requests.view.own"}})
	defer requester.Cancel()

	b.Publish(mkEvent("id-001", "download_job_updated", Capability("jobs.read"), clock.now()))
	b.Publish(mkEvent("id-002", "proposal_updated", Capability("jobs.manage"), clock.now()))
	b.Publish(mkEvent("id-003", "want_updated", Broadcast, clock.now()))

	// jobs.read yes, jobs.manage no, broadcast always.
	if got := ids(drain(operator.Out)); !equalIDs(got, "id-001", "id-003") {
		t.Fatalf("operator received %v, want id-001 and id-003", got)
	}
	if got := ids(drain(requester.Out)); !equalIDs(got, "id-003") {
		t.Fatalf("requester received %v, want only the broadcast", got)
	}
	if got := ids(requester.Session.replay.snapshot()); !equalIDs(got, "id-003") {
		t.Fatalf("requester ring holds %v — an ineligible event must not be buffered either", got)
	}
}

// An event whose recipient was never set reaches nobody. Delivery fails closed,
// so a forgotten recipient surfaces as a missing update rather than as a leak.
func TestZeroRecipientDeliversToNobody(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)

	att := b.Attach(AttachParams{UserID: uuid.New(), Capabilities: []string{"jobs.read"}})
	defer att.Cancel()

	b.Publish(mkEvent("id-001", "mystery", Recipient{}, clock.now()))

	if got := ids(drain(att.Out)); len(got) != 0 {
		t.Fatalf("delivered = %v, want nothing", got)
	}
}

// Reattaching re-resolves capabilities, so a revoked grant stops delivery at
// the next reconnect rather than persisting for the session's lifetime.
func TestReattachRefreshesCapabilities(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := newBroker(clock.now)
	user := uuid.New()

	first := b.Attach(AttachParams{UserID: user, Capabilities: []string{"jobs.read"}})
	first.Cancel()

	second := b.Attach(AttachParams{SessionID: first.Session.ID, UserID: user, Capabilities: nil})
	defer second.Cancel()

	b.Publish(mkEvent("id-001", "download_job_updated", Capability("jobs.read"), clock.now()))

	if got := ids(drain(second.Out)); len(got) != 0 {
		t.Fatalf("delivered = %v, want nothing after the grant was dropped", got)
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
