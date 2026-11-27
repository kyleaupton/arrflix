-- resolved_at marks a notification that no longer needs attention: the condition
-- it asks about has cleared. A resolved in_app row stays in the bell but doesn't
-- count as unread. It says nothing about how the condition cleared; any outcome a
-- reader shows comes from the notification's subject (for request.pending, the
-- request's status). Undelivered push/email rows for the same condition move to
-- status 'superseded' instead.
ALTER TABLE notification_outbox
  ADD COLUMN resolved_at TIMESTAMPTZ;
