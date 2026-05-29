-- Phase 4 of the matcher cutover (specs/modules/matching/README.md §
-- Re-match, un-match, detach). Adds `detached` as a 7th match_outcome
-- enum value.
--
-- Why a distinct outcome rather than reusing `no_match`:
--   - `no_match` means "we don't know what this file is" — the file
--     stays in the library scope as an inbox item awaiting human pick.
--   - `detached` means "this file doesn't belong in the library at all"
--     — the file is removed from arrflix's view (optionally quarantined
--     on disk). The audit row preserves the decision; no follow-up
--     match is expected.
-- Keeping them distinct lets the Activity view + matcher inbox
-- correctly filter "needs a decision" from "user said no, this isn't
-- mine." Distinguishing only via `decided_by: user:<id>` collapses the
-- two when reading the chain back.

ALTER TYPE match_outcome ADD VALUE IF NOT EXISTS 'detached';
