-- Quality profiles: the persisted half of the pure internal/qualityprofile
-- engine. A profile carries two kinds of constraint — structured bin identity
-- (ranked allowed bins + cutoff) and condition trees (hard gates, scored custom
-- formats) — plus runtime-scaled size bands. The service resolver
-- (service/quality.go) assembles a row into the in-memory qualityprofile.Profile
-- the engine evaluates.
--
-- Structured JSONB columns (bins, cutoff, gates, size_overrides, indexers) follow
-- the routing-conditions philosophy: each value is loaded and written whole,
-- never queried by interior node. bins is an ordered array of
-- {source,resolution,modifier} (rank = array order); cutoff is one such object;
-- gates is [{name, tree}] where tree is the canonical rules.Condition JSON;
-- size_overrides is [{bin:{...}, min, preferred, max}]. The (source,resolution,
-- modifier) triple is parsing.BinKey's identity — see internal/parsing/bin.go.
--
-- indexers and gates live as JSONB on the profile (loaded whole, empty in the v1
-- presets). A future join table is the FK-correct option for indexers but is
-- unnecessary now. Preset profile + tier_binding rows are NOT seeded here — that
-- is the service's idempotent seeding from qualityprofile.DefaultProfiles() (P4).
CREATE TABLE IF NOT EXISTS quality_profile (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name TEXT NOT NULL,
  domain TEXT NOT NULL CHECK (domain IN ('movie','series')),
  bins JSONB NOT NULL DEFAULT '[]',
  cutoff JSONB NOT NULL,
  min_seeders INT NOT NULL DEFAULT 0,
  indexers JSONB NOT NULL DEFAULT '[]',
  gates JSONB NOT NULL DEFAULT '[]',
  size_overrides JSONB NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Custom formats: a reusable named scorer (the *arr-style entity). conditions is
-- the canonical rules.Condition tree, stored whole. A format earns its weight on
-- a profile via the quality_profile_format join.
CREATE TABLE IF NOT EXISTS custom_format (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name TEXT NOT NULL,
  domain TEXT NOT NULL CHECK (domain IN ('movie','series')),
  conditions JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Join: a profile assigns a weight to a custom format. Deleting either side
-- removes the assignment.
CREATE TABLE IF NOT EXISTS quality_profile_format (
  profile_id UUID NOT NULL REFERENCES quality_profile(id) ON DELETE CASCADE,
  custom_format_id UUID NOT NULL REFERENCES custom_format(id) ON DELETE CASCADE,
  weight INT NOT NULL,
  PRIMARY KEY (profile_id, custom_format_id)
);

-- Global per-domain per-bin size defaults in bytes-per-minute. The engine scales
-- these by a release's runtime (minutes) to a byte range; a profile's
-- size_overrides apply on top. A bin with no row here is unbounded — the safe
-- default while runtime population rolls out. A zero bound means "unbounded" for
-- that side, matching the engine.
CREATE TABLE IF NOT EXISTS quality_size_default (
  domain TEXT NOT NULL CHECK (domain IN ('movie','series')),
  source TEXT NOT NULL,
  resolution TEXT NOT NULL,
  modifier TEXT NOT NULL,
  min_bytes_per_min BIGINT NOT NULL DEFAULT 0,
  preferred_bytes_per_min BIGINT NOT NULL DEFAULT 0,
  max_bytes_per_min BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (domain, source, resolution, modifier)
);

-- Binds a coarse (tier, domain) selection to a concrete profile. Seeded by the
-- service (P4), not here.
CREATE TABLE IF NOT EXISTS tier_binding (
  tier TEXT NOT NULL CHECK (tier IN ('HD','4K')),
  domain TEXT NOT NULL CHECK (domain IN ('movie','series')),
  profile_id UUID NOT NULL REFERENCES quality_profile(id) ON DELETE CASCADE,
  PRIMARY KEY (tier, domain)
);

-- Seed size defaults for the HD/4K bins that appear in
-- qualityprofile.DefaultProfiles(). min/preferred/max are ported verbatim from
-- Sonarr's QualityDefinition defaults in
-- submodules/sonarr/src/NzbDrone.Core/Qualities/Quality.cs (DefaultQualityDefinitions),
-- which are MB/min — converted here to bytes/min via MiB (×1048576), matching
-- upstream's MinSize.Megabytes() in AcceptableSizeSpecification.cs. Upstream's
-- null (unbounded) max becomes 0, the engine's "unbounded" sentinel. The bin
-- identity (source,resolution,modifier) is parsing.BinKey's string vocabulary:
-- modifier '' is ModNone, 'REMUX' is ModRemux. Both domains share the same bin
-- identities, so the rows are the cross product of {movie,series} × the bins.
INSERT INTO quality_size_default
  (domain, source, resolution, modifier, min_bytes_per_min, preferred_bytes_per_min, max_bytes_per_min)
SELECT d.domain, b.source, b.resolution, b.modifier,
       (b.min_mb * 1048576)::bigint,
       (b.pref_mb * 1048576)::bigint,
       (b.max_mb * 1048576)::bigint
FROM (VALUES ('movie'), ('series')) AS d(domain)
CROSS JOIN (VALUES
  -- source,   resolution, modifier, min_mb, pref_mb, max_mb   (Quality.cs name)
  ('TV',     '720p',  '',      3,  95, 125),   -- HDTV-720p
  ('WEB-DL', '720p',  '',      3,  95, 130),   -- WEBDL-720p
  ('WEBRip', '720p',  '',      3,  95, 130),   -- WEBRip-720p
  ('BluRay', '720p',  '',      4,  95, 130),   -- Bluray-720p
  ('TV',     '1080p', '',      4,  95, 125),   -- HDTV-1080p
  ('WEB-DL', '1080p', '',      4,  95, 130),   -- WEBDL-1080p
  ('WEBRip', '1080p', '',      4,  95, 130),   -- WEBRip-1080p
  ('BluRay', '1080p', '',      4,  95, 155),   -- Bluray-1080p
  ('BluRay', '1080p', 'REMUX', 35, 95, 0),     -- Bluray-1080p Remux (max null → unbounded)
  ('TV',     '2160p', '',      35, 95, 199.9), -- HDTV-2160p
  ('WEB-DL', '2160p', '',      35, 95, 0),     -- WEBDL-2160p (max null)
  ('WEBRip', '2160p', '',      35, 95, 0),     -- WEBRip-2160p (max null)
  ('BluRay', '2160p', '',      35, 95, 0),     -- Bluray-2160p (max null)
  ('BluRay', '2160p', 'REMUX', 35, 95, 0)      -- Bluray-2160p Remux (max null)
) AS b(source, resolution, modifier, min_mb, pref_mb, max_mb)
ON CONFLICT (domain, source, resolution, modifier) DO NOTHING;
