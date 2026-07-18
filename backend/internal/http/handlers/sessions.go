package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// Sessions is the caller's own device/login management surface: list every active
// session (each carrying its push state), revoke one ("log out that device"), or
// revoke all but the current ("log out other devices"). Every operation is scoped
// to the caller (user id + session id from the token), so the authz gate
// classifies these as authenticated-only — no permission key.
type Sessions struct {
	svc *service.Services
}

func NewSessions(s *service.Services) *Sessions { return &Sessions{svc: s} }

// ----- List -----

// sessionView is the caller-facing shape of one device/login. It omits the refresh
// hashes (a session's secrets) entirely — those never leave the server. The push
// fields surface the device's push capability so the unified devices UI renders
// logins and push in one list.
type sessionView struct {
	ID                 uuid.UUID  `json:"id" format:"uuid" doc:"Session id"`
	IsCurrent          bool       `json:"isCurrent" doc:"Whether this is the session the request was made from"`
	UserAgent          *string    `json:"userAgent" doc:"Device label captured at login (User-Agent), null if unknown"`
	Label              *string    `json:"label" doc:"User-set device label, null if unset"`
	CreatedAt          time.Time  `json:"createdAt" doc:"When this session was created (login)"`
	LastUsedAt         time.Time  `json:"lastUsedAt" doc:"When this session last refreshed"`
	PushEnabled        bool       `json:"pushEnabled" doc:"Whether push notifications are enabled on this device"`
	PushSubscriptionID *uuid.UUID `json:"pushSubscriptionId,omitempty" format:"uuid" doc:"Push subscription id for this device; use it to disable or test push. Null when push is off"`
	PushLastNotifiedAt *time.Time `json:"pushLastNotifiedAt,omitempty" doc:"When a push was last delivered to this device; null if never"`
}

type SessionsListInput struct{}

type SessionsListOutput struct {
	Body []sessionView
}

func (h *Sessions) List(ctx context.Context, _ *SessionsListInput) (*SessionsListOutput, error) {
	userID, err := userIDFromCtx(ctx, "SessionsHandler.List")
	if err != nil {
		return nil, err
	}
	// sid flags the current row; a token without one still lists (nothing current).
	currentSID, _ := sidFromCtx(ctx, "SessionsHandler.List")

	devices, err := h.svc.Sessions.ListDevicesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]sessionView, 0, len(devices))
	for _, d := range devices {
		views = append(views, sessionView{
			ID:                 d.ID,
			IsCurrent:          d.ID == currentSID,
			UserAgent:          d.UserAgent,
			Label:              d.Label,
			CreatedAt:          d.CreatedAt,
			LastUsedAt:         d.LastUsedAt,
			PushEnabled:        d.PushSubscriptionID != nil,
			PushSubscriptionID: d.PushSubscriptionID,
			PushLastNotifiedAt: d.PushLastNotifiedAt,
		})
	}
	return &SessionsListOutput{Body: views}, nil
}

// ----- Revoke -----

type SessionsRevokeInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Session id to revoke"`
}

type SessionsRevokeOutput struct{}

func (h *Sessions) Revoke(ctx context.Context, input *SessionsRevokeInput) (*SessionsRevokeOutput, error) {
	userID, err := userIDFromCtx(ctx, "SessionsHandler.Revoke")
	if err != nil {
		return nil, err
	}
	if err := h.svc.Sessions.RevokeForUser(ctx, userID, input.ID); err != nil {
		return nil, err
	}
	return &SessionsRevokeOutput{}, nil
}

// ----- Revoke others -----

type SessionsRevokeAllInput struct{}

type SessionsRevokeAllOutput struct{}

// RevokeAll logs out every other device, keeping the caller's current session. A
// token without a sid revokes nothing (there's no current session to preserve) and
// is a 401 rather than a surprise full logout.
func (h *Sessions) RevokeAll(ctx context.Context, _ *SessionsRevokeAllInput) (*SessionsRevokeAllOutput, error) {
	userID, err := userIDFromCtx(ctx, "SessionsHandler.RevokeAll")
	if err != nil {
		return nil, err
	}
	currentSID, err := sidFromCtx(ctx, "SessionsHandler.RevokeAll")
	if err != nil {
		return nil, err
	}
	if _, err := h.svc.Sessions.RevokeOthersForUser(ctx, userID, currentSID); err != nil {
		return nil, err
	}
	return &SessionsRevokeAllOutput{}, nil
}

// ----- Register -----

func (h *Sessions) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "sessions-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/sessions",
		Summary:     "List my sessions",
		Description: "Returns the caller's active sessions (devices), each with its push state. The current session is flagged isCurrent.",
		Tags:        []string{"auth"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID:   "sessions-revoke",
		Method:        http.MethodDelete,
		Path:          "/api/v1/auth/sessions/{id}",
		Summary:       "Revoke a session",
		Description:   "Logs out one of the caller's own devices by session id. 404 if the session isn't the caller's.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsRead,
	}, h.Revoke)

	huma.Register(api, huma.Operation{
		OperationID:   "sessions-revoke-all",
		Method:        http.MethodDelete,
		Path:          "/api/v1/auth/sessions",
		Summary:       "Log out other devices",
		Description:   "Revokes every active session except the caller's current one.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, h.RevokeAll)
}
