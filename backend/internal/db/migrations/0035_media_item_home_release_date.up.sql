-- A movie's home-release date: the earliest digital or physical release across
-- the priority regions, written by enrichment alongside release_date. It is
-- when a release can first appear, so it drives when a movie want is searched.
-- NULL means no home release is known (and is always NULL for series).
ALTER TABLE media_item
  ADD COLUMN home_release_date DATE;

-- Movies with a live, not-yet-available want are made due for a metadata
-- refresh, so the enrichment sweep fills home_release_date for them on the
-- first startup rather than at their next scheduled refresh.
UPDATE media_item
SET next_refresh_at = NULL
WHERE type = 'movie'
  AND id IN (
    SELECT media_item_id FROM want
    WHERE status NOT IN ('available', 'failed', 'canceled')
  );
