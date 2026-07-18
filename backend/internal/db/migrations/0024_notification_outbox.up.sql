-- Notifications persistence floor: the durable delivery record (notification_outbox)
-- and the per-user routing toggles (notification_preference). This is v1 of the
-- notification system (specs/modules/notifications/README.md) — the spine only:
-- `user` audience over the `in_app` channel. The schema carries the full audience/
-- channel/status vocabulary the spec defines so later slices (admin audience, push,
-- email, the resolution lifecycle) add columns or values without reshaping these
-- tables.
--
-- Enum-shaped attributes are TEXT + CHECK, matching request/want/download_job — a
-- CHECK widens with a one-line ALTER, where an ALTER TYPE ADD VALUE can't run inside
-- a transaction. Columns only later slices exercise are deferred: resolvable/
-- resolution_state/resolved_at (the resolution lifecycle, v1.1) and recipient_literal
-- (system-audience non-user recipients, v1.3).

-- notification_outbox: one row per (recipient, channel) delivery attempt, and the
-- notification history itself — the bell-icon UI is a query against it. The delivery
-- worker polls queued+due rows, marks them delivering (so a crash leaves provable
-- in-flight state), then transitions to delivered, back to queued (transient retry,
-- with backoff on next_attempt_at), or dead (permanent). recipient_user_id CASCADEs:
-- a deleted user's notification history dies with the account (v1 has no other
-- recipient shape).
CREATE TABLE IF NOT EXISTS notification_outbox (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  event_type TEXT NOT NULL,
  audience TEXT NOT NULL CHECK (audience IN ('user', 'admin', 'system')),
  recipient_user_id UUID REFERENCES app_user(id) ON DELETE CASCADE,
  channel TEXT NOT NULL CHECK (channel IN ('push', 'in_app', 'email')),
  payload JSONB NOT NULL,
  -- Optional producer-chosen key that collapses a storm of identical enqueues; see
  -- the partial unique index below. NULL = no coalescing (each enqueue is distinct).
  dedup_key TEXT,
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN (
    'queued',           -- awaiting delivery (fresh, or rescheduled after a transient failure)
    'delivering',       -- claimed by the worker; provable in-flight state across a crash
    'delivered',        -- handed to the channel adapter successfully (terminal success)
    'failed',           -- transient failure awaiting retry (reserved; v1 reschedules to 'queued')
    'dead',             -- permanent failure or retries exhausted (terminal)
    'awaiting_config',  -- adapter not configured (email without SMTP); parked, not failed (v1.3)
    'superseded'        -- resolvable condition cleared before delivery; cancelled (v1.1)
  )),
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  delivered_at TIMESTAMPTZ,
  -- in_app only: set by the bell-icon UI when the user reads the entry. NULL = unread.
  read_at TIMESTAMPTZ
);

-- Worker poll: claim queued rows whose backoff has elapsed, oldest first.
CREATE INDEX IF NOT EXISTS idx_notification_outbox_due
  ON notification_outbox (next_attempt_at)
  WHERE status = 'queued';

-- Bell-icon inbox read: a user's in_app rows, newest first.
CREATE INDEX IF NOT EXISTS idx_notification_outbox_inbox
  ON notification_outbox (recipient_user_id, created_at DESC)
  WHERE channel = 'in_app';

-- Dedup floor: while a row for (recipient, channel, dedup_key) is still pending or
-- in-flight, a repeat enqueue collides here so the producer's ON CONFLICT can drop
-- it. Scoped to queued/delivering so the same key may recur once the prior delivery
-- settles — dedup collapses concurrent storms, it doesn't dedupe across time. NULL
-- dedup_key rows are exempt (NULLs never collide), so uncoalesced events are
-- unbounded.
CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_outbox_dedup
  ON notification_outbox (recipient_user_id, channel, dedup_key)
  WHERE dedup_key IS NOT NULL AND status IN ('queued', 'delivering');

-- notification_preference: per-user channel toggles at two scopes. A 'bundle' row
-- ('my_requests', 'library_activity', …) is the coarse default; an 'event' row (an
-- event-type string) is the power-user override. Resolution at enqueue: event row
-- wins over bundle row wins over the in-code bundle default. New users are seeded
-- bundle rows from those defaults on account creation.
CREATE TABLE IF NOT EXISTS notification_preference (
  user_id UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  scope TEXT NOT NULL CHECK (scope IN ('bundle', 'event')),
  value TEXT NOT NULL,   -- bundle name (scope='bundle') or event-type string (scope='event')
  channel TEXT NOT NULL CHECK (channel IN ('push', 'in_app', 'email')),
  enabled BOOLEAN NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, scope, value, channel)
);
