package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// Notifications is the bell-icon surface: a user reads and clears their own
// delivered in_app notifications. Every operation is scoped to the caller's user
// id (resolved from the request context, enforced in the repo), so the authz gate
// classifies them as authenticated-only — no permission key.
type Notifications struct{ svc *service.Services }

func NewNotifications(s *service.Services) *Notifications { return &Notifications{svc: s} }

// ----- List -----

type NotificationsListInput struct {
	Limit int32 `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Max notifications to return, newest first"`
}

type NotificationsListOutput struct {
	Body []model.InboxNotification
}

func (h *Notifications) List(ctx context.Context, input *NotificationsListInput) (*NotificationsListOutput, error) {
	userID, err := userIDFromCtx(ctx, "NotificationsHandler.List")
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Notifications.Inbox(ctx, userID, input.Limit)
	if err != nil {
		return nil, err
	}
	return &NotificationsListOutput{Body: out}, nil
}

// ----- Unread count -----

type NotificationsUnreadInput struct{}

type notificationsUnreadBody struct {
	Count int64 `json:"count" doc:"Delivered-but-unread in_app notification count"`
}

type NotificationsUnreadOutput struct {
	Body notificationsUnreadBody
}

func (h *Notifications) UnreadCount(ctx context.Context, _ *NotificationsUnreadInput) (*NotificationsUnreadOutput, error) {
	userID, err := userIDFromCtx(ctx, "NotificationsHandler.UnreadCount")
	if err != nil {
		return nil, err
	}
	n, err := h.svc.Notifications.UnreadCount(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &NotificationsUnreadOutput{Body: notificationsUnreadBody{Count: n}}, nil
}

// ----- Mark read -----

type NotificationsMarkReadInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Notification ID"`
}

type NotificationsMarkReadOutput struct{}

func (h *Notifications) MarkRead(ctx context.Context, input *NotificationsMarkReadInput) (*NotificationsMarkReadOutput, error) {
	userID, err := userIDFromCtx(ctx, "NotificationsHandler.MarkRead")
	if err != nil {
		return nil, err
	}
	if err := h.svc.Notifications.MarkRead(ctx, input.ID, userID); err != nil {
		return nil, err
	}
	return &NotificationsMarkReadOutput{}, nil
}

// ----- Mark all read -----

type NotificationsMarkAllReadInput struct{}

type NotificationsMarkAllReadOutput struct{}

func (h *Notifications) MarkAllRead(ctx context.Context, _ *NotificationsMarkAllReadInput) (*NotificationsMarkAllReadOutput, error) {
	userID, err := userIDFromCtx(ctx, "NotificationsHandler.MarkAllRead")
	if err != nil {
		return nil, err
	}
	if err := h.svc.Notifications.MarkAllRead(ctx, userID); err != nil {
		return nil, err
	}
	return &NotificationsMarkAllReadOutput{}, nil
}

// ----- Preferences (get) -----

type NotificationsPrefsGetInput struct{}

type notificationsPrefsBody struct {
	Bundles []model.BundlePreference `json:"bundles" doc:"Per-bundle channel preferences, one entry per user-facing bundle"`
}

type NotificationsPrefsGetOutput struct {
	Body notificationsPrefsBody
}

func (h *Notifications) PreferencesGet(ctx context.Context, _ *NotificationsPrefsGetInput) (*NotificationsPrefsGetOutput, error) {
	userID, err := userIDFromCtx(ctx, "NotificationsHandler.PreferencesGet")
	if err != nil {
		return nil, err
	}
	bundles, err := h.svc.Notifications.Preferences(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Fill per-channel deliverability. in_app is always available; email delivers
	// only when an enabled SMTP provider is configured. Reading the provider here
	// is an internal service call (not the admin-gated HTTP op), so a non-admin
	// learns whether email can deliver without seeing the config itself.
	cfg, found, err := h.svc.EmailProvider.Get(ctx)
	if err != nil {
		return nil, err
	}
	emailAvailable := found && cfg.Enabled
	for i := range bundles {
		for j := range bundles[i].Channels {
			switch bundles[i].Channels[j].Channel {
			case string(model.ChannelInApp):
				bundles[i].Channels[j].Available = true
			case string(model.ChannelEmail):
				bundles[i].Channels[j].Available = emailAvailable
			case string(model.ChannelPush):
				// Push self-configures (the VAPID keypair always exists), so the
				// channel can always deliver — to zero devices is a no-op, just
				// like in_app when the bell is never opened. Whether this browser
				// is subscribed is surfaced separately by the devices card.
				bundles[i].Channels[j].Available = true
			}
		}
	}

	return &NotificationsPrefsGetOutput{Body: notificationsPrefsBody{Bundles: bundles}}, nil
}

// ----- Preferences (set) -----

type notificationsPrefsSetBody struct {
	Scope   string `json:"scope" required:"true" minLength:"1" doc:"Preference scope; v1 accepts 'bundle' only"`
	Value   string `json:"value" required:"true" minLength:"1" doc:"Bundle name the toggle applies to"`
	Channel string `json:"channel" required:"true" minLength:"1" doc:"Deliverable channel: 'in_app' or 'email'"`
	Enabled bool   `json:"enabled" doc:"Whether the channel is enabled for the bundle"`
}

type NotificationsPrefsSetInput struct {
	Body notificationsPrefsSetBody
}

type NotificationsPrefsSetOutput struct{}

func (h *Notifications) PreferencesSet(ctx context.Context, input *NotificationsPrefsSetInput) (*NotificationsPrefsSetOutput, error) {
	userID, err := userIDFromCtx(ctx, "NotificationsHandler.PreferencesSet")
	if err != nil {
		return nil, err
	}
	if err := h.svc.Notifications.SetPreference(ctx, userID,
		input.Body.Scope, input.Body.Value, input.Body.Channel, input.Body.Enabled); err != nil {
		return nil, err
	}
	return &NotificationsPrefsSetOutput{}, nil
}

// ----- Register -----

func (h *Notifications) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "notifications-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications",
		Summary:     "List my notifications",
		Description: "Returns the caller's delivered in-app notifications, newest first, each with its title and body rendered from the event template.",
		Tags:        []string{"notifications"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "notifications-unread-count",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications/unread-count",
		Summary:     "Count my unread notifications",
		Tags:        []string{"notifications"},
	}, h.UnreadCount)

	huma.Register(api, huma.Operation{
		OperationID:   "notifications-mark-read",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/{id}/read",
		Summary:       "Mark a notification read",
		Description:   "Marks one of the caller's notifications read. Idempotent, and a no-op for an id that isn't the caller's.",
		Tags:          []string{"notifications"},
		DefaultStatus: http.StatusNoContent,
	}, h.MarkRead)

	huma.Register(api, huma.Operation{
		OperationID:   "notifications-mark-all-read",
		Method:        http.MethodPost,
		Path:          "/api/v1/notifications/read-all",
		Summary:       "Mark all my notifications read",
		Tags:          []string{"notifications"},
		DefaultStatus: http.StatusNoContent,
	}, h.MarkAllRead)

	huma.Register(api, huma.Operation{
		OperationID: "notification-preferences-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications/preferences",
		Summary:     "Get my notification preferences",
		Description: "Returns the caller's per-bundle channel preferences (resolved enablement) plus whether each channel can deliver right now.",
		Tags:        []string{"notifications"},
	}, h.PreferencesGet)

	huma.Register(api, huma.Operation{
		OperationID:   "notification-preferences-set",
		Method:        http.MethodPut,
		Path:          "/api/v1/notifications/preferences",
		Summary:       "Set a notification preference",
		Description:   "Toggles one channel for one bundle for the caller. v1 accepts bundle-scope writes only.",
		Tags:          []string{"notifications"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusUnprocessableEntity},
	}, h.PreferencesSet)
}
