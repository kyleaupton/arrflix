-- proposal: the deferred grab — the persistence for acquisition autonomy's
-- 'propose' rung. When a tracking's effective autonomy for a want's segment is
-- 'propose', the acquisition front-half searches → gates → scores → picks exactly
-- like 'auto', then instead of grabbing writes a proposal (the picked release,
-- held for one-tap approval) and parks the covered want(s) at hold='proposed'.
--
-- Approve replays the stored resolved-grab columns into the existing grab path —
-- a pure DB replay, no re-parse and no routing.Dispatch that could 502 at approve
-- time. Decline excludes the release (want_release_exclusion, reason 'declined')
-- and re-arms the covered wants so the next search proposes a different release.
--
-- Two shipped CHECKs are widened here (0016/0017 can't be edited in place): the
-- want.hold vocabulary gains 'proposed' and the want_release_exclusion.reason
-- vocabulary gains 'declined'.

ALTER TABLE want
  DROP CONSTRAINT want_hold_check,
  ADD CONSTRAINT want_hold_check CHECK (hold IN ('needs_pick', 'proposed'));

ALTER TABLE want_release_exclusion
  DROP CONSTRAINT want_release_exclusion_reason_check,
  ADD CONSTRAINT want_release_exclusion_reason_check CHECK (reason IN ('download_failed', 'declined'));

-- proposal has no status column: proposals are ephemeral, like download_candidate
-- and unlike want — deleted on approve/decline, updated in place on supersede. An
-- open proposal is simply one that still exists (its proposal_want links resolve).
--
-- The resolved-grab columns mirror CreateDownloadJobParams so Approve rebuilds
-- that struct with zero external I/O. downloader_id/library_id/name_template_id
-- are bare UUID columns (no FK), matching download_job's posture for those three,
-- so tests need not materialize those rows. tracking_id/media_item_id/season_id/
-- episode_id keep FKs. bin_source/bin_resolution/bin_modifier + score are the
-- supersede comparison keys (a strictly-better later pick replaces the release in
-- place); size/seeders are display only.
CREATE TABLE IF NOT EXISTS proposal (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tracking_id UUID NOT NULL REFERENCES tracking(id) ON DELETE CASCADE,
  media_item_id UUID NOT NULL REFERENCES media_item(id) ON DELETE RESTRICT,

  is_pack BOOLEAN NOT NULL DEFAULT false,

  -- Resolved-grab columns (mirror CreateDownloadJobParams).
  protocol TEXT NOT NULL CHECK (protocol IN ('torrent', 'usenet')),
  media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'series')),
  season_id UUID REFERENCES media_season(id) ON DELETE RESTRICT,
  episode_id UUID REFERENCES media_episode(id) ON DELETE RESTRICT,
  indexer_id BIGINT NOT NULL,
  guid TEXT NOT NULL,
  candidate_title TEXT NOT NULL,
  candidate_link TEXT NOT NULL,
  downloader_id UUID NOT NULL,
  library_id UUID NOT NULL,
  name_template_id UUID NOT NULL,

  -- Display.
  size BIGINT NOT NULL DEFAULT 0,
  seeders INT NOT NULL DEFAULT 0,

  -- Supersede comparison keys: the picked bin identity + custom-format score.
  bin_source TEXT NOT NULL,
  bin_resolution TEXT NOT NULL,
  bin_modifier TEXT NOT NULL,
  score INT NOT NULL DEFAULT 0,

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_proposal_tracking ON proposal(tracking_id);
CREATE INDEX IF NOT EXISTS idx_proposal_media ON proposal(media_item_id);

-- proposal_want: the M:N edge between a proposal and the wants it would advance —
-- a verbatim mirror of download_job_want. A single proposal links its one want; a
-- pack proposal links every in-flight sibling it covers, so approve/decline act on
-- the whole covered set from any covered episode. Both FKs cascade.
CREATE TABLE IF NOT EXISTS proposal_want (
  proposal_id UUID NOT NULL REFERENCES proposal(id) ON DELETE CASCADE,
  want_id UUID NOT NULL REFERENCES want(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (proposal_id, want_id)
);

CREATE INDEX IF NOT EXISTS idx_pw_proposal ON proposal_want(proposal_id);
CREATE INDEX IF NOT EXISTS idx_pw_want ON proposal_want(want_id);
