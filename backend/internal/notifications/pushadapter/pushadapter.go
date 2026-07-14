// Package pushadapter is the Web Push ChannelAdapter: it renders a due outbox
// row into a title + body, then fans the message out to every push subscription
// the recipient has registered (one per device/browser).
//
// It lives outside the pure internal/notifications core because it needs the
// subscription store, a push Sender, and a Renderer — dependencies the
// registry/routing core deliberately excludes. It depends on narrow surfaces so
// it can be unit-tested with fakes.
package pushadapter

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/push"
)

// subscriptionStore is the narrow persistence surface the adapter needs: the
// recipient's live subscriptions, a way to prune a dead endpoint, and a way to
// stamp a successful delivery. *repo.Repository satisfies it; a test passes a fake.
type subscriptionStore interface {
	ListPushSubscriptionsByUser(ctx context.Context, userID uuid.UUID) ([]model.PushSubscription, error)
	DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) (int64, error)
	MarkPushSubscriptionNotified(ctx context.Context, endpoint string) error
}

// senderBuilder builds a push.Sender bound to the stored VAPID identity.
// *push.Manager satisfies it; a test passes a fake returning a canned Sender.
type senderBuilder interface {
	BuildSender(ctx context.Context) (push.Sender, error)
}

// Adapter delivers notifications over Web Push.
type Adapter struct {
	subs     subscriptionStore
	push     senderBuilder
	renderer *notifications.Renderer
}

// New builds the push adapter. The renderer is passed in (rather than parsed
// here) so it renders from the same embedded template tree the worker verified
// at startup.
func New(subs subscriptionStore, push senderBuilder, renderer *notifications.Renderer) *Adapter {
	return &Adapter{subs: subs, push: push, renderer: renderer}
}

func (a *Adapter) Name() model.NotificationChannel { return model.ChannelPush }

// IsConfigured is always true: the VAPID keypair self-generates at boot, so push
// never needs operator setup and the worker never parks a push row as
// awaiting_config. A recipient with no subscriptions is handled in Deliver as a
// success no-op, not an unconfigured channel.
func (a *Adapter) IsConfigured(context.Context) bool { return true }

// Deliver renders the row and fans it out to the recipient's subscriptions.
//
// Fan-out rollup — one outbox row maps to N devices but the worker acts on a
// single returned error, so the policy avoids duplicate re-sends on retry:
//   - a 404/410 endpoint is pruned and ignored (the browser unsubscribed);
//   - if any device was delivered, the row is done (nil) — a sibling that failed
//     transiently misses this one notification rather than every delivered device
//     getting it twice on retry;
//   - if nothing was delivered, a transient failure is returned (the worker
//     retries the whole row; pruned endpoints are already gone so no re-hit),
//     else a permanent failure marks the row dead;
//   - no subscriptions at all is a success no-op — there is nothing to deliver,
//     and a later subscribe does not resurrect past notifications.
func (a *Adapter) Deliver(ctx context.Context, row model.NotificationOutbox) error {
	const op = "PushAdapter.Deliver"

	if row.RecipientUserID == nil {
		return apperrors.Validation("push notification has no recipient user").Op(op)
	}

	title, body, err := a.renderer.Render(row.EventType, model.ChannelPush, row.Payload)
	if err != nil {
		return apperrors.Internalf("render push for %q: %v", row.EventType, err).NotRetryable().Op(op)
	}
	payload, err := json.Marshal(push.Message{
		Title: title,
		Body:  body,
		Tag:   tagFor(row),
		Media: mediaFor(row.Payload),
	})
	if err != nil {
		return apperrors.Internalf("marshal push message: %v", err).NotRetryable().Op(op)
	}

	subs, err := a.subs.ListPushSubscriptionsByUser(ctx, *row.RecipientUserID)
	if err != nil {
		return err // DB blip is retryable; propagate verbatim.
	}
	if len(subs) == 0 {
		return nil // No devices — nothing to deliver.
	}

	sender, err := a.push.BuildSender(ctx)
	if err != nil {
		return err
	}

	var delivered int
	var lastRetryable, lastPermanent error
	for _, sub := range subs {
		switch sendErr := sender.Send(ctx, sub, payload); {
		case sendErr == nil:
			delivered++
			// Best-effort: stamp last_notified_at for the devices UI. A lost stamp
			// only leaves the UI slightly stale, so it never fails the row.
			_ = a.subs.MarkPushSubscriptionNotified(ctx, sub.Endpoint)
		case errors.Is(sendErr, push.ErrSubscriptionGone):
			// Endpoint dead: prune it. Best-effort — a failed prune just retries
			// next delivery, so it does not fail the row.
			_, _ = a.subs.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint)
		case apperrors.IsRetryable(sendErr):
			lastRetryable = sendErr
		default:
			lastPermanent = sendErr
		}
	}

	switch {
	case delivered > 0:
		return nil
	case lastRetryable != nil:
		return lastRetryable
	case lastPermanent != nil:
		return lastPermanent
	default:
		return nil // Every subscription was gone (and pruned); nothing left to send.
	}
}

// tagFor gives a notification its tray identity, keyed to one logical
// notification rather than to the app. An event that declares a dedup key has
// already named its own coalescing identity, so re-emissions of it replace in
// the tray; otherwise the outbox row id makes each notification distinct, which
// is what keeps two titles becoming available from overwriting each other.
func tagFor(row model.NotificationOutbox) string {
	if row.DedupKey != nil && *row.DedupKey != "" {
		return *row.DedupKey
	}
	return row.ID.String()
}

// mediaFor extracts the routing slice of the payload's media envelope so the
// service worker can deep-link the tap. Best-effort by design: a payload with no
// media, or one whose media has no TMDB id (unmatched title), yields nil and the
// worker falls back to opening the app root — a notification that lands and
// opens the app beats one held back over a missing link.
func mediaFor(payload []byte) *push.MessageMedia {
	ref, ok := notifications.MediaRefFrom(payload)
	if !ok || ref.TmdbID == 0 {
		return nil
	}
	return &push.MessageMedia{TmdbID: ref.TmdbID, Type: string(ref.Type)}
}

var _ notifications.ChannelAdapter = (*Adapter)(nil)
