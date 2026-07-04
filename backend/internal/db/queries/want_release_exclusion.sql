-- want_release_exclusion: per-want release blocklist. See migration 0017.

-- AddWantReleaseExclusion records that a release (indexer_id, guid) must not be
-- re-picked for a want. Idempotent: ON CONFLICT DO NOTHING keeps the first
-- row's reason/detail — the earliest cause is the interesting one, and a
-- re-failure of the same release adds no information.
-- name: AddWantReleaseExclusion :exec
INSERT INTO want_release_exclusion (want_id, indexer_id, guid, reason, detail)
VALUES (
  sqlc.arg(want_id),
  sqlc.arg(indexer_id),
  sqlc.arg(guid),
  sqlc.arg(reason),
  sqlc.narg(detail)
)
ON CONFLICT (want_id, indexer_id, guid) DO NOTHING;

-- ListWantReleaseExclusionsForWants returns the exclusions for a set of wants in
-- one round trip — the processed want plus its in-flight season siblings — so the
-- front-half consults them all before searching. Mirrors GrabWantsForPack's
-- id-array batching.
-- name: ListWantReleaseExclusionsForWants :many
SELECT * FROM want_release_exclusion
WHERE want_id = ANY(sqlc.arg(want_ids)::uuid[]);
