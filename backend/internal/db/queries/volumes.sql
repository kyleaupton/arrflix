-- name: ListVolumes :many
select * from volume
order by lower(name) asc;

-- name: GetVolume :one
select * from volume
where id = $1;

-- name: CreateVolume :one
insert into volume (name, arrflix_base)
values (sqlc.arg(name), sqlc.arg(arrflix_base))
returning *;

-- name: UpdateVolume :one
update volume
set name = sqlc.arg(name),
    arrflix_base = sqlc.arg(arrflix_base),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeleteVolume :execrows
delete from volume where id = $1;

-- name: ListVolumeAliases :many
select * from volume_alias
order by volume_id, base;

-- name: ListVolumeAliasesForVolume :many
select * from volume_alias
where volume_id = $1
order by base;

-- name: CreateVolumeAlias :one
insert into volume_alias (volume_id, downloader_id, base)
values (sqlc.arg(volume_id), sqlc.arg(downloader_id), sqlc.arg(base))
returning *;

-- name: DeleteVolumeAliases :exec
delete from volume_alias where volume_id = $1;
