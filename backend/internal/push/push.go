// Package push is the Web Push transport seam. It owns the server's VAPID
// identity (a singleton keypair generated once on first boot) and turns a stored
// push_subscription into an encrypted, VAPID-signed POST to the browser's push
// service.
//
// It mirrors internal/email: a repo-backed Manager assembles the send-time
// config, and a Sender does the actual transmission behind a narrow interface so
// the notification adapter can be unit-tested with a fake. Unlike email, push
// needs no operator setup — the keypair self-generates — so there is no
// "configured?" gate; the adapter's IsConfigured is unconditionally true.
package push

import (
	"context"
	"errors"

	"github.com/kyleaupton/arrflix/internal/model"
)

// TestMessage is the canned diagnostic a user's "send a test to this device"
// action delivers: it round-trips the full VAPID sign → encrypt → push-service
// path so a green toast proves this specific browser can actually receive pushes.
var TestMessage = Message{
	Title: "arrflix",
	Body:  "Push notifications are working on this device 🎉",
}

// DefaultSubject is the VAPID `sub` claim used when none is set: a syntactically
// valid contact URI, not a mailbox we send to. A push service may use it to
// reach the operator about abuse/issues but essentially never does. The settings
// UI defaults this to the admin's email and lets the operator edit it.
const DefaultSubject = "mailto:admin@arrflix.local"

// ErrSubscriptionGone signals that a push endpoint returned 404/410 — the
// browser has unsubscribed or the subscription expired. The adapter prunes the
// dead row and does not count it as a delivery failure. It is a sentinel, not an
// apperror: check it with errors.Is before consulting apperrors.IsRetryable.
var ErrSubscriptionGone = errors.New("push subscription gone")

// Message is the wire contract the service worker parses on the browser `push`
// event: a title and body it hands to showNotification. Both the delivery
// adapter (rendered from an event template) and the diagnostic test-send
// (a canned message) marshal this exact shape, so the service worker parses one
// schema regardless of origin.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Sender transmits one payload to one subscription. Return values:
//
//   - nil                     — delivered.
//   - ErrSubscriptionGone     — endpoint dead; caller prunes the subscription.
//   - a retryable apperror    — transient push-service failure (network/5xx/429).
//   - a non-retryable apperror — permanent rejection (malformed/oversized/auth).
type Sender interface {
	Send(ctx context.Context, sub model.PushSubscription, payload []byte) error
}
