-- A user_session is the first-class record of "this device is logged in as this
-- user". The short-lived access JWT is a stateless projection of it; this row is
-- the source of truth for authentication, and the refresh token is how a client
-- mints a fresh access token without re-entering credentials.
--
-- refresh_hash / prev_refresh_hash store sha256(secret), never the raw token:
-- the wire token is "<id>.<secret>", so we look up by id and compare the hash.
-- Two generations are kept so a benign rotation race (two tabs refreshing at
-- once) can leapfrog within a short grace window instead of tripping reuse
-- detection; a presented secret that matches neither is treated as theft and
-- revokes the row.
CREATE TABLE IF NOT EXISTS user_session (
  id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id            UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  refresh_hash       BYTEA NOT NULL,
  prev_refresh_hash  BYTEA,
  rotated_at         TIMESTAMPTZ,
  refresh_expires_at TIMESTAMPTZ NOT NULL,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at         TIMESTAMPTZ,
  user_agent         TEXT,   -- best-effort device label for the sessions UI
  ip                 TEXT,   -- coarse "last seen from"; populated opportunistically
  label              TEXT    -- user-renamable device name
);

-- The active-sessions lookup for a user (the devices UI, "log out everywhere"),
-- and the join push delivery uses to skip revoked logins.
CREATE INDEX IF NOT EXISTS idx_user_session_user_active
  ON user_session (user_id) WHERE revoked_at IS NULL;
