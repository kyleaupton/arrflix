// events.go is the humachi-shaped events handler. It exposes a single
// long-lived Server-Sent Events (SSE) stream that consumers (the FE events
// store, primarily) subscribe to for download / import / scan progress and
// other real-time updates.
//
// The wire shape mirrors the pre-migration Echo implementation: each event
// has an `event` name, an optional `id`, and a JSON `data` payload. The
// huma SSE adapter (huma/v2/sse) dispatches the event name from the *Go
// type* of the payload passed to `send.Data(...)`; we keep one wrapper
// struct per known event name so the OpenAPI spec documents the discriminated
// set of possible events.
//
// Event payloads originate from the in-process sse.Broker as opaque
// json.RawMessage values — services and workers know how to shape their
// own payloads (e.g. `model.DownloadJobWithSummary` for `download_job_updated`)
// and the broker just hauls bytes around. The handler-local wrapper types
// are named aliases of `json.RawMessage` (with custom MarshalJSON + Schema
// implementations) so the wire shape is the raw payload itself, matching
// Echo's pre-migration output exactly. The OpenAPI spec documents each
// event name as a discriminated `oneOf` with an unconstrained `data`
// schema; payload conventions are documented per-type below.
//
// Kept identical to the Echo behavior:
//   - `?type=a&type=b` filters events to the listed names; absent = all.
//   - On connect, a `ready` event is emitted, plus a `download_jobs_snapshot`
//     event with the current ListWithImportSummary result.
//   - A `ping` event fires every 15 seconds as a heartbeat.
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
// One wrapper type per known event name; the huma SSE adapter dispatches
// the event name from `reflect.TypeOf(msg.Data)`, so each named event must
// have a distinct Go type. To preserve the pre-migration wire shape (the
// SSE `data:` line was the payload directly, not an envelope), each type
// is a named json.RawMessage alias with custom MarshalJSON. The schema
// generator sees an unconstrained-object schema via the Schema method —
// payloads are documented in code comments and (where applicable) link to
// the canonical model.* type.
//
// EventReadyData and EventPingData are emitted by the handler itself with
// known shapes; EventDownloadJobsSnapshotData and the rest carry opaque
// JSON published onto the broker by services / workers (e.g.
// model.DownloadJobWithSummary for download_job_updated).

// rawEventBytes is the shared backing for all event-payload alias types.
// Encoding to JSON emits the raw bytes verbatim (no envelope), matching
// the pre-migration Echo wire shape. Schema generation returns an
// unconstrained-object schema since the payload type varies per emitter.
type rawEventBytes []byte

func (r rawEventBytes) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return []byte(r), nil
}

func (rawEventBytes) Schema(_ huma.Registry) *huma.Schema {
	// Unconstrained — payload shape varies per emitter; documented in the
	// per-event-type comment.
	return &huma.Schema{}
}

// EventReadyData is emitted on connect as a "connected" signal. Wire is
// the literal `{"ok":true}`.
type EventReadyData rawEventBytes

func (e EventReadyData) MarshalJSON() ([]byte, error) { return rawEventBytes(e).MarshalJSON() }
func (e EventReadyData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventPingData is the heartbeat payload sent every 15 seconds. Wire is
// `{"ts": <unix-seconds>}`.
type EventPingData rawEventBytes

func (e EventPingData) MarshalJSON() ([]byte, error) { return rawEventBytes(e).MarshalJSON() }
func (e EventPingData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventDownloadJobsSnapshotData carries the initial download-jobs list on
// connect. Wire shape: `[]model.DownloadJobWithSummary`.
type EventDownloadJobsSnapshotData rawEventBytes

func (e EventDownloadJobsSnapshotData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventDownloadJobsSnapshotData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventDownloadJobUpdatedData is emitted whenever a download job's state
// changes. Wire shape: `model.DownloadJobWithSummary`.
type EventDownloadJobUpdatedData rawEventBytes

func (e EventDownloadJobUpdatedData) MarshalJSON() ([]byte, error) {
	return rawEventBytes(e).MarshalJSON()
}
func (e EventDownloadJobUpdatedData) Schema(r huma.Registry) *huma.Schema {
	return rawEventBytes(e).Schema(r)
}

// EventImportTaskUpdatedData is emitted when an import task changes.
// Currently the broker publishes a null payload; consumers re-fetch the
// task by id (the id is on the SSE `id:` line, not in `data:`).
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

// EventsStreamInput carries the optional event-name filter. `?type=a&type=b`
// limits the stream to the listed names; an empty list means "all events".
type EventsStreamInput struct {
	Types []string `query:"type,explode" doc:"Filter to specific event names; repeatable. Empty = all events."`
}

// Stream pumps events from the in-process broker to the connected SSE client.
// Heartbeats every 15 seconds keep proxies happy. The client disconnects by
// closing the connection; the goroutine exits when ctx is canceled.
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

	// Ready event signals "connected" to the FE events store.
	if typeAllowed("ready") {
		_ = send.Data(EventReadyData(`{"ok":true}`))
	}

	// Initial snapshot (convenience for consumers like the downloads page).
	if typeAllowed("download_jobs_snapshot") && h.svc != nil {
		jobs, err := h.svc.DownloadJobs.ListWithImportSummary(ctx)
		if err == nil {
			if b, err := json.Marshal(jobs); err == nil {
				_ = send.Data(EventDownloadJobsSnapshotData(b))
			}
		}
	}

	if h.broker == nil {
		// No broker means nothing to subscribe to; the connection just sits
		// open until the client disconnects. Match the Echo behavior of
		// holding the stream rather than closing.
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

// sendBrokerEvent dispatches a broker.Event onto the SSE wire by wrapping
// its payload in the right typed struct (so the huma SSE adapter picks the
// correct event name from `reflect.TypeOf`). Unknown event names are
// silently dropped — the broker is the source of truth, and an unfamiliar
// event name there is a config bug rather than a wire concern. The id on
// the wire (SSE `id:` line) isn't carried through here; huma's `Message.ID`
// is an int while the broker uses string ids (often a UUID), so we drop it
// rather than coerce. Pre-migration the id was emitted as a string —
// flagging this in the migration report.
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

// makePingPayload builds a fresh `{"ts": <unix-seconds>}` payload.
func makePingPayload() EventPingData {
	return EventPingData([]byte(`{"ts":` + strconv.FormatInt(time.Now().Unix(), 10) + `}`))
}

// ----- Register -----

// RegisterHumachi wires the events stream onto the humachi API. Phase 4
// will delete the Echo registration alongside the rest of the legacy
// handlers; the FE consumes the same `/api/v1/events` URL throughout.
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
