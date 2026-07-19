-- Generalize the notification outbox for transactional literal-recipient email
-- (invites now; password reset next) — the "recipient_literal (system-audience
-- non-user recipients)" the 0024 schema reserved.
--
-- Two orthogonal additions:
--   * recipient_email — a literal destination address for a row with no app_user.
--     Email to a known user still uses recipient_user_id; a literal address is how
--     the email adapter reaches someone who has no account yet (an invitee).
--   * transactional — opts a row out of preference-gating (enforced at enqueue: a
--     transactional email is never fanned out through a user's bundle prefs) and
--     out of awaiting_config parking (enforced by the worker: a transactional email
--     must send or fail loudly, never wait silently for SMTP to appear). An invite
--     email is both literal and transactional.
ALTER TABLE notification_outbox
  ADD COLUMN recipient_email TEXT,
  ADD COLUMN transactional BOOLEAN NOT NULL DEFAULT false;

-- Every row must have a destination: a known user (in_app/push, or email to an
-- account) or a literal address (transactional email). Existing rows all carry
-- recipient_user_id, so this validates clean.
ALTER TABLE notification_outbox
  ADD CONSTRAINT notification_outbox_has_recipient
  CHECK (recipient_user_id IS NOT NULL OR recipient_email IS NOT NULL);
