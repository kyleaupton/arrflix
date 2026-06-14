-- Tracking: the ongoing-intent primitive that sits between requests (its
-- producer) and acquisition (its consumer). The Story-1 flow is
-- request → tracking → want → acquisition → import. This migration is the
-- persistence floor for the movie-only PoC: five tables plus the want-linkage
-- columns on download_job/import_task, no service logic.
--
-- Lifecycle states are TEXT + CHECK (matching download_job/import_task) rather
-- than enum types — easier to evolve than ALTER TYPE. The app sets
-- updated_at = now() on write; there are no triggers.
--
-- Tables are created in FK-dependency order: a table is created before any
-- table that references it (tracking before request, which carries a
-- spawned_tracking_id back-reference).

-- user_policy: the approval seam. Minimal in the PoC — a single per-user
-- auto-approve flag for movies, not the full per-tier/per-type matrix.
CREATE TABLE IF NOT EXISTS user_policy (
  user_id UUID PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
  auto_approve_movie BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- tracking: the ongoing-intent primitive. For movies it is the single-atom
-- degenerate case — scope is inert, there is one want per tracking. The
-- UNIQUE(media_item_id) is the dedup boundary: at most one tracking per media
-- item, so concurrent requests for the same movie converge on one row.
--
-- quality_profile_id is single-valued here, resolved at spawn from the
-- requesting tier. Reconciling two requesters at different tiers (HD vs 4K) is
-- deferred to multi-tier/series work.
--
-- scope/upgrade_behavior/schedule_strategy exist but are inert — placeholders
-- where series tracking and the scheduler plug in.
CREATE TABLE IF NOT EXISTS tracking (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  media_item_id UUID NOT NULL REFERENCES media_item(id) ON DELETE RESTRICT,
  quality_profile_id UUID NOT NULL REFERENCES quality_profile(id) ON DELETE RESTRICT,
  state TEXT NOT NULL CHECK (state IN ('active', 'paused', 'archived', 'canceled')),
  scope TEXT NOT NULL DEFAULT 'self',
  upgrade_behavior TEXT NOT NULL DEFAULT 'none' CHECK (upgrade_behavior IN ('auto', 'propose', 'none')),
  schedule_strategy TEXT NOT NULL DEFAULT 'smart' CHECK (schedule_strategy IN ('smart', 'fixed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (media_item_id)
);

-- request: the frozen user-intent artifact. A request is approved (or denied),
-- then spawns exactly one tracking — spawned_tracking_id is null until then,
-- and ON DELETE SET NULL keeps the request row if its tracking is removed.
-- requested_by is RESTRICT: a user with live requests can't be hard-deleted.
CREATE TABLE IF NOT EXISTS request (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  requested_by UUID NOT NULL REFERENCES app_user(id) ON DELETE RESTRICT,
  tmdb_id BIGINT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('movie', 'series')),
  tier TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'spawned', 'denied')),
  spawned_tracking_id UUID REFERENCES tracking(id) ON DELETE SET NULL,
  denied_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- tracking_requester: live per-requester intent and the dedup association. When
-- a second user requests an already-tracked movie they join here rather than
-- creating a second tracking. The (tracking_id, user_id) PK makes the join
-- idempotent.
CREATE TABLE IF NOT EXISTS tracking_requester (
  tracking_id UUID NOT NULL REFERENCES tracking(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  tier TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tracking_id, user_id)
);

-- want: the work item, born shaped as a durable work-dispatch queue (per
-- specs/patterns/work-dispatch). Nothing consumes it in this PoC, but the
-- columns and indexes mirror download_job so the future AcquisitionWorker can
-- claim it with FOR UPDATE SKIP LOCKED. media_item_id and quality_profile_id
-- are denormalized/snapshotted at creation for claim convenience, like
-- download_job. next_run_at is the scheduler's future home (a movie will anchor
-- it to release_date later).
CREATE TABLE IF NOT EXISTS want (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tracking_id UUID NOT NULL REFERENCES tracking(id) ON DELETE CASCADE,
  media_item_id UUID NOT NULL REFERENCES media_item(id) ON DELETE RESTRICT,
  quality_profile_id UUID NOT NULL REFERENCES quality_profile(id) ON DELETE RESTRICT,
  status TEXT NOT NULL CHECK (status IN (
    'pending',      -- created, not yet searched
    'searching',    -- claimed by the worker, hunting a release
    'grabbed',      -- a release was sent to the downloader
    'downloading',  -- download in progress
    'imported',     -- files imported, awaiting availability confirmation
    'available',    -- satisfied (terminal success)
    'failed',       -- permanent failure (terminal)
    'canceled'      -- user/system canceled (terminal)
  )),
  next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  attempt_count INT NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_want_status ON want(status);
CREATE INDEX IF NOT EXISTS idx_want_next_run ON want(next_run_at)
  WHERE status IN ('pending', 'searching', 'grabbed', 'downloading', 'imported');
CREATE INDEX IF NOT EXISTS idx_want_tracking ON want(tracking_id);

-- want linkage: download_job and import_task carry the want they satisfy, so a
-- transition can mirror back onto the want's lifecycle. Null for the
-- interactive (manual-grab) path, which has no want. ON DELETE SET NULL keeps
-- the job/task row if its want is removed. The partial indexes back the pre-grab
-- dedup lookup ("does this want already have an in-flight job?").
ALTER TABLE download_job
  ADD COLUMN want_id UUID REFERENCES want(id) ON DELETE SET NULL;

ALTER TABLE import_task
  ADD COLUMN want_id UUID REFERENCES want(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_download_job_want ON download_job(want_id) WHERE want_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_import_task_want ON import_task(want_id) WHERE want_id IS NOT NULL;
