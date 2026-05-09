package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humasse "github.com/danielgtaylor/huma/v2/sse"
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

// ----- Event payload DTOs -----
//
// The huma SSE adapter dispatches the event name from `reflect.TypeOf(msg.Data)`,
// so each named event needs a distinct Go type. Each one is a named alias of
// rawEventBytes (a []byte) with a custom MarshalJSON that emits the bytes
// verbatim — the SSE `data:` line is the payload itself, not an envelope.

type rawEventBytes []byte

func (r rawEventBytes) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return []byte(r), nil
}

func (rawEventBytes) Schema(_ huma.Registry) *huma.Schema {
	// Unconstrained — payload shape varies per emitter.
	return &huma.Schema{}
}

// EventReadyData is emitted on connect. Wire: `{"ok":true}`.
type EventReadyData rawEventBytes

func (e EventReadyData) MarshalJSON() ([]byte, error) { return rawEventBytes(e).MarshalJSON() }
func (e EventReadyData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventPingData is the 15-second heartbeat. Wire: `{"ts": <unix-seconds>}`.
type EventPingData rawEventBytes

func (e EventPingData) MarshalJSON() ([]byte, error) { return rawEventBytes(e).MarshalJSON() }
func (e EventPingData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventDownloadJobsSnapshotData carries the initial download-jobs list on
// connect. Wire: `[]model.DownloadJobWithSummary`.
type EventDownloadJobsSnapshotData rawEventBytes

func (e EventDownloadJobsSnapshotData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventDownloadJobsSnapshotData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventDownloadJobUpdatedData is emitted whenever a download job's state
// changes. Wire: `model.DownloadJobWithSummary`.
type EventDownloadJobUpdatedData rawEventBytes

func (e EventDownloadJobUpdatedData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventDownloadJobUpdatedData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventImportTaskUpdatedData is emitted when an import task changes. The
// broker publishes a null payload; consumers re-fetch the task by id (the id
// is on the SSE `id:` line, not in `data:`).
type EventImportTaskUpdatedData rawEventBytes

func (e EventImportTaskUpdatedData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventImportTaskUpdatedData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventScanStartedData is emitted when a library scan kicks off. Wire:
// `{ scanId, libraryId }`.
type EventScanStartedData rawEventBytes

func (e EventScanStartedData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventScanStartedData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventScanProgressData is emitted periodically while a scan runs. Wire:
// `{ scanId, libraryId, filesSeen, mediaItemsCreated }`.
type EventScanProgressData rawEventBytes

func (e EventScanProgressData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventScanProgressData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventScanCompletedData is emitted when a scan finishes. Wire includes
// totals + duration; see `internal/service/scan.go` for the full shape.
type EventScanCompletedData rawEventBytes

func (e EventScanCompletedData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventScanCompletedData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventScanFailedData is emitted when a scan errors. Wire:
// `{ scanId, libraryId, error }`.
type EventScanFailedData rawEventBytes

func (e EventScanFailedData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventScanFailedData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// ----- Stream -----

type EventsStreamInput struct {
	Types []string `query:"type,explode" doc:"Filter to specific event names; repeatable. Empty = all events."`
}

func (h *Events) Stream(ctx context.Context, input *EventsStreamInput, send humasse.Sender) {
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

	if typeAllowed("ready") {
		_ = send.Data(EventReadyData(`{"ok":true}`))
	}

	if typeAllowed("download_jobs_snapshot") && h.svc != nil {
		jobs, err := h.svc.DownloadJobs.ListWithImportSummary(ctx)
		if err == nil {
			if b, err := json.Marshal(jobs); err == nil {
				_ = send.Data(EventDownloadJobsSnapshotData(b))
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
			if typeAllowed("ping") {
				_ = send.Data(makePingPayload())
			}
		case ev, ok := <-sub:
			if !ok {
				return
			}
			if !typeAllowed(ev.Type) {
				continue
			}
			if err := sendBrokerEvent(send, ev); err != nil {
				return
			}
		}
	}
}

// sendBrokerEvent wraps the broker payload in the right typed struct so the
// huma SSE adapter picks the correct event name from `reflect.TypeOf`.
// Unknown event names are silently dropped — the broker is the source of
// truth, and an unfamiliar event there is a config bug, not a wire concern.
// The broker's id (often a UUID string) isn't carried through: huma's
// Message.ID is an int and we'd rather drop than coerce.
func sendBrokerEvent(send humasse.Sender, ev sse.Event) error {
	switch ev.Type {
	case "download_jobs_snapshot":
		return send(humasse.Message{Data: EventDownloadJobsSnapshotData(ev.Data)})
	case "download_job_updated":
		return send(humasse.Message{Data: EventDownloadJobUpdatedData(ev.Data)})
	case "import_task_updated":
		return send(humasse.Message{Data: EventImportTaskUpdatedData(ev.Data)})
	case "scan_started":
		return send(humasse.Message{Data: EventScanStartedData(ev.Data)})
	case "scan_progress":
		return send(humasse.Message{Data: EventScanProgressData(ev.Data)})
	case "scan_completed":
		return send(humasse.Message{Data: EventScanCompletedData(ev.Data)})
	case "scan_failed":
		return send(humasse.Message{Data: EventScanFailedData(ev.Data)})
	case "ready":
		return send(humasse.Message{Data: EventReadyData(`{"ok":true}`)})
	case "ping":
		return send(humasse.Message{Data: makePingPayload()})
	}
	return nil
}

func makePingPayload() EventPingData {
	return EventPingData([]byte(`{"ts":` + strconv.FormatInt(time.Now().Unix(), 10) + `}`))
}

// ----- Register -----

func (h *Events) RegisterHumachi(api huma.API) {
	humasse.Register(api, huma.Operation{
		OperationID: "events-stream",
		Method:      http.MethodGet,
		Path:        "/api/v1/events",
		Summary:     "Subscribe to server-sent events",
		Description: "Long-lived SSE stream of download / import / scan progress and other real-time events. Each event has an `event` name (one of the discriminated set below), an optional `id`, and a JSON `data` payload.",
		Tags:        []string{"events"},
	}, map[string]any{
		"ready":                  EventReadyData{},
		"ping":                   EventPingData{},
		"download_jobs_snapshot": EventDownloadJobsSnapshotData{},
		"download_job_updated":   EventDownloadJobUpdatedData{},
		"import_task_updated":    EventImportTaskUpdatedData{},
		"scan_started":           EventScanStartedData{},
		"scan_progress":          EventScanProgressData{},
		"scan_completed":         EventScanCompletedData{},
		"scan_failed":            EventScanFailedData{},
	}, h.Stream)
}
