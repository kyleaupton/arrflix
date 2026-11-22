-- name: ListUserGrants :many
-- All grants that apply to the user: direct user-subject grants plus grants on
-- any role the user holds. The user_role join is internal to the query, so the
-- repo method takes just a user id.
SELECT g.* FROM permission_grant g
WHERE (g.subject_type = 'user' AND g.subject_id = @user_id)
   OR (g.subject_type = 'role' AND g.subject_id IN (
        SELECT ur.role_id FROM user_role ur WHERE ur.user_id = @user_id
   ));

-- name: DeleteUserGrantsByKeys :exec
-- Remove the user's own (user-subject) grants on the given keys. Role grants are
-- never touched.
DELETE FROM permission_grant
WHERE subject_type = 'user' AND subject_id = @user_id
  AND permission_key = ANY(@keys::text[]);

-- name: InsertUserGrants :exec
-- Grant the user each of the given keys globally (unscoped, allow).
INSERT INTO permission_grant (subject_type, subject_id, permission_key, effect)
SELECT 'user', @user_id, k, 'allow'
FROM unnest(@keys::text[]) AS k
ON CONFLICT DO NOTHING;
