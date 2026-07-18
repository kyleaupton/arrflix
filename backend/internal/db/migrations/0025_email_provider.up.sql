-- Email provider stores the single configured email-sending provider.
-- Singleton: at most one row ever (see the expression unique index below).
-- SMTP fields are first-class columns so the write-only password handling
-- stays clean and the row is queryable; config_json is reserved for future
-- HTTP-API providers behind the transport seam.
CREATE TABLE IF NOT EXISTS email_provider (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  provider TEXT NOT NULL CHECK (provider IN ('smtp')),
  from_address TEXT NOT NULL,
  from_name TEXT,
  reply_to TEXT,
  host TEXT,
  port INT,
  security TEXT CHECK (security IN ('starttls','implicit_tls','none')),
  auth BOOLEAN NOT NULL DEFAULT true,
  username TEXT,
  password TEXT,                       -- plaintext (convention); write-only on the wire
  skip_tls_verify BOOLEAN NOT NULL DEFAULT false,
  config_json JSONB,                   -- reserved for future providers
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Singleton: the whole table collapses to one uniqueness bucket, so a second
-- INSERT violates the constraint. The service does get-or-create instead of
-- relying on ON CONFLICT against this expression index.
CREATE UNIQUE INDEX IF NOT EXISTS uq_email_provider_singleton ON email_provider ((true));
