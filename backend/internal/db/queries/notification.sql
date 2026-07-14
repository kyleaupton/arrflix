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

-- MarkOutboxDelivering is the worker's claim: it flips a queued row to
-- delivering, guarded by status so the transition is a compare-and-set. A row
-- another worker already claimed (status no longer 'queued') matches nothing and
-- returns no row, which the repo surfaces as NotFound — the caller skips it.
-- name: MarkOutboxDelivering :one
UPDATE notification_outbox
SET status = 'delivering'
WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING *;

-- name: MarkOutboxDelivered :one
UPDATE notification_outbox
SET status = 'delivered', delivered_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- MarkOutboxAwaitingConfig parks a queued row whose channel adapter isn't
-- configured yet (email without SMTP). Guarded by status='queued' so the
-- transition is a compare-and-set mirroring MarkOutboxDelivering: a row another
-- worker already moved matches nothing and surfaces as NotFound. ListDueOutbox
-- filters status='queued', so a parked row naturally waits until RequeueAwaitingConfig
-- returns it to the queue.
-- name: MarkOutboxAwaitingConfig :one
UPDATE notification_outbox
SET status = 'awaiting_config'
WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING *;

-- RequeueAwaitingConfig drains parked rows back to the queue once a channel
-- becomes configured — called best-effort after an email provider is saved
-- enabled. Scoped to rows parked since @since (a bounded recent window) so a
-- save doesn't resurrect ancient parked mail. next_attempt_at resets to now so
-- the next drain picks them up immediately. Returns the number requeued.
-- name: RequeueAwaitingConfig :execrows
UPDATE notification_outbox
SET status = 'queued', next_attempt_at = now()
WHERE status = 'awaiting_config' AND created_at >= sqlc.arg(since);

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

-- name: GetOutbox :one
SELECT * FROM notification_outbox WHERE id = sqlc.arg(id);

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

-- Preference writes are one column at a time: the caller toggles exactly one of a
-- bundle's flags (subscribed / email / push), and the other columns must keep their
-- value (or stay NULL = "defer to the registry default"). Each query inserts the row
-- with just its column set — the rest default to NULL — and on conflict updates only
-- that column, so a push toggle never freezes the email default and vice versa. This
-- is what makes the lazy model lazy: an untouched flag stays NULL and keeps tracking
-- the in-code default.

-- name: SetBundleSubscribed :one
INSERT INTO notification_preference (user_id, bundle, subscribed)
VALUES (sqlc.arg(user_id), sqlc.arg(bundle), sqlc.arg(subscribed))
ON CONFLICT (user_id, bundle)
DO UPDATE SET subscribed = EXCLUDED.subscribed, updated_at = now()
RETURNING *;

-- name: SetBundleEmail :one
INSERT INTO notification_preference (user_id, bundle, email)
VALUES (sqlc.arg(user_id), sqlc.arg(bundle), sqlc.arg(email))
ON CONFLICT (user_id, bundle)
DO UPDATE SET email = EXCLUDED.email, updated_at = now()
RETURNING *;

-- name: SetBundlePush :one
INSERT INTO notification_preference (user_id, bundle, push)
VALUES (sqlc.arg(user_id), sqlc.arg(bundle), sqlc.arg(push))
ON CONFLICT (user_id, bundle)
DO UPDATE SET push = EXCLUDED.push, updated_at = now()
RETURNING *;

-- name: ListPreferences :many
SELECT * FROM notification_preference
WHERE user_id = sqlc.arg(user_id);
