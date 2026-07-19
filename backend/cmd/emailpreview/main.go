// Command emailpreview renders each email template through the real notifications
// Renderer with curated sample payloads, writing a browsable index.html plus one
// file per variant into an output directory (default .ignore/email-preview/, which
// is gitignored). It is a dev aid for iterating on the MJML templates: run
// `just email-preview` to compile the MJML and regenerate the preview, then open
// the index in a browser.
//
// The preview is faithful because it uses the same Renderer.RenderEmail the worker
// uses at send time — html/template auto-escaping and Go control flow
// ({{if .plexLink}}…) resolve exactly as they will in a real email. Opening a raw
// compiled .html.tmpl in a browser instead would show literal {{ }} tokens.
package main

import (
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/kyleaupton/arrflix/internal/notifications"
)

// sample is one rendered card in the preview: an event type, a human label for the
// scenario it exercises, and the JSON payload fed to the template (the same shape
// the outbox stores). Multiple samples per event type exercise different template
// branches.
type sample struct {
	eventType string
	variant   string
	payload   string
}

// samples is curated, not reflected: a preview is only useful if its data hits the
// interesting branches. Add a variant here when a template grows a new conditional.
var samples = []sample{
	{
		eventType: "want.available",
		variant:   "with Plex link",
		payload:   `{"media":{"title":"Dune: Part Two","year":2024},"plexLink":"https://plex.example/web/index.html#!/server/abc/details?key=%2Flibrary%2Fmetadata%2F123"}`,
	},
	{
		eventType: "want.available",
		variant:   "no Plex link",
		payload:   `{"media":{"title":"The Bear","year":2023}}`,
	},
	{
		eventType: "want.available",
		variant:   "no year",
		payload:   `{"media":{"title":"Severance"},"plexLink":"https://plex.example/web"}`,
	},
	{
		eventType: "invite.created",
		variant:   "default",
		payload:   `{"acceptUrl":"https://arrflix.example/accept?token=demo-1a2b3c4d5e6f"}`,
	},
	{
		eventType: "email.test",
		variant:   "default",
		payload:   `{}`,
	},
}

// rendered pairs a sample with the output file it landed in, the subject line its
// subject template produced, and the HTML body (embedded into the index via srcdoc
// so the contact sheet is self-contained — some browsers refuse file:// iframes
// that point at sibling files).
type rendered struct {
	sample
	file     string
	darkFile string
	subject  string
	body     string
	darkBody string
}

func main() {
	out := flag.String("out", ".ignore/email-preview", "output directory for the rendered preview")
	flag.Parse()

	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "emailpreview:", err)
		os.Exit(1)
	}
}

func run(outDir string) error {
	r := notifications.MustNewRenderer()

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	var rs []rendered
	for _, s := range samples {
		subject, body, err := r.RenderEmail(s.eventType, []byte(s.payload))
		if err != nil {
			return fmt.Errorf("render %s (%s): %w", s.eventType, s.variant, err)
		}
		base := slug(s.eventType) + "__" + slug(s.variant)
		file, darkFile := base+".html", base+"__dark.html"
		darkBody := forceDark(body)
		if err := os.WriteFile(filepath.Join(outDir, file), []byte(body), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, darkFile), []byte(darkBody), 0o644); err != nil {
			return err
		}
		rs = append(rs, rendered{
			sample:   s,
			file:     file,
			darkFile: darkFile,
			subject:  subject,
			body:     body,
			darkBody: darkBody,
		})
	}

	if err := os.WriteFile(filepath.Join(outDir, "index.html"), []byte(buildIndex(rs)), 0o644); err != nil {
		return err
	}

	fmt.Printf("Wrote %d email preview(s) to %s/index.html\n", len(rs), outDir)
	return nil
}

// buildIndex renders a dark contact sheet: each sample shows its event type,
// scenario label, subject line, and the email body embedded twice side by side —
// light and forced-dark — so both color schemes are visible regardless of the
// viewer's OS setting.
func buildIndex(rs []rendered) string {
	var b strings.Builder
	b.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Arrflix email preview</title>
<style>
  :root { color-scheme: dark; }
  body { margin:0; padding:32px; background:#0b0d10; color:#c7ccd8;
         font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; }
  h1 { color:#fff; font-size:20px; margin:0 0 4px; }
  .hint { color:#5b6172; font-size:13px; margin:0 0 32px; }
  .hint code { color:#8a90a2; }
  .card { margin-bottom:40px; }
  .meta { display:flex; align-items:baseline; gap:10px; margin-bottom:10px; flex-wrap:wrap; }
  .event { color:#fafafa; font-size:13px; font-weight:600; letter-spacing:0.04em; }
  .variant { color:#8a90a2; font-size:13px; }
  .subject-label { color:#5b6172; font-size:12px; text-transform:uppercase; letter-spacing:0.08em; margin-right:6px; }
  .subject { color:#fff; font-size:15px; font-weight:600; }
  .previews { display:flex; gap:16px; flex-wrap:wrap; }
  .preview { display:flex; flex-direction:column; gap:8px; }
  .scheme { color:#5b6172; font-size:12px; text-transform:uppercase; letter-spacing:0.08em; }
  .preview a { color:#8a90a2; font-size:12px; text-decoration:none; }
  .preview a:hover { color:#fafafa; }
  iframe { width:520px; max-width:100%; height:720px; border:1px solid #232733; border-radius:12px; }
  iframe.light { background:#fafafa; }
  iframe.dark { background:#0a0a0a; }
</style>
</head>
<body>
<h1>Arrflix email preview</h1>
<p class="hint">Rendered through the real notification Renderer with sample payloads. Regenerate with <code>just email-preview</code>.</p>
`)
	for _, r := range rs {
		fmt.Fprintf(&b, `<section class="card">
  <div class="meta"><span class="event">%s</span><span class="variant">%s</span></div>
  <div class="meta"><span class="subject-label">Subject</span><span class="subject">%s</span></div>
  <div class="previews">
    <div class="preview"><span class="scheme">Light</span><iframe class="light" srcdoc="%s" title="%s — %s (light)"></iframe><a href="%s" target="_blank" rel="noopener">open full &#8599;</a></div>
    <div class="preview"><span class="scheme">Dark</span><iframe class="dark" srcdoc="%s" title="%s — %s (dark)"></iframe><a href="%s" target="_blank" rel="noopener">open full &#8599;</a></div>
  </div>
</section>
`,
			html.EscapeString(r.eventType), html.EscapeString(r.variant),
			html.EscapeString(r.subject),
			html.EscapeString(r.body), html.EscapeString(r.eventType), html.EscapeString(r.variant), r.file,
			html.EscapeString(r.darkBody), html.EscapeString(r.eventType), html.EscapeString(r.variant), r.darkFile)
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// forceDark rewrites a rendered email so its prefers-color-scheme:dark rules apply
// unconditionally — the preview can then show dark mode regardless of the viewer's
// OS setting. It strips the "@media (prefers-color-scheme: dark) { … }" wrapper by
// brace-matching, leaving the (…!important) rules inside to win against the inline
// light styles, exactly as a dark-mode client would apply them. Returns the input
// unchanged when there is no such block.
func forceDark(doc string) string {
	const marker = "@media (prefers-color-scheme: dark) {"
	start := strings.Index(doc, marker)
	if start < 0 {
		return doc
	}
	open := start + len(marker) - 1 // the '{' that opens the media block
	depth := 0
	for j := open; j < len(doc); j++ {
		switch doc[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				inner := doc[open+1 : j] // the rules between the media braces
				return doc[:start] + inner + doc[j+1:]
			}
		}
	}
	return doc // unbalanced braces — leave it be
}

// slug lowercases and collapses any run of non-alphanumeric characters to a single
// dash so "want.available" / "with Plex link" become filename-safe segments.
func slug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
