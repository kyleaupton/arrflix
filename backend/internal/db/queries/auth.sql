-- name: GetUserByEmail :one
SELECT * FROM app_user WHERE lower(email) = lower($1) AND is_active = true;

-- name: GetUserByLogin :one
SELECT * FROM app_user
WHERE (lower(email) = lower($1) OR lower(username) = lower($1))
  AND is_active = true;

-- name: CreateUser :one
INSERT INTO app_user (email, username, password_hash, is_active)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpsertIdentity :one
INSERT INTO user_identity (user_id, provider, subject, username, access_token, refresh_token, token_expires_at, raw)
VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8,'{}'::jsonb))
ON CONFLICT (provider, subject) DO UPDATE
SET user_id=EXCLUDED.user_id, username=EXCLUDED.username, access_token=EXCLUDED.access_token,
    refresh_token=EXCLUDED.refresh_token, token_expires_at=EXCLUDED.token_expires_at, raw=EXCLUDED.raw
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE app_user
SET password_hash = $2, updated_at = now()
WHERE id = $1;

-- name: ListUsers :many
SELECT u.*,
  COALESCE(
    json_agg(
      json_build_object('id', r.id, 'name', r.name, 'description', r.description)
    ) FILTER (WHERE r.id IS NOT NULL),
    '[]'
  ) as roles
FROM app_user u
LEFT JOIN user_role ur ON ur.user_id = u.id
LEFT JOIN role r ON r.id = ur.role_id
GROUP BY u.id
ORDER BY u.created_at DESC;

-- name: GetUser :one
SELECT u.*,
  COALESCE(
    json_agg(
      json_build_object('id', r.id, 'name', r.name, 'description', r.description)
    ) FILTER (WHERE r.id IS NOT NULL),
    '[]'
  ) as roles
FROM app_user u
LEFT JOIN user_role ur ON ur.user_id = u.id
LEFT JOIN role r ON r.id = ur.role_id
WHERE u.id = $1
GROUP BY u.id;

-- name: UpdateUser :one
UPDATE app_user
SET email = $2,
    username = $3,
    is_active = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteUser :exec
DELETE FROM app_user WHERE id = $1;

-- name: UnassignAllRoles :exec
DELETE FROM user_role WHERE user_id = $1;

-- name: ListRoles :many
SELECT * FROM role ORDER BY name ASC;

-- name: GetRoleByName :one
SELECT * FROM role WHERE name = $1;

-- name: LockRoleByName :one
SELECT * FROM role WHERE name = $1 FOR UPDATE;

-- name: CountActiveUsersByRole :one
SELECT COUNT(*) FROM user_role ur
JOIN app_user u ON u.id = ur.user_id
WHERE ur.role_id = $1 AND u.is_active;

-- name: GetIdentityByProviderSubject :one
SELECT * FROM user_identity WHERE provider = $1 AND subject = $2;

-- name: GetUserByID :one
SELECT * FROM app_user WHERE id = $1;