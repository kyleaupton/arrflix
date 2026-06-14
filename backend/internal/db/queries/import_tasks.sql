-- Import tasks (per-file import tracking with reimport chain)

-- name: CreateImportTask :one
INSERT INTO import_task (
  status,
  download_job_id,
  source_path,
  previous_task_id,
  want_id,
  media_type,
  media_item_id,
  episode_id,
  library_id,
  name_template_id
)
VALUES (
  'pending',
  sqlc.arg(download_job_id),
  sqlc.arg(source_path),
  sqlc.arg(previous_task_id),
  sqlc.arg(want_id),
  sqlc.arg(media_type),
  sqlc.arg(media_item_id),
  sqlc.arg(episode_id),
  sqlc.arg(library_id),
  sqlc.arg(name_template_id)
)
RETURNING *;

-- name: GetImportTask :one
SELECT * FROM import_task
WHERE id = $1;

-- name: ListImportTasks :many
SELECT * FROM import_task
ORDER BY created_at DESC
LIMIT sqlc.arg(limit_val)
OFFSET sqlc.arg(offset_val);

-- name: ListImportTasksByDownloadJob :many
SELECT * FROM import_task
WHERE download_job_id = $1
ORDER BY created_at DESC;

-- name: ListImportTasksByMediaItem :many
SELECT * FROM import_task
WHERE media_item_id = $1
ORDER BY created_at DESC;

-- name: ListImportTasksByEpisode :many
SELECT * FROM import_task
WHERE episode_id = $1
ORDER BY created_at DESC;

-- name: ListImportTasksByStatus :many
SELECT * FROM import_task
WHERE status = $1
ORDER BY created_at DESC
LIMIT sqlc.arg(limit_val)
OFFSET sqlc.arg(offset_val);

-- name: GetImportTaskHistory :many
-- Get reimport chain for a task (follows previous_task_id links)
WITH RECURSIVE task_chain AS (
  -- Start with the given task
  SELECT it.*, 0 AS chain_depth
  FROM import_task it
  WHERE it.id = $1

  UNION ALL

  -- Follow previous_task_id links
  SELECT prev.*, tc.chain_depth + 1
  FROM import_task prev
  JOIN task_chain tc ON tc.previous_task_id = prev.id
  WHERE tc.chain_depth < 50  -- Safety limit
)
SELECT * FROM task_chain
ORDER BY chain_depth ASC;

-- name: SetImportTaskInProgress :one
UPDATE import_task
SET status = 'in_progress',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetImportTaskCompleted :one
UPDATE import_task
SET status = 'completed',
    dest_path = sqlc.arg(dest_path),
    import_method = sqlc.arg(import_method),
    file_id = sqlc.arg(file_id),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetImportTaskFailed :one
UPDATE import_task
SET status = 'failed',
    last_error = sqlc.arg(last_error),
    error_kind = sqlc.arg(error_kind),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CancelImportTask :one
UPDATE import_task
SET status = 'cancelled',
    updated_at = now()
WHERE id = $1
  AND status = 'pending'
RETURNING *;

-- name: CancelPendingImportTasksForJob :exec
-- Cancel all pending import tasks for a download job
UPDATE import_task
SET status = 'cancelled',
    updated_at = now()
WHERE download_job_id = $1
  AND status = 'pending';

-- name: ScheduleImportTaskRetry :one
UPDATE import_task
SET attempt_count = attempt_count + 1,
    last_error = sqlc.arg(last_error),
    error_kind = sqlc.arg(error_kind),
    next_run_at = sqlc.arg(next_run_at),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ClaimRunnableImportTasks :many
-- Claims tasks that are ready to be processed (pending only, not in_progress)
-- Uses FOR UPDATE SKIP LOCKED to prevent duplicate processing
WITH cte AS (
  SELECT id
  FROM import_task
  WHERE status = 'pending'
    AND next_run_at <= now()
  ORDER BY next_run_at ASC
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE import_task t
SET status = 'in_progress',
    updated_at = now()
FROM cte
WHERE t.id = cte.id
RETURNING t.*;

-- name: GetImportTaskWithDetails :one
-- Get import task with related media info and name template
SELECT
  it.*,
  mi.title AS media_title,
  mi.year AS media_year,
  mi.tmdb_id AS media_tmdb_id,
  mi.type AS media_item_type,
  me.episode_number,
  me.title AS episode_title,
  ms.season_number,
  l.name AS library_name,
  l.root_path AS library_root_path,
  nt.template AS name_template,
  nt.movie_dir_template,
  nt.series_show_template,
  nt.series_season_template,
  dj.candidate_title
FROM import_task it
JOIN media_item mi ON mi.id = it.media_item_id
LEFT JOIN media_episode me ON me.id = it.episode_id
LEFT JOIN media_season ms ON ms.id = me.season_id
JOIN library l ON l.id = it.library_id
JOIN name_template nt ON nt.id = it.name_template_id
LEFT JOIN download_job dj ON dj.id = it.download_job_id
WHERE it.id = $1;

-- name: UpdateImportTaskSourcePath :exec
UPDATE import_task
SET source_path = $2, updated_at = now()
WHERE id = $1;

-- name: CountImportTasksByStatus :one
SELECT
  COUNT(*) FILTER (WHERE status = 'pending')::int AS pending,
  COUNT(*) FILTER (WHERE status = 'in_progress')::int AS in_progress,
  COUNT(*) FILTER (WHERE status = 'completed')::int AS completed,
  COUNT(*) FILTER (WHERE status = 'failed')::int AS failed,
  COUNT(*) FILTER (WHERE status = 'cancelled')::int AS cancelled
FROM import_task;
