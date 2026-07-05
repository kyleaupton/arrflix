-- Persist the requester's series-scope choice on the frozen request row. Scope
-- is part of the request's intent (the size of the ask), so it must survive the
-- pending state: an operator approving a queued request spawns with the
-- requester's choice, not the 'all' default. The CHECK mirrors the presets the
-- request API accepts — the parameterless subset of tracking_requester's full
-- preset set (richer presets need a season param the request doesn't carry).
-- Inert for movie requests, which always store the 'all' default.
ALTER TABLE request
  ADD COLUMN scope_rule TEXT NOT NULL DEFAULT 'all'
    CHECK (scope_rule IN ('all', 'future_only'));
