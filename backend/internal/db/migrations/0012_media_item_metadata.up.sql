-- Add display-relevant metadata columns to media_item
ALTER TABLE media_item
    ADD COLUMN poster_path     TEXT,
    ADD COLUMN backdrop_path   TEXT,
    ADD COLUMN overview        TEXT,
    ADD COLUMN vote_average    DOUBLE PRECISION,
    ADD COLUMN vote_count      INT,
    ADD COLUMN runtime         INT,
    ADD COLUMN status          TEXT,
    ADD COLUMN certification   TEXT,
    ADD COLUMN genres          JSONB,
    ADD COLUMN release_date    DATE,
    ADD COLUMN last_air_date   DATE,
    ADD COLUMN in_production   BOOLEAN,
    ADD COLUMN metadata_updated_at TIMESTAMPTZ;

-- Index for staleness queries (enrichment worker)
CREATE INDEX idx_media_item_metadata_stale
    ON media_item (metadata_updated_at ASC NULLS FIRST)
    WHERE tmdb_id IS NOT NULL;

-- Raw source table for future multi-source support
CREATE TABLE media_metadata_source (
    media_item_id UUID NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    source        TEXT NOT NULL,
    data          JSONB NOT NULL,
    fetched_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (media_item_id, source)
);
