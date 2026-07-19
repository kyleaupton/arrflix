package notifications

// Transactional emails are the second class of outbound mail, distinct from the
// user-audience notification events above. They are addressed to a literal email
// (someone who may have no account yet), bypass preference-gating, and are
// email-only — there is no in_app or push face. They therefore don't implement the
// Event interface (no Recipients/Bundle) and don't go through the preference-gated
// Enqueue; a producer hands the event type + payload to
// NotificationService.EnqueueTransactionalEmail directly.

// EventInviteCreated is the invite email: an admin invited an address, and (when
// SMTP is configured and a public URL is known) we email the magic link.
const EventInviteCreated = "invite.created"

// EventEmailTest is the "send test email" probe from Settings ▸ Email. Unlike the
// other transactional emails it is rendered and sent synchronously by the
// email-provider handler, never enqueued to the outbox — but it is registered
// below so the renderer verifies its templates at boot, same as the rest.
const EventEmailTest = "email.test"

// RegisteredTransactionalEmail lists the email-only transactional event types the
// renderer must have templates for. It is the transactional analogue of Registered:
// the worker verifies email subject + HTML body exist for each at startup, so a
// missing template is a loud boot failure rather than a first-send surprise. These
// events have no in_app/push parts, so they're verified for email only.
var RegisteredTransactionalEmail = []string{EventInviteCreated, EventEmailTest}

// InviteCreatedPayload is the template payload for the invite email — exactly the
// variables invite/created/email.* may reference. AcceptURL is the fully-built
// magic link (the service resolves the public base URL and appends the token); the
// template renders it into the CTA. Keeping URL construction in the service keeps
// the template a pure function of its payload.
type InviteCreatedPayload struct {
	AcceptURL string `json:"acceptUrl"`
}
