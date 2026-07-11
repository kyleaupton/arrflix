-- Request approval lifecycle. Makes approval real: pending requests can now be
-- approved/denied/canceled, and each decision records who made it. Auto-approve
-- moves off the user_policy boolean onto the per-tier requests.auto_approve
-- permission, so this migration also retires the vestigial policy table.

-- Widen the status vocabulary to admit cancellation. The original CHECK is the
-- unnamed column-level constraint Postgres auto-names request_status_check; drop
-- and re-add it with 'canceled' (the codebase spelling, matching tracking/want).
ALTER TABLE request DROP CONSTRAINT IF EXISTS request_status_check;
ALTER TABLE request ADD CONSTRAINT request_status_check
  CHECK (status IN ('pending', 'approved', 'spawned', 'denied', 'canceled'));

-- Decision provenance: who decided, when, and whether it was the automatic
-- (permission-driven) path. decided_by SET NULL on user delete keeps the request
-- row's history intact after the deciding operator is gone. decision_auto is NULL
-- for a cancel (a withdrawal, not an approve/deny decision).
ALTER TABLE request
  ADD COLUMN decided_by UUID REFERENCES app_user(id) ON DELETE SET NULL,
  ADD COLUMN decided_at TIMESTAMPTZ,
  ADD COLUMN decision_auto BOOLEAN;

-- Retire user_policy. Its only column (auto_approve_movie) is replaced by the
-- requests.auto_approve:<type>:<tier> permission; quota — the other thing this
-- table might have held — is deferred to a global app setting, not a per-user
-- column. The table is vestigial, so drop it.
DROP TABLE user_policy;

-- The approval queue reads WHERE status='pending'. Low-volume, but a partial
-- index keeps the queue scan cheap as the request table grows.
CREATE INDEX idx_request_pending ON request (status) WHERE status = 'pending';
