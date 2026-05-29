-- Match decision queries — the matcher's decision-log artifact.
-- See specs/modules/matching/README.md § "The match-decision artifact".

-- name: InsertMatchDecision :one
insert into match_decision (
    file_id,
    outcome,
    chosen_source,
    chosen_external_id,
    chosen_season,
    chosen_episode,
    chosen_edition,
    confidence,
    ranked_candidates,
    resolvers_consulted,
    evidence,
    evidence_truncated,
    decided_by
)
values (
    sqlc.arg(file_id),
    sqlc.arg(outcome),
    sqlc.arg(chosen_source),
    sqlc.arg(chosen_external_id),
    sqlc.arg(chosen_season),
    sqlc.arg(chosen_episode),
    sqlc.arg(chosen_edition),
    sqlc.arg(confidence),
    sqlc.arg(ranked_candidates),
    sqlc.arg(resolvers_consulted),
    sqlc.arg(evidence),
    sqlc.arg(evidence_truncated),
    sqlc.arg(decided_by)
)
returning id;

-- name: SupersedeMatchDecision :exec
-- Marks the prior current decision for a file as superseded by the
-- given decision ID. The "prior" row is the one row for this file_id
-- whose superseded_at is still NULL and whose id differs from the
-- new one. A no-op when no prior current exists (first-ever match).
update match_decision
set superseded_at = now(),
    superseded_by = sqlc.arg(superseding_id)
where file_id = sqlc.arg(file_id)
  and superseded_at is null
  and id <> sqlc.arg(superseding_id);

-- name: GetCurrentMatchDecision :one
-- Returns the current (non-superseded) decision for a file, if any.
select id, file_id, outcome, chosen_source, chosen_external_id,
       chosen_season, chosen_episode, chosen_edition, confidence,
       ranked_candidates, resolvers_consulted, evidence, evidence_truncated,
       decided_by, decided_at, superseded_at, superseded_by
from match_decision
where file_id = sqlc.arg(file_id)
  and superseded_at is null
order by decided_at desc
limit 1;

-- name: ListMatchDecisionHistoryForFile :many
-- Returns every decision for a file in newest-first order. Used by the
-- evidence-history endpoint (?include_history=true) to render the
-- supersede chain.
select id, file_id, outcome, chosen_source, chosen_external_id,
       chosen_season, chosen_episode, chosen_edition, confidence,
       ranked_candidates, resolvers_consulted, evidence, evidence_truncated,
       decided_by, decided_at, superseded_at, superseded_by
from match_decision
where file_id = sqlc.arg(file_id)
order by decided_at desc;
