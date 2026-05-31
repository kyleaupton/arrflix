package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/realtime"
)

// streamWriteTimeout bounds a single SSE frame write. Forked from humasse's
// WriteTimeout (5s); a stuck client write trips the deadline rather than
// pinning the handler goroutine forever.
const streamWriteTimeout = 5 * time.Second

type unwrapper interface {
	Unwrap() http.ResponseWriter
}

type writeDeadliner interface {
	SetWriteDeadline(time.Time) error
}

// streamFrame is one SSE message destined for the wire. Unlike humasse's
// Message, the event name is an explicit field (no reflect.TypeOf dispatch)
// and the id is a sortable string (not an int).
type streamFrame struct {
	ID    string
	Event string
	Data  []byte
}

// streamSender writes SSE frames to a single connected client.
type streamSender func(streamFrame) error

// registerEventStream registers the SSE GET operation on api. It is a fork of
// humasse.Register, kept close to the Events handler because it is
// huma-coupled. Differences from humasse that matter:
//
//   - id is typed string (humasse: integer) — carries the sortable UUIDv7
//     Last-Event-ID resume needs.
//   - the event name is read from streamFrame.Event (humasse: reflect.TypeOf
//     of the data value), so events don't each need a distinct Go type.
//   - the handler sets X-Accel-Buffering / Cache-Control response headers
//     (humasse sets only Content-Type before handing off, with no hook).
//   - the per-event data schema is derived from the realtime.Registry payload
//     samples rather than a caller-supplied eventTypeMap.
//
// What it keeps from humasse: the OpenAPI oneOf-array schema construction, the
// flusher + SetWriteDeadline plumbing (the Unwrap() chain walk), and the
// public-only huma surface (huma.Register, huma.StreamResponse,
// ctx.BodyWriter, ctx.SetHeader).
func registerEventStream(api huma.API, op huma.Operation, handler func(ctx context.Context, input *EventsStreamInput, send streamSender)) {
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	if op.Responses["200"] == nil {
		op.Responses["200"] = &huma.Response{}
	}
	if op.Responses["200"].Content == nil {
		op.Responses["200"].Content = map[string]*huma.MediaType{}
	}

	dataSchemas := make([]*huma.Schema, 0, len(realtime.Registry))
	for _, ev := range realtime.Registry {
		name := ev.Name
		s := &huma.Schema{
			Title: "Event " + name,
			Type:  huma.TypeObject,
			Properties: map[string]*huma.Schema{
				"id": {
					Type:        huma.TypeString,
					Description: "The event ID (sortable; used for Last-Event-ID resume).",
				},
				"event": {
					Type:        huma.TypeString,
					Description: "The event name.",
					Extensions: map[string]any{
						"const": name,
					},
				},
				"data": api.OpenAPI().Components.Schemas.Schema(
					reflect.TypeOf(ev.PayloadSample), true, name,
				),
				"retry": {
					Type:        huma.TypeInteger,
					Description: "The retry time in milliseconds.",
				},
			},
			Required: []string{"data", "event"},
		}
		dataSchemas = append(dataSchemas, s)
	}

	slices.SortFunc(dataSchemas, func(a, b *huma.Schema) int {
		return strings.Compare(a.Title, b.Title)
	})

	op.Responses["200"].Content["text/event-stream"] = &huma.MediaType{
		Schema: &huma.Schema{
			Title:       "Server Sent Events",
			Description: "Each oneOf object in the array represents one possible Server Sent Events (SSE) message, serialized as UTF-8 text according to the SSE specification.",
			Type:        huma.TypeArray,
			Items: &huma.Schema{
				Extensions: map[string]any{
					"oneOf": dataSchemas,
				},
			},
		},
	}

	huma.Register(api, op, func(ctx context.Context, input *EventsStreamInput) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{
			Body: func(hctx huma.Context) {
				hctx.SetHeader("Content-Type", "text/event-stream")
				hctx.SetHeader("Cache-Control", "no-cache")
				// Belt-and-suspenders for nginx setups the user hasn't
				// customized: disables proxy buffering on this response.
				hctx.SetHeader("X-Accel-Buffering", "no")

				bw := hctx.BodyWriter()

				flusher := unwrapTo[http.Flusher](bw)
				deadliner := unwrapTo[writeDeadliner](bw)

				send := func(frame streamFrame) error {
					if deadliner != nil {
						if err := deadliner.SetWriteDeadline(time.Now().Add(streamWriteTimeout)); err != nil {
							fmt.Fprintf(os.Stderr, "warning: unable to set write deadline: %v\n", err)
						}
					}

					if frame.ID != "" {
						if _, err := fmt.Fprintf(bw, "id: %s\n", frame.ID); err != nil {
							return err
						}
					}
					if frame.Event != "" {
						if _, err := fmt.Fprintf(bw, "event: %s\n", frame.Event); err != nil {
							return err
						}
					}
					if _, err := bw.Write([]byte("data: ")); err != nil {
						return err
					}
					if _, err := bw.Write(frame.Data); err != nil {
						return err
					}
					if _, err := bw.Write([]byte("\n\n")); err != nil {
						return err
					}

					if flusher == nil {
						return fmt.Errorf("unable to flush: %w", http.ErrNotSupported)
					}
					flusher.Flush()
					return nil
				}

				handler(hctx.Context(), input, send)
			},
		}, nil
	})
}

// unwrapTo walks the ResponseWriter's Unwrap() chain looking for one that
// satisfies T. Mirrors humasse's flusher/deadliner discovery.
func unwrapTo[T any](w any) T {
	for {
		if t, ok := w.(T); ok {
			return t
		}
		u, ok := w.(unwrapper)
		if !ok {
			var zero T
			return zero
		}
		w = u.Unwrap()
	}
}
