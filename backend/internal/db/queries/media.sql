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
insert into media_season (media_item_id, season_number, air_date)
values (sqlc.arg(media_item_id), sqlc.arg(season_number), sqlc.arg(air_date))
on conflict (media_item_id, season_number)
do update set air_date = excluded.air_date
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
insert into media_episode (season_id, episode_number, title, air_date, tmdb_id, tvdb_id)
values (sqlc.arg(season_id), sqlc.arg(episode_number), sqlc.arg(title), sqlc.arg(air_date), sqlc.arg(tmdb_id), sqlc.arg(tvdb_id))
on conflict (season_id, episode_number)
do update set title = excluded.title,
              air_date = excluded.air_date,
              tmdb_id = excluded.tmdb_id,
              tvdb_id = excluded.tvdb_id
returning *;

-- Files (removed season_id and status)

-- name: GetMediaFile :one
select * from media_file where id = $1;

-- name: GetMediaFileByLibraryAndPath :one
select * from media_file where library_id = $1 and path = $2;

-- name: CreateMediaFile :one
insert into media_file (library_id, media_item_id, episode_id, path)
values (sqlc.arg(library_id), sqlc.arg(media_item_id), sqlc.arg(episode_id), sqlc.arg(path))
returning *;

-- name: CreateMediaFileWithID :one
-- Inserts a media_file with an explicit id. Used by the manual match
-- flow (Phase 4) to preserve the stable file_id across an
-- unmatched_file → media_file transition: the match_decision rows are
-- keyed by file_id, so the join key has to survive.
insert into media_file (id, library_id, media_item_id, episode_id, path)
values (sqlc.arg(id), sqlc.arg(library_id), sqlc.arg(media_item_id), sqlc.arg(episode_id), sqlc.arg(path))
returning *;

-- name: DeleteMediaFile :exec
delete from media_file where id = $1;

-- name: ListMediaFilesForItem :many
select
  mf.id,
  mf.library_id,
  mf.media_item_id,
  mf.episode_id,
  mf.path,
  mf.created_at,
  ms.id as season_id,
  ms.season_number,
  me.episode_number,
  mfs.file_exists,
  mfs.file_size,
  mfs.last_verified_at
from media_file mf
left join media_episode me on mf.episode_id = me.id
left join media_season ms on me.season_id = ms.id
left join media_file_state mfs on mf.id = mfs.media_file_id
where mf.media_item_id = $1
order by mf.created_at desc;

-- name: ListEpisodeAvailabilityForSeries :many
select
  ms.season_number,
  me.episode_number,
  me.id as episode_id,
  me.title,
  me.air_date,
  mf.id as file_id,
  mf.library_id,
  mfs.file_exists
from media_episode me
join media_season ms on me.season_id = ms.id
join media_item mi on ms.media_item_id = mi.id
left join media_file mf on mf.episode_id = me.id
left join media_file_state mfs on mf.id = mfs.media_file_id
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

-- Media File State queries

-- name: CreateMediaFileState :one
insert into media_file_state (media_file_id, file_exists, file_size, last_verified_at)
values (sqlc.arg(media_file_id), sqlc.arg(file_exists), sqlc.arg(file_size), now())
returning *;

-- name: UpsertMediaFileState :one
insert into media_file_state (media_file_id, file_exists, file_size, last_verified_at)
values (sqlc.arg(media_file_id), sqlc.arg(file_exists), sqlc.arg(file_size), now())
on conflict (media_file_id)
do update set file_exists = excluded.file_exists,
              file_size = excluded.file_size,
              last_verified_at = now()
returning *;

-- name: GetMediaFileState :one
select * from media_file_state where media_file_id = $1;

-- name: UpdateMediaFileState :one
update media_file_state
set file_exists = sqlc.arg(file_exists),
    file_size = sqlc.arg(file_size),
    last_verified_at = now()
where media_file_id = sqlc.arg(media_file_id)
returning *;

-- name: ListMissingFiles :many
select mf.*, mfs.file_size, mfs.last_verified_at
from media_file mf
join media_file_state mfs on mf.id = mfs.media_file_id
where mfs.file_exists = false
order by mfs.last_verified_at desc;

-- name: ListFilesNeedingVerification :many
select mf.*, mfs.file_exists, mfs.file_size, mfs.last_verified_at
from media_file mf
join media_file_state mfs on mf.id = mfs.media_file_id
where mfs.last_verified_at < sqlc.arg(before_time)
order by mfs.last_verified_at asc
limit sqlc.arg(limit_val);

-- Media File Import queries

-- name: CreateMediaFileImport :one
insert into media_file_import (media_file_id, import_task_id, method, source_path, dest_path, success, error_message)
values (sqlc.arg(media_file_id), sqlc.arg(import_task_id), sqlc.arg(method), sqlc.arg(source_path), sqlc.arg(dest_path), sqlc.arg(success), sqlc.arg(error_message))
returning *;

-- name: GetMediaFileImport :one
select * from media_file_import where id = $1;

-- name: ListImportsForMediaFile :many
select * from media_file_import
where media_file_id = $1
order by attempted_at desc;

-- name: ListImportsForImportTask :many
select * from media_file_import
where import_task_id = $1
order by attempted_at desc;

-- name: ListRecentImports :many
select * from media_file_import
order by attempted_at desc
limit sqlc.arg(limit_val);

-- name: ListFailedImports :many
select * from media_file_import
where success = false
order by attempted_at desc
limit sqlc.arg(limit_val);

-- Bulk path loading for scanner

-- name: ListMediaFilePathsForLibrary :many
SELECT path FROM media_file WHERE library_id = $1;

-- name: ListUnmatchedFilePathsForLibrary :many
SELECT path FROM unmatched_file WHERE library_id = $1 AND resolved_at IS NULL;

-- Unmatched File queries

-- name: CreateUnmatchedFile :one
insert into unmatched_file (library_id, path, file_size, suggested_matches, partial_series)
values (sqlc.arg(library_id), sqlc.arg(path), sqlc.arg(file_size), sqlc.arg(suggested_matches), sqlc.arg(partial_series))
returning *;

-- name: UpsertUnmatchedFile :one
insert into unmatched_file (library_id, path, file_size, suggested_matches, partial_series)
values (sqlc.arg(library_id), sqlc.arg(path), sqlc.arg(file_size), sqlc.arg(suggested_matches), sqlc.arg(partial_series))
on conflict (library_id, path)
do update set file_size = excluded.file_size,
              suggested_matches = excluded.suggested_matches,
              partial_series = excluded.partial_series,
              discovered_at = now()
returning *;

-- name: GetUnmatchedFile :one
select * from unmatched_file where id = $1;

-- name: GetUnmatchedFileByPath :one
select * from unmatched_file where library_id = $1 and path = $2;

-- name: ListUnmatchedFiles :many
select * from unmatched_file
where resolved_at is null
order by discovered_at desc;

-- name: ListUnmatchedFilesForLibrary :many
select * from unmatched_file
where library_id = $1 and resolved_at is null
order by discovered_at desc;

-- name: ListUnmatchedFilesPaginated :many
select * from unmatched_file
where resolved_at is null
  and (sqlc.narg(library_id)::uuid is null or library_id = sqlc.narg(library_id))
order by discovered_at desc
limit sqlc.arg(page_size)::int offset sqlc.arg(offset_val)::int;

-- name: CountUnmatchedFiles :one
select count(*) from unmatched_file
where resolved_at is null
  and (sqlc.narg(library_id)::uuid is null or library_id = sqlc.narg(library_id));

-- name: ResolveUnmatchedFile :one
update unmatched_file
set resolved_at = now(),
    resolved_media_file_id = sqlc.arg(resolved_media_file_id)
where id = sqlc.arg(id)
returning *;

-- name: DismissUnmatchedFile :one
update unmatched_file
set resolved_at = now()
where id = sqlc.arg(id)
returning *;

-- name: UpdateUnmatchedFileSuggestions :one
update unmatched_file
set suggested_matches = sqlc.arg(suggested_matches)
where id = sqlc.arg(id)
returning *;

-- name: DeleteUnmatchedFile :exec
delete from unmatched_file where id = $1;

-- name: DeleteResolvedUnmatchedFilesOlderThan :exec
delete from unmatched_file
where resolved_at is not null
  and resolved_at < sqlc.arg(before_time);

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
    imdb_id            = sqlc.arg(imdb_id),
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
