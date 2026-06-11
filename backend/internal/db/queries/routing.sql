-- Routing rules

-- name: ListRoutingRules :many
select * from routing_rule
order by "position" asc;

-- name: GetRoutingRule :one
select * from routing_rule
where id = $1;

-- name: CreateRoutingRule :one
insert into routing_rule (name, enabled, "position", conditions, downloader_id, library_id, name_template_id, "continue")
values (
  sqlc.arg(name),
  sqlc.arg(enabled),
  coalesce((select max("position") + 1 from routing_rule), 0),
  sqlc.arg(conditions),
  sqlc.arg(downloader_id),
  sqlc.arg(library_id),
  sqlc.arg(name_template_id),
  sqlc.arg(rule_continue)
)
returning *;

-- name: UpdateRoutingRule :one
update routing_rule
set name = sqlc.arg(name),
    enabled = sqlc.arg(enabled),
    conditions = sqlc.arg(conditions),
    downloader_id = sqlc.arg(downloader_id),
    library_id = sqlc.arg(library_id),
    name_template_id = sqlc.arg(name_template_id),
    "continue" = sqlc.arg(rule_continue),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeleteRoutingRule :exec
delete from routing_rule where id = $1;

-- name: UpdateRoutingRulePositions :exec
update routing_rule as r
set "position" = u.pos,
    updated_at = now()
from (
  select unnest(sqlc.arg(ids)::uuid[]) as id,
         unnest(sqlc.arg(positions)::int[]) as pos
) as u
where r.id = u.id;
