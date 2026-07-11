-- Notifications: the outbox (durable delivery record + bell-icon history) and the
-- per-user preference toggles. See specs/modules/notifications/README.md.

-- EnqueueOutbox writes one delivery row. status/attempts/timestamps take their
-- column defaults (queued, 0, now()); the delivery worker owns every later
-- transition. dedup collision handling (ON CONFLICT) arrives with the typed
-- constructors that own the drop-vs-replace policy.
-- name: EnqueueOutbox :one
INSERT INTO notification_outbox (
  event_type, audience, recipient_user_id, channel, payload, dedup_key
)
VALUES (
  sqlc.arg(event_type),
  sqlc.arg(audience),
  sqlc.narg(recipient_user_id),
  sqlc.arg(channel),
  sqlc.arg(payload),
  sqlc.narg(dedup_key)
)
RETURNING *;

-- ListDueOutbox claims a batch of deliverable rows for the worker: queued and past
-- their backoff, oldest first. FOR UPDATE SKIP LOCKED lets worker goroutines drain
-- without contending; the caller marks each row delivering in the same transaction.
-- name: ListDueOutbox :many
SELECT * FROM notification_outbox
WHERE status = 'queued' AND next_attempt_at <= now()
ORDER BY created_at
LIMIT sqlc.arg(batch_size)
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxDelivering :one
UPDATE notification_outbox
SET status = 'delivering'
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOutboxDelivered :one
UPDATE notification_outbox
SET status = 'delivered', delivered_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- RescheduleOutbox returns a transiently-failed row to the queue with an
-- incremented attempt count and a future next_attempt_at (the worker computes the
-- backoff). last_error keeps the most recent failure reason for history/debugging.
-- name: RescheduleOutbox :one
UPDATE notification_outbox
SET status = 'queued',
    attempts = attempts + 1,
    next_attempt_at = sqlc.arg(next_attempt_at),
    last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOutboxDead :one
UPDATE notification_outbox
SET status = 'dead',
    attempts = attempts + 1,
    last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id)
RETURNING *;

-- ListInbox is the bell-icon read: a user's delivered in_app notifications, newest
-- first. Delivered-only — queued/failed rows aren't user-visible.
-- name: ListInbox :many
SELECT * FROM notification_outbox
WHERE recipient_user_id = sqlc.arg(user_id)
  AND channel = 'in_app'
  AND status = 'delivered'
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit);

-- name: CountUnreadInbox :one
SELECT count(*) FROM notification_outbox
WHERE recipient_user_id = sqlc.arg(user_id)
  AND channel = 'in_app'
  AND status = 'delivered'
  AND read_at IS NULL;

-- MarkInboxRead marks one entry read, guarded by recipient so a user only touches
-- their own rows. Idempotent — re-marking a read row is a no-op.
-- name: MarkInboxRead :exec
UPDATE notification_outbox
SET read_at = now()
WHERE id = sqlc.arg(id)
  AND recipient_user_id = sqlc.arg(user_id)
  AND channel = 'in_app'
  AND read_at IS NULL;

-- name: MarkAllInboxRead :exec
UPDATE notification_outbox
SET read_at = now()
WHERE recipient_user_id = sqlc.arg(user_id)
  AND channel = 'in_app'
  AND status = 'delivered'
  AND read_at IS NULL;

-- UpsertPreference sets one (user, scope, value, channel) toggle — used both to seed
-- a new user's bundle defaults and to persist a later change; ON CONFLICT updates the
-- flag in place.
-- name: UpsertPreference :one
INSERT INTO notification_preference (user_id, scope, value, channel, enabled)
VALUES (
  sqlc.arg(user_id),
  sqlc.arg(scope),
  sqlc.arg(value),
  sqlc.arg(channel),
  sqlc.arg(enabled)
)
ON CONFLICT (user_id, scope, value, channel)
DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now()
RETURNING *;

-- name: ListPreferences :many
SELECT * FROM notification_preference
WHERE user_id = sqlc.arg(user_id);
