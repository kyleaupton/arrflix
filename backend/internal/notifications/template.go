package notifications

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
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
// distinct fields.
var textParts = []string{"title", "body"}

// emailParts are the template files the email channel renders: a plain-text
// subject line and an auto-escaped HTML body. "subject" lives in the text map
// (no escaping needed); "body.html" lives in the html map (its key ends in
// ".html", which hasTemplate/render routing keys on). The parts differ from a
// text channel's title/body, so Verify demands them separately.
var emailParts = []string{"subject", "body.html"}

// Renderer holds the parsed notification templates, keyed by
// "<event_path>/<channel>.<part>" (e.g. "want/available/in_app.title"). It is
// built once at startup from the embedded template tree; templates are pure
// functions of an event's payload, so one Renderer is safe to share.
//
// Text templates (title, body, email subject) parse with text/template;
// email HTML bodies parse with html/template so a payload value landing in an
// HTML context is contextually auto-escaped. The two maps share the key scheme;
// a key ending in ".html" belongs to htmlTemplates.
type Renderer struct {
	templates     map[string]*template.Template
	htmlTemplates map[string]*htmltemplate.Template
}

// NewRenderer parses every embedded .tmpl file. A malformed template is a hard
// error the caller turns into a loud startup failure — the spec's "missing or
// bad template is a startup error, not a silent skip."
func NewRenderer() (*Renderer, error) {
	r := &Renderer{
		templates:     map[string]*template.Template{},
		htmlTemplates: map[string]*htmltemplate.Template{},
	}
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
		// A ".html.tmpl" file is an HTML body: parse with html/template so its
		// payload interpolations are contextually auto-escaped. Everything else
		// (title, body, email subject) is text/template.
		if strings.HasSuffix(path, ".html.tmpl") {
			t, err := htmltemplate.New(key).Parse(string(content))
			if err != nil {
				return fmt.Errorf("parse notification html template %q: %w", key, err)
			}
			r.htmlTemplates[key] = t
			return nil
		}
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

// MustNewRenderer is NewRenderer for a non-fallible construction path (service
// wiring, where the surrounding constructor returns no error). A parse failure is
// a programmer error in embedded, build-time-fixed templates — deterministic, not
// environmental — so it panics at startup, mirroring text/template's Must. The
// worker uses the fallible NewRenderer because it pairs parsing with a
// channel-composition Verify that legitimately returns an error.
func MustNewRenderer() *Renderer {
	r, err := NewRenderer()
	if err != nil {
		panic(err)
	}
	return r
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

// RenderEmail executes the email subject (text) and HTML body (auto-escaped)
// templates for an event against its stored JSON payload. The subject comes
// from the text map (<event>/email.subject); the body from the html map
// (<event>/email.body.html), where html/template contextually escapes any
// payload value so a title containing < or & can't break the markup.
func (r *Renderer) RenderEmail(eventType string, payload []byte) (subject, htmlBody string, err error) {
	var data any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &data); err != nil {
			return "", "", fmt.Errorf("decode %q payload: %w", eventType, err)
		}
	}
	if subject, err = r.render(eventType, model.ChannelEmail, "subject", data); err != nil {
		return "", "", err
	}
	if htmlBody, err = r.renderHTML(eventType, model.ChannelEmail, "body.html", data); err != nil {
		return "", "", err
	}
	return subject, htmlBody, nil
}

func (r *Renderer) renderHTML(eventType string, ch model.NotificationChannel, part string, data any) (string, error) {
	key := templateKey(eventType, ch, part)
	t, ok := r.htmlTemplates[key]
	if !ok {
		return "", fmt.Errorf("no notification html template %q", key)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render notification html template %q: %w", key, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// hasTemplate reports whether a parsed template exists for a key, routing to the
// html map for ".html" keys (email bodies) and the text map otherwise. Verify
// uses it so one loop can span both maps.
func (r *Renderer) hasTemplate(key string) bool {
	if strings.HasSuffix(key, ".html") {
		return r.htmlTemplates[key] != nil
	}
	return r.templates[key] != nil
}

// Verify confirms every event has the templates each wired channel needs. It is
// the startup guard the spec requires: a registered event missing a template for
// a channel the worker will deliver on fails here, not at first delivery.
// Callers pass the channels their adapters actually serve, so a channel with no
// adapter wired (push in v1) never demands templates that don't exist yet. The
// email channel requires a subject + HTML body; other channels a title + body.
func (r *Renderer) Verify(events []Event, channels []model.NotificationChannel) error {
	var missing []string
	for _, ev := range events {
		for _, ch := range channels {
			parts := textParts
			if ch == model.ChannelEmail {
				parts = emailParts
			}
			for _, part := range parts {
				if key := templateKey(ev.EventType(), ch, part); !r.hasTemplate(key) {
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

// VerifyTransactionalEmail confirms every email-only transactional event type has
// its email subject (text) and HTML body (html) templates. It is the transactional
// analogue of Verify: RegisteredTransactionalEmail events have no in_app/push face,
// so only the email parts are demanded. A missing part is a loud startup error,
// same as Verify — a transactional email that can't render is a silently-dropped
// invite/reset, which is worse than a boot failure.
func (r *Renderer) VerifyTransactionalEmail(eventTypes []string) error {
	var missing []string
	for _, et := range eventTypes {
		for _, part := range emailParts {
			if key := templateKey(et, model.ChannelEmail, part); !r.hasTemplate(key) {
				missing = append(missing, key)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing transactional email templates: %s", strings.Join(missing, ", "))
	}
	return nil
}

// templateKey maps an event type's dotted identifier to its template path prefix
// ("want.available" → "want/available") and appends the channel and part.
func templateKey(eventType string, ch model.NotificationChannel, part string) string {
	return strings.ReplaceAll(eventType, ".", "/") + "/" + string(ch) + "." + part
}
