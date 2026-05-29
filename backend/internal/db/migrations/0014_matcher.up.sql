-- Matcher schema: the decision-log artifact plus the unmatched_file
-- columns the matcher's low-confidence outcomes write.
--
-- See specs/modules/matching/README.md § "The match-decision artifact"
-- and § "What evolves".

-- match_outcome is the confidence band the aggregator assigns, plus the
-- user-only `detached` action. The aggregator emits the first six;
-- `detached` is written only by the manual detach flow ("this file
-- doesn't belong in the library at all"). It's distinct from `no_match`
-- ("we don't know what this is") so the inbox can filter "needs a
-- decision" from "user said no" when reading the chain back.
CREATE TYPE match_outcome AS ENUM (
    'confident',
    'confident_review',
    'low_confidence',
    'ambiguous',
    'no_match',
    'partial_series',
    'detached'
);

-- match_decision is the matcher's decision-log artifact: one row per
-- consequential identity decision (auto-match, manual match, re-match,
-- un-match, detach). Current match for a file = the latest non-superseded
-- row.
CREATE TABLE match_decision (
    id                  BIGSERIAL PRIMARY KEY,
    -- file_id is the file being identified. Deliberately not an FK: the
    -- file may live in either media_file or unmatched_file depending on
    -- the outcome, so there's no single FK target. The scan loop and the
    -- manual-match flow both keep this id stable across transitions.
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
    -- for manual re-match / un-match / detach, 'rule:<id>' for future
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

-- partial_series flag on unmatched_file. The matcher's partial_series
-- outcome means "series identity resolved but the episode is unresolved"
-- — the file still needs a human to land an episode pick. It writes
-- unmatched_file like the other low-band outcomes, distinguished by this
-- column. Default false so the common case passes through unchanged.
ALTER TABLE unmatched_file
    ADD COLUMN partial_series BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX idx_unmatched_file_partial_series
    ON unmatched_file (library_id)
    WHERE partial_series = true;
