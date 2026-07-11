-- name: ListUserGrants :many
-- All grants that apply to the user: direct user-subject grants plus grants on
-- any role the user holds. The user_role join is internal to the query, so the
-- repo method takes just a user id.
SELECT g.* FROM permission_grant g
WHERE (g.subject_type = 'user' AND g.subject_id = @user_id)
   OR (g.subject_type = 'role' AND g.subject_id IN (
        SELECT ur.role_id FROM user_role ur WHERE ur.user_id = @user_id
   ));
