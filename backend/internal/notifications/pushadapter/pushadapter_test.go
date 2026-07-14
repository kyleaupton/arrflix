package pushadapter

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/push"
)

// fakeStore returns canned subscriptions and records which endpoints were pruned
// and which were stamped notified.
type fakeStore struct {
	subs     []model.PushSubscription
	listErr  error
	pruned   []string
	notified []string
}

func (s *fakeStore) ListPushSubscriptionsByUser(context.Context, uuid.UUID) ([]model.PushSubscription, error) {
	return s.subs, s.listErr
}

func (s *fakeStore) DeletePushSubscriptionByEndpoint(_ context.Context, endpoint string) (int64, error) {
	s.pruned = append(s.pruned, endpoint)
	return 1, nil
}

func (s *fakeStore) MarkPushSubscriptionNotified(_ context.Context, endpoint string) error {
	s.notified = append(s.notified, endpoint)
	return nil
}

// fakeSender maps each subscription endpoint to a canned Send result and records
// the subscriptions and payload bytes it was handed.
type fakeSender struct {
	byEndpoint map[string]error
	sent       []model.PushSubscription
	payloads   [][]byte
}

func (f *fakeSender) Send(_ context.Context, sub model.PushSubscription, payload []byte) error {
	f.sent = append(f.sent, sub)
	f.payloads = append(f.payloads, payload)
	return f.byEndpoint[sub.Endpoint]
}

// fakeBuilder hands back a fixed Sender (or a build error).
type fakeBuilder struct {
	sender   push.Sender
	buildErr error
}

func (b fakeBuilder) BuildSender(context.Context) (push.Sender, error) {
	return b.sender, b.buildErr
}

func sub(endpoint string) model.PushSubscription {
	return model.PushSubscription{ID: uuid.New(), Endpoint: endpoint, P256dh: "p", Auth: "a"}
}

func wantAvailableRow(t *testing.T, recipient *uuid.UUID) model.NotificationOutbox {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"media": map[string]any{"title": "Sentinel", "year": 2024, "tmdbId": 693134, "type": "movie"},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return model.NotificationOutbox{
		ID:              uuid.New(),
		EventType:       "want.available",
		Channel:         string(model.ChannelPush),
		RecipientUserID: recipient,
		Payload:         payload,
	}
}

func TestAdapter_IsConfiguredAlwaysTrue(t *testing.T) {
	t.Parallel()
	a := New(&fakeStore{}, fakeBuilder{}, notifications.MustNewRenderer())
	if !a.IsConfigured(context.Background()) {
		t.Fatal("push IsConfigured should be unconditionally true")
	}
}

// TestAdapter_DeliverFansOut proves the happy path: every subscription receives
// a well-formed push message and the row settles delivered.
func TestAdapter_DeliverFansOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	fs := &fakeSender{byEndpoint: map[string]error{"e1": nil, "e2": nil}}
	store := &fakeStore{subs: []model.PushSubscription{sub("e1"), sub("e2")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	if err := a.Deliver(ctx, wantAvailableRow(t, &id)); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(fs.sent) != 2 {
		t.Fatalf("sent to %d devices, want 2", len(fs.sent))
	}
	// Every successful send stamps last_notified_at for that endpoint.
	if len(store.notified) != 2 {
		t.Fatalf("notified %d endpoints, want 2 (one per delivered device)", len(store.notified))
	}
	// The wire payload the adapter marshaled is the {title, body} contract the
	// service worker parses — assert what the sender actually received.
	var msg push.Message
	if err := json.Unmarshal(fs.payloads[0], &msg); err != nil {
		t.Fatalf("payload is not valid push.Message json: %v", err)
	}
	if msg.Title != "Sentinel is ready to watch" {
		t.Fatalf("title = %q", msg.Title)
	}
	if msg.Body == "" {
		t.Fatal("body is empty, want the rendered body")
	}
	// The media identity rides along so the service worker can deep-link the tap.
	if msg.Media == nil || msg.Media.TmdbID != 693134 || msg.Media.Type != "movie" {
		t.Fatalf("media = %+v, want the payload's tmdb id and type", msg.Media)
	}
}

// TestAdapter_TagIsPerNotification proves the tray-collapse rule: two rows carry
// distinct tags, so two titles becoming available stack instead of overwriting
// each other — the bug a single app-wide tag caused.
func TestAdapter_TagIsPerNotification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	fs := &fakeSender{byEndpoint: map[string]error{"e1": nil}}
	store := &fakeStore{subs: []model.PushSubscription{sub("e1")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	first, second := wantAvailableRow(t, &id), wantAvailableRow(t, &id)
	for _, row := range []model.NotificationOutbox{first, second} {
		if err := a.Deliver(ctx, row); err != nil {
			t.Fatalf("deliver: %v", err)
		}
	}

	tags := make([]string, 0, 2)
	for _, p := range fs.payloads {
		var msg push.Message
		if err := json.Unmarshal(p, &msg); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		tags = append(tags, msg.Tag)
	}
	if tags[0] == tags[1] {
		t.Fatalf("both notifications tagged %q — they would collapse to one in the tray", tags[0])
	}
	if tags[0] != first.ID.String() {
		t.Fatalf("tag = %q, want the outbox row id %q", tags[0], first.ID)
	}
}

// TestAdapter_DedupKeyIsTheTag proves an event that declares a coalescing
// identity uses it as the tag, so re-emissions replace rather than stack.
func TestAdapter_DedupKeyIsTheTag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	fs := &fakeSender{byEndpoint: map[string]error{"e1": nil}}
	store := &fakeStore{subs: []model.PushSubscription{sub("e1")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	row := wantAvailableRow(t, &id)
	key := "want.available:693134"
	row.DedupKey = &key

	if err := a.Deliver(ctx, row); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	var msg push.Message
	if err := json.Unmarshal(fs.payloads[0], &msg); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if msg.Tag != key {
		t.Fatalf("tag = %q, want the row's dedup key %q", msg.Tag, key)
	}
}

// TestAdapter_MediaOmittedWhenUnroutable proves a payload the worker can't build
// a route from still delivers — the notification lands and opens the app root
// rather than being held back over a missing deep link.
func TestAdapter_MediaOmittedWhenUnroutable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	payload, err := json.Marshal(map[string]any{"media": map[string]any{"title": "Sentinel"}})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	row := wantAvailableRow(t, &id)
	row.Payload = payload

	fs := &fakeSender{byEndpoint: map[string]error{"e1": nil}}
	store := &fakeStore{subs: []model.PushSubscription{sub("e1")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	if err := a.Deliver(ctx, row); err != nil {
		t.Fatalf("a title with no tmdb id should still deliver, got %v", err)
	}
	var msg push.Message
	if err := json.Unmarshal(fs.payloads[0], &msg); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if msg.Media != nil {
		t.Fatalf("media = %+v, want nil so the worker opens the app root", msg.Media)
	}
}

// TestAdapter_NoSubscriptionsIsNoOp proves a recipient with no devices settles
// delivered (nothing to send) rather than dead or retried forever.
func TestAdapter_NoSubscriptionsIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	store := &fakeStore{subs: nil}
	a := New(store, fakeBuilder{sender: &fakeSender{}}, notifications.MustNewRenderer())
	if err := a.Deliver(ctx, wantAvailableRow(t, &id)); err != nil {
		t.Fatalf("no-subscription deliver should be a nil no-op, got %v", err)
	}
}

// TestAdapter_PrunesGoneAndDelivers proves a 404/410 endpoint is pruned and,
// because a sibling delivered, the row still settles delivered.
func TestAdapter_PrunesGoneAndDelivers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	fs := &fakeSender{byEndpoint: map[string]error{"dead": push.ErrSubscriptionGone, "live": nil}}
	store := &fakeStore{subs: []model.PushSubscription{sub("dead"), sub("live")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	if err := a.Deliver(ctx, wantAvailableRow(t, &id)); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(store.pruned) != 1 || store.pruned[0] != "dead" {
		t.Fatalf("pruned = %v, want [dead]", store.pruned)
	}
}

// TestAdapter_AllGoneIsNoOp proves that when every endpoint is gone (all pruned,
// none delivered) the row settles delivered — there is nothing left to send.
func TestAdapter_AllGoneIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	fs := &fakeSender{byEndpoint: map[string]error{"g1": push.ErrSubscriptionGone, "g2": push.ErrSubscriptionGone}}
	store := &fakeStore{subs: []model.PushSubscription{sub("g1"), sub("g2")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	if err := a.Deliver(ctx, wantAvailableRow(t, &id)); err != nil {
		t.Fatalf("all-gone deliver should be a nil no-op, got %v", err)
	}
	if len(store.pruned) != 2 {
		t.Fatalf("pruned %d, want 2", len(store.pruned))
	}
}

// TestAdapter_RetryableWhenNothingDelivered proves that if no device was reached
// and a send failed transiently, the row is retried (retryable error surfaced).
func TestAdapter_RetryableWhenNothingDelivered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	fs := &fakeSender{byEndpoint: map[string]error{"e1": apperrors.BadGatewayf("503")}}
	store := &fakeStore{subs: []model.PushSubscription{sub("e1")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	err := a.Deliver(ctx, wantAvailableRow(t, &id))
	if err == nil || !apperrors.IsRetryable(err) {
		t.Fatalf("want retryable error when nothing delivered, got %v", err)
	}
}

// TestAdapter_DeliveredWinsOverTransientSibling proves the anti-duplicate policy:
// one device delivered, another failed transiently → the row is done (nil), so a
// retry won't re-send to the delivered device.
func TestAdapter_DeliveredWinsOverTransientSibling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	fs := &fakeSender{byEndpoint: map[string]error{"ok": nil, "blip": apperrors.BadGatewayf("503")}}
	store := &fakeStore{subs: []model.PushSubscription{sub("ok"), sub("blip")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	if err := a.Deliver(ctx, wantAvailableRow(t, &id)); err != nil {
		t.Fatalf("a delivered device should settle the row despite a transient sibling, got %v", err)
	}
}

// TestAdapter_PermanentWhenAllPermanent proves that when every send fails
// permanently (none delivered, none retryable), the permanent error surfaces so
// the worker marks the row dead.
func TestAdapter_PermanentWhenAllPermanent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	fs := &fakeSender{byEndpoint: map[string]error{"e1": apperrors.Internalf("413").NotRetryable()}}
	store := &fakeStore{subs: []model.PushSubscription{sub("e1")}}
	a := New(store, fakeBuilder{sender: fs}, notifications.MustNewRenderer())

	err := a.Deliver(ctx, wantAvailableRow(t, &id))
	if err == nil || apperrors.IsRetryable(err) {
		t.Fatalf("want permanent error when all sends permanently fail, got %v", err)
	}
}

// TestAdapter_NilRecipientIsPermanent proves a row with no recipient user is a
// permanent failure (push is user-audience only).
func TestAdapter_NilRecipientIsPermanent(t *testing.T) {
	t.Parallel()
	a := New(&fakeStore{}, fakeBuilder{sender: &fakeSender{}}, notifications.MustNewRenderer())
	err := a.Deliver(context.Background(), wantAvailableRow(t, nil))
	if err == nil || apperrors.IsRetryable(err) {
		t.Fatalf("nil recipient should be a permanent error, got %v", err)
	}
}

var _ notifications.ChannelAdapter = (*Adapter)(nil)
