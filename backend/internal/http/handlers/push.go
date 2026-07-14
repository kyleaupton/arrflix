package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/kyleaupton/arrflix/internal/push"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// Push is the Web Push subscription surface: the caller fetches the server's
// VAPID public key, then registers or removes the push subscription for the
// browser they are on. Every write is scoped to the caller's user id (resolved
// from the request context), so the authz gate classifies these as
// authenticated-only — no permission key.
//
// The public key comes straight from the push Manager (the VAPID identity),
// mirroring how EmailProvider reads its transport Manager directly; subscription
// storage goes through NotificationService.
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
	userID, err := userIDFromCtx(ctx, "PushHandler.Subscribe")
	if err != nil {
		return nil, err
	}
	var ua *string
	if input.UserAgent != "" {
		ua = &input.UserAgent
	}
	if err := h.svc.Notifications.RegisterPushSubscription(ctx, userID,
		input.Body.Endpoint, input.Body.P256dh, input.Body.Auth, ua); err != nil {
		return nil, err
	}
	return &PushSubscribeOutput{}, nil
}

// ----- Unsubscribe -----

type pushUnsubscribeBody struct {
	Endpoint string `json:"endpoint" required:"true" minLength:"1" doc:"Push service endpoint URL to remove"`
}

type PushUnsubscribeInput struct {
	Body pushUnsubscribeBody
}

type PushUnsubscribeOutput struct{}

func (h *Push) Unsubscribe(ctx context.Context, input *PushUnsubscribeInput) (*PushUnsubscribeOutput, error) {
	userID, err := userIDFromCtx(ctx, "PushHandler.Unsubscribe")
	if err != nil {
		return nil, err
	}
	if err := h.svc.Notifications.UnregisterPushSubscription(ctx, userID, input.Body.Endpoint); err != nil {
		return nil, err
	}
	return &PushUnsubscribeOutput{}, nil
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
		OperationID:   "push-unsubscribe",
		Method:        http.MethodDelete,
		Path:          "/api/v1/notifications/push/subscriptions",
		Summary:       "Remove a push subscription",
		Description:   "Removes the caller's Web Push subscription for the given endpoint. Idempotent.",
		Tags:          []string{"notifications"},
		DefaultStatus: http.StatusNoContent,
	}, h.Unsubscribe)
}
