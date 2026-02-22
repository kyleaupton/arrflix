-- Download job retry mechanism: previous_job_id chain (mirrors import_task's previous_task_id)

-- Add previous_job_id for retry chain
ALTER TABLE download_job
  ADD COLUMN previous_job_id UUID REFERENCES download_job(id) ON DELETE SET NULL;

-- Drop the table-level unique constraint to allow retries of failed/cancelled jobs
ALTER TABLE download_job
  DROP CONSTRAINT download_job_indexer_id_guid_key;

-- Partial unique index: prevent duplicate active downloads, allow retrying failed/cancelled ones
CREATE UNIQUE INDEX download_job_indexer_guid_active
  ON download_job(indexer_id, guid)
  WHERE status NOT IN ('failed', 'cancelled');

-- Index for traversing the retry chain
CREATE INDEX idx_download_job_previous
  ON download_job(previous_job_id)
  WHERE previous_job_id IS NOT NULL;

-- Add 'retry_requested' to download_job_event event_type CHECK constraint
ALTER TABLE download_job_event
  DROP CONSTRAINT download_job_event_event_type_check;

ALTER TABLE download_job_event
  ADD CONSTRAINT download_job_event_event_type_check
  CHECK (event_type IN (
    'created',
    'status_changed',
    'error',
    'retry_scheduled',
    'retry_requested'
  ));
