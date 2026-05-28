-- match_decision is the matcher's decision-log artifact: one row per
-- consequential identity decision (auto-match, manual match, re-match,
-- un-match). Current match for a file = the latest non-superseded row.
--
-- See specs/modules/matching/README.md § "The match-decision artifact".
-- All six outcome bands ship in v1 even though phase-1 (no real
-- resolvers registered) only emits no_match in practice; carrying the
-- full enum from day one avoids a later ALTER TYPE.

CREATE TYPE match_outcome AS ENUM (
    'confident',
    'confident_review',
    'low_confidence',
    'ambiguous',
    'no_match',
    'partial_series'
);

CREATE TABLE match_decision (
    id                  BIGSERIAL PRIMARY KEY,
    -- file_id is the file being identified. Deliberately not an FK in
    -- phase 1: the file may live in either media_file or unmatched_file
    -- depending on the outcome, and the canonical producer of the ID is
    -- the future matcher-aware scan loop (phase 3). Until that lands the
    -- column is opaque-but-stable.
    file_id             UUID NOT NULL,
    outcome             match_outcome NOT NULL,
    -- chosen_* fields are populated only for confident /
    -- confident_review / low_confidence / partial_series outcomes —
    -- i.e. whenever a winning candidate exists. NULL otherwise.
    chosen_source       TEXT,
    chosen_external_id  TEXT,
    chosen_season       INT,
    chosen_episode      INT,
    chosen_edition      TEXT,
    confidence          DOUBLE PRECISION NOT NULL,
    -- resolvers_consulted is the top-line audit trail: array of
    -- {name, tier, candidate_count, top_confidence}. Cheap to scan.
    resolvers_consulted JSONB NOT NULL,
    -- evidence carries the per-resolver raw payloads, capped at 8KB
    -- (matching spec OQ#10). Truncation order is deterministic: largest
    -- payload first, then by key name.
    evidence            JSONB NOT NULL,
    evidence_truncated  BOOLEAN NOT NULL DEFAULT false,
    -- decided_by names the actor: 'auto' for the aggregator, 'user:<id>'
    -- for manual re-match / un-match, 'rule:<id>' for future
    -- bulk-override surfaces (matching spec § Killer UX moves #3).
    decided_by          TEXT NOT NULL,
    decided_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- supersession chain: a re-match writes a new row and back-points
    -- the prior current row at it. Reversibility (the antidote to
    -- Sonarr's irreversible-wrong-match story) walks this chain.
    superseded_at       TIMESTAMPTZ,
    superseded_by       BIGINT REFERENCES match_decision(id)
);

CREATE INDEX match_decision_file_id_idx ON match_decision (file_id);

-- Partial index for the "current match for a file" query — the single
-- hottest read path in the matcher inbox and the scan re-process loop.
CREATE INDEX match_decision_current_idx
    ON match_decision (file_id)
    WHERE superseded_at IS NULL;
