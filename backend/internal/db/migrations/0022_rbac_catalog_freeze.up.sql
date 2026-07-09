-- 0022: RBAC catalog freeze. Replaces the old roles/permissions/grants seeded in
-- 0002 with the frozen catalog (specs/modules/users/README.md). Up-only; transforms
-- whatever 0002 produced. No behavior change — nothing enforces these grants yet.

-- 1. Rename built-in roles to the frozen set. UPDATE (not drop+insert) preserves
--    role.id and every user_role link, so existing memberships map cleanly
--    (manager->co_admin, user->requester, guest->viewer). 'admin' is unchanged.
UPDATE role SET name='co_admin', description='Operational control, without user management' WHERE name='manager';
UPDATE role SET name='requester', description='Browse and request content'                  WHERE name='user';
UPDATE role SET name='viewer',    description='Read-only browse access'                       WHERE name='guest';

-- 2. Wipe the permission catalog. permission_grant.permission_key -> permission(key)
--    is ON DELETE CASCADE, so this removes every existing grant (role- and user-
--    subject) automatically — clean slate for the re-seed below.
DELETE FROM permission;

-- 3. The frozen catalog: 33 enumerated concrete keys.
INSERT INTO permission (key, description) VALUES
  ('requests.create:movie:hd','Request a movie at HD'),
  ('requests.create:movie:4k','Request a movie at 4K'),
  ('requests.create:series:hd','Request a series at HD'),
  ('requests.create:series:4k','Request a series at 4K'),
  ('requests.auto_approve:movie:hd','HD movie requests auto-spawn (bypass queue)'),
  ('requests.auto_approve:movie:4k','4K movie requests auto-spawn (bypass queue)'),
  ('requests.auto_approve:series:hd','HD series requests auto-spawn (bypass queue)'),
  ('requests.auto_approve:series:4k','4K series requests auto-spawn (bypass queue)'),
  ('requests.approve:movie','Approve pending movie requests'),
  ('requests.approve:series','Approve pending series requests'),
  ('requests.deny:movie','Deny pending movie requests'),
  ('requests.deny:series','Deny pending series requests'),
  ('requests.cancel.own','Withdraw own requests'),
  ('requests.cancel.any','Cancel anyone''s requests'),
  ('requests.view.own','View own requests'),
  ('requests.view.any','View the full request queue'),
  ('library.read','Browse library content'),
  ('library.write','Create/edit library configs'),
  ('library.scan','Trigger scans'),
  ('media.write','Fix-match, manual matching, edit metadata'),
  ('tracking.create.own','Subscribe self'),
  ('tracking.create.any','Subscribe on behalf of others'),
  ('tracking.cancel.own','Cancel own tracking'),
  ('tracking.cancel.any','Cancel any tracking'),
  ('jobs.read','View queue and job states'),
  ('jobs.manage','Retry/cancel jobs'),
  ('hygiene.read','View hygiene findings'),
  ('hygiene.resolve','Take remediation action'),
  ('admin.users.manage','Invite, edit, disable users; assign roles'),
  ('admin.settings.read','View system settings'),
  ('admin.settings.write','Change system settings'),
  ('admin.indexers.manage','Manage indexer connections'),
  ('admin.downloaders.manage','Manage downloader connections')
ON CONFLICT (key) DO NOTHING;

-- 4. Seed grants per built-in role (global scope, effect=allow). Linked by role
--    name, matching the 0002 style. Renames in step 1 run first, so these resolve.

-- admin: the entire catalog
INSERT INTO permission_grant (subject_type, subject_id, permission_key, effect)
SELECT 'role', r.id, p.key, 'allow' FROM role r, permission p
WHERE r.name='admin'
ON CONFLICT DO NOTHING;

-- co_admin: everything EXCEPT user management (the sole admin/co_admin difference)
INSERT INTO permission_grant (subject_type, subject_id, permission_key, effect)
SELECT 'role', r.id, p.key, 'allow' FROM role r, permission p
WHERE r.name='co_admin' AND p.key <> 'admin.users.manage'
ON CONFLICT DO NOTHING;

-- requester: browse + request HD, manage own
INSERT INTO permission_grant (subject_type, subject_id, permission_key, effect)
SELECT 'role', r.id, p.key, 'allow' FROM role r
JOIN permission p ON p.key IN (
  'library.read',
  'requests.create:movie:hd',
  'requests.create:series:hd',
  'requests.cancel.own',
  'requests.view.own'
)
WHERE r.name='requester'
ON CONFLICT DO NOTHING;

-- viewer: browse only
INSERT INTO permission_grant (subject_type, subject_id, permission_key, effect)
SELECT 'role', r.id, p.key, 'allow' FROM role r
JOIN permission p ON p.key = 'library.read'
WHERE r.name='viewer'
ON CONFLICT DO NOTHING;
