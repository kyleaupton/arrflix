-- Downloaders store configuration for torrent/usenet clients
CREATE TABLE IF NOT EXISTS downloader (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('qbittorrent')),
  protocol TEXT NOT NULL CHECK (protocol IN ('torrent', 'usenet')),
  url TEXT NOT NULL,
  username TEXT,
  password TEXT,
  config_json JSONB,
  enabled BOOLEAN NOT NULL DEFAULT true,
  "default" BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_downloader_name_ci ON downloader (lower(name));
CREATE INDEX IF NOT EXISTS idx_downloader_type ON downloader (type);
CREATE INDEX IF NOT EXISTS idx_downloader_protocol ON downloader (protocol);
CREATE INDEX IF NOT EXISTS idx_downloader_enabled ON downloader (enabled);
CREATE UNIQUE INDEX IF NOT EXISTS uq_downloader_default_protocol ON downloader (protocol) WHERE "default" = true;
