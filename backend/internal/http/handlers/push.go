package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/push"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// Push is the Web Push subscription surface: the caller fetches the server's
// VAPID public key, registers the push subscription for the browser they are on,
// then manages their registered devices — listing them, removing one, or sending
// a test push to prove a device can receive notifications. Every operation is
// scoped to the caller's user id (resolved from the request context), so the
// authz gate classifies these as authenticated-only — no permission key.
//
// The public key comes straight from the push Manager (the VAPID identity),
// mirroring how EmailProvider reads its transport Manager directly; subscription
// storage goes through NotificationService, and the test-send composes both.
type Push struct {
	svc         *service.Services
	pushManager *push.Manager
}

func NewPush(s *service.Services, manager *push.Manager) *Push {
	return &Push{svc: s, pushManager: manager}
}

// ----- Public key -----

type PushPublicKeyInput struct{}

type pushPublicKeyBody struct {
	PublicKey string `json:"publicKey" doc:"VAPID application server public key (base64url); pass to pushManager.subscribe"`
}

type PushPublicKeyOutput struct {
	Body pushPublicKeyBody
}

func (h *Push) PublicKey(ctx context.Context, _ *PushPublicKeyInput) (*PushPublicKeyOutput, error) {
	key, err := h.pushManager.PublicKey(ctx)
	if err != nil {
		return nil, err
	}
	return &PushPublicKeyOutput{Body: pushPublicKeyBody{PublicKey: key}}, nil
}

// ----- Subscribe -----

// pushSubscribeBody is the flattened PushSubscription the browser produces
// (subscription.endpoint + the two getKey values). Flattened rather than nesting
// a `keys` object so the request schema stays simple; the frontend maps
// subscription.toJSON() into this shape.
type pushSubscribeBody struct {
	Endpoint string `json:"endpoint" required:"true" minLength:"1" doc:"Push service endpoint URL"`
	P256dh   string `json:"p256dh" required:"true" minLength:"1" doc:"Client ECDH public key (base64url)"`
	Auth     string `json:"auth" required:"true" minLength:"1" doc:"Client auth secret (base64url)"`
}

// PushSubscribeInput binds the User-Agent header as a best-effort device label
// so the client need not send it and can't spoof another device's name.
type PushSubscribeInput struct {
	UserAgent string `header:"User-Agent" doc:"Device label, captured server-side"`
	Body      pushSubscribeBody
}

type PushSubscribeOutput struct{}

func (h *Push) Subscribe(ctx context.Context, input *PushSubscribeInput) (*PushSubscribeOutput, error) {
	sessionID, err := sidFromCtx(ctx, "PushHandler.Subscribe")
	if err != nil {
		return nil, err
	}
	var ua *string
	if input.UserAgent != "" {
		ua = &input.UserAgent
	}
	if err := h.svc.Notifications.RegisterPushSubscription(ctx, sessionID,
		input.Body.Endpoint, input.Body.P256dh, input.Body.Auth, ua); err != nil {
		return nil, err
	}
	return &PushSubscribeOutput{}, nil
}

// The caller's registered devices are listed by the sessions endpoint
// (GET /api/v1/auth/sessions), where each session row carries its push state — a
// push subscription is a capability of a session, so there is no separate push
// device list.

// ----- Remove -----

type PushRemoveInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Subscription id to remove"`
}

type PushRemoveOutput struct{}

func (h *Push) Remove(ctx context.Context, input *PushRemoveInput) (*PushRemoveOutput, error) {
	userID, err := userIDFromCtx(ctx, "PushHandler.Remove")
	if err != nil {
		return nil, err
	}
	if err := h.svc.Notifications.RemovePushSubscription(ctx, userID, input.ID); err != nil {
		return nil, err
	}
	return &PushRemoveOutput{}, nil
}

// ----- Test -----

type PushTestInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Subscription id to send a test push to"`
}

type PushTestOutput struct{}

// Test sends the canned diagnostic push to one of the caller's own devices,
// synchronously, so the UI gets an immediate pass/fail. Owner scope is enforced
// by the lookup (a foreign or unknown id is a 404 before any send). If the push
// service reports the endpoint gone, the row is pruned and the caller told to
// re-enable — the device is no longer reachable.
func (h *Push) Test(ctx context.Context, input *PushTestInput) (*PushTestOutput, error) {
	userID, err := userIDFromCtx(ctx, "PushHandler.Test")
	if err != nil {
		return nil, err
	}
	sub, err := h.svc.Notifications.GetPushSubscription(ctx, userID, input.ID)
	if err != nil {
		return nil, err
	}
	if err := h.pushManager.SendTest(ctx, sub); err != nil {
		if errors.Is(err, push.ErrSubscriptionGone) {
			_ = h.svc.Notifications.RemovePushSubscription(ctx, userID, input.ID)
			return nil, apperrors.NotFoundf(
				"push subscription %s is no longer reachable and was removed; re-enable notifications on this device", input.ID).
				Op("PushHandler.Test")
		}
		return nil, err
	}
	return &PushTestOutput{}, nil
}

// ----- Register -----

func (h *Push) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "push-public-key",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications/push/public-key",
		Summary:     "Get the VAPID public key",
		Description: "Returns the server's VAPID application server key, used by the browser to create a push subscription.",
		Tags:        []string{"notifications"},
	}, h.PublicKey)

	huma.Register(api, huma.Operation{
		OperationID:   "push-subscribe",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/push/subscriptions",
		Summary:       "Register a push subscription",
		Description:   "Stores (or refreshes) the caller's Web Push subscription for this browser. Idempotent on the endpoint.",
		Tags:          []string{"notifications"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusUnprocessableEntity},
	}, h.Subscribe)

	huma.Register(api, huma.Operation{
		OperationID:   "push-remove",
		Method:        http.MethodDelete,
		Path:          "/api/v1/notifications/push/subscriptions/{id}",
		Summary:       "Remove a push device",
		Description:   "Removes one of the caller's registered Web Push devices by id. Idempotent.",
		Tags:          []string{"notifications"},
		DefaultStatus: http.StatusNoContent,
	}, h.Remove)

	huma.Register(api, huma.Operation{
		OperationID:   "push-test",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/push/subscriptions/{id}/test",
		Summary:       "Send a test push to a device",
		Description:   "Sends a canned test notification to one of the caller's registered devices to verify it can receive pushes. Prunes and 404s a device the push service reports gone.",
		Tags:          []string{"notifications"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound},
	}, h.Test)
}
