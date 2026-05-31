package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// ----- Handler -----

type Events struct {
	svc    *service.Services
	broker *sse.Broker
}

func NewEvents(s *service.Services, broker *sse.Broker) *Events {
	return &Events{svc: s, broker: broker}
}

// ----- Stream -----

type EventsStreamInput struct {
	Types []string `query:"type,explode" doc:"Filter to specific event names; repeatable. Empty = all events."`
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

	typeAllowed := func(t string) bool {
		if len(input.Types) == 0 {
			return true
		}
		for _, allowed := range input.Types {
			if allowed == t {
				return true
			}
		}
		return false
	}

	emit := func(e realtime.Event) bool {
		return send(streamFrame{ID: e.ID, Event: e.Name, Data: e.Data}) == nil
	}

	if h.broker == nil {
		// No broker to scope against: still emit ready (with a freshly allocated
		// session id) and the snapshot for parity, then hold the connection open
		// until the client disconnects.
		if typeAllowed(realtime.NameReady) {
			if !emit(realtime.Ready(uuid.New().String())) {
				return
			}
		}
		if typeAllowed(realtime.NameDownloadJobsSnapshot) && h.svc != nil {
			if jobs, err := h.svc.DownloadJobs.ListWithImportSummary(ctx); err == nil {
				if !emit(realtime.DownloadJobsSnapshot(jobs)) {
					return
				}
			}
		}
		<-ctx.Done()
		return
	}

	session, cancel := h.broker.Subscribe(sse.SubscribeParams{
		UserID: userID,
		Topics: input.Types,
	})
	defer cancel()

	if typeAllowed(realtime.NameReady) {
		if !emit(realtime.Ready(session.ID.String())) {
			return
		}
	}

	// Connect-time download-jobs snapshot stays here for parity; a later phase
	// moves snapshots onto the subscribe REST response.
	if typeAllowed(realtime.NameDownloadJobsSnapshot) && h.svc != nil {
		jobs, err := h.svc.DownloadJobs.ListWithImportSummary(ctx)
		if err == nil {
			if !emit(realtime.DownloadJobsSnapshot(jobs)) {
				return
			}
		}
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if typeAllowed(realtime.NamePing) {
				if !emit(realtime.Ping()) {
					return
				}
			}
		case ev, ok := <-session.Events():
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
}
