-- An invite carries the request settings the invited user starts with. They're
-- applied as user-subject grants when the invite is accepted.
ALTER TABLE user_invite
  ADD COLUMN IF NOT EXISTS auto_approve BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS allow_4k BOOLEAN NOT NULL DEFAULT false;
