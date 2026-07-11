package notifications

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"text/template"

	"github.com/kyleaupton/arrflix/internal/model"
)

//go:embed templates
var templateFS embed.FS

// Registered is every event the system can emit — the authoritative list
// build-time verification walks. Adding an event means adding it here (alongside
// its `var _ Event` assertion); Renderer.Verify then requires templates for it,
// so a new event without templates fails worker startup rather than silently
// never rendering.
var Registered = []Event{
	WantAvailable{},
}

// textParts are the template files a text channel (in_app, push) renders — a
// title and a body, executed independently, since push systems treat them as
// distinct fields. Email's subject/HTML parts differ and are verified separately
// when that channel lands.
var textParts = []string{"title", "body"}

// Renderer holds the parsed notification templates, keyed by
// "<event_path>/<channel>.<part>" (e.g. "want/available/in_app.title"). It is
// built once at startup from the embedded template tree; templates are pure
// functions of an event's payload, so one Renderer is safe to share.
type Renderer struct {
	templates map[string]*template.Template
}

// NewRenderer parses every embedded .tmpl file. A malformed template is a hard
// error the caller turns into a loud startup failure — the spec's "missing or
// bad template is a startup error, not a silent skip."
func NewRenderer() (*Renderer, error) {
	r := &Renderer{templates: map[string]*template.Template{}}
	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".tmpl") {
			return nil
		}
		content, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}
		key := strings.TrimSuffix(strings.TrimPrefix(path, "templates/"), ".tmpl")
		t, err := template.New(key).Parse(string(content))
		if err != nil {
			return fmt.Errorf("parse notification template %q: %w", key, err)
		}
		r.templates[key] = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Render executes the title and body templates for an (event, channel) pair
// against the event's stored JSON payload. The payload decodes to a generic
// structure, so a template references the payload's JSON keys (e.g.
// {{.media.title}}) — exactly the variables the constructor declared. Output is
// space-trimmed so a trailing newline in a template file never leaks into a
// title.
func (r *Renderer) Render(eventType string, ch model.NotificationChannel, payload []byte) (title, body string, err error) {
	var data any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &data); err != nil {
			return "", "", fmt.Errorf("decode %q payload: %w", eventType, err)
		}
	}
	if title, err = r.render(eventType, ch, "title", data); err != nil {
		return "", "", err
	}
	if body, err = r.render(eventType, ch, "body", data); err != nil {
		return "", "", err
	}
	return title, body, nil
}

func (r *Renderer) render(eventType string, ch model.NotificationChannel, part string, data any) (string, error) {
	key := templateKey(eventType, ch, part)
	t, ok := r.templates[key]
	if !ok {
		return "", fmt.Errorf("no notification template %q", key)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render notification template %q: %w", key, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// Verify confirms every event has the text templates each wired channel needs.
// It is the startup guard the spec requires: a registered event missing a
// template for a channel the worker will deliver on fails here, not at first
// delivery. Callers pass the channels their adapters actually serve, so v1
// (in_app only) doesn't demand push/email templates that don't exist yet.
func (r *Renderer) Verify(events []Event, channels []model.NotificationChannel) error {
	var missing []string
	for _, ev := range events {
		for _, ch := range channels {
			for _, part := range textParts {
				if key := templateKey(ev.EventType(), ch, part); r.templates[key] == nil {
					missing = append(missing, key)
				}
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing notification templates: %s", strings.Join(missing, ", "))
	}
	return nil
}

// templateKey maps an event type's dotted identifier to its template path prefix
// ("want.available" → "want/available") and appends the channel and part.
func templateKey(eventType string, ch model.NotificationChannel, part string) string {
	return strings.ReplaceAll(eventType, ".", "/") + "/" + string(ch) + "." + part
}
