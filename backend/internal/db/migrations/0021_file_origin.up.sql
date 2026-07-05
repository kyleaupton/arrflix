-- file_origin: the cold, insert-once provenance sidecar of `file` — the durable
-- record of the source name a file's quality/release tokens derive from, plus a
-- materialized parse for querying and audit. Sibling of file_state (1:1 on
-- file_id), but the opposite temperature: file_state is a hot UPSERT rewritten
-- every scan; file_origin is written once, at the moment the current bytes first
-- enter the library, and never overwritten.
--
-- The problem it solves: to re-render a file under a changed name template you
-- must re-parse its ORIGINAL release title — not its current on-disk name, which
-- after an arrflix rename is the rendered output (circular). Today that title is
-- reachable only through a fragile file → file_import → download_job chain with
-- two ON DELETE SET NULL links, and is simply gone for scan-discovered files or
-- once the download_job is pruned. file_origin captures it independently of
-- download_job lifecycle, with an explicit origin discriminator so a consumer
-- knows from THIS row alone what kind of source name (if any) exists.
--
-- MediaInfo is intentionally excluded (re-gatherable via ffprobe; a separate
-- hygiene concern). Name-template versioning is out of scope for this phase.
CREATE TABLE IF NOT EXISTS file_origin (
  file_id UUID PRIMARY KEY REFERENCES file(id) ON DELETE CASCADE,

  -- How the current bytes entered the library — NOT how identity was later
  -- assigned. A scan-discovered file a user later matches stays 'scan'; only a
  -- genuinely closed-world import create is 'grab'/'manual'. Lets a consumer
  -- know, from this row alone, what kind of source name we hold, with no join
  -- through download_job.
  origin TEXT NOT NULL CHECK (origin IN ('grab', 'scan', 'manual')),

  -- The string quality/release tokens derive from. 'grab' → the release title
  -- the pick+naming used (download_job.candidate_title). 'scan'/'manual' → the
  -- relpath as FIRST discovered, before any arrflix rename. NULL = unknown.
  source_title TEXT,

  -- Materialized parse of source_title at capture: the decision record
  -- acquisition currently discards. bin_* are the projected Sonarr/Radarr bin
  -- (same projection proposals store) so library quality is directly queryable
  -- and comparable; parsed holds the full release blob for future tokens and
  -- parser-drift audit. All nullable because the legacy/unknown case has no
  -- parse — the renderer treats source_title as source of truth and re-parses,
  -- so these never gate re-render.
  bin_source     TEXT,
  bin_resolution TEXT,
  bin_modifier   TEXT,
  release_group  TEXT,
  edition        TEXT,
  parsed         JSONB,

  -- Durable release identity + soft backref. indexer_id/guid are copied out so
  -- they survive download_job pruning (they match want_release_exclusion's key).
  -- download_job_id is drill-down only, ON DELETE SET NULL — NOT load-bearing.
  indexer_id      BIGINT,
  guid            TEXT,
  download_job_id UUID REFERENCES download_job(id) ON DELETE SET NULL,

  -- No updated_at: the row is immutable after creation (insert-once via
  -- ON CONFLICT DO NOTHING), and its absence signals that.
  captured_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
