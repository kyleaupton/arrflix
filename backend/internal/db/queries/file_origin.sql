-- File Origin queries — the insert-once provenance sidecar of `file`.

-- name: CreateFileOriginIfAbsent :exec
-- Stamps the origin the first time a file row is minted; later identity updates
-- and re-scans never overwrite it (ON CONFLICT DO NOTHING is what protects the
-- pre-rename source_title). :exec, not :one — a RETURNING on a no-op conflict
-- yields zero rows, which apperrors.FromPg would misread as NotFound, and the
-- write sites don't need the row back.
insert into file_origin (
  file_id, origin, source_title,
  bin_source, bin_resolution, bin_modifier,
  release_group, edition, parsed,
  indexer_id, guid, download_job_id
) values (
  sqlc.arg(file_id), sqlc.arg(origin), sqlc.arg(source_title),
  sqlc.arg(bin_source), sqlc.arg(bin_resolution), sqlc.arg(bin_modifier),
  sqlc.arg(release_group), sqlc.arg(edition), sqlc.arg(parsed),
  sqlc.arg(indexer_id), sqlc.arg(guid), sqlc.arg(download_job_id)
)
on conflict (file_id) do nothing;

-- name: GetFileOrigin :one
select * from file_origin where file_id = $1;
