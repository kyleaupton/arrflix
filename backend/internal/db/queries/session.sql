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
-- updated once the session is revoked. ip refreshes the "last seen from" address;
-- COALESCE leaves the stored ip untouched when the refresh carried none.
UPDATE user_session
SET prev_refresh_hash = refresh_hash,
    refresh_hash = sqlc.arg(refresh_hash),
    rotated_at = now(),
    last_used_at = now(),
    ip = COALESCE(sqlc.narg(ip), ip)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
RETURNING *;

-- name: RevokeSession :execrows
UPDATE user_session
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeAllSessionsForUser :execrows
UPDATE user_session
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- RevokeSessionForUser owner-scopes a single revoke: only a row belonging to the
-- caller is touched, so a guessed id can't log out someone else. 0 rows affected =
-- not found (or not yours), which the service maps to 404.
-- name: RevokeSessionForUser :execrows
UPDATE user_session
SET revoked_at = now()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- RevokeOtherSessionsForUser is "log out other devices": every active session for
-- the caller except the one they're currently on (keep_id = the token's sid).
-- name: RevokeOtherSessionsForUser :execrows
UPDATE user_session
SET revoked_at = now()
WHERE user_id = sqlc.arg(user_id) AND id <> sqlc.arg(keep_id) AND revoked_at IS NULL;

-- ListActiveSessionDevicesByUser is the unified device list: each live session with
-- its push subscription (if any). LEFT JOIN so a session without push still lists;
-- push_subscription_id NULL means push is off for that device.
-- name: ListActiveSessionDevicesByUser :many
SELECT us.*, ps.id AS push_subscription_id, ps.last_notified_at AS push_last_notified_at
FROM user_session us
LEFT JOIN push_subscription ps ON ps.session_id = us.id
WHERE us.user_id = sqlc.arg(user_id) AND us.revoked_at IS NULL AND us.refresh_expires_at > now()
ORDER BY us.last_used_at DESC;

-- DeleteTerminalSessions garbage-collects sessions that are terminal (revoked, or
-- past their absolute expiry) and have been so since before the retention cutoff.
-- COALESCE keys off revoked_at when set, else refresh_expires_at; an active
-- session's future expiry never falls before the cutoff, so only dead rows match.
-- The durable auth record lives in auth_audit (no session FK), so these rows are
-- pure garbage; each deleted session's push subscription cascades away with it.
-- name: DeleteTerminalSessions :execrows
DELETE FROM user_session
WHERE COALESCE(revoked_at, refresh_expires_at) < sqlc.arg(before);
