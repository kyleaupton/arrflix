package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// ----- Handler -----

type Events struct {
	broker *sse.Broker
}

func NewEvents(broker *sse.Broker) *Events {
	return &Events{broker: broker}
}

// ----- Stream -----

type EventsStreamInput struct {
	Session     uuid.UUID `query:"session" format:"uuid" doc:"Reattach to a prior session (from the ready event) to resume via Last-Event-ID. Omit for a fresh session."`
	LastEventID string    `header:"Last-Event-ID" doc:"The id of the last event the client received; resumes the stream from just after it within the reattached session."`
}

func (h *Events) Stream(ctx context.Context, input *EventsStreamInput, send streamSender) {
	// Resolve the authenticated user from the JWT subject. The chi JWT
	// middleware should already have rejected an unauthenticated request, so a
	// missing/unparseable subject here is anomalous — close the stream defensively
	// rather than subscribing an unscoped session.
	userID, err := userIDFromContext(ctx, "EventsHandler.Stream")
	if err != nil {
		return
	}

	emit := func(e realtime.Event) bool {
		return send(streamFrame{ID: e.ID, Event: e.Name, Data: e.Data}) == nil
	}

	if h.broker == nil {
		// No broker to scope against: still emit ready (with a freshly allocated
		// session id) so the client captures a session, then hold the connection
		// open until it disconnects.
		if !emit(realtime.Ready(uuid.New().String())) {
			return
		}
		<-ctx.Done()
		return
	}

	// Fresh sessions start with an empty topic set, which the broker treats as
	// "deliver all events this user is eligible for". Page-scoped narrowing
	// happens later via the subscription control plane, not a connect-time
	// filter.
	att := h.broker.Attach(sse.AttachParams{
		SessionID:   input.Session,
		UserID:      userID,
		LastEventID: input.LastEventID,
	})
	defer att.Cancel()

	if !emit(realtime.Ready(att.Session.ID.String())) {
		return
	}

	switch {
	case att.Gapped:
		// The resume point aged out of the replay ring. Tell the client to
		// refetch (its queries reseed from REST) rather than replay.
		if !emit(realtime.ResumeGap()) {
			return
		}
	case len(att.Replay) > 0:
		// Reattach within the window: replay the missed events in order. The
		// client kept its query cache across the reconnect and the replayed
		// deltas bring it current.
		for _, ev := range att.Replay {
			if send(streamFrame{ID: ev.ID, Event: ev.Type, Data: ev.Data}) != nil {
				return
			}
		}
	}
	// A fresh session (or a reattach with nothing missed) just goes live; the
	// client's queries own their REST baseline, so there's no connect-time
	// snapshot to send.

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-att.Kick:
			// Overflow teardown: the outbound channel filled. The dropped
			// events are in the replay ring, so returning here (deferred Cancel
			// detaches, keeping the ring) lets the client reconnect and replay.
			return
		case <-heartbeat.C:
			if !emit(realtime.Ping()) {
				return
			}
		case ev, ok := <-att.Out:
			if !ok {
				return
			}
			// The broker already filtered this event by recipient and the
			// session's topic set; name, id, and pre-marshaled data map straight
			// through to the wire frame.
			if send(streamFrame{ID: ev.ID, Event: ev.Type, Data: ev.Data}) != nil {
				return
			}
		}
	}
}

// ----- Subscriptions: shared -----

// The session handle travels in the X-Realtime-Session header on every
// control-plane request (not a cookie — cookies leak across tabs, and each tab
// is its own session). It is declared as a direct field on each Input below:
// huma extracts header parameters from direct fields, not from an embedded
// shared struct.

// canSubscribe is the subscribe-time eligibility seam. Today any authenticated
// user may subscribe to any topic. Per-resource scoping (e.g. subscribing to
// "scan_progress:library_42" requires library.read on library 42) lands with
// the dotted-topic + scope work; that check goes here. When it becomes
// reachable, enumerate errsForbidden on the add operation.
func canSubscribe(_ uuid.UUID, _ string) bool { return true }

// ----- Subscriptions: list -----

type EventsSubscriptionsListInput struct {
	SessionID uuid.UUID `header:"X-Realtime-Session" required:"true" format:"uuid" doc:"Session id from the stream's ready event."`
}

type EventsSubscriptionsListOutput struct {
	Body struct {
		Topics []string `json:"topics" doc:"The session's current topic filter. Empty means all events."`
	}
}

// SubscriptionsList returns the session's current topic set so a reconnecting
// client can resync rather than blindly re-POST its subscriptions.
func (h *Events) SubscriptionsList(ctx context.Context, input *EventsSubscriptionsListInput) (*EventsSubscriptionsListOutput, error) {
	userID, err := userIDFromContext(ctx, "EventsHandler.SubscriptionsList")
	if err != nil {
		return nil, err
	}
	topics, ok := h.topicsForUser(input.SessionID, userID)
	if !ok {
		return nil, apperrors.NotFoundf("session not found").Op("EventsHandler.SubscriptionsList")
	}
	out := &EventsSubscriptionsListOutput{}
	out.Body.Topics = topics
	return out, nil
}

// ----- Subscriptions: add -----

type EventsSubscriptionsAddInput struct {
	SessionID uuid.UUID `header:"X-Realtime-Session" required:"true" format:"uuid" doc:"Session id from the stream's ready event."`
	Body      struct {
		Topics []string `json:"topics" required:"true" minItems:"1" doc:"Topics to add to the session's filter."`
	}
}

type EventsSubscriptionsAddOutput struct{}

// SubscriptionsAdd admits topics to the session's filter (after the
// subscribe-time eligibility check). Returns 204: the client seeds its state
// from the relevant query's REST baseline, not from a snapshot on this
// response.
func (h *Events) SubscriptionsAdd(ctx context.Context, input *EventsSubscriptionsAddInput) (*EventsSubscriptionsAddOutput, error) {
	userID, err := userIDFromContext(ctx, "EventsHandler.SubscriptionsAdd")
	if err != nil {
		return nil, err
	}

	for _, topic := range input.Body.Topics {
		if !canSubscribe(userID, topic) {
			return nil, apperrors.Forbiddenf("not eligible to subscribe to %q", topic).Op("EventsHandler.SubscriptionsAdd")
		}
	}

	if h.broker == nil || !h.broker.AddTopics(input.SessionID, userID, input.Body.Topics) {
		return nil, apperrors.NotFoundf("session not found").Op("EventsHandler.SubscriptionsAdd")
	}

	return &EventsSubscriptionsAddOutput{}, nil
}

// ----- Subscriptions: remove -----

type EventsSubscriptionsRemoveInput struct {
	SessionID uuid.UUID `header:"X-Realtime-Session" required:"true" format:"uuid" doc:"Session id from the stream's ready event."`
	Topic     string    `path:"topic" doc:"Topic to drop from the session's filter."`
}

type EventsSubscriptionsRemoveOutput struct{}

// SubscriptionsRemove drops a topic from the session's filter. Removing a topic
// the session never held is a no-op (still 204) — only a missing/foreign
// session is a 404.
func (h *Events) SubscriptionsRemove(ctx context.Context, input *EventsSubscriptionsRemoveInput) (*EventsSubscriptionsRemoveOutput, error) {
	userID, err := userIDFromContext(ctx, "EventsHandler.SubscriptionsRemove")
	if err != nil {
		return nil, err
	}
	if h.broker == nil || !h.broker.RemoveTopic(input.SessionID, userID, input.Topic) {
		return nil, apperrors.NotFoundf("session not found").Op("EventsHandler.SubscriptionsRemove")
	}
	return &EventsSubscriptionsRemoveOutput{}, nil
}

// topicsForUser fetches a session's topics, treating a nil broker as
// "not found" so the control plane degrades the same way the stream does.
func (h *Events) topicsForUser(sessionID, userID uuid.UUID) ([]string, bool) {
	if h.broker == nil {
		return nil, false
	}
	return h.broker.Topics(sessionID, userID)
}

// ----- Register -----

func (h *Events) RegisterHumachi(api huma.API) {
	registerEventStream(api, huma.Operation{
		OperationID: "events-stream",
		Method:      http.MethodGet,
		Path:        "/api/v1/events",
		Summary:     "Subscribe to server-sent events",
		Description: "Long-lived SSE stream of download / import / scan progress and other real-time events. Each event has an `event` name (one of the discriminated set below), an `id`, and a JSON `data` payload.",
		Tags:        []string{"events"},
	}, h.Stream)

	huma.Register(api, huma.Operation{
		OperationID: "events-subscriptions-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/events/subscriptions",
		Summary:     "List the session's subscribed topics",
		Description: "Returns the current topic filter for the session identified by the X-Realtime-Session header, so a reconnecting client can resync.",
		Tags:        []string{"events"},
		Errors:      errsRead,
	}, h.SubscriptionsList)

	huma.Register(api, huma.Operation{
		OperationID:   "events-subscriptions-add",
		Method:        http.MethodPost,
		Path:          "/api/v1/events/subscriptions",
		Summary:       "Subscribe the session to topics",
		Description:   "Adds topics to the session's filter. The client seeds state from the relevant query's REST baseline, so this returns 204.",
		Tags:          []string{"events"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errs(errsRead, []int{http.StatusUnprocessableEntity}),
	}, h.SubscriptionsAdd)

	huma.Register(api, huma.Operation{
		OperationID:   "events-subscriptions-remove",
		Method:        http.MethodDelete,
		Path:          "/api/v1/events/subscriptions/{topic}",
		Summary:       "Unsubscribe the session from a topic",
		Tags:          []string{"events"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsRead,
	}, h.SubscriptionsRemove)
}
