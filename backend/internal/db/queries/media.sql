-- Media queries

-- name: ListMediaItems :many
select * from media_item
order by created_at desc;

-- name: GetMediaItem :one
select * from media_item
where id = $1;

-- name: GetMediaItemByTmdbIDAndType :one
select * from media_item
where tmdb_id = $1 and type = $2;

-- name: GetMediaItemByTmdbID :one
select * from media_item
where tmdb_id = $1;

-- name: UpsertMediaItem :one
insert into media_item (type, title, year, tmdb_id)
values (sqlc.arg(type), sqlc.arg(title), sqlc.arg(year), sqlc.arg(tmdb_id))
on conflict (type, tmdb_id)
do update set title = excluded.title,
              year = excluded.year,
              updated_at = now()
returning *;

-- name: CreateMediaItem :one
insert into media_item (type, title, year, tmdb_id)
values (sqlc.arg(type), sqlc.arg(title), sqlc.arg(year), sqlc.arg(tmdb_id))
returning *;

-- name: UpdateMediaItem :one
update media_item
set title = $2,
    year = $3,
    tmdb_id = $4,
    updated_at = now()
where id = $1
returning *;

-- name: DeleteMediaItem :exec
delete from media_item where id = $1;

-- Seasons

-- name: ListSeasonsForMedia :many
select * from media_season
where media_item_id = $1
order by season_number asc;

-- name: GetSeason :one
select * from media_season
where id = $1;

-- name: GetSeasonByNumber :one
select * from media_season
where media_item_id = $1 and season_number = $2;

-- name: UpsertSeason :one
-- COALESCE on the metadata columns so a sparse writer (match-commit, which
-- only knows the season number) never nulls out values a full sync wrote.
insert into media_season (media_item_id, season_number, name, overview, poster_path, air_date)
values (sqlc.arg(media_item_id), sqlc.arg(season_number), sqlc.arg(name), sqlc.arg(overview), sqlc.arg(poster_path), sqlc.arg(air_date))
on conflict (media_item_id, season_number)
do update set name = coalesce(excluded.name, media_season.name),
              overview = coalesce(excluded.overview, media_season.overview),
              poster_path = coalesce(excluded.poster_path, media_season.poster_path),
              air_date = coalesce(excluded.air_date, media_season.air_date)
returning *;

-- Episodes

-- name: ListEpisodesForSeason :many
select * from media_episode
where season_id = $1
order by episode_number asc;

-- name: GetEpisode :one
select * from media_episode
where id = $1;

-- name: GetEpisodeByNumber :one
select * from media_episode
where season_id = $1 and episode_number = $2;

-- name: UpsertEpisode :one
-- Keyed on the stable TMDB episode id (partial unique index). season_id and
-- episode_number use plain excluded so a TMDB renumber moves the row in place
-- (the row's UUID — and any file.episode_id — survives). Metadata columns
-- COALESCE so a sparse writer (match-commit) doesn't null out sync's values.
-- deprecated resets to false: if TMDB reports the episode again, it's live.
insert into media_episode (season_id, episode_number, title, air_date, overview, still_path, vote_average, runtime, tmdb_id)
values (sqlc.arg(season_id), sqlc.arg(episode_number), sqlc.arg(title), sqlc.arg(air_date), sqlc.arg(overview), sqlc.arg(still_path), sqlc.arg(vote_average), sqlc.arg(runtime), sqlc.arg(tmdb_id))
on conflict (tmdb_id) where tmdb_id is not null
do update set season_id = excluded.season_id,
              episode_number = excluded.episode_number,
              title = coalesce(excluded.title, media_episode.title),
              air_date = coalesce(excluded.air_date, media_episode.air_date),
              overview = coalesce(excluded.overview, media_episode.overview),
              still_path = coalesce(excluded.still_path, media_episode.still_path),
              vote_average = coalesce(excluded.vote_average, media_episode.vote_average),
              runtime = coalesce(excluded.runtime, media_episode.runtime),
              deprecated = false
returning *;

-- name: DeprecateRemovedEpisodes :exec
-- Marks a series' episodes deprecated when TMDB no longer reports their id
-- (the synced set). Rows are preserved (never deleted) so any imported file's
-- episode_id stays valid. Callers MUST skip this when the synced set is empty
-- (a total sync failure) — an empty array would deprecate everything.
update media_episode
set deprecated = true
where season_id in (select id from media_season where media_item_id = sqlc.arg(media_item_id))
  and tmdb_id is not null
  and tmdb_id <> all(sqlc.arg(synced_tmdb_ids)::bigint[])
  and deprecated = false;

-- Files. Identity (media_item_id, episode_id) is nullable; "unmatched" is
-- media_item_id IS NULL. Files soft-delete (deleted_at); every live read
-- filters deleted_at IS NULL.

-- name: GetFile :one
select * from file where id = $1 and deleted_at is null;

-- name: GetFileByLibraryAndPath :one
-- Returns the live file at (library, path). Soft-deleted rows are
-- excluded so a path freed by a detach reads as absent.
select * from file
where library_id = $1 and path = $2 and deleted_at is null;

-- name: CreateFile :one
-- Inserts a file with an explicit id. The id is the stable join key
-- match_decision.file_id points at, threaded from the scan/match loop so
-- the decision chain reconnects to the row.
insert into file (id, library_id, path, media_item_id, episode_id, edition)
values (sqlc.arg(id), sqlc.arg(library_id), sqlc.arg(path), sqlc.arg(media_item_id), sqlc.arg(episode_id), sqlc.arg(edition))
returning *;

-- name: SetFileIdentity :one
-- Points a file at an identity (media_item + optional episode + edition)
-- in place. Used by match / re-match — the row, its file_state snapshot,
-- and its file_import history all survive.
update file
set media_item_id = sqlc.arg(media_item_id),
    episode_id = sqlc.arg(episode_id),
    edition = sqlc.arg(edition),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: ClearFileIdentity :one
-- Un-match: clears identity in place (back to the inbox). The file stays
-- on disk and in the table; only media_item_id / episode_id / edition go
-- NULL.
update file
set media_item_id = null,
    episode_id = null,
    edition = null,
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: SoftDeleteFile :one
-- Detach: marks the file gone from the library scope. The row survives as
-- the audit anchor; live reads exclude it.
update file
set deleted_at = now(),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: ListFilesForItem :many
select
  f.id,
  f.library_id,
  f.media_item_id,
  f.episode_id,
  f.path,
  f.created_at,
  ms.id as season_id,
  ms.season_number,
  me.episode_number,
  fs.exists,
  fs.size_bytes,
  fs.last_verified_at
from file f
left join media_episode me on f.episode_id = me.id
left join media_season ms on me.season_id = ms.id
left join file_state fs on f.id = fs.file_id
where f.media_item_id = $1 and f.deleted_at is null
order by f.created_at desc;

-- name: ListInboxItems :many
-- The matcher inbox: every live file whose current (non-superseded)
-- match_decision banded to something other than confident or detached.
-- Pulls in confident_review / partial_series — identity set but flagged —
-- which a bare "media_item_id IS NULL" predicate would drop.
--
-- Display title/year/type COALESCE the identified media_item over the
-- decision's parsed_snapshot. file_state is left-joined for the size (no
-- per-row N+1). Optional library and outcome filters.
select
  f.id,
  f.library_id,
  f.path,
  f.created_at,
  fs.size_bytes,
  md.outcome,
  md.confidence,
  coalesce(mi.title, md.parsed_snapshot->>'title', '')     as display_title,
  coalesce(mi.year, (md.parsed_snapshot->>'year')::int)    as display_year,
  coalesce(mi.type, md.parsed_snapshot->>'type', '')       as display_type
from file f
join match_decision md on md.file_id = f.id and md.superseded_at is null
left join file_state fs on f.id = fs.file_id
left join media_item mi on f.media_item_id = mi.id
where f.deleted_at is null
  and md.outcome not in ('confident', 'detached')
  and (sqlc.narg(library_id)::uuid is null or f.library_id = sqlc.narg(library_id))
  and (sqlc.narg(outcome)::match_outcome is null or md.outcome = sqlc.narg(outcome))
order by f.created_at desc
limit sqlc.arg(page_size)::int offset sqlc.arg(offset_val)::int;

-- name: GetInboxItem :one
-- One inbox row by file id — same joins as ListInboxItems, no outcome
-- filter. Excludes soft-deleted and confident/detached files, so a file
-- not awaiting review returns no row.
select
  f.id,
  f.library_id,
  f.path,
  f.created_at,
  fs.size_bytes,
  md.outcome,
  md.confidence,
  coalesce(mi.title, md.parsed_snapshot->>'title', '')     as display_title,
  coalesce(mi.year, (md.parsed_snapshot->>'year')::int)    as display_year,
  coalesce(mi.type, md.parsed_snapshot->>'type', '')       as display_type
from file f
join match_decision md on md.file_id = f.id and md.superseded_at is null
left join file_state fs on f.id = fs.file_id
left join media_item mi on f.media_item_id = mi.id
where f.id = sqlc.arg(file_id)
  and f.deleted_at is null
  and md.outcome not in ('confident', 'detached');

-- name: CountInboxByOutcome :many
-- Per-band totals over the same inbox join, library filter applied, no
-- pagination. The service folds these into a map and derives the page Total.
select md.outcome, count(*) as count
from file f
join match_decision md on md.file_id = f.id and md.superseded_at is null
where f.deleted_at is null
  and md.outcome not in ('confident', 'detached')
  and (sqlc.narg(library_id)::uuid is null or f.library_id = sqlc.narg(library_id))
group by md.outcome;

-- name: ListEpisodeAvailabilityForSeries :many
select
  ms.season_number,
  me.episode_number,
  me.id as episode_id,
  me.title,
  me.air_date,
  f.id as file_id,
  f.library_id,
  fs.exists
from media_episode me
join media_season ms on me.season_id = ms.id
join media_item mi on ms.media_item_id = mi.id
left join file f on f.episode_id = me.id and f.deleted_at is null
left join file_state fs on f.id = fs.file_id
where mi.id = $1
order by ms.season_number, me.episode_number;

-- Paginated library queries

-- name: ListMediaItemsPaginated :many
SELECT * FROM media_item
WHERE
    (sqlc.narg(type_filter)::text IS NULL OR type = sqlc.narg(type_filter)) AND
    (sqlc.narg(search)::text IS NULL OR title ILIKE '%' || sqlc.narg(search) || '%')
ORDER BY
    CASE WHEN sqlc.arg(sort_by)::text = 'title' AND sqlc.arg(sort_dir)::text = 'asc' THEN title END ASC,
    CASE WHEN sqlc.arg(sort_by)::text = 'title' AND sqlc.arg(sort_dir)::text = 'desc' THEN title END DESC,
    CASE WHEN sqlc.arg(sort_by)::text = 'year' AND sqlc.arg(sort_dir)::text = 'asc' THEN year END ASC NULLS LAST,
    CASE WHEN sqlc.arg(sort_by)::text = 'year' AND sqlc.arg(sort_dir)::text = 'desc' THEN year END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_by)::text = 'createdAt' AND sqlc.arg(sort_dir)::text = 'asc' THEN created_at END ASC,
    CASE WHEN sqlc.arg(sort_by)::text = 'createdAt' AND sqlc.arg(sort_dir)::text = 'desc' THEN created_at END DESC,
    created_at DESC
LIMIT sqlc.arg(page_size)::int OFFSET sqlc.arg(offset_val)::int;

-- name: CountMediaItems :one
SELECT COUNT(*) FROM media_item
WHERE
    (sqlc.narg(type_filter)::text IS NULL OR type = sqlc.narg(type_filter)) AND
    (sqlc.narg(search)::text IS NULL OR title ILIKE '%' || sqlc.narg(search) || '%');

-- name: GetMediaItemsByTmdbIDs :many
SELECT tmdb_id FROM media_item
WHERE tmdb_id = ANY(sqlc.arg(tmdb_ids)::bigint[]) AND type = sqlc.arg(type);

-- File State queries

-- name: CreateFileState :one
insert into file_state (file_id, exists, size_bytes, osdb_hash, last_verified_at)
values (sqlc.arg(file_id), sqlc.arg(exists), sqlc.arg(size_bytes), sqlc.arg(osdb_hash), now())
returning *;

-- name: UpsertFileState :one
insert into file_state (file_id, exists, size_bytes, osdb_hash, last_verified_at)
values (sqlc.arg(file_id), sqlc.arg(exists), sqlc.arg(size_bytes), sqlc.arg(osdb_hash), now())
on conflict (file_id)
do update set exists = excluded.exists,
              size_bytes = excluded.size_bytes,
              osdb_hash = excluded.osdb_hash,
              last_verified_at = now()
returning *;

-- name: GetFileState :one
select * from file_state where file_id = $1;

-- name: UpdateFileState :one
update file_state
set exists = sqlc.arg(exists),
    size_bytes = sqlc.arg(size_bytes),
    osdb_hash = sqlc.arg(osdb_hash),
    last_verified_at = now()
where file_id = sqlc.arg(file_id)
returning *;

-- name: ListMissingFiles :many
select f.*, fs.size_bytes, fs.last_verified_at
from file f
join file_state fs on f.id = fs.file_id
where fs.exists = false and f.deleted_at is null
order by fs.last_verified_at desc;

-- name: ListFilesNeedingVerification :many
select f.*, fs.exists, fs.size_bytes, fs.last_verified_at
from file f
join file_state fs on f.id = fs.file_id
where fs.last_verified_at < sqlc.arg(before_time) and f.deleted_at is null
order by fs.last_verified_at asc
limit sqlc.arg(limit_val);

-- File Import queries

-- name: CreateFileImport :one
insert into file_import (file_id, import_task_id, method, source_path, dest_path, success, error_message)
values (sqlc.arg(file_id), sqlc.arg(import_task_id), sqlc.arg(method), sqlc.arg(source_path), sqlc.arg(dest_path), sqlc.arg(success), sqlc.arg(error_message))
returning *;

-- name: GetFileImport :one
select * from file_import where id = $1;

-- name: ListImportsForFile :many
select * from file_import
where file_id = $1
order by attempted_at desc;

-- name: ListImportsForImportTask :many
select * from file_import
where import_task_id = $1
order by attempted_at desc;

-- name: ListRecentImports :many
select * from file_import
order by attempted_at desc
limit sqlc.arg(limit_val);

-- name: ListFailedImports :many
select * from file_import
where success = false
order by attempted_at desc
limit sqlc.arg(limit_val);

-- Bulk path loading for scanner

-- name: ListFilePathsForLibrary :many
SELECT path FROM file WHERE library_id = $1 AND deleted_at IS NULL;

-- Metadata enrichment queries

-- name: UpdateMediaItemMetadata :one
UPDATE media_item
SET poster_path        = sqlc.arg(poster_path),
    backdrop_path      = sqlc.arg(backdrop_path),
    overview           = sqlc.arg(overview),
    vote_average       = sqlc.arg(vote_average),
    vote_count         = sqlc.arg(vote_count),
    runtime            = sqlc.arg(runtime),
    status             = sqlc.arg(status),
    certification      = sqlc.arg(certification),
    genres             = sqlc.arg(genres),
    release_date       = sqlc.arg(release_date),
    last_air_date      = sqlc.arg(last_air_date),
    in_production      = sqlc.arg(in_production),
    metadata_updated_at = now(),
    updated_at         = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListStaleMediaItems :many
SELECT * FROM media_item
WHERE tmdb_id IS NOT NULL
  AND (metadata_updated_at IS NULL OR metadata_updated_at < sqlc.arg(stale_before))
ORDER BY metadata_updated_at ASC NULLS FIRST
LIMIT sqlc.arg(batch_size);

-- name: UpsertMediaMetadataSource :exec
INSERT INTO media_metadata_source (media_item_id, source, data, fetched_at)
VALUES (sqlc.arg(media_item_id), sqlc.arg(source), sqlc.arg(data), now())
ON CONFLICT (media_item_id, source)
DO UPDATE SET data = excluded.data, fetched_at = now();

-- name: GetMediaMetadataSource :one
SELECT * FROM media_metadata_source
WHERE media_item_id = sqlc.arg(media_item_id) AND source = sqlc.arg(source);

-- name: UpsertMediaItemExternalID :exec
-- Secondary-namespace cross-reference for a media_item (imdb/tvdb/…). One row
-- per (item, source); re-running enrichment refreshes the external_id in place.
insert into media_item_external_id (media_item_id, source, external_id)
values (sqlc.arg(media_item_id), sqlc.arg(source), sqlc.arg(external_id))
on conflict (media_item_id, source)
do update set external_id = excluded.external_id, updated_at = now();
