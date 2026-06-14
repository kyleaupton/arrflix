ALTER TABLE download_job
  ADD COLUMN want_id UUID REFERENCES want(id) ON DELETE SET NULL;

ALTER TABLE import_task
  ADD COLUMN want_id UUID REFERENCES want(id) ON DELETE SET NULL;

-- Supports the Phase 5 pre-grab dedup lookup ("does this want already have an in-flight job?")
CREATE INDEX IF NOT EXISTS idx_download_job_want ON download_job(want_id) WHERE want_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_import_task_want ON import_task(want_id) WHERE want_id IS NOT NULL;
