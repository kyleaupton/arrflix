package notifications

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
)

// TestRenderer_Render proves the want.available in_app templates render against a
// real payload: the title reads naturally and the body interpolates the year via
// its {{if}} guard.
func TestRenderer_Render(t *testing.T) {
	t.Parallel()

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}

	payload, _ := json.Marshal(WantAvailable{
		Media:    MediaRef{Title: "Sentinel", Year: 2024},
		PlexLink: "https://plex/watch/1",
	}.Payload())

	title, body, err := r.Render("want.available", model.ChannelInApp, payload)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if title != "Sentinel is ready to watch" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(body, "Sentinel (2024)") {
		t.Fatalf("body = %q, want it to mention Sentinel (2024)", body)
	}
}

// TestRenderer_RenderOmitsYear proves the body's {{if .media.year}} guard drops
// the parenthetical when year is absent (MediaRef.Year is omitempty), rather than
// rendering an empty "()".
func TestRenderer_RenderOmitsYear(t *testing.T) {
	t.Parallel()

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	payload, _ := json.Marshal(WantAvailable{Media: MediaRef{Title: "Sentinel"}}.Payload())

	_, body, err := r.Render("want.available", model.ChannelInApp, payload)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(body, "(") {
		t.Fatalf("body = %q, want no year parenthetical", body)
	}
}

// TestRenderer_RenderEmail proves the email path renders a plain-text subject
// and an HTML body from the same want.available payload the in_app path uses.
func TestRenderer_RenderEmail(t *testing.T) {
	t.Parallel()

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	payload, _ := json.Marshal(WantAvailable{
		Media:    MediaRef{Title: "Sentinel", Year: 2024},
		PlexLink: "https://plex/watch/1",
	}.Payload())

	subject, html, err := r.RenderEmail("want.available", payload)
	if err != nil {
		t.Fatalf("render email: %v", err)
	}
	if subject != "Sentinel is ready to watch" {
		t.Fatalf("subject = %q", subject)
	}
	if !strings.Contains(html, "Sentinel") {
		t.Fatalf("html body = %q, want it to mention Sentinel", html)
	}
	if !strings.Contains(html, "2024") {
		t.Fatalf("html body = %q, want it to mention the year", html)
	}
	if !strings.Contains(html, "https://plex/watch/1") {
		t.Fatalf("html body = %q, want the play CTA link", html)
	}
}

// TestRenderer_RenderEmailEscapesHTML proves the body goes through html/template:
// a payload value carrying markup metacharacters is contextually escaped rather
// than injected raw. This is the whole reason the HTML body isn't text/template.
func TestRenderer_RenderEmailEscapesHTML(t *testing.T) {
	t.Parallel()

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	payload, _ := json.Marshal(WantAvailable{
		Media: MediaRef{Title: `Tom & <script>evil</script>`},
	}.Payload())

	_, html, err := r.RenderEmail("want.available", payload)
	if err != nil {
		t.Fatalf("render email: %v", err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("html body must escape injected markup, got raw <script>: %q", html)
	}
	if !strings.Contains(html, "&amp;") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("html body = %q, want & and < escaped", html)
	}
}

// TestRenderer_VerifyEmail proves the channel-aware startup guard: the email
// channel demands a subject (text map) and an HTML body (html map), and a
// missing one of either is reported by name.
func TestRenderer_VerifyEmail(t *testing.T) {
	t.Parallel()

	// Templates present → email verify passes.
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	if err := r.Verify(Registered, []model.NotificationChannel{model.ChannelEmail}); err != nil {
		t.Fatalf("verify email should pass with templates present: %v", err)
	}

	// Drop the HTML body → verify names it missing.
	delete(r.htmlTemplates, "want/available/email.body.html")
	if err := r.Verify(Registered, []model.NotificationChannel{model.ChannelEmail}); err == nil ||
		!strings.Contains(err.Error(), "want/available/email.body.html") {
		t.Fatalf("verify should flag the missing html body, got %v", err)
	}

	// Fresh renderer, drop the subject → verify names it missing.
	r2, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	delete(r2.templates, "want/available/email.subject")
	if err := r2.Verify(Registered, []model.NotificationChannel{model.ChannelEmail}); err == nil ||
		!strings.Contains(err.Error(), "want/available/email.subject") {
		t.Fatalf("verify should flag the missing subject, got %v", err)
	}
}

// TestRenderer_Verify proves the build-time guard for the text channels: every
// registered event ships in_app and push templates (a title + body each), and a
// missing one is reported by name.
func TestRenderer_Verify(t *testing.T) {
	t.Parallel()

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	if err := r.Verify(Registered, []model.NotificationChannel{model.ChannelInApp}); err != nil {
		t.Fatalf("verify in_app: %v", err)
	}
	if err := r.Verify(Registered, []model.NotificationChannel{model.ChannelPush}); err != nil {
		t.Fatalf("verify push should pass with templates present: %v", err)
	}

	// Drop the push body → verify names it missing.
	delete(r.templates, "want/available/push.body")
	if err := r.Verify(Registered, []model.NotificationChannel{model.ChannelPush}); err == nil ||
		!strings.Contains(err.Error(), "want/available/push.body") {
		t.Fatalf("verify should flag the missing push body, got %v", err)
	}
}
