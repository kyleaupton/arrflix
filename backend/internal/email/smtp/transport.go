// Package smtp is the SMTP Transport for the email seam. It maps the stored
// SMTP fields onto wneessen/go-mail's client options: the three security modes
// onto its TLS policy (STARTTLS / implicit-TLS / none), optional SMTP auth, a
// send timeout, and the skip-verify toggle onto a custom tls.Config.
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/kyleaupton/arrflix/internal/email"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/wneessen/go-mail"
)

// transport holds the resolved SMTP settings. A fresh go-mail client is dialed
// per Send so there's no long-lived connection to manage in Phase 1.
type transport struct {
	fromAddress   string
	fromName      string
	replyTo       string
	host          string
	port          int
	security      string
	auth          bool
	username      string
	password      string
	skipTLSVerify bool
}

func (t *transport) Provider() email.Provider { return email.ProviderSMTP }

// client builds a go-mail client from the transport settings. The TLS-port
// policy sets a default port, so WithPort is applied last to let the operator's
// explicit port win.
func (t *transport) client() (*mail.Client, error) {
	opts := []mail.Option{}

	switch t.security {
	case "implicit_tls":
		opts = append(opts, mail.WithSSL())
	case "none":
		opts = append(opts, mail.WithTLSPortPolicy(mail.NoTLS))
	default: // "starttls"
		opts = append(opts, mail.WithTLSPortPolicy(mail.TLSMandatory))
	}

	if t.port > 0 {
		opts = append(opts, mail.WithPort(t.port))
	}

	if t.auth {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(t.username),
			mail.WithPassword(t.password),
		)
	}

	// Mirror go-mail's secure default (ServerName + TLS 1.2 floor) and layer the
	// operator-opt-in skip-verify toggle on top. Driving InsecureSkipVerify from
	// the field (not a literal true) keeps it honest: it's false unless the
	// operator turned it on for a self-signed relay.
	opts = append(opts, mail.WithTLSConfig(&tls.Config{
		ServerName:         t.host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: t.skipTLSVerify,
	}))

	opts = append(opts, mail.WithTimeout(10*time.Second))

	return mail.NewClient(t.host, opts...)
}

func (t *transport) buildMessage(msg email.Message) (*mail.Msg, error) {
	m := mail.NewMsg()
	if t.fromName != "" {
		if err := m.FromFormat(t.fromName, t.fromAddress); err != nil {
			return nil, fmt.Errorf("set from: %w", err)
		}
	} else if err := m.From(t.fromAddress); err != nil {
		return nil, fmt.Errorf("set from: %w", err)
	}
	if err := m.To(msg.To...); err != nil {
		return nil, fmt.Errorf("set recipients: %w", err)
	}
	if t.replyTo != "" {
		if err := m.ReplyTo(t.replyTo); err != nil {
			return nil, fmt.Errorf("set reply-to: %w", err)
		}
	}
	m.Subject(msg.Subject)
	m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
	if msg.HTMLBody != "" {
		m.AddAlternativeString(mail.TypeTextHTML, msg.HTMLBody)
	}
	return m, nil
}

func (t *transport) Send(ctx context.Context, msg email.Message) error {
	m, err := t.buildMessage(msg)
	if err != nil {
		return err
	}
	c, err := t.client()
	if err != nil {
		return err
	}
	// The relay's own error text rides along in every classify branch, so the
	// test-send UX still shows the operator the real failure.
	return classify(c.DialAndSendWithContext(ctx, m))
}

// classify maps a go-mail delivery failure onto the typed-error model the
// notification worker retries on (apperrors.IsRetryable). Without it every SMTP
// failure reaches the worker untyped, and an untyped error is retryable by
// default — so a permanently-rejected address would burn the row's whole attempt
// budget over hours before dying.
//
// go-mail reports a server rejection as *mail.SendError carrying the SMTP reply
// code: 5xx is a permanent refusal (unknown mailbox, message rejected, relay
// denied) that retrying cannot fix, so it is NotRetryable. Everything else stays
// retryable, which is the safe default and covers the cases that genuinely do
// heal on their own:
//
//   - 4xx replies (greylisting, "try again later", rate limits) — SendError with
//     a 4xx code;
//   - failures go-mail generates itself rather than reading off the wire (dial,
//     DNS, TLS, timeouts, a cancelled context) — these are plain wrapped errors,
//     not *mail.SendError, and ErrorCode would read 0 for them anyway;
//   - auth rejections, which surface as a plain error from the dial path. Wrong
//     credentials won't heal by themselves, but the operator fixing them mid-
//     backoff makes the queued row succeed on its next attempt — better than
//     killing the notification outright.
//
// A rejection is BadGateway rather than Internal so the relay's reason reaches
// the caller — Internal hides its detail from the wire, which renders a bad
// mailbox as a bare 500 with nothing to act on. Mirrors push's classify.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var sendErr *mail.SendError
	if errors.As(err, &sendErr) {
		if code := sendErr.ErrorCode(); code >= 500 && code < 600 {
			return apperrors.BadGatewayf("smtp relay rejected the message: %v", err).NotRetryable()
		}
	}
	return apperrors.BadGatewayf("smtp send failed: %v", err)
}
