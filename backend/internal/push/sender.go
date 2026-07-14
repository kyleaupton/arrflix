package push

import (
	"context"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// deliveryTTLSeconds is how long the push service holds a message for an offline
// device before discarding it. A day keeps "your title is available" relevant
// across an overnight offline stretch without lingering for weeks.
const deliveryTTLSeconds = 24 * 60 * 60

// vapidSender is the concrete Sender: it encrypts the payload for a subscription
// and POSTs it to the push service, signed with the stored VAPID keypair.
type vapidSender struct {
	cfg    model.VAPIDConfig
	client *http.Client
}

func (s *vapidSender) Send(ctx context.Context, sub model.PushSubscription, payload []byte) error {
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		Subscriber:      s.cfg.Subject,
		VAPIDPublicKey:  s.cfg.PublicKey,
		VAPIDPrivateKey: s.cfg.PrivateKey,
		TTL:             deliveryTTLSeconds,
		HTTPClient:      s.client,
	})
	if err != nil {
		// Transport-level failure (DNS, connection, timeout): transient → retry.
		return apperrors.BadGatewayf("push send to %s: %v", sub.Endpoint, err)
	}
	defer resp.Body.Close()
	return classify(resp.StatusCode)
}

// classify maps a push service's HTTP status to the delivery outcome the worker
// acts on. Per RFC 8030 a successful push returns 201; 404/410 mean the
// subscription is gone; 429 and 5xx are transient; other 4xx are permanent
// rejections (oversized payload, bad VAPID auth) that retrying won't fix.
func classify(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusNotFound || status == http.StatusGone:
		return ErrSubscriptionGone
	case status == http.StatusTooManyRequests || status >= 500:
		return apperrors.BadGatewayf("push service returned %d", status)
	default:
		return apperrors.Internalf("push service rejected notification: %d", status).NotRetryable()
	}
}

var _ Sender = (*vapidSender)(nil)
