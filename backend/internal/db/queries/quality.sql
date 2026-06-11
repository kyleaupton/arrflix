-- Quality profiles

-- name: ListQualityProfiles :many
select * from quality_profile
order by domain, lower(name);

-- name: ListQualityProfilesByDomain :many
select * from quality_profile
where domain = sqlc.arg(domain)
order by lower(name);

-- name: GetQualityProfile :one
select * from quality_profile
where id = $1;

-- name: CreateQualityProfile :one
insert into quality_profile (name, domain, bins, cutoff, min_seeders, indexers, gates, size_overrides)
values (
  sqlc.arg(name),
  sqlc.arg(domain),
  sqlc.arg(bins),
  sqlc.arg(cutoff),
  sqlc.arg(min_seeders),
  sqlc.arg(indexers),
  sqlc.arg(gates),
  sqlc.arg(size_overrides)
)
returning *;

-- name: UpdateQualityProfile :one
update quality_profile
set name = sqlc.arg(name),
    domain = sqlc.arg(domain),
    bins = sqlc.arg(bins),
    cutoff = sqlc.arg(cutoff),
    min_seeders = sqlc.arg(min_seeders),
    indexers = sqlc.arg(indexers),
    gates = sqlc.arg(gates),
    size_overrides = sqlc.arg(size_overrides),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeleteQualityProfile :exec
delete from quality_profile where id = $1;

-- SeedQualityProfile inserts a preset profile under its explicit fixed ID,
-- leaving an existing row untouched so a user's edits to a preset survive a
-- restart. The idempotent, non-clobbering counterpart of CreateQualityProfile.
-- name: SeedQualityProfile :exec
insert into quality_profile (id, name, domain, bins, cutoff, min_seeders, indexers, gates, size_overrides)
values (
  sqlc.arg(id),
  sqlc.arg(name),
  sqlc.arg(domain),
  sqlc.arg(bins),
  sqlc.arg(cutoff),
  sqlc.arg(min_seeders),
  sqlc.arg(indexers),
  sqlc.arg(gates),
  sqlc.arg(size_overrides)
)
on conflict (id) do nothing;

-- Custom formats

-- name: ListCustomFormats :many
select * from custom_format
order by domain, lower(name);

-- name: ListCustomFormatsByDomain :many
select * from custom_format
where domain = sqlc.arg(domain)
order by lower(name);

-- name: GetCustomFormat :one
select * from custom_format
where id = $1;

-- name: CreateCustomFormat :one
insert into custom_format (name, domain, conditions)
values (sqlc.arg(name), sqlc.arg(domain), sqlc.arg(conditions))
returning *;

-- name: UpdateCustomFormat :one
update custom_format
set name = sqlc.arg(name),
    domain = sqlc.arg(domain),
    conditions = sqlc.arg(conditions),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeleteCustomFormat :exec
delete from custom_format where id = $1;

-- SeedCustomFormat inserts a preset custom format under its explicit fixed ID,
-- leaving an existing row untouched. Idempotent, non-clobbering counterpart of
-- CreateCustomFormat.
-- name: SeedCustomFormat :exec
insert into custom_format (id, name, domain, conditions)
values (sqlc.arg(id), sqlc.arg(name), sqlc.arg(domain), sqlc.arg(conditions))
on conflict (id) do nothing;

-- Profile/format join

-- name: ListProfileFormats :many
select custom_format_id, weight from quality_profile_format
where profile_id = $1;

-- name: AddProfileFormat :exec
insert into quality_profile_format (profile_id, custom_format_id, weight)
values (sqlc.arg(profile_id), sqlc.arg(custom_format_id), sqlc.arg(weight))
on conflict (profile_id, custom_format_id) do update set weight = excluded.weight;

-- name: DeleteProfileFormats :exec
delete from quality_profile_format where profile_id = $1;

-- name: DeleteProfileFormat :exec
delete from quality_profile_format
where profile_id = sqlc.arg(profile_id) and custom_format_id = sqlc.arg(custom_format_id);

-- Tier bindings

-- name: ListTierBindings :many
select * from tier_binding;

-- name: GetTierBinding :one
select * from tier_binding
where tier = sqlc.arg(tier) and domain = sqlc.arg(domain);

-- name: UpsertTierBinding :one
insert into tier_binding (tier, domain, profile_id)
values (sqlc.arg(tier), sqlc.arg(domain), sqlc.arg(profile_id))
on conflict (tier, domain) do update set profile_id = excluded.profile_id
returning *;

-- Size defaults

-- name: ListSizeDefaultsByDomain :many
select * from quality_size_default
where domain = sqlc.arg(domain);
