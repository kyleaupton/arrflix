-- want_release_exclusion: a per-want blocklist of releases the acquisition
-- front-half must not re-pick. A release is identified by (indexer_id, guid) —
-- the canonical release identity everywhere upstream (SearchResult carries it,
-- download_job's partial UNIQUE key is on it) and the only handle available
-- before a download exists (infohash is a post-download property).
--
-- Producer today: failed-download recovery (S4) — a terminally-failed
-- download_job excludes its release per linked want and returns the want to
-- 'pending', so the re-search picks a different release instead of looping on
-- the one that just failed. The consumer is the front-half's pick loop, which
-- drops any candidate excluded for the want being processed. Exclusions are
-- durable state, not audit: like acquisition's decision logs, the "why" is
-- carried in reason/detail, no separate audit trail.
--
-- reason is a small closed vocabulary; 'declined' (propose-decline, A3) and
-- 'regate_failed' (re-gate phase) join it when those producers land.
CREATE TABLE IF NOT EXISTS want_release_exclusion (
  want_id UUID NOT NULL REFERENCES want(id) ON DELETE CASCADE,
  indexer_id BIGINT NOT NULL,
  guid TEXT NOT NULL,
  reason TEXT NOT NULL CHECK (reason IN ('download_failed')),
  detail TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- The only read is "which releases are excluded for these wants"
  -- (WHERE want_id = ANY(...)), served by the PK's leading column — no
  -- secondary index needed.
  PRIMARY KEY (want_id, indexer_id, guid)
);
