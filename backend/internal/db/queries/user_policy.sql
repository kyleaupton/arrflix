-- User policy: the per-user approval seam.

-- name: GetUserPolicy :one
SELECT * FROM user_policy
WHERE user_id = $1;

-- name: UpsertUserPolicy :one
INSERT INTO user_policy (user_id, auto_approve_movie)
VALUES (sqlc.arg(user_id), sqlc.arg(auto_approve_movie))
ON CONFLICT (user_id) DO UPDATE
SET auto_approve_movie = excluded.auto_approve_movie,
    updated_at = now()
RETURNING *;
