-- Download jobs (refactored: 6 states, no import states)

-- name: CreateDownloadJob :one
INSERT INTO download_job (
  status,
  protocol,
  media_type,
  media_item_id,
  season_id,
  episode_id,
  indexer_id,
  guid,
  candidate_title,
  candidate_link,
  downloader_id,
  library_id,
  name_template_id
)
VALUES (
  'created',
  sqlc.arg(protocol),
  sqlc.arg(media_type),
  sqlc.arg(media_item_id),
  sqlc.arg(season_id),
  sqlc.arg(episode_id),
  sqlc.arg(indexer_id),
  sqlc.arg(guid),
  sqlc.arg(candidate_title),
  sqlc.arg(candidate_link),
  sqlc.arg(downloader_id),
  sqlc.arg(library_id),
  sqlc.arg(name_template_id)
)
ON CONFLICT (indexer_id, guid) WHERE status NOT IN ('failed', 'cancelled') DO UPDATE
SET updated_at = now()
RETURNING *;

-- name: GetDownloadJob :one
SELECT * FROM download_job
WHERE id = $1;

-- name: GetDownloadJobByCandidate :one
SELECT * FROM download_job
WHERE indexer_id = $1 AND guid = $2;

-- name: ListDownloadJobsByMediaItem :many
SELECT * FROM download_job
WHERE media_item_id = $1
ORDER BY created_at DESC;

-- name: ListDownloadJobs :many
SELECT * FROM download_job
ORDER BY created_at DESC;

-- name: ListDownloadJobsByTmdbMovieID :many
SELECT j.*
FROM download_job j
JOIN media_item mi ON mi.id = j.media_item_id
WHERE mi.type = 'movie' AND mi.tmdb_id = $1
ORDER BY j.created_at DESC;

-- name: ListDownloadJobsByTmdbSeriesID :many
SELECT j.*,
       ms.season_number,
       me.episode_number
FROM download_job j
JOIN media_item mi ON mi.id = j.media_item_id
LEFT JOIN media_episode me ON me.id = j.episode_id
LEFT JOIN media_season ms ON ms.id = me.season_id
WHERE mi.type = 'series' AND mi.tmdb_id = $1
ORDER BY j.created_at DESC;

-- name: CancelDownloadJob :one
UPDATE download_job
SET status = 'cancelled',
    updated_at = now()
WHERE id = $1
  AND status NOT IN ('completed', 'failed', 'cancelled')
RETURNING *;

-- name: SetDownloadJobEnqueued :one
UPDATE download_job
SET status = 'enqueued',
    downloader_external_id = sqlc.arg(downloader_external_id),
    attempt_count = attempt_count + 1,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetDownloadJobDownloadSnapshot :one
UPDATE download_job
SET status = sqlc.arg(status),
    downloader_status = sqlc.arg(downloader_status),
    progress = sqlc.arg(progress),
    save_path = sqlc.arg(save_path),
    content_path = sqlc.arg(content_path),
    download_speed = sqlc.arg(download_speed),
    eta_seconds = sqlc.arg(eta_seconds),
    total_size = sqlc.arg(total_size),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetDownloadJobCompleted :one
UPDATE download_job
SET status = 'completed',
    save_path = sqlc.arg(save_path),
    content_path = sqlc.arg(content_path),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ScheduleDownloadJobRetry :one
UPDATE download_job
SET attempt_count = attempt_count + 1,
    last_error = sqlc.arg(last_error),
    error_category = sqlc.arg(error_category),
    next_run_at = sqlc.arg(next_run_at),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkDownloadJobFailed :one
UPDATE download_job
SET status = 'failed',
    last_error = sqlc.arg(last_error),
    error_category = sqlc.arg(error_category),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ClaimRunnableDownloadJobs :many
-- Claims jobs that are ready to be processed (created, enqueued, or downloading)
-- Uses FOR UPDATE SKIP LOCKED to prevent duplicate processing
WITH cte AS (
  SELECT id
  FROM download_job
  WHERE status IN ('created', 'enqueued', 'downloading')
    AND next_run_at <= now()
  ORDER BY next_run_at ASC
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE download_job j
SET updated_at = now()
FROM cte
WHERE j.id = cte.id
RETURNING j.*;

-- name: GetDownloadJobWithImportSummary :one
-- Returns download job with computed import status summary
-- Counts "leaf" tasks (most recent in each reimport chain) to show current state
SELECT
  dj.*,
  mi.tmdb_id,
  mi.poster_path AS media_poster_path,
  mi.title AS media_title,
  mi.year AS media_year,
  mi.certification AS media_certification,
  ms.season_number,
  me.episode_number,
  COUNT(it.id)::int AS total_import_tasks,
  COUNT(it.id) FILTER (WHERE it.status = 'pending')::int AS pending_imports,
  COUNT(it.id) FILTER (WHERE it.status = 'in_progress')::int AS active_imports,
  COUNT(it.id) FILTER (WHERE it.status = 'completed')::int AS completed_imports,
  COUNT(it.id) FILTER (WHERE it.status = 'failed')::int AS failed_imports,
  COUNT(it.id) FILTER (WHERE it.status = 'cancelled')::int AS cancelled_imports,
  CASE
    WHEN dj.status NOT IN ('completed', 'failed', 'cancelled') THEN 'download_pending'
    WHEN dj.status IN ('failed', 'cancelled') THEN 'download_' || dj.status
    WHEN COUNT(it.id) = 0 THEN 'awaiting_import'
    WHEN COUNT(it.id) FILTER (WHERE it.status IN ('pending', 'in_progress')) > 0 THEN 'importing'
    WHEN COUNT(it.id) FILTER (WHERE it.status = 'failed') > 0
         AND COUNT(it.id) FILTER (WHERE it.status = 'completed') > 0 THEN 'partial_failure'
    WHEN COUNT(it.id) FILTER (WHERE it.status = 'failed') = COUNT(it.id) THEN 'import_failed'
    WHEN COUNT(it.id) = COUNT(it.id) FILTER (WHERE it.status = 'completed') THEN 'fully_imported'
    ELSE 'unknown'
  END AS import_status
FROM download_job dj
LEFT JOIN media_item mi ON mi.id = dj.media_item_id
LEFT JOIN media_episode me ON me.id = dj.episode_id
LEFT JOIN media_season ms ON ms.id = COALESCE(dj.season_id, me.season_id)
LEFT JOIN import_task it ON it.download_job_id = dj.id
  AND NOT EXISTS (
    SELECT 1 FROM import_task child
    WHERE child.previous_task_id = it.id
  )
WHERE dj.id = $1
GROUP BY dj.id, mi.tmdb_id, mi.poster_path, mi.title, mi.year, mi.certification, ms.season_number, me.episode_number;

-- name: RetryDownloadJob :one
-- Creates a new download job by copying from a failed job, setting previous_job_id
INSERT INTO download_job (
  status,
  protocol,
  media_type,
  media_item_id,
  season_id,
  episode_id,
  indexer_id,
  guid,
  candidate_title,
  candidate_link,
  downloader_id,
  library_id,
  name_template_id,
  previous_job_id
)
SELECT
  'created',
  old.protocol,
  old.media_type,
  old.media_item_id,
  old.season_id,
  old.episode_id,
  old.indexer_id,
  old.guid,
  old.candidate_title,
  old.candidate_link,
  old.downloader_id,
  old.library_id,
  old.name_template_id,
  old.id
FROM download_job old
WHERE old.id = $1
RETURNING *;

-- name: GetDownloadJobHistory :many
-- Get retry chain for a job (follows previous_job_id links)
WITH RECURSIVE job_chain AS (
  -- Start with the given job
  SELECT dj.*, 0 AS chain_depth
  FROM download_job dj
  WHERE dj.id = $1

  UNION ALL

  -- Follow previous_job_id links
  SELECT prev.*, jc.chain_depth + 1
  FROM download_job prev
  JOIN job_chain jc ON jc.previous_job_id = prev.id
  WHERE jc.chain_depth < 50  -- Safety limit
)
SELECT * FROM job_chain
ORDER BY chain_depth ASC;

-- name: GetDownloadJobTimeline :many
-- Combined event log for a download job (download events + related import events)
SELECT
  'download' AS source,
  e.id,
  e.event_type,
  e.old_status,
  e.new_status,
  e.message,
  e.metadata,
  e.created_at,
  NULL::uuid AS import_task_id
FROM download_job_event e
WHERE e.download_job_id = $1

UNION ALL

SELECT
  'import' AS source,
  ie.id,
  ie.event_type,
  ie.old_status,
  ie.new_status,
  ie.message,
  ie.metadata,
  ie.created_at,
  ie.import_task_id
FROM import_task_event ie
JOIN import_task it ON it.id = ie.import_task_id
WHERE it.download_job_id = $1

ORDER BY created_at ASC;

-- name: ListDownloadJobsWithImportSummary :many
-- Returns leaf download jobs (no child retry pointing to them) with computed import status summary
-- Counts "leaf" tasks (most recent in each reimport chain) to show current state
SELECT
  dj.*,
  mi.tmdb_id,
  mi.poster_path AS media_poster_path,
  mi.title AS media_title,
  mi.year AS media_year,
  mi.certification AS media_certification,
  ms.season_number,
  me.episode_number,
  COUNT(it.id)::int AS total_import_tasks,
  COUNT(it.id) FILTER (WHERE it.status = 'pending')::int AS pending_imports,
  COUNT(it.id) FILTER (WHERE it.status = 'in_progress')::int AS active_imports,
  COUNT(it.id) FILTER (WHERE it.status = 'completed')::int AS completed_imports,
  COUNT(it.id) FILTER (WHERE it.status = 'failed')::int AS failed_imports,
  COUNT(it.id) FILTER (WHERE it.status = 'cancelled')::int AS cancelled_imports,
  CASE
    WHEN dj.status NOT IN ('completed', 'failed', 'cancelled') THEN 'download_pending'
    WHEN dj.status IN ('failed', 'cancelled') THEN 'download_' || dj.status
    WHEN COUNT(it.id) = 0 THEN 'awaiting_import'
    WHEN COUNT(it.id) FILTER (WHERE it.status IN ('pending', 'in_progress')) > 0 THEN 'importing'
    WHEN COUNT(it.id) FILTER (WHERE it.status = 'failed') > 0
         AND COUNT(it.id) FILTER (WHERE it.status = 'completed') > 0 THEN 'partial_failure'
    WHEN COUNT(it.id) FILTER (WHERE it.status = 'failed') = COUNT(it.id) THEN 'import_failed'
    WHEN COUNT(it.id) = COUNT(it.id) FILTER (WHERE it.status = 'completed') THEN 'fully_imported'
    ELSE 'unknown'
  END AS import_status
FROM download_job dj
LEFT JOIN media_item mi ON mi.id = dj.media_item_id
LEFT JOIN media_episode me ON me.id = dj.episode_id
LEFT JOIN media_season ms ON ms.id = COALESCE(dj.season_id, me.season_id)
LEFT JOIN import_task it ON it.download_job_id = dj.id
  AND NOT EXISTS (
    SELECT 1 FROM import_task child
    WHERE child.previous_task_id = it.id
  )
WHERE NOT EXISTS (
  SELECT 1 FROM download_job child
  WHERE child.previous_job_id = dj.id
)
GROUP BY dj.id, mi.tmdb_id, mi.poster_path, mi.title, mi.year, mi.certification, ms.season_number, me.episode_number
ORDER BY dj.updated_at DESC;
