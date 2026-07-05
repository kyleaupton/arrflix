-- Per-item, state-derived refresh scheduling for media_item. The refresh engine
-- computes a next_refresh_at from item state and stamps it wherever it stamps
-- metadata_updated_at, so the due sweep reads a precomputed due-time instead of
-- comparing against a single flat staleness threshold. Failure bookkeeping
-- (last-attempt / last-error / attempt-count) mirrors the retry columns on
-- download_job (migration 0008) and drives exponential back-off.
--
-- All columns are nullable or defaulted, so existing inserts are unaffected. A
-- NULL next_refresh_at reads as "due immediately" — existing rows backfill on
-- the first sweep after this migration.
ALTER TABLE media_item
    ADD COLUMN next_refresh_at            TIMESTAMPTZ,
    ADD COLUMN metadata_last_attempted_at TIMESTAMPTZ,
    ADD COLUMN metadata_last_error        TEXT,
    ADD COLUMN metadata_attempt_count     INT NOT NULL DEFAULT 0;

-- Index the due query. NULLS FIRST so un-scheduled rows (the one-time backfill)
-- sort ahead of scheduled ones. Partial on tmdb_id IS NOT NULL — only items
-- with a canonical id are enrichable.
CREATE INDEX idx_media_item_refresh_due
    ON media_item (next_refresh_at ASC NULLS FIRST)
    WHERE tmdb_id IS NOT NULL;

-- Superseded by idx_media_item_refresh_due: staleness is now a precomputed
-- due-time on next_refresh_at, not a metadata_updated_at threshold.
DROP INDEX IF EXISTS idx_media_item_metadata_stale;
