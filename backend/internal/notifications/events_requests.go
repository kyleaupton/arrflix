package notifications

import (
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
)

// Request-review events. Each declares the dedup key request:<id>:<event>, which
// the push adapter uses as the tray tag, so a later notification about the same
// request and event replaces its earlier self.

// RequestPending — audience: admin, bundle: admin_alerts. Emitted when a request
// is created pending (the requester has no auto-approve grant). Deciders is the
// set of active users who may decide the request, resolved by
// NotificationService before the event is built; the requester is never in it.
type RequestPending struct {
	Deciders  []uuid.UUID `json:"-"`
	RequestID uuid.UUID   `json:"requestId"`
	Media     MediaRef    `json:"media"`
	Requester string      `json:"requester"`
}

func (e RequestPending) EventType() string                    { return EventRequestPending }
func (e RequestPending) Audience() model.NotificationAudience { return model.AudienceAdmin }
func (e RequestPending) Bundle() string                       { return BundleAdminAlerts }
func (e RequestPending) Recipients() []uuid.UUID              { return e.Deciders }
func (e RequestPending) DedupKey() string                     { return requestDedupKey(e.RequestID, EventRequestPending) }
func (e RequestPending) Payload() any                         { return e }

// RequestApproved — audience: user, bundle: my_requests. Emitted when an operator
// approves a pending request. An auto-approved request never emits it.
type RequestApproved struct {
	Recipient uuid.UUID `json:"-"`
	RequestID uuid.UUID `json:"requestId"`
	Media     MediaRef  `json:"media"`
}

func (e RequestApproved) EventType() string                    { return EventRequestApproved }
func (e RequestApproved) Audience() model.NotificationAudience { return model.AudienceUser }
func (e RequestApproved) Bundle() string                       { return BundleMyRequests }
func (e RequestApproved) Recipients() []uuid.UUID              { return []uuid.UUID{e.Recipient} }
func (e RequestApproved) DedupKey() string                     { return requestDedupKey(e.RequestID, EventRequestApproved) }
func (e RequestApproved) Payload() any                         { return e }

// RequestDenied — audience: user, bundle: my_requests. Emitted when an operator
// denies a pending request; Reason is the operator's required explanation.
type RequestDenied struct {
	Recipient uuid.UUID `json:"-"`
	RequestID uuid.UUID `json:"requestId"`
	Media     MediaRef  `json:"media"`
	Reason    string    `json:"reason"`
}

func (e RequestDenied) EventType() string                    { return EventRequestDenied }
func (e RequestDenied) Audience() model.NotificationAudience { return model.AudienceUser }
func (e RequestDenied) Bundle() string                       { return BundleMyRequests }
func (e RequestDenied) Recipients() []uuid.UUID              { return []uuid.UUID{e.Recipient} }
func (e RequestDenied) DedupKey() string                     { return requestDedupKey(e.RequestID, EventRequestDenied) }
func (e RequestDenied) Payload() any                         { return e }

// Request event types. The service worker keys its tap target on these, so they
// are part of the push wire contract.
const (
	EventRequestPending  = "request.pending"
	EventRequestApproved = "request.approved"
	EventRequestDenied   = "request.denied"
)

func requestDedupKey(id uuid.UUID, eventType string) string {
	return "request:" + id.String() + ":" + eventType
}

var (
	_ Event = RequestPending{}
	_ Event = RequestApproved{}
	_ Event = RequestDenied{}
)
