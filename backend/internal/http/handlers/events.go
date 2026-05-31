package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
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
	allowed := map[string]bool{}
	for _, t := range input.Types {
		if t != "" {
			allowed[t] = true
		}
	}
	typeAllowed := func(t string) bool {
		if len(allowed) == 0 {
			return true
		}
		return allowed[t]
	}

	emit := func(e realtime.Event) bool {
		return send(streamFrame{ID: e.ID, Event: e.Name, Data: e.Data}) == nil
	}

	if typeAllowed(realtime.NameReady) {
		if !emit(realtime.Ready()) {
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

	if h.broker == nil {
		// Hold the connection open until the client disconnects rather than
		// closing it eagerly when there's nothing to subscribe to.
		<-ctx.Done()
		return
	}

	sub, cancel := h.broker.Subscribe()
	defer cancel()

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
		case ev, ok := <-sub:
			if !ok {
				return
			}
			if !typeAllowed(ev.Type) {
				continue
			}
			// The broker is the source of truth for the wire frame: name, id,
			// and pre-marshaled data map straight through. The realtime
			// constructors already produced this shape at the publish site.
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
