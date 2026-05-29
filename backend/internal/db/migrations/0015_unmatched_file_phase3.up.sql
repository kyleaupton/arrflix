-- Phase 3 of the matcher cutover (specs/modules/matching/README.md §
-- Migration from v0). Two changes to unmatched_file:
--
-- 1. partial_series flag. The matcher's partial_series outcome means
--    "series identity resolved but the episode is unresolved" — the
--    file still needs a human to land an episode pick. It writes
--    unmatched_file like the other low-band outcomes, distinguished by
--    this column. Default false so existing rows (and the common case)
--    pass through unchanged.
--
-- 2. suggested_matches shape evolution. The v0 column held
--    [{tmdbId, title, year, type, score}] (position-rank). Phase 3
--    writes [{external_ref, confidence, contributing_resolvers,
--    evidence, title, year, type}] (per matching § "What evolves").
--    Since Kyle is the only user, we truncate rather than ship a
--    runtime back-compat reader — the next scan repopulates with the
--    new shape. Rows referenced by media_file.resolved_media_file_id
--    are also wiped; the resolved column is a back-pointer to a file
--    that itself stays put, so the foreign-key shape is unaffected by
--    the truncate.

ALTER TABLE unmatched_file
    ADD COLUMN partial_series BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_unmatched_file_partial_series
    ON unmatched_file (library_id)
    WHERE partial_series = true;

-- Drop every row so the next scan repopulates with the new
-- suggested_matches shape. We also clear match_decision rows that
-- reference the now-vanished files, so the supersede chain is
-- consistent — a re-scan mints fresh decisions against fresh file IDs.
TRUNCATE unmatched_file CASCADE;
TRUNCATE match_decision CASCADE;
