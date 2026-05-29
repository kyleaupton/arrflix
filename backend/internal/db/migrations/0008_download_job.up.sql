-- Download System: Separate downloading from importing
-- download_job: 6 states (created, enqueued, downloading, completed, failed, cancelled)
-- import_task: Per-file import tracking with reimport chain
-- Event tables for audit trails

-- Download jobs (6 states, no import states)
CREATE TABLE IF NOT EXISTS download_job (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

  -- Simplified states (no import states)
  status TEXT NOT NULL CHECK (status IN (
    'created',      -- Job created, not yet sent to downloader
    'enqueued',     -- Sent to downloader, waiting to start
    'downloading',  -- Download in progress
    'completed',    -- Files ready for import (terminal success)
    'failed',       -- Permanent failure (terminal)
    'cancelled'     -- User cancelled (terminal)
  )),

  -- What to download (from indexer)
  protocol TEXT NOT NULL CHECK (protocol IN ('torrent', 'usenet')),
  indexer_id BIGINT NOT NULL,
  guid TEXT NOT NULL,
  candidate_title TEXT NOT NULL,
  candidate_link TEXT NOT NULL,

  -- Target media (for spawning import tasks)
  media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'series')),
  media_item_id UUID NOT NULL REFERENCES media_item(id) ON DELETE RESTRICT,
  season_id UUID REFERENCES media_season(id) ON DELETE RESTRICT,
  episode_id UUID REFERENCES media_episode(id) ON DELETE RESTRICT,
  library_id UUID NOT NULL REFERENCES library(id) ON DELETE RESTRICT,
  name_template_id UUID NOT NULL REFERENCES name_template(id) ON DELETE RESTRICT,

  -- Downloader state
  downloader_id UUID NOT NULL REFERENCES downloader(id) ON DELETE RESTRICT,
  downloader_external_id TEXT,
  downloader_status TEXT,
  progress DOUBLE PRECISION,

  -- Where files ended up (populated on completion)
  save_path TEXT,
  content_path TEXT,

  -- Retry logic
  attempt_count INT NOT NULL DEFAULT 0,
  next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error TEXT,
  error_category TEXT CHECK (error_category IS NULL OR error_category IN ('transient', 'permanent')),

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (indexer_id, guid)
);

CREATE INDEX IF NOT EXISTS idx_download_job_status ON download_job(status);
CREATE INDEX IF NOT EXISTS idx_download_job_next_run ON download_job(next_run_at)
  WHERE status IN ('created', 'enqueued', 'downloading');
CREATE INDEX IF NOT EXISTS idx_download_job_media ON download_job(media_item_id);
CREATE INDEX IF NOT EXISTS idx_download_job_season ON download_job(season_id);
CREATE INDEX IF NOT EXISTS idx_download_job_episode ON download_job(episode_id);
CREATE INDEX IF NOT EXISTS idx_download_job_created ON download_job(created_at DESC);

-- Import tasks (per-file import tracking)
CREATE TABLE IF NOT EXISTS import_task (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

  status TEXT NOT NULL CHECK (status IN (
    'pending',      -- Waiting to be processed
    'in_progress',  -- Currently importing
    'completed',    -- Successfully imported (terminal)
    'failed',       -- Failed after max retries (terminal)
    'cancelled'     -- User cancelled (terminal)
  )),

  -- Source
  download_job_id UUID REFERENCES download_job(id) ON DELETE SET NULL,
  source_path TEXT NOT NULL,  -- Absolute path to source file

  -- Re-import chain (for audit trail)
  previous_task_id UUID REFERENCES import_task(id) ON DELETE SET NULL,

  -- Target
  media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'series')),
  media_item_id UUID NOT NULL REFERENCES media_item(id) ON DELETE RESTRICT,
  episode_id UUID REFERENCES media_episode(id) ON DELETE RESTRICT,
  library_id UUID NOT NULL REFERENCES library(id) ON DELETE RESTRICT,
  name_template_id UUID NOT NULL REFERENCES name_template(id) ON DELETE RESTRICT,

  -- Result (populated on success)
  dest_path TEXT,  -- Relative path within library
  import_method TEXT CHECK (import_method IS NULL OR import_method IN ('hardlink', 'copy')),
  file_id UUID REFERENCES file(id) ON DELETE SET NULL,

  -- Retry logic
  attempt_count INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 5,
  next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error TEXT,
  error_category TEXT CHECK (error_category IS NULL OR error_category IN ('transient', 'permanent')),

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- Enforce consistency: movies must not have episode_id, series must have it
  CONSTRAINT media_type_episode_check CHECK (
    (media_type = 'movie' AND episode_id IS NULL) OR
    (media_type = 'series' AND episode_id IS NOT NULL)
  )
);

CREATE INDEX IF NOT EXISTS idx_import_task_status ON import_task(status);
CREATE INDEX IF NOT EXISTS idx_import_task_next_run ON import_task(next_run_at)
  WHERE status IN ('pending', 'in_progress');
CREATE INDEX IF NOT EXISTS idx_import_task_download ON import_task(download_job_id);
CREATE INDEX IF NOT EXISTS idx_import_task_media ON import_task(media_item_id);
CREATE INDEX IF NOT EXISTS idx_import_task_episode ON import_task(episode_id);
CREATE INDEX IF NOT EXISTS idx_import_task_previous ON import_task(previous_task_id)
  WHERE previous_task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_import_task_created ON import_task(created_at DESC);

-- Download job events (audit log)
CREATE TABLE IF NOT EXISTS download_job_event (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  download_job_id UUID NOT NULL REFERENCES download_job(id) ON DELETE CASCADE,

  event_type TEXT NOT NULL CHECK (event_type IN (
    'created',
    'status_changed',
    'error',
    'retry_scheduled'
  )),

  old_status TEXT,
  new_status TEXT,
  message TEXT,
  metadata JSONB,

  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_dje_job ON download_job_event(download_job_id);
CREATE INDEX IF NOT EXISTS idx_dje_created ON download_job_event(created_at DESC);

-- Import task events (audit log)
CREATE TABLE IF NOT EXISTS import_task_event (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  import_task_id UUID NOT NULL REFERENCES import_task(id) ON DELETE CASCADE,

  event_type TEXT NOT NULL CHECK (event_type IN (
    'created',
    'status_changed',
    'error',
    'retry_scheduled',
    'reimport_requested'
  )),

  old_status TEXT,
  new_status TEXT,
  message TEXT,
  metadata JSONB,

  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ite_task ON import_task_event(import_task_id);
CREATE INDEX IF NOT EXISTS idx_ite_created ON import_task_event(created_at DESC);

-- Add FK from file_import to import_task now that import_task exists
ALTER TABLE file_import
  ADD CONSTRAINT fk_file_import_task
  FOREIGN KEY (import_task_id)
  REFERENCES import_task(id)
  ON DELETE SET NULL;
