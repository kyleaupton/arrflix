-- Tracking requesters: live per-requester intent and the dedup association.

-- AddRequester joins a user to a tracking, or refreshes their tier if they were
-- already a requester. Idempotent on (tracking_id, user_id).
-- name: AddRequester :one
INSERT INTO tracking_requester (tracking_id, user_id, tier)
VALUES (sqlc.arg(tracking_id), sqlc.arg(user_id), sqlc.arg(tier))
ON CONFLICT (tracking_id, user_id) DO UPDATE
SET tier = excluded.tier,
    updated_at = now()
RETURNING *;

-- FindRequester checks whether a user is already a requester on a tracking. A
-- no-row result is a normal "not yet a requester" signal (NotFound via FromPg).
-- name: FindRequester :one
SELECT * FROM tracking_requester
WHERE tracking_id = sqlc.arg(tracking_id) AND user_id = sqlc.arg(user_id);

-- name: ListRequestersByTracking :many
SELECT * FROM tracking_requester
WHERE tracking_id = $1
ORDER BY created_at ASC;

-- name: RemoveRequester :exec
DELETE FROM tracking_requester
WHERE tracking_id = sqlc.arg(tracking_id) AND user_id = sqlc.arg(user_id);
