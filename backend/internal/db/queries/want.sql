-- Wants: the work item, shaped as a durable work-dispatch queue.

-- name: CreateWant :one
INSERT INTO want (tracking_id, media_item_id, quality_profile_id, status)
VALUES (
  sqlc.arg(tracking_id),
  sqlc.arg(media_item_id),
  sqlc.arg(quality_profile_id),
  sqlc.arg(status)
)
RETURNING *;

-- name: GetWant :one
SELECT * FROM want
WHERE id = $1;

-- name: ListWantsByTracking :many
SELECT * FROM want
WHERE tracking_id = $1
ORDER BY created_at DESC;

-- ClaimRunnableWants atomically claims pending wants that are due, flipping them
-- to 'searching' so a concurrent claim skips them. FOR UPDATE SKIP LOCKED is the
-- work-dispatch pattern shared with ClaimRunnableDownloadJobs/ImportTasks.
-- name: ClaimRunnableWants :many
WITH cte AS (
  SELECT id
  FROM want
  WHERE status = 'pending'
    AND next_run_at <= now()
  ORDER BY next_run_at ASC
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE want w
SET status = 'searching',
    updated_at = now()
FROM cte
WHERE w.id = cte.id
RETURNING w.*;

-- name: SetWantStatus :one
UPDATE want
SET status = sqlc.arg(status),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- ScheduleWantRetry returns a want to 'pending' with a backoff so the
-- AcquisitionWorker can reclaim it; want has no error_kind column, only
-- last_error. Mirrors ScheduleDownloadJobRetry.
-- name: ScheduleWantRetry :one
UPDATE want
SET status = 'pending',
    attempt_count = attempt_count + 1,
    last_error = sqlc.arg(last_error),
    next_run_at = sqlc.arg(next_run_at),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- MarkWantFailed terminally fails a want. Mirrors MarkDownloadJobFailed.
-- name: MarkWantFailed :one
UPDATE want
SET status = 'failed',
    last_error = sqlc.arg(last_error),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
