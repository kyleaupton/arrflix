-- A volume is one storage location (one filesystem) that Arrflix and each
-- downloader may reach at a different path. arrflix_base is the canonical path
-- as Arrflix sees it; each alias is the same storage as one downloader sees it.
CREATE TABLE IF NOT EXISTS volume (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name TEXT NOT NULL,
  arrflix_base TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_volume_name_ci ON volume (lower(name));
CREATE UNIQUE INDEX IF NOT EXISTS uq_volume_arrflix_base ON volume (arrflix_base);

-- flavor is reserved for non-POSIX actors (separator, case, drive letters);
-- only 'posix' is accepted until such an actor exists.
CREATE TABLE IF NOT EXISTS volume_alias (
  volume_id UUID NOT NULL REFERENCES volume(id) ON DELETE CASCADE,
  downloader_id UUID NOT NULL REFERENCES downloader(id) ON DELETE CASCADE,
  base TEXT NOT NULL,
  flavor TEXT NOT NULL DEFAULT 'posix' CHECK (flavor IN ('posix')),
  PRIMARY KEY (volume_id, downloader_id)
);

CREATE INDEX IF NOT EXISTS idx_volume_alias_downloader ON volume_alias (downloader_id);

-- An import task records which translation, if any, mapped the downloader's
-- reported path into Arrflix's frame.
ALTER TABLE import_task_event
  DROP CONSTRAINT IF EXISTS import_task_event_event_type_check;
ALTER TABLE import_task_event
  ADD CONSTRAINT import_task_event_event_type_check
  CHECK (event_type IN (
    'created',
    'status_changed',
    'error',
    'retry_scheduled',
    'reimport_requested',
    'path_resolved'
  ));
