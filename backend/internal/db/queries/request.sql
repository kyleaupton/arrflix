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

-- FindPendingRequestForUser returns the caller's still-pending request for a
-- title (tmdb id + type), if any. The acquisition-status endpoint joins this so
-- a focus page shows the Pending badge without pulling the user's whole request
-- list. Most-recent first, at most one row.
-- name: FindPendingRequestForUser :one
SELECT * FROM request
WHERE requested_by = sqlc.arg(requested_by)
  AND tmdb_id = sqlc.arg(tmdb_id)
  AND type = sqlc.arg(type)
  AND status = 'pending'
ORDER BY created_at DESC
LIMIT 1;

-- ListRequests joins app_user so the list carries the requester's display name.
-- A co_admin approver lacks admin.users.manage and so can't resolve names via
-- the users endpoint; the join makes the queue self-sufficient. Write-path
-- queries stay bare — the UI refetches this joined list after any mutation.
-- name: ListRequests :many
SELECT request.*, app_user.username AS requester_name
FROM request
JOIN app_user ON app_user.id = request.requested_by
ORDER BY request.created_at DESC;

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

-- FindLatestRequestForUser returns the caller's most recent request for a title
-- (tmdb id + type) whatever its status. The title-status projection folds the
-- request into the headline state, and needs denied as well as pending — a
-- denial is something the requester must be told about, not just a pending wait.
-- name: FindLatestRequestForUser :one
SELECT * FROM request
WHERE requested_by = sqlc.arg(requested_by)
  AND tmdb_id = sqlc.arg(tmdb_id)
  AND type = sqlc.arg(type)
ORDER BY created_at DESC
LIMIT 1;
