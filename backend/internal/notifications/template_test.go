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

// TestRenderer_Verify proves the build-time guard: every registered event has
// in_app templates, but demanding a channel with no templates (push, not shipped
// in v1) reports the exact missing keys.
func TestRenderer_Verify(t *testing.T) {
	t.Parallel()

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	if err := r.Verify(Registered, []model.NotificationChannel{model.ChannelInApp}); err != nil {
		t.Fatalf("verify in_app: %v", err)
	}
	err = r.Verify(Registered, []model.NotificationChannel{model.ChannelPush})
	if err == nil {
		t.Fatalf("verify push should fail — no push templates in v1")
	}
	if !strings.Contains(err.Error(), "want/available/push.title") {
		t.Fatalf("verify error = %v, want it to name the missing push template", err)
	}
}
