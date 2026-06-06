-- Name templates define file naming patterns for movies and series
CREATE TABLE IF NOT EXISTS name_template (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('movie','series')),
  template TEXT NOT NULL,
  movie_dir_template TEXT,
  series_show_template TEXT,
  series_season_template TEXT,
  "default" BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_name_template_name_ci ON name_template (lower(name));
CREATE INDEX IF NOT EXISTS idx_name_template_type ON name_template (type);
CREATE UNIQUE INDEX IF NOT EXISTS uq_name_template_default_type ON name_template (type) WHERE "default" = true;

-- Routing rules: one global ordered dispatch list evaluated at grab time.
-- Conditions are the rules substrate's canonical JSON tree, stored whole —
-- a tree is always loaded and written as a unit, never queried by interior
-- node. Action targets are nullable FKs: a deleted downloader/library/template
-- nulls the slot (the rule still matches, the slot falls back to the default).
CREATE TABLE IF NOT EXISTS routing_rule (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT true,
  "position" INTEGER NOT NULL,
  conditions JSONB NOT NULL,
  downloader_id UUID REFERENCES downloader(id) ON DELETE SET NULL,
  library_id UUID REFERENCES library(id) ON DELETE SET NULL,
  name_template_id UUID REFERENCES name_template(id) ON DELETE SET NULL,
  "continue" BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- Deferred so a single reorder statement can permute positions without
  -- tripping per-row uniqueness mid-update.
  CONSTRAINT uq_routing_rule_position UNIQUE ("position") DEFERRABLE INITIALLY DEFERRED
);
