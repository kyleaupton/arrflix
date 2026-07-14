-- Web Push: the server's VAPID identity (singleton) and each browser's push
-- subscription. See specs/modules/notifications/README.md.

-- vapid_config holds the single VAPID keypair the server signs push requests
-- with. Generated once on first boot and never rotated silently (rotation
-- invalidates every existing subscription). Singleton, like email_provider.
--
-- private_key is the ES256 signing secret: plaintext by convention, write-only
-- on the wire (never serialized to an HTTP response). public_key is safe to
-- expose — the browser needs it as applicationServerKey to subscribe.
--
-- subject is the VAPID `sub` claim: a mailto:/https: contact URI a push service
-- may use to reach the operator. It is NOT an address we send mail to and need
-- not be a working inbox; push services essentially never contact it. Defaulted
-- from the admin email when present, else a placeholder, and operator-editable.
CREATE TABLE IF NOT EXISTS vapid_config (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  public_key TEXT NOT NULL,
  private_key TEXT NOT NULL,           -- ES256 secret; write-only on the wire
  subject TEXT NOT NULL,               -- VAPID `sub` contact URI (mailto:/https:)
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Singleton: the whole table collapses to one uniqueness bucket, so a second
-- INSERT violates the constraint. The service does get-or-create rather than
-- relying on ON CONFLICT against this expression index.
CREATE UNIQUE INDEX IF NOT EXISTS uq_vapid_config_singleton ON vapid_config ((true));

-- push_subscription is one browser's Web Push registration for one user. A user
-- may hold several (one per device/browser), so the push adapter fans a single
-- outbox row out to every row for the recipient.
--
-- endpoint is the push service's per-subscription delivery URL and is globally
-- unique — it is the natural key. A re-subscribe from the same browser yields
-- the same endpoint, so the store upserts on it (refreshing the encryption keys)
-- rather than accumulating duplicates. p256dh/auth are the client's ECDH public
-- key and auth secret (base64url) the adapter encrypts each payload with.
--
-- A 404/410 from the endpoint means the subscription is dead; the adapter prunes
-- that row. user_id CASCADEs so a deleted user's subscriptions vanish with them.
CREATE TABLE IF NOT EXISTS push_subscription (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  endpoint TEXT NOT NULL UNIQUE,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  user_agent TEXT,                     -- best-effort device label for the UI
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The fan-out lookup: all live subscriptions for a recipient.
CREATE INDEX IF NOT EXISTS idx_push_subscription_user
  ON push_subscription (user_id);
