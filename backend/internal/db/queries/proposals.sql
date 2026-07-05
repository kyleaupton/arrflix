-- proposal: the deferred grab. See migration 0018. Proposals are ephemeral —
-- created when a propose-segment want picks a release, updated in place on a
-- strictly-better later pick (supersede), deleted on approve/decline. An "open"
-- proposal is one that still exists.

-- name: CreateProposal :one
INSERT INTO proposal (
  tracking_id,
  media_item_id,
  is_pack,
  protocol,
  media_type,
  season_id,
  episode_id,
  indexer_id,
  guid,
  candidate_title,
  candidate_link,
  downloader_id,
  library_id,
  name_template_id,
  size,
  seeders,
  bin_source,
  bin_resolution,
  bin_modifier,
  score
)
VALUES (
  sqlc.arg(tracking_id),
  sqlc.arg(media_item_id),
  sqlc.arg(is_pack),
  sqlc.arg(protocol),
  sqlc.arg(media_type),
  sqlc.arg(season_id),
  sqlc.arg(episode_id),
  sqlc.arg(indexer_id),
  sqlc.arg(guid),
  sqlc.arg(candidate_title),
  sqlc.arg(candidate_link),
  sqlc.arg(downloader_id),
  sqlc.arg(library_id),
  sqlc.arg(name_template_id),
  sqlc.arg(size),
  sqlc.arg(seeders),
  sqlc.arg(bin_source),
  sqlc.arg(bin_resolution),
  sqlc.arg(bin_modifier),
  sqlc.arg(score)
)
RETURNING *;

-- name: GetProposal :one
SELECT * FROM proposal
WHERE id = $1;

-- GetProposalForUpdate locks the proposal row for the duration of the tx —
-- approve/decline/supersede all take it so concurrent resolutions serialize. A
-- 0-row match (the proposal was already resolved) surfaces as pgx.ErrNoRows.
-- name: GetProposalForUpdate :one
SELECT * FROM proposal
WHERE id = $1
FOR UPDATE;

-- FindOpenProposalForWant returns the proposal covering a want, if any. Because a
-- proposal is deleted the moment it's resolved, "a proposal_want edge exists" is
-- the same as "an open proposal exists". Backs the propose-or-supersede decision.
-- A 0-row match surfaces as pgx.ErrNoRows.
-- name: FindOpenProposalForWant :one
SELECT p.* FROM proposal p
JOIN proposal_want pw ON pw.proposal_id = p.id
WHERE pw.want_id = $1
LIMIT 1;

-- name: ListProposalsForTracking :many
SELECT * FROM proposal
WHERE tracking_id = $1
ORDER BY created_at DESC;

-- UpdateProposal replaces a proposal's release/display/bin/score columns in place
-- — the supersede write, keeping the same id (and thus the same UI card) while
-- swapping in a strictly-better later pick. The covered set is re-linked
-- separately via proposal_want.
-- name: UpdateProposal :one
UPDATE proposal
SET is_pack = sqlc.arg(is_pack),
    protocol = sqlc.arg(protocol),
    media_type = sqlc.arg(media_type),
    season_id = sqlc.arg(season_id),
    episode_id = sqlc.arg(episode_id),
    indexer_id = sqlc.arg(indexer_id),
    guid = sqlc.arg(guid),
    candidate_title = sqlc.arg(candidate_title),
    candidate_link = sqlc.arg(candidate_link),
    downloader_id = sqlc.arg(downloader_id),
    library_id = sqlc.arg(library_id),
    name_template_id = sqlc.arg(name_template_id),
    size = sqlc.arg(size),
    seeders = sqlc.arg(seeders),
    bin_source = sqlc.arg(bin_source),
    bin_resolution = sqlc.arg(bin_resolution),
    bin_modifier = sqlc.arg(bin_modifier),
    score = sqlc.arg(score),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- DeleteProposal removes a resolved proposal (its proposal_want edges cascade).
-- RETURNING id makes a concurrent double-resolve a clean 0-row conflict rather
-- than a silent no-op — the caller reads pgx.ErrNoRows as "already resolved".
-- name: DeleteProposal :one
DELETE FROM proposal
WHERE id = $1
RETURNING id;

-- LinkProposalWant records that a proposal would advance a want — the M:N edge,
-- mirror of LinkDownloadJobWant. Idempotent via ON CONFLICT DO NOTHING.
-- name: LinkProposalWant :exec
INSERT INTO proposal_want (proposal_id, want_id)
VALUES (sqlc.arg(proposal_id), sqlc.arg(want_id))
ON CONFLICT (proposal_id, want_id) DO NOTHING;

-- UnlinkProposalWant removes one proposal↔want edge — the inverse, used on
-- supersede when a want drops out of the newer pick's coverage.
-- name: UnlinkProposalWant :exec
DELETE FROM proposal_want
WHERE proposal_id = sqlc.arg(proposal_id) AND want_id = sqlc.arg(want_id);

-- ListWantsByProposal returns the wants a proposal covers, joined through
-- proposal_want. Mirror of ListWantsByDownloadJob.
-- name: ListWantsByProposal :many
SELECT w.* FROM want w
JOIN proposal_want pw ON pw.want_id = w.id
WHERE pw.proposal_id = $1
ORDER BY w.created_at ASC;

-- ListWantIDsByProposal returns just the covered want ids — the set approve/
-- decline iterate over without hydrating full want rows.
-- name: ListWantIDsByProposal :many
SELECT want_id FROM proposal_want
WHERE proposal_id = $1;

-- ListCoveredEpisodeIDsForProposals returns each proposal's covered episode ids
-- (via the covered wants) in one round trip, so the read view-model can map an
-- episode to its proposal in the series grid. Movie wants have a NULL episode_id
-- and are filtered out.
-- name: ListCoveredEpisodeIDsForProposals :many
SELECT pw.proposal_id, w.episode_id
FROM proposal_want pw
JOIN want w ON w.id = pw.want_id
WHERE pw.proposal_id = ANY(sqlc.arg(proposal_ids)::uuid[])
  AND w.episode_id IS NOT NULL;
