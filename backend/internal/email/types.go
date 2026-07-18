// Package email is the transport seam for sending mail. It mirrors
// internal/downloader: a builder-registry keyed by Provider maps a stored (or
// unsaved) config record to a Transport that actually talks to the relay.
//
// Phase 1 ships one provider — SMTP — behind the seam, decoupled from the
// notification delivery pipeline: the manager builds a Transport for the
// "send test email" flow directly, needing none of the worker/outbox/renderer.
// A future HTTP-API provider (Resend/Mailgun) registers another builder here.
package email

import "context"

// Provider identifies an email transport implementation.
type Provider string

const (
	ProviderSMTP Provider = "smtp"
)

// Message is a provider-agnostic outbound email. HTMLBody is optional; when
// empty the message is plain-text only.
type Message struct {
	To       []string
	Subject  string
	TextBody string
	HTMLBody string
}

// Transport sends mail via a concrete provider. Test sends a minimal probe
// message to `to` and returns the provider error verbatim, so the handler can
// surface the raw connection/auth failure to the operator.
type Transport interface {
	Provider() Provider
	Send(ctx context.Context, msg Message) error
	Test(ctx context.Context, to string) error
}

// ConfigRecord is the flattened, provider-agnostic input a Builder consumes.
// The manager assembles it from the stored model.EmailProvider (or, for
// test-before-save, from the handler's write body). Nullable SMTP fields are
// dereferenced to their zero value here; Username/Password stay pointers so a
// builder can distinguish "unset" from empty.
type ConfigRecord struct {
	Provider      Provider
	FromAddress   string
	FromName      string
	ReplyTo       string
	Host          string
	Port          int
	Security      string
	Auth          bool
	Username      *string
	Password      *string
	SkipTLSVerify bool
	Config        []byte
}

// Builder constructs a Transport from a ConfigRecord.
type Builder func(ConfigRecord) (Transport, error)
