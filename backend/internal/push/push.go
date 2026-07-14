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
//
// The title carries the result rather than the app name because both platforms
// already label the notification themselves — Chrome with the origin, iOS with
// the PWA's home-screen name — so a branded title renders as "Arrflix / Arrflix
// / body". The fixed tag collapses repeated test sends onto one tray entry
// instead of stacking a pile of identical diagnostics.
var TestMessage = Message{
	Title: "Push notifications are working 🎉",
	Body:  "You'll get notified here when your requests are ready to watch.",
	Tag:   "test",
}

// DefaultSubject is the VAPID `sub` claim used when none is set: a contact of
// record for the push service, not a mailbox we send to. A push service may use
// it to reach the operator about abuse/issues but essentially never does.
//
// It is the project URL rather than an address on purpose. Apple's push service
// rejects a JWT whose sub names a non-routable domain (admin@host.local — the
// shape a self-hosted default naturally takes) with 403 BadJwtToken, silently
// killing push to every iOS device. An https URL is always a valid sub, needs no
// operator input, and cannot be typo'd into a dead push channel. Operators who
// want their own contact there can override the stored subject.
const DefaultSubject = "https://github.com/kyleaupton/arrflix"

// ErrSubscriptionGone signals that a push endpoint returned 404/410 — the
// browser has unsubscribed or the subscription expired. The adapter prunes the
// dead row and does not count it as a delivery failure. It is a sentinel, not an
// apperror: check it with errors.Is before consulting apperrors.IsRetryable.
var ErrSubscriptionGone = errors.New("push subscription gone")

// Message is the wire contract the service worker parses on the browser `push`
// event: a title and body it hands to showNotification, plus the metadata it
// needs to group and route the notification. Both the delivery adapter (rendered
// from an event template) and the diagnostic test-send (a canned message)
// marshal this exact shape, so the service worker parses one schema regardless
// of origin.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Tag is the notification's identity in the tray: messages sharing a tag
	// collapse (the later replaces the earlier), distinct tags stack. It is
	// scoped to one logical notification, so two titles becoming available never
	// overwrite each other. An empty tag never collapses.
	Tag string `json:"tag,omitempty"`
	// Media identifies the title the notification is about, letting the service
	// worker build the tap target itself — route shapes stay in the frontend,
	// which is the only layer that owns them. A message without media (a test
	// send, a future non-media alert) opens the app root.
	Media *MessageMedia `json:"media,omitempty"`
}

// MessageMedia is the routing slice of a notification's media: enough to
// construct the title's route without a lookup.
type MessageMedia struct {
	TmdbID int64  `json:"tmdbId"`
	Type   string `json:"type"`
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
