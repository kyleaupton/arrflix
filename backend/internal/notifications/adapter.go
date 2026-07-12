package notifications

import (
	"context"

	"github.com/kyleaupton/arrflix/internal/model"
)

// ChannelAdapter transmits a notification over one channel. The worker owns the
// outbox lifecycle (claim → deliver → settle); an adapter only transmits and
// reports the outcome:
//
//   - nil                     — delivered.
//   - a retryable error       — transient failure; the worker backs off and requeues.
//   - a non-retryable error   — permanent failure; the worker marks the row dead.
//
// Retryability follows the typed-error model (apperrors.IsRetryable): a
// BadGateway/upstream blip retries; a Validation-shaped or explicitly
// NotRetryable failure (a malformed subscription, a rejected address) dies.
//
// A text-transmitting adapter (push, email) renders the row's payload through a
// Renderer it holds; in_app renders nothing at delivery time.
//
// IsConfigured gates delivery on a channel that can be un-configured: the email
// adapter reports false until SMTP is set up. The worker parks a due row for an
// unconfigured channel as awaiting_config (rather than delivering or killing it),
// so it waits for the operator to configure the channel. A channel that can
// always deliver (in_app) returns true unconditionally.
type ChannelAdapter interface {
	Name() model.NotificationChannel
	IsConfigured(ctx context.Context) bool
	Deliver(ctx context.Context, row model.NotificationOutbox) error
}

// InAppAdapter is the in_app channel. The outbox row is itself the notification
// the bell UI reads, so delivery is a no-op confirmation — the worker marking
// the row delivered is what surfaces it. It renders nothing here; the bell read
// path renders the row's payload for display.
type InAppAdapter struct{}

func (InAppAdapter) Name() model.NotificationChannel { return model.ChannelInApp }

// IsConfigured is always true: the in_app channel needs no external setup — the
// outbox row itself is the delivered notification.
func (InAppAdapter) IsConfigured(context.Context) bool { return true }

func (InAppAdapter) Deliver(context.Context, model.NotificationOutbox) error { return nil }

var _ ChannelAdapter = InAppAdapter{}
