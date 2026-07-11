-- Requests: the frozen user-intent artifact.

-- CreateRequest inserts a request in either the pending state (no decision yet)
-- or, on the auto-approve path, the approved state with decision provenance. The
-- decided_* args are NULL for a pending request and stamped for the auto path.
-- name: CreateRequest :one
INSERT INTO request (
  requested_by, tmdb_id, type, tier, status, scope_rule,
  decided_by, decided_at, decision_auto
)
VALUES (
  sqlc.arg(requested_by),
  sqlc.arg(tmdb_id),
  sqlc.arg(type),
  sqlc.arg(tier),
  sqlc.arg(status),
  sqlc.arg(scope_rule),
  sqlc.narg(decided_by),
  sqlc.narg(decided_at),
  sqlc.narg(decision_auto)
)
RETURNING *;

-- name: GetRequest :one
SELECT * FROM request
WHERE id = $1;

-- name: ListRequests :many
SELECT * FROM request
ORDER BY created_at DESC;

-- name: SetRequestApproved :one
UPDATE request
SET status = 'approved',
    decided_by = sqlc.arg(decided_by),
    decided_at = now(),
    decision_auto = false,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

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
    decided_by = sqlc.arg(decided_by),
    decided_at = now(),
    decision_auto = false,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- SetRequestCanceled records a withdrawal: decision_auto is left NULL (a cancel
-- is neither an auto nor a manual approve/deny decision).
-- name: SetRequestCanceled :one
UPDATE request
SET status = 'canceled',
    decided_by = sqlc.arg(decided_by),
    decided_at = now(),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
