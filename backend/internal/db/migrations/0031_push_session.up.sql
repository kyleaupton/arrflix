-- Unify push subscriptions under sessions: a push subscription becomes a
-- capability of a login (user_session), not a free-floating per-user record. This
-- makes revocation free — the fan-out joins only live sessions, so revoking a
-- session silently stops its push — and collapses the two device concepts (logins,
-- push devices) into one list.
--
-- Greenfield (no users, no real subscriptions yet) so we recreate the table clean
-- rather than backfilling a session_id onto existing rows. Owner is derived via
-- session_id -> user_session.user_id, so user_id is dropped.
DROP TABLE IF EXISTS push_subscription;

CREATE TABLE push_subscription (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  session_id UUID NOT NULL REFERENCES user_session(id) ON DELETE CASCADE,
  endpoint TEXT NOT NULL UNIQUE,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  user_agent TEXT,                     -- best-effort device label for the UI
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_notified_at TIMESTAMPTZ         -- set by the push adapter on delivery; NULL = never
);

-- 1:1 — one browser (session) holds at most one push subscription. The subscribe
-- flow clears any prior row for the session (or endpoint) before inserting, so this
-- invariant holds without a data-modifying-CTE upsert.
CREATE UNIQUE INDEX idx_push_subscription_session ON push_subscription (session_id);
