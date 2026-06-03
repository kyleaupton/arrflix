-- Media core schema: media_item, media_season, media_episode (the content
-- layer) plus the physical-file layer: file, file_state, file_import.
--
-- The `media_*` prefix is the content namespace; `file*` is the physical
-- namespace. See specs/modules/files/README.md for the file entity.

-- Media items (movies or series) with unique constraint on type+tmdb_id
CREATE TABLE IF NOT EXISTS media_item (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  type TEXT NOT NULL CHECK (type IN ('movie','series')),
  -- series_type is the per-series numbering-regime anchor (series only).
  -- Seam taken now; detection/population lands with the anime/series-type
  -- phase. See specs/modules/metadata/README.md "Series type".
  series_type TEXT NOT NULL DEFAULT 'standard' CHECK (series_type IN ('standard','daily','anime')),
  title TEXT NOT NULL,
  year INT,
  tmdb_id BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (type, tmdb_id)
);

CREATE INDEX IF NOT EXISTS idx_media_item_type ON media_item (type);
CREATE INDEX IF NOT EXISTS idx_media_item_tmdb ON media_item (tmdb_id);

-- External ID registry: secondary cross-reference namespaces for an item
-- (imdb, tvdb; anidb reserved). The canonical tmdb id stays a typed column
-- on media_item (canonical writer owns it; it's the matching key) — `tmdb`
-- is a reserved-valid source but column-canonical in v1, not stored here.
-- `source` is an open vocabulary (provider-layer config, not schema).
-- See specs/modules/metadata/README.md "Identity".
CREATE TABLE IF NOT EXISTS media_item_external_id (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  media_item_id UUID NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  external_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (media_item_id, source)
);
CREATE INDEX IF NOT EXISTS idx_media_item_external_id_lookup
  ON media_item_external_id (source, external_id);

-- Seasons (only for series)
CREATE TABLE IF NOT EXISTS media_season (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  media_item_id UUID NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
  season_number INT NOT NULL,
  name TEXT,
  overview TEXT,
  poster_path TEXT,
  air_date DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (media_item_id, season_number)
);

CREATE INDEX IF NOT EXISTS idx_media_season_media ON media_season (media_item_id);

-- Episodes (only for series). Identity is keyed on the stable TMDB episode
-- id (idx_media_episode_tmdb below), NOT on (season_id, episode_number), so a
-- TMDB renumber updates the row in place and any file.episode_id stays
-- attached. `deprecated` marks episodes TMDB stopped reporting (preserved,
-- never deleted). `absolute_number` is a seam (anime), populated later.
CREATE TABLE IF NOT EXISTS media_episode (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  season_id UUID NOT NULL REFERENCES media_season(id) ON DELETE CASCADE,
  episode_number INT NOT NULL,
  title TEXT,
  air_date DATE,
  overview TEXT,
  still_path TEXT,
  vote_average DOUBLE PRECISION,
  runtime INT,
  absolute_number INT,
  deprecated BOOLEAN NOT NULL DEFAULT false,
  tmdb_id BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_media_episode_season ON media_episode (season_id);
-- Non-unique: serves (season, number) lookups; uniqueness now lives on tmdb_id.
CREATE INDEX IF NOT EXISTS idx_media_episode_season_number ON media_episode (season_id, episode_number);
-- The episode identity key — one row per TMDB episode id. Partial so manual
-- (no-tmdb) rows aren't constrained.
CREATE UNIQUE INDEX IF NOT EXISTS idx_media_episode_tmdb ON media_episode (tmdb_id) WHERE tmdb_id IS NOT NULL;

-- A file is one physical media file under a library root. Identity is
-- nullable state on the row: media_item_id is the TITLE (movie or
-- series), episode_id the episode LEAF when resolved. Both NULL means
-- "not identified yet" — "unmatched" is a query, not a separate table.
-- Files soft-delete (deleted_at) so the decision log and import history
-- can anchor to a stable, never-purged row.
CREATE TABLE IF NOT EXISTS file (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  library_id UUID NOT NULL REFERENCES library(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  media_item_id UUID REFERENCES media_item(id) ON DELETE SET NULL,
  episode_id UUID REFERENCES media_episode(id) ON DELETE SET NULL,
  edition TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ,
  CHECK (path !~ '^/')
);

CREATE INDEX IF NOT EXISTS idx_file_library ON file (library_id);
CREATE INDEX IF NOT EXISTS idx_file_media ON file (media_item_id);
CREATE INDEX IF NOT EXISTS idx_file_episode ON file (episode_id);

-- (library_id, path) is unique among LIVE rows only; a path freed by a
-- detach (deleted_at set) can be re-discovered into a fresh file row.
CREATE UNIQUE INDEX IF NOT EXISTS idx_file_library_path_live
  ON file (library_id, path)
  WHERE deleted_at IS NULL;

-- file_state holds the hot, every-scan-rewritten filesystem facts —
-- split from `file` so the cold identity row doesn't churn on each stat
-- pass. osdb_hash is captured during the same read pass for hash-based
-- drop-in re-identification and dedup (every file, identified or not).
-- Inode columns (st_dev/st_ino/st_nlink) are deferred to the hardlinks
-- work and intentionally not declared here.
CREATE TABLE IF NOT EXISTS file_state (
  file_id UUID PRIMARY KEY REFERENCES file(id) ON DELETE CASCADE,
  exists BOOLEAN NOT NULL DEFAULT true,
  size_bytes BIGINT,
  osdb_hash TEXT,
  last_verified_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_file_state_exists ON file_state (exists);
CREATE INDEX IF NOT EXISTS idx_file_state_verified ON file_state (last_verified_at);
CREATE INDEX IF NOT EXISTS idx_file_state_osdb_hash ON file_state (osdb_hash);

-- Per-attempt import audit (successes and failures).
-- Note: import_task_id FK is added in 0008 after import_task table is created
CREATE TABLE IF NOT EXISTS file_import (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  file_id UUID REFERENCES file(id) ON DELETE SET NULL,
  import_task_id UUID,  -- FK added after import_task table is created in 0008
  method TEXT NOT NULL CHECK (method IN ('hardlink','copy','scan','manual_match')),
  source_path TEXT,
  dest_path TEXT NOT NULL,
  attempted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  success BOOLEAN NOT NULL,
  error_message TEXT
);

CREATE INDEX IF NOT EXISTS idx_file_import_file ON file_import (file_id);
CREATE INDEX IF NOT EXISTS idx_file_import_task ON file_import (import_task_id);
CREATE INDEX IF NOT EXISTS idx_file_import_attempted ON file_import (attempted_at DESC);
CREATE INDEX IF NOT EXISTS idx_file_import_success ON file_import (success);
