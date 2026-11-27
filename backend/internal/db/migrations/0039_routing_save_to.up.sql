-- save_to is routing's save-location action: a folder as Arrflix sees it,
-- translated into the chosen downloader's frame at grab. NULL leaves the slot
-- to later rules and then the default (app_setting routing.save_to_default),
-- where unset means "send no save path".
ALTER TABLE routing_rule
  ADD COLUMN IF NOT EXISTS save_to TEXT;

-- A proposal stores the resolved grab, so it carries the save path (already in
-- the downloader's frame) that approve hands to the download job.
ALTER TABLE proposal
  ADD COLUMN IF NOT EXISTS save_path TEXT;
