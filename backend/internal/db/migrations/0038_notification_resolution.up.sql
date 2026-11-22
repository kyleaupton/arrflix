-- A resolvable in_app notification (today only request.pending) is resolved once
-- the condition it asks about clears: the request was approved, denied or
-- withdrawn. A resolved row stays in the bell, dimmed and labelled with its
-- outcome, but no longer counts as unread. Both columns are set together and only
-- ever on in_app rows; undelivered push/email rows for the same condition move to
-- status 'superseded' instead.
ALTER TABLE notification_outbox
  ADD COLUMN resolved_at TIMESTAMPTZ,
  ADD COLUMN resolution TEXT CHECK (resolution IN ('approved', 'denied', 'withdrawn')),
  ADD CONSTRAINT notification_outbox_resolution_pair
    CHECK ((resolved_at IS NULL) = (resolution IS NULL));
