-- Policies

-- name: ListPolicies :many
select * from policy
order by priority desc, created_at desc;

-- name: GetPolicy :one
select * from policy
where id = $1;

-- name: CreatePolicy :one
insert into policy (name, description, enabled, priority)
values (sqlc.arg(name), sqlc.arg(description), sqlc.arg(enabled), sqlc.arg(priority))
returning *;

-- name: UpdatePolicy :one
update policy
set name = sqlc.arg(name),
    description = sqlc.arg(description),
    enabled = sqlc.arg(enabled),
    priority = sqlc.arg(priority),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeletePolicy :exec
delete from policy where id = $1;

-- Rules

-- name: GetRuleForPolicy :one
select * from rule
where policy_id = $1;

-- name: CreateRule :one
insert into rule (policy_id, left_operand, operator, right_operand)
values (sqlc.arg(policy_id), sqlc.arg(left_operand), sqlc.arg(operator), sqlc.arg(right_operand))
returning *;

-- name: UpdateRule :one
update rule
set left_operand = sqlc.arg(left_operand),
    operator = sqlc.arg(operator),
    right_operand = sqlc.arg(right_operand),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeleteRule :exec
delete from rule where id = $1;

-- name: DeleteRuleForPolicy :exec
delete from rule where policy_id = $1;

-- Actions

-- name: ListActionsForPolicy :many
select * from action
where policy_id = $1
order by action."order" asc;

-- name: GetAction :one
select * from action
where id = $1;

-- name: CreateAction :one
insert into action (policy_id, type, value, "order")
values (sqlc.arg(policy_id), sqlc.arg(type), sqlc.arg(value), sqlc.arg(action_order))
returning *;

-- name: UpdateAction :one
update action
set type = sqlc.arg(type),
    value = sqlc.arg(value),
    "order" = sqlc.arg(action_order),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeleteAction :exec
delete from action where id = $1;

-- name: ListAllRules :many
SELECT * FROM rule ORDER BY policy_id;

-- name: ListAllActions :many
SELECT * FROM action ORDER BY policy_id, "order" ASC;

-- name: UpdatePolicyPriority :exec
UPDATE policy SET priority = sqlc.arg(priority), updated_at = now()
WHERE id = sqlc.arg(id);

