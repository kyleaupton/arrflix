-- notification_outbox.claimed_at: when the delivery worker claimed a row into
-- 'delivering'. It is what makes a wedged claim detectable.
--
-- The worker claims a row (queued → delivering), hands it to a channel adapter,
-- then settles it (delivered / queued-with-backoff / dead). A crash anywhere in
-- between — an ordinary restart lands in this window, since an SMTP dial or a
-- push POST takes seconds — leaves the row at 'delivering' forever: ListDueOutbox
-- selects only 'queued' and MarkOutboxDelivering compare-and-swaps on 'queued',
-- so nothing looks at that row again and the notification is silently lost.
--
-- With a claim stamp the worker's reaper (ReclaimStaleDelivering) can return rows
-- claimed longer ago than the lease to the queue. It mirrors the AcquisitionWorker's
-- crash-window reaper for wants wedged in 'searching'.
--
-- The column is meaningful only while status = 'delivering'; on a settled row it
-- is the historical claim time of the last attempt and nothing reads it.
ALTER TABLE notification_outbox ADD COLUMN claimed_at TIMESTAMPTZ;

-- Backfill rows already stranded in 'delivering' by a crash predating this column,
-- so the reaper's `claimed_at < cutoff` sees them (NULL would compare false and
-- strand them permanently). created_at is a lower bound on the real claim time,
-- which is all the cutoff needs. Migrations apply at API startup before the
-- workers start, so no live claim can be reclaimed out from under a running send.
UPDATE notification_outbox
SET claimed_at = created_at
WHERE status = 'delivering' AND claimed_at IS NULL;
