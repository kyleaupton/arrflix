package push

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// deliveryTTLSeconds is how long the push service holds a message for an offline
// device before discarding it. A day keeps "your title is available" relevant
// across an overnight offline stretch without lingering for weeks.
const deliveryTTLSeconds = 24 * 60 * 60

// maxReasonBytes bounds how much of a push service's error body we quote back.
// The diagnostics are one short line ({"reason":"BadJwtToken"}); the cap keeps a
// misbehaving upstream from pasting a page of HTML into an error string.
const maxReasonBytes = 512

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
		Subscriber:      subscriberFor(s.cfg.Subject),
		VAPIDPublicKey:  s.cfg.PublicKey,
		VAPIDPrivateKey: s.cfg.PrivateKey,
		TTL:             deliveryTTLSeconds,
		HTTPClient:      s.client,
	})
	if err != nil {
		// Transport-level failure (DNS, connection, timeout): transient → retry.
		return apperrors.BadGatewayf("push send to %s: %v", sub.Endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return classify(resp.StatusCode, reason(resp.Body))
}

// subscriberFor adapts a stored subject to what webpush-go's Options.Subscriber
// expects: a bare email address, or an https URL passed through untouched. The
// library prepends "mailto:" to anything not already starting with "https:", so
// handing it a mailto:-prefixed subject signs a malformed "mailto:mailto:…"
// sub claim — which Apple rejects with 403 BadJwtToken while FCM, not
// validating sub at all, accepts. Strip the prefix so a subject stored in either
// shape signs correctly.
func subscriberFor(subject string) string {
	return strings.TrimPrefix(subject, "mailto:")
}

// reason extracts the push service's own explanation of a rejection, which the
// status code alone can't carry: a 403 is actionable as BadJwtToken (the sub
// claim is malformed) and inscrutable without it.
func reason(body io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(body, maxReasonBytes))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// classify maps a push service's HTTP status to the delivery outcome the worker
// acts on. Per RFC 8030 a successful push returns 201; 404/410 mean the
// subscription is gone; 429 and 5xx are transient; other 4xx are permanent
// rejections (oversized payload, bad VAPID auth) that retrying won't fix.
//
// A rejection is BadGateway rather than Internal so the upstream's reason
// reaches the caller — Internal hides its detail from the wire, which renders a
// misconfigured VAPID subject as a bare 500 with nothing to act on.
func classify(status int, reason string) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusNotFound || status == http.StatusGone:
		return ErrSubscriptionGone
	case status == http.StatusTooManyRequests || status >= 500:
		return apperrors.BadGatewayf("push service returned %s", describe(status, reason))
	default:
		return apperrors.BadGatewayf("push service rejected notification: %s", describe(status, reason)).
			NotRetryable()
	}
}

// describe renders a status with the upstream's reason appended when it gave one.
func describe(status int, reason string) string {
	if reason == "" {
		return strconv.Itoa(status)
	}
	return fmt.Sprintf("%d: %s", status, reason)
}

var _ Sender = (*vapidSender)(nil)
