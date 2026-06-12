-- Tracking: the ongoing-intent primitive.

-- name: CreateTracking :one
INSERT INTO tracking (media_item_id, quality_profile_id, state, scope, upgrade_behavior, schedule_strategy)
VALUES (
  sqlc.arg(media_item_id),
  sqlc.arg(quality_profile_id),
  sqlc.arg(state),
  sqlc.arg(scope),
  sqlc.arg(upgrade_behavior),
  sqlc.arg(schedule_strategy)
)
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
