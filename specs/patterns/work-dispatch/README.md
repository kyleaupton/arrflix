# Work dispatch — non-poll worker wake-ups over a durable queue

**Status:** Draft, iteration 1

This doc defines a **cross-cutting pattern**: how background workers pick up newly-available work *promptly* without busy-polling, while keeping the at-least-once guarantee that work is never silently dropped. It is the server→server sibling of [realtime](../../modules/realtime/README.md), which is server→browser. The two look similar ("something happened, react to it") but have **opposite reliability contracts**, and conflating them is a correctness bug — see [Why this is separate from realtime](#why-this-is-separate-from-realtime).

Like [audit](../audit/README.md) and [connectivity-health](../connectivity-health/README.md), this is **descriptive, not prescriptive**: there is no shared worker and no shared Go interface. Each worker owns its own claim query and loop; this pattern is the agreement they conform to. It also **formalizes what the download and import workers already do** — they're 90% of the way here already; the missing 10% is the wake-up hint.

## TL;DR

- **The database table is the source of truth, not the event.** Work to do is a row in a queue-shaped table (`download_job`, `import_task`, `want`, …). A worker claims runnable rows atomically with `FOR UPDATE SKIP LOCKED` + a status flip. This is **level-triggered**: the worker re-reads truth, it doesn't react to a fire-and-forget signal.
- **`LISTEN/NOTIFY` is a wake-up *hint*, never the work itself.** Whoever inserts a runnable row fires `pg_notify('<queue>', …)`. The worker's loop has a `case <-notify:` that calls its existing claim-tick early. The payload is advisory ("something landed, go look") — losing it costs latency, never correctness.
- **The ticker stays, demoted to a fallback heartbeat.** Notifications are best-effort (not delivered across a dropped `LISTEN` connection, coalesced under load). A long-interval ticker (~30s) guarantees that any work a missed `NOTIFY` left behind still gets claimed. **Hint for latency + poll for safety.**
- **At-least-once, restart-safe.** A row sits in the table until a worker completes it. Process restart, missed notify, crashed mid-job → the next claim (hint-driven or ticker-driven) picks it up. Idempotent processing + the claim's status flip prevent double-execution.
- **This is the home for acquisition's "internal event bus."** `want.created` waking the `AcquisitionWorker`, `want.regate_failed` waking re-search, etc. ([acquisition](../../modules/acquisition/README.md#event-bus--messaging) gestures at "we likely need an internal event bus" — it lands here.)
- **Not for browser updates.** A worker may emit a [realtime](../../modules/realtime/README.md) event *after* doing work, but that's a separate, lossy bus. Work dispatch and realtime never share a channel.

## Why this is separate from realtime

[Realtime](../../modules/realtime/README.md)'s broker is deliberately **lossy**: on a slow consumer it drops events (`broker.Publish` is non-blocking by design). That's correct for a browser — a missed scan-progress tick is invisible after the next tick or a refetch. Reusing that bus to dispatch work would mean **a dropped event = work that silently never runs.** The contracts are mirror opposites:

| Property | Realtime (server→browser) | Work dispatch (server→server) |
| --- | --- | --- |
| Delivery | at-most-once (drops on overflow) | **at-least-once** |
| Loss tolerance | fine — refetch covers it | **none** — a lost job is lost work |
| Survives restart | no (clients reconnect) | **yes** (row persists in the table) |
| Trigger model | edge (react to the event) | **level** (re-read the truth) |
| Source of truth | the event payload | **the database row** |

The shared insight: **the event is never the work.** In work dispatch the event is only a hint to look at the table sooner; the table is authoritative. That single property is what keeps the at-least-once guarantee while still killing poll latency.

## What it replaces: pure polling

Today the workers are pure poll loops. From `download/worker.go`:

```go
ticker := time.NewTicker(w.pollInterval) // 3s
for {
    select {
    case <-ctx.Done():
        return
    case <-ticker.C:
        w.tick(ctx) // ClaimRunnableDownloadJobs → process each
    }
}
```

This is already **correct** — `ClaimRunnableDownloadJobs` uses `FOR UPDATE SKIP LOCKED` and atomically flips status, so two workers (or a restart mid-tick) never double-claim. The *only* problem is latency: a job created the instant after a tick waits the full poll interval (2-3s) to be noticed. At a steady cadence that's also wasted DB round-trips when there's nothing to do.

The fix is additive, not a rewrite. The claim query is untouched. The truth model is untouched. We add a hint channel and demote the ticker.

## The shape

### 1. The queue table + atomic claim (already exists)

A worker's input is a set of rows in a runnable state. The claim is a single statement that selects runnable rows and flips them to an in-flight state in one transaction:

```sql
-- ClaimRunnableImportTasks (existing)
WITH claimable AS (
  SELECT id FROM import_task
  WHERE status = 'pending' AND <runnable predicate>
  ORDER BY created_at
  LIMIT $1
  FOR UPDATE SKIP LOCKED          -- the heart of it: concurrent claimers never collide
)
UPDATE import_task t SET status = 'in_progress', updated_at = now()
FROM claimable WHERE t.id = claimable.id
RETURNING ...
```

`FOR UPDATE SKIP LOCKED` is what makes the pattern safe under concurrency and restart: a row claimed by one worker is invisible to another's claim, and a worker that dies mid-job leaves the row reclaimable (after a visibility/lease policy — see [open questions](#open-questions)).

### 2. The producer fires a NOTIFY hint

Whoever transitions a row *into* a runnable state issues a `NOTIFY` on a per-queue channel, in the **same transaction** as the insert/update so the hint can't precede the row's visibility:

```sql
-- after inserting/updating into a runnable state, in the same tx:
SELECT pg_notify('import_task_runnable', '');   -- payload optional; '' is fine
```

The payload is intentionally minimal — at most a coarse key, never the work itself. A worker that receives it does exactly what a ticker tick does: call `claim → process`. So a malformed, duplicated, or missed payload degrades to "claim ran a bit early / a bit late," never to wrong behavior.

### 3. The worker listens, with the ticker as a safety net

```go
notify, release := w.listener.Subscribe(ctx, "import_task_runnable") // pgx LISTEN
defer release()

ticker := time.NewTicker(30 * time.Second) // fallback heartbeat, was the primary poll
defer ticker.Stop()

for {
    select {
    case <-ctx.Done():
        return
    case <-notify:    // hint: work likely landed — look now
        w.tick(ctx)
    case <-ticker.C:  // safety: catch anything a missed NOTIFY left behind
        w.tick(ctx)
    }
}
```

Both arms call the same `tick`. Coalescing many notifies into one tick is fine and desirable — `tick` claims a batch. The ticker interval lengthens (3s → ~30s) because it's no longer the latency mechanism, just the backstop.

### Connection management

`LISTEN` needs a dedicated, long-lived connection (a listening connection can't be shared with the pool's query traffic). The pattern recommends **one shared listener component** that owns a single `pgx` connection, multiplexes `NOTIFY` to per-channel Go subscribers, and — critically — **re-establishes `LISTEN` on reconnect and fires a synthetic wake on reconnect** (a dropped connection may have eaten notifies; the reconnect wake forces a catch-up tick). This is the one piece with real failure-mode nuance; everything else is a thin addition to existing loops.

## Reliability properties

- **No lost work.** A runnable row persists until claimed-and-completed. Missed `NOTIFY` → the ticker catches it. Listener disconnect → reconnect fires a catch-up wake. Process crash mid-job → the row's lease expires and it's reclaimed.
- **No double work.** `FOR UPDATE SKIP LOCKED` + status flip means a row is claimed by exactly one worker. Combined with idempotent processing, a redelivered hint is harmless.
- **Bounded latency without busy-polling.** Steady state: the worker sleeps on `notify`/`ticker` and wakes within milliseconds of a producer's commit, instead of within a poll interval. Idle: one cheap claim every ~30s instead of every 2-3s.
- **Graceful degradation.** If `LISTEN/NOTIFY` is unavailable for any reason, the worker silently falls back to its ticker — i.e. exactly today's behavior. The hint is a pure optimization layer over a correct base.

## When to use it — and when not

**Use it** for any worker whose input is a durable queue of rows that must all eventually be processed: the download worker, the import worker, the [acquisition](../../modules/acquisition/README.md) worker (`want.created`), the search scheduler's due-signals, enrichment backfill.

**Don't reach for it** when:

- The signal is genuinely ephemeral and lossy-OK (a browser UI update) → that's [realtime](../../modules/realtime/README.md).
- Two in-process components need a simple hand-off and a Go channel suffices, with no durability or cross-restart requirement. Don't add a Postgres round-trip for an in-memory hand-off. The acquisition spec's note that "in-process pub/sub is sufficient for v1" still holds for the *signalling* between co-located components — this pattern is specifically for the **durable, must-not-drop, survives-restart** wake-ups, which the want lifecycle needs.

The litmus test: **if the signal were dropped, would work silently fail to happen?** Yes → durable queue + this pattern. No → a Go channel or realtime is lighter and fine.

## Relationship to neighbors

| Neighbor | Relationship |
| --- | --- |
| **[Realtime](../../modules/realtime/README.md)** | Sibling, opposite contract. A worker woken by this pattern may `realtime.Emit(...)` a browser event after doing work; they never share a bus. |
| **[Acquisition](../../modules/acquisition/README.md)** | Primary consumer. The `AcquisitionWorker` and `SearchScheduler` wake on `want.created` / due-signals via this pattern instead of polling. The "internal event bus" the acquisition spec anticipates is this. |
| **[Connectivity-health](../connectivity-health/README.md)** | A `blocked` want resumes on a `<resource>.health` recovery — that recovery transition can fire a work-dispatch hint to wake the blocked work immediately rather than waiting for back-off. |

## Open questions

1. **Lease / reclaim policy for crashed workers.** A row flipped to `in_progress` by a worker that then crashes must become reclaimable. Options: a `claimed_at` + visibility-timeout sweep, a heartbeat/lease column, or rely on the existing retry/`max_attempts` machinery. Lean: a visibility-timeout sweep reusing the existing attempt counters; pin per-queue.
2. **One listener connection or one per queue.** A single shared listener multiplexing all channels is fewer connections; per-worker listeners are simpler but spend a dedicated connection each. Lean: one shared listener component, given small N of queues.
3. **NOTIFY payload contents.** Empty string (pure "go look") vs a coarse key (e.g. the `library_id`) that lets a worker scope its claim. Lean: start empty; add a key only if a worker can meaningfully narrow its claim with it.
4. **Coalescing / debounce.** Under a burst (a season pack creating many wants at once), many notifies collapse into few ticks naturally via the select loop, but a tiny debounce could cut redundant claims further. Lean: rely on natural coalescing first; measure before adding debounce.
5. **Migration sequencing.** Which worker adopts the hint first (download and import are the obvious low-risk pilots since their loops and claim queries already exist), and whether the shared listener ships before or alongside the first adopter. Owned by the realtime/work-dispatch implementation plan.

## What we're explicitly not deciding here

- Exact channel names, payload schemas, or the listener component's Go interface — implementation.
- Per-queue claim predicates, batch sizes, lease durations — owned by each worker's module.
- Whether to ever graduate to an external broker (NATS, Redis streams, a real queue). In-process workers + Postgres `LISTEN/NOTIFY` is the answer while Arrflix is a single container; this only revisits if the deployment model splits into multiple processes.

## Doc neighbors

- [Realtime](../../modules/realtime/README.md) — the server→browser sibling; lossy fan-out, opposite reliability contract
- [Acquisition](../../modules/acquisition/README.md) — primary consumer; the want-lifecycle workers wake via this pattern
- [Connectivity-health](../connectivity-health/README.md) — health-recovery transitions can fire wake hints to resume blocked work
- [Audit](../audit/README.md), [Errors](../errors/README.md) — sibling descriptive patterns (shared contract, not shared implementation)
- `backend/internal/jobs/download/worker.go`, `backend/internal/jobs/import/worker.go` — the existing poll loops this pattern upgrades
- `backend/internal/db/queries/download_jobs.sql`, `import_tasks.sql` — the `FOR UPDATE SKIP LOCKED` claim queries that are already the durable-queue primitive
