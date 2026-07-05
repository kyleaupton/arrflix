-- Tracking: the ongoing-intent primitive.

-- name: CreateTracking :one
INSERT INTO tracking (media_item_id, quality_profile_id, state, scope, upgrade_behavior, schedule_strategy, autonomy_backfill, autonomy_ongoing)
VALUES (
  sqlc.arg(media_item_id),
  sqlc.arg(quality_profile_id),
  sqlc.arg(state),
  sqlc.arg(scope),
  sqlc.arg(upgrade_behavior),
  sqlc.arg(schedule_strategy),
  sqlc.arg(autonomy_backfill),
  sqlc.arg(autonomy_ongoing)
)
RETURNING *;

-- CreateTrackingIfAbsent is the race-safe get-or-create for the dedup boundary:
-- ON CONFLICT (media_item_id) DO NOTHING means a concurrent spawn that lost the
-- insert matches 0 rows (surfaced as pgx.ErrNoRows) instead of erroring on the
-- UNIQUE violation. The caller reads no-row as "already tracked, re-read it".
-- name: CreateTrackingIfAbsent :one
INSERT INTO tracking (media_item_id, quality_profile_id, state, scope, upgrade_behavior, schedule_strategy, autonomy_backfill, autonomy_ongoing)
VALUES (
  sqlc.arg(media_item_id),
  sqlc.arg(quality_profile_id),
  sqlc.arg(state),
  sqlc.arg(scope),
  sqlc.arg(upgrade_behavior),
  sqlc.arg(schedule_strategy),
  sqlc.arg(autonomy_backfill),
  sqlc.arg(autonomy_ongoing)
)
ON CONFLICT (media_item_id) DO NOTHING
RETURNING *;

-- name: GetTracking :one
SELECT * FROM tracking
WHERE id = $1;

-- name: ListTrackings :many
SELECT * FROM tracking
ORDER BY created_at DESC;

-- FindTrackingByMediaItem is the dedup lookup: a movie request resolves to its
-- existing tracking (if any) before deciding to create a new one. No-row is a
-- normal "not yet tracked" signal, surfaced as NotFound via FromPg.
-- name: FindTrackingByMediaItem :one
SELECT * FROM tracking
WHERE media_item_id = $1;

-- name: SetTrackingState :one
UPDATE tracking
SET state = sqlc.arg(state),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- SetTrackingAutonomy sets both per-segment autonomy dials. The service holds
-- the hold/release of affected wants in the same transaction.
-- name: SetTrackingAutonomy :one
UPDATE tracking
SET autonomy_backfill = sqlc.arg(autonomy_backfill),
    autonomy_ongoing = sqlc.arg(autonomy_ongoing),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
