-- name: CreateSession :one
INSERT INTO user_session (user_id, refresh_hash, refresh_expires_at, user_agent, ip)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSession :one
SELECT * FROM user_session WHERE id = $1;

-- name: ListActiveSessionsByUser :many
SELECT * FROM user_session
WHERE user_id = $1 AND revoked_at IS NULL AND refresh_expires_at > now()
ORDER BY last_used_at DESC;

-- name: RotateSession :one
-- Shift the current hash into prev and install a freshly generated hash. Uniform
-- for both rotation cases (the client matched current, or matched prev within the
-- grace window) — either way the new prev is the current-at-rotation. No row is
-- updated once the session is revoked.
UPDATE user_session
SET prev_refresh_hash = refresh_hash,
    refresh_hash = $2,
    rotated_at = now(),
    last_used_at = now()
WHERE id = $1 AND revoked_at IS NULL
RETURNING *;

-- name: RevokeSession :execrows
UPDATE user_session
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeAllSessionsForUser :execrows
UPDATE user_session
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;
