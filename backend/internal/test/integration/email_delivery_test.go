//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/kyleaupton/arrflix/internal/email"
	emailsmtp "github.com/kyleaupton/arrflix/internal/email/smtp"
	notificationworker "github.com/kyleaupton/arrflix/internal/jobs/notification"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/notifications/emailadapter"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// --- in-process SMTP catcher -------------------------------------------------

// capturedEmail is what the catcher records from one SMTP conversation.
type capturedEmail struct {
	from string   // the verbatim MAIL FROM line
	to   []string // the verbatim RCPT TO lines
	data string   // the raw DATA payload (headers + MIME body)
}

// smtpCatcher is a dependency-free, plain-text SMTP server for tests: it speaks
// just enough of the protocol for go-mail's NoTLS/no-auth client to hand over a
// message, which it captures for assertions. Nothing here needs a real relay.
type smtpCatcher struct {
	ln       net.Listener
	received chan capturedEmail
	// rcptReply is the verbatim reply to RCPT TO, letting a test make the relay
	// refuse a recipient (5xx permanent, 4xx transient) instead of accepting it.
	// Fixed at construction so the serve goroutine never races a test setting it.
	rcptReply string
}

func newSMTPCatcher(t *testing.T) *smtpCatcher {
	t.Helper()
	return newSMTPCatcherWithRcptReply(t, "250 2.0.0 OK\r\n")
}

// newSMTPCatcherWithRcptReply builds a catcher that answers RCPT TO with the
// given reply — the seam for exercising relay rejections.
func newSMTPCatcherWithRcptReply(t *testing.T, rcptReply string) *smtpCatcher {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := &smtpCatcher{ln: ln, received: make(chan capturedEmail, 4), rcptReply: rcptReply}
	go c.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return c
}

func (c *smtpCatcher) addr() (host string, port int) {
	a := c.ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func (c *smtpCatcher) serve() {
	for {
		conn, err := c.ln.Accept()
		if err != nil {
			return // listener closed by cleanup
		}
		go c.handle(conn)
	}
}

func (c *smtpCatcher) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s)) }

	write("220 localhost ESMTP\r\n")
	var msg capturedEmail
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		switch upper := strings.ToUpper(trimmed); {
		case strings.HasPrefix(upper, "EHLO"):
			// Multiline reply; the last line uses a space (not a hyphen) to end it.
			write("250-localhost\r\n250-8BITMIME\r\n250-SMTPUTF8\r\n250 HELP\r\n")
		case strings.HasPrefix(upper, "HELO"):
			write("250 localhost\r\n")
		case strings.HasPrefix(upper, "MAIL FROM"):
			msg.from = trimmed
			write("250 2.0.0 OK\r\n")
		case strings.HasPrefix(upper, "RCPT TO"):
			msg.to = append(msg.to, trimmed)
			write(c.rcptReply)
		case strings.HasPrefix(upper, "DATA"):
			write("354 End data with <CR><LF>.<CR><LF>\r\n")
			var body strings.Builder
			for {
				dl, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if dl == ".\r\n" || dl == ".\n" {
					break
				}
				body.WriteString(dl)
			}
			msg.data = body.String()
			write("250 2.0.0 OK: queued\r\n")
			c.received <- msg
		case strings.HasPrefix(upper, "QUIT"):
			write("221 2.0.0 Bye\r\n")
			return
		default: // RSET, NOOP, and anything else — stay lenient.
			write("250 2.0.0 OK\r\n")
		}
	}
}

func (c *smtpCatcher) waitForEmail(t *testing.T) capturedEmail {
	t.Helper()
	select {
	case e := <-c.received:
		return e
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the SMTP catcher to receive a message")
		return capturedEmail{}
	}
}

// --- helpers -----------------------------------------------------------------

// seedEmailProvider upserts the singleton SMTP provider pointing at the catcher.
// security=none / auth=false matches what the catcher speaks.
func seedEmailProvider(t *testing.T, ctx context.Context, r *repo.Repository, host string, port int, enabled bool) {
	t.Helper()
	security := "none"
	svc := service.NewEmailProviderService(r)
	if _, err := svc.Save(ctx, service.SaveEmailProviderParams{
		Provider:    "smtp",
		FromAddress: "arrflix@test.local",
		Host:        &host,
		Port:        &port,
		Security:    &security,
		Auth:        false,
		Enabled:     enabled,
	}); err != nil {
		t.Fatalf("seed email provider: %v", err)
	}
}

// newEmailWorker builds a notification worker wired with both the in_app and the
// real email adapter (SMTP transport over the given repo's stored provider).
func newEmailWorker(t *testing.T, r *repo.Repository) *notificationworker.Worker {
	t.Helper()
	registry := email.NewRegistry()
	emailsmtp.Register(registry)
	mgr := email.NewManager(registry, r)
	adapter := emailadapter.New(mgr, notifications.MustNewRenderer(), r)
	w, err := notificationworker.New(r, logger.New(false), notifications.InAppAdapter{}, adapter)
	if err != nil {
		t.Fatalf("new email worker: %v", err)
	}
	return w
}

// enqueueEmail writes one queued email row for a fresh user (with an address).
func enqueueEmail(t *testing.T, ctx context.Context, r *repo.Repository, addr string) (model.User, model.NotificationOutbox) {
	t.Helper()
	user := newNotifUser(t, ctx, r, addr)
	row, err := r.EnqueueOutbox(ctx, repo.EnqueueOutboxParams{
		EventType:       "want.available",
		Audience:        string(model.AudienceUser),
		RecipientUserID: &user.ID,
		Channel:         string(model.ChannelEmail),
		Payload:         json.RawMessage(`{"media":{"title":"Sentinel","year":2024},"plexLink":"https://plex/watch/1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue email: %v", err)
	}
	return user, row
}

// --- tests -------------------------------------------------------------------

// TestEmailManager_IsConfigured proves the config gate the worker consults:
// no provider and a disabled provider both read false; an enabled provider reads
// true.
func TestEmailManager_IsConfigured(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()
	mgr := email.NewManager(email.NewRegistry(), r)

	if mgr.IsConfigured(ctx) {
		t.Fatal("IsConfigured = true with no provider, want false")
	}
	seedEmailProvider(t, ctx, r, "127.0.0.1", 2525, false)
	if mgr.IsConfigured(ctx) {
		t.Fatal("IsConfigured = true with a disabled provider, want false")
	}
	seedEmailProvider(t, ctx, r, "127.0.0.1", 2525, true)
	if !mgr.IsConfigured(ctx) {
		t.Fatal("IsConfigured = false with an enabled provider, want true")
	}
}

// TestNotification_EnqueueEmailWhenOptedIn proves the service now writes an email
// row alongside the in_app one when the recipient has opted the email channel in
// (email defaults off, so a plain user gets in_app only).
func TestNotification_EnqueueEmailWhenOptedIn(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	user := newNotifUser(t, ctx, r, "notif-email-optin@test.local")
	if err := r.SetBundlePreference(ctx, user.ID, notifications.BundleMyRequests, string(model.ChannelEmail), true); err != nil {
		t.Fatalf("opt into email: %v", err)
	}
	if err := svc.Enqueue(ctx, notifications.WantAvailable{
		Recipient: user.ID,
		Media:     notifications.MediaRef{Title: "Sentinel", Year: 2024},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	due, err := r.ListDueOutbox(ctx, 100)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	channels := map[string]bool{}
	for _, row := range due {
		channels[row.Channel] = true
	}
	if !channels[string(model.ChannelInApp)] || !channels[string(model.ChannelEmail)] {
		t.Fatalf("channels enqueued = %v, want both in_app and email", channels)
	}
}

// TestNotificationWorker_EmailDelivers is the end-to-end happy path: an enabled
// provider + a recipient with an address → a Drain sends the message to the
// in-process catcher (subject + recipient asserted) and settles the row delivered.
func TestNotificationWorker_EmailDelivers(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	catcher := newSMTPCatcher(t)
	host, port := catcher.addr()
	seedEmailProvider(t, ctx, r, host, port, true)

	_, row := enqueueEmail(t, ctx, r, "email-deliver@test.local")
	w := newEmailWorker(t, r)

	n, err := w.Drain(ctx)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 1 {
		t.Fatalf("delivered = %d, want 1", n)
	}

	got := catcher.waitForEmail(t)
	if !strings.Contains(got.data, "Sentinel is ready to watch") {
		t.Fatalf("message missing the rendered subject:\n%s", got.data)
	}
	// The HTML body is quoted-printable encoded; drop soft line breaks so a short
	// word can't be split across the 76-column wrap before we look for it.
	unwrapped := strings.ReplaceAll(got.data, "=\r\n", "")
	if !strings.Contains(unwrapped, "Sentinel") {
		t.Fatalf("message body missing the title:\n%s", got.data)
	}
	if !strings.Contains(strings.Join(got.to, " "), "email-deliver@test.local") {
		t.Fatalf("RCPT TO missing the recipient: %v", got.to)
	}

	settled := fetchOutbox(t, ctx, r, row.ID)
	if settled.Status != string(model.OutboxDelivered) {
		t.Fatalf("status = %q, want delivered", settled.Status)
	}
}

// TestNotificationWorker_EmailParksWhenUnconfigured proves an email row with no
// SMTP configured is parked as awaiting_config — not delivered, and crucially not
// killed. It waits for the operator to configure a relay.
func TestNotificationWorker_EmailParksWhenUnconfigured(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	_, row := enqueueEmail(t, ctx, r, "email-unconfigured@test.local")
	w := newEmailWorker(t, r)

	n, err := w.Drain(ctx)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 0 {
		t.Fatalf("delivered = %d, want 0 (unconfigured)", n)
	}

	got := fetchOutbox(t, ctx, r, row.ID)
	if got.Status != string(model.OutboxAwaitingConfig) {
		t.Fatalf("status = %q, want awaiting_config", got.Status)
	}
	// Parked rows are excluded from the due poll, so a re-drain leaves it be.
	if due, _ := r.ListDueOutbox(ctx, 10); containsOutbox(due, row.ID) {
		t.Fatal("a parked row should not be due")
	}
}

// TestNotificationWorker_EmailRequeueDrains proves the park→configure→drain flow:
// a row parked while unconfigured is returned to the queue by RequeueAwaitingConfig
// once a provider is enabled, and the next Drain delivers it.
func TestNotificationWorker_EmailRequeueDrains(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	catcher := newSMTPCatcher(t)
	host, port := catcher.addr()

	_, row := enqueueEmail(t, ctx, r, "email-requeue@test.local")
	w := newEmailWorker(t, r)

	// 1. No provider yet → the drain parks the row.
	if _, err := w.Drain(ctx); err != nil {
		t.Fatalf("first drain: %v", err)
	}
	if got := fetchOutbox(t, ctx, r, row.ID); got.Status != string(model.OutboxAwaitingConfig) {
		t.Fatalf("status = %q, want awaiting_config", got.Status)
	}

	// 2. Configure an enabled relay and drain the parked backlog back to queued.
	seedEmailProvider(t, ctx, r, host, port, true)
	n, err := service.NewNotificationService(r).RequeueAwaitingConfig(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if n != 1 {
		t.Fatalf("requeued = %d, want 1", n)
	}
	if got := fetchOutbox(t, ctx, r, row.ID); got.Status != string(model.OutboxQueued) {
		t.Fatalf("status = %q, want queued after requeue", got.Status)
	}

	// 3. The next drain delivers it.
	d, err := w.Drain(ctx)
	if err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if d != 1 {
		t.Fatalf("delivered = %d, want 1 after requeue", d)
	}
	catcher.waitForEmail(t)
	if got := fetchOutbox(t, ctx, r, row.ID); got.Status != string(model.OutboxDelivered) {
		t.Fatalf("status = %q, want delivered", got.Status)
	}
}

// TestEmailDelivery_PermanentRejectionDies pins the transport's error
// classification. go-mail reports a relay refusal as an ordinary error value, and
// the typed-error model treats an untyped error as retryable — so without
// smtp.classify a permanently-refused address would burn the row's whole attempt
// budget over hours of backoff before dying. A 5xx must kill it on the first pass.
func TestEmailDelivery_PermanentRejectionDies(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	catcher := newSMTPCatcherWithRcptReply(t, "550 5.1.1 no such mailbox\r\n")
	host, port := catcher.addr()
	seedEmailProvider(t, ctx, r, host, port, true)
	_, row := enqueueEmail(t, ctx, r, "nobody@test.local")

	w := newEmailWorker(t, r)
	if n, err := w.Drain(ctx); err != nil || n != 0 {
		t.Fatalf("drain = %d (err %v), want 0 delivered", n, err)
	}

	got := fetchOutbox(t, ctx, r, row.ID)
	if got.Status != string(model.OutboxDead) {
		t.Fatalf("status = %q, want dead — a 5xx refusal must not consume the retry budget", got.Status)
	}
	// The relay's own reason is what makes the dead row diagnosable.
	if got.LastError == nil || !strings.Contains(*got.LastError, "no such mailbox") {
		t.Fatalf("last_error = %v, want the relay's reason", got.LastError)
	}
}

// TestEmailDelivery_TransientRejectionRetries is the other half of the split: a
// 4xx (greylisting, rate limiting) is exactly what retrying is for, so the row
// must requeue with backoff rather than die alongside the permanent refusals.
func TestEmailDelivery_TransientRejectionRetries(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	catcher := newSMTPCatcherWithRcptReply(t, "451 4.7.1 greylisted, try again later\r\n")
	host, port := catcher.addr()
	seedEmailProvider(t, ctx, r, host, port, true)
	_, row := enqueueEmail(t, ctx, r, "greylisted@test.local")

	w := newEmailWorker(t, r)
	if n, err := w.Drain(ctx); err != nil || n != 0 {
		t.Fatalf("drain = %d (err %v), want 0 delivered", n, err)
	}

	got := fetchOutbox(t, ctx, r, row.ID)
	if got.Status != string(model.OutboxQueued) {
		t.Fatalf("status = %q, want queued — a 4xx is retryable", got.Status)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.Attempts)
	}
	if !got.NextAttemptAt.After(row.NextAttemptAt) {
		t.Fatalf("next_attempt_at = %v, want backed off past %v", got.NextAttemptAt, row.NextAttemptAt)
	}
}
