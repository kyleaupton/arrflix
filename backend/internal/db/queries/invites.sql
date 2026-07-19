-- name: UpsertInvite :one
-- Create or re-issue an invite. Re-inviting an email (ON CONFLICT) regenerates the
-- token/expiry/role and clears claimed_at — the admin "resend" gesture.
INSERT INTO user_invite (email, invited_by, token_hash, expires_at, role)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (email) DO UPDATE SET
  invited_by = EXCLUDED.invited_by,
  token_hash = EXCLUDED.token_hash,
  expires_at = EXCLUDED.expires_at,
  role       = EXCLUDED.role,
  claimed_at = NULL,
  created_at = now()
RETURNING *;

-- name: GetInviteByEmail :one
SELECT * FROM user_invite
WHERE lower(email) = lower($1) AND claimed_at IS NULL;

-- name: GetInviteByTokenHash :one
SELECT * FROM user_invite
WHERE token_hash = $1 AND claimed_at IS NULL AND expires_at > now();

-- name: ClaimInvite :exec
UPDATE user_invite
SET claimed_at = now()
WHERE id = $1 AND claimed_at IS NULL;

-- name: ListInvites :many
SELECT * FROM user_invite
ORDER BY created_at DESC;

-- name: DeleteInvite :exec
DELETE FROM user_invite WHERE id = $1;
