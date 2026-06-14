-- Wants: the work item, shaped as a durable work-dispatch queue.

-- name: CreateWant :one
INSERT INTO want (tracking_id, media_item_id, episode_id, quality_profile_id, status)
VALUES (
  sqlc.arg(tracking_id),
  sqlc.arg(media_item_id),
  sqlc.arg(episode_id),
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

-- CancelWant terminally cancels a want via compare-and-swap: it succeeds only
-- while the want is non-terminal, so a want already 'available'/'failed'/
-- 'canceled' matches 0 rows (surfaced as pgx.ErrNoRows). The service reads that
-- as "nothing to cancel" — idempotent on an already-canceled want, a conflict
-- otherwise.
-- name: CancelWant :one
UPDATE want
SET status = 'canceled',
    updated_at = now()
WHERE id = $1
  AND status NOT IN ('available', 'failed', 'canceled')
RETURNING *;

-- MirrorWantStatus is the terminal-sticky want-status mirror the workers route
-- through: it sets status only while the want is non-terminal, so a want flipped
-- 'canceled' (or already 'available'/'failed') can't be resurrected by a late
-- in-flight job/task completing after a cancel. A 0-row match (pgx.ErrNoRows)
-- means the guard blocked the mirror. Every legitimate forward mirror has a
-- non-terminal source state, so the guard only ever blocks resurrection.
-- name: MirrorWantStatus :one
UPDATE want
SET status = sqlc.arg(status),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status NOT IN ('available', 'failed', 'canceled')
RETURNING *;

-- GrabWant claims a searching want for grab via compare-and-swap: it succeeds
-- only while the want is still 'searching', and the UPDATE holds the row lock
-- through commit so concurrent grabbers serialize and the loser matches 0 rows.
-- This is the dedup guard against the reaper re-claiming a want a live worker
-- still holds. A 0-row match surfaces as pgx.ErrNoRows; the repo reads it as
-- "no longer owned".
-- name: GrabWant :one
UPDATE want
SET status = 'grabbed',
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'searching'
RETURNING *;

-- GrabWantManual flips a want to 'grabbed' for the manual-grab path via
-- compare-and-swap. The IN (...) set is the race-safe gate: it fires only from
-- the pre-grab/terminal states a manual grab may pre-empt or reactivate
-- ('pending'/'searching' pre-empts the autonomous search; 'failed'/'canceled'
-- reactivates). A want the worker already advanced ('grabbed'/'downloading'/
-- 'imported'/'available') isn't in the set, so the manual grab loses cleanly
-- (0 rows, surfaced as pgx.ErrNoRows) and the service rejects it as a conflict.
-- name: GrabWantManual :one
UPDATE want
SET status = 'grabbed',
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status IN ('pending', 'searching', 'failed', 'canceled')
RETURNING *;

-- ReclaimStaleSearchingWants resets wants wedged in 'searching' past the cutoff
-- back to 'pending' so the AcquisitionWorker reclaims them — the self-heal for a
-- crash between ClaimRunnableWants' flip and a terminal transition. Reset-only:
-- attempt_count is untouched and no want is failed (mirrors the "no release yet"
-- reschedule). The cutoff is passed from Go for testability.
-- name: ReclaimStaleSearchingWants :many
UPDATE want
SET status = 'pending',
    last_error = sqlc.arg(last_error),
    next_run_at = now(),
    updated_at = now()
WHERE status = 'searching'
  AND updated_at < sqlc.arg(stale_before)
RETURNING *;

-- ScheduleWantRetry returns a want to 'pending' with a backoff so the
-- AcquisitionWorker can reclaim it; want has no error_kind column, only
-- last_error. Mirrors ScheduleDownloadJobRetry. The 'searching' guard keeps the
-- worker from reverting a want the reaper reset and another worker re-grabbed.
-- name: ScheduleWantRetry :one
UPDATE want
SET status = 'pending',
    attempt_count = attempt_count + 1,
    last_error = sqlc.arg(last_error),
    next_run_at = sqlc.arg(next_run_at),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'searching'
RETURNING *;

-- RearmWant resumes a terminal want when its movie is re-requested, flipping a
-- 'failed'/'canceled' want back to 'pending' with the backoff reset so the
-- AcquisitionWorker re-claims it. The single-atom invariant means a re-request
-- re-arms the one existing want rather than creating a second. A 0-row match
-- (pgx.ErrNoRows) means the want is in-flight or already 'available' — nothing
-- to resume. Mirrors GrabWant's CAS contract.
-- name: RearmWant :one
UPDATE want
SET status = 'pending',
    attempt_count = 0,
    last_error = NULL,
    next_run_at = now(),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status IN ('failed', 'canceled')
RETURNING *;

-- MarkWantFailed terminally fails a want. Mirrors MarkDownloadJobFailed. The
-- 'searching' guard keeps the worker from clobbering a want the reaper reset and
-- another worker re-grabbed.
-- name: MarkWantFailed :one
UPDATE want
SET status = 'failed',
    last_error = sqlc.arg(last_error),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'searching'
RETURNING *;
