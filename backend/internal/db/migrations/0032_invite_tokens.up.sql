-- Invite tokens: an invite becomes a token-bearing magic link. It carries a
-- target role and an expiry; the raw token lives only in the emailed/copied link,
-- and only its sha256 hash is stored (a DB read can't reconstruct a usable link).
--
-- Existing token-less rows keep a NULL token_hash / expires_at: they remain
-- claimable by a matching Plex-verified email (LoginWithPlex still email-matches),
-- but not via the link-acceptance flow. Re-invite to issue a link. role defaults
-- to 'requester' so existing rows get the same default the code hardcoded before.
ALTER TABLE user_invite
  ADD COLUMN IF NOT EXISTS token_hash BYTEA,
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'requester';

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_invite_token_hash
  ON user_invite (token_hash);
