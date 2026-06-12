-- Requests: the frozen user-intent artifact.

-- name: CreateRequest :one
INSERT INTO request (requested_by, tmdb_id, type, tier, status)
VALUES (
  sqlc.arg(requested_by),
  sqlc.arg(tmdb_id),
  sqlc.arg(type),
  sqlc.arg(tier),
  sqlc.arg(status)
)
RETURNING *;

-- name: GetRequest :one
SELECT * FROM request
WHERE id = $1;

-- name: ListRequests :many
SELECT * FROM request
ORDER BY created_at DESC;

-- name: SetRequestSpawned :one
UPDATE request
SET status = 'spawned',
    spawned_tracking_id = sqlc.arg(spawned_tracking_id),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetRequestDenied :one
UPDATE request
SET status = 'denied',
    denied_reason = sqlc.arg(denied_reason),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
