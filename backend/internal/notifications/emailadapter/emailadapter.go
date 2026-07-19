// Package emailadapter is the email ChannelAdapter: it renders a due outbox row
// into a subject + HTML body and hands it to the email Transport built from the
// stored SMTP provider.
//
// It lives outside the pure internal/notifications core because it needs the
// email Manager, a Renderer, and a recipient lookup — dependencies the registry/
// routing core deliberately excludes. It depends on narrow surfaces (a
// recipientResolver, the email Manager) so it can be unit-tested with fakes.
package emailadapter

import (
	"context"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/email"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
)

// recipientResolver is the narrow user-lookup surface the adapter needs to turn
// a row's recipient id into a deliverable email address. *repo.Repository
// satisfies it; a test passes a fake.
type recipientResolver interface {
	GetUserByID(ctx context.Context, id uuid.UUID) (model.User, error)
}

// mailer is the narrow email-transport surface the adapter needs: whether a
// relay is configured, and how to build one from the stored provider.
// *email.Manager satisfies it; a test passes a fake with a canned transport.
type mailer interface {
	IsConfigured(ctx context.Context) bool
	BuildFromStored(ctx context.Context) (email.Transport, error)
}

// Adapter delivers notifications over email.
type Adapter struct {
	manager  mailer
	renderer *notifications.Renderer
	users    recipientResolver
}

// New builds the email adapter. The renderer is passed in (rather than parsed
// here) so it renders from the same embedded template tree the worker verified
// at startup.
func New(manager mailer, renderer *notifications.Renderer, users recipientResolver) *Adapter {
	return &Adapter{manager: manager, renderer: renderer, users: users}
}

func (a *Adapter) Name() model.NotificationChannel { return model.ChannelEmail }

// IsConfigured reports whether an enabled SMTP provider is set up. When false,
// the worker parks the row as awaiting_config rather than delivering or killing
// it.
func (a *Adapter) IsConfigured(ctx context.Context) bool { return a.manager.IsConfigured(ctx) }

// Deliver resolves the recipient's address, renders the email, and sends it. The
// address comes from a literal recipient_email (a transactional email to someone
// with no account — an invitee) when present, else from the recipient user's stored
// address. A missing recipient or empty address is a permanent (Validation →
// non-retryable) failure the worker marks dead; a render error is likewise
// permanent; a transport/send failure propagates verbatim so apperrors.IsRetryable
// governs retry-vs-dead (a connection blip retries, a 5xx-rejected address dies —
// the transport classifies the relay's reply, see smtp.classify).
func (a *Adapter) Deliver(ctx context.Context, row model.NotificationOutbox) error {
	const op = "EmailAdapter.Deliver"

	to, err := a.resolveRecipient(ctx, row)
	if err != nil {
		return err
	}

	subject, htmlBody, err := a.renderer.RenderEmail(row.EventType, row.Payload)
	if err != nil {
		return apperrors.Internalf("render email for %q: %v", row.EventType, err).NotRetryable().Op(op)
	}

	transport, err := a.manager.BuildFromStored(ctx)
	if err != nil {
		return err
	}
	return transport.Send(ctx, email.Message{
		To:       []string{to},
		Subject:  subject,
		HTMLBody: htmlBody,
	})
}

// resolveRecipient returns the destination address for a row: a literal
// recipient_email (transactional mail with no account) wins; otherwise the row's
// recipient user is looked up and their stored address used. An addressless row is
// a permanent failure.
func (a *Adapter) resolveRecipient(ctx context.Context, row model.NotificationOutbox) (string, error) {
	const op = "EmailAdapter.Deliver"

	if row.RecipientEmail != nil && *row.RecipientEmail != "" {
		return *row.RecipientEmail, nil
	}
	if row.RecipientUserID == nil {
		return "", apperrors.Validation("email notification has no recipient").Op(op)
	}
	user, err := a.users.GetUserByID(ctx, *row.RecipientUserID)
	if err != nil {
		// A deleted/unknown user surfaces as NotFound (non-retryable → dead); a
		// transient DB error surfaces retryable and requeues.
		return "", err
	}
	if user.Email == nil || *user.Email == "" {
		return "", apperrors.Validation("recipient has no email address").Op(op)
	}
	return *user.Email, nil
}

var _ notifications.ChannelAdapter = (*Adapter)(nil)
