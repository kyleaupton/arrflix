-- Replace the legacy `error_category` (transient/permanent) string column on
-- worker tables with a typed `error_kind` enum that mirrors the Kind axis in
-- internal/errors. See specs/errors/README.md for the rationale.
--
-- error_category values were never read by any query (the worker branched on
-- in-memory error state, not the persisted column), so dropping the column
-- loses no functional signal — only the audit metadata. The new error_kind
-- column carries the typed kind for observability ("show me jobs that failed
-- due to BadGateway") and is the single source of truth on the wire.

CREATE TYPE error_kind AS ENUM (
    'not_found',
    'conflict',
    'validation',
    'forbidden',
    'unauthenticated',
    'bad_gateway',
    'internal'
);

ALTER TABLE download_job
    DROP COLUMN error_category,
    ADD COLUMN error_kind error_kind;

ALTER TABLE import_task
    DROP COLUMN error_category,
    ADD COLUMN error_kind error_kind;
