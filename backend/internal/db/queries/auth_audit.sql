-- name: InsertAuthAudit :exec
-- Append an auth audit row. The service owns the per-event detail shape; this
-- layer just persists the pre-marshaled JSON. user_id is nullable (app_user
-- ON DELETE SET NULL) so the trail survives account deletion.
INSERT INTO auth_audit (user_id, event, detail)
VALUES ($1, $2, $3);
