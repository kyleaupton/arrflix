# Indexers — release discovery via Prowlarr

**Status:** Draft, iteration 1

An indexer is a source of release metadata — a torrent tracker or usenet provider — that Arrflix queries to find candidate releases for a [want](../acquisition/README.md). Arrflix does not talk to trackers directly: it delegates to a **bundled Prowlarr** instance, which owns the tracker definitions, credentials, and the actual scraping. Arrflix is Prowlarr's client and aggregator.

Today that relationship is a thin pass-through: Arrflix holds no indexer state of its own, runs free-text searches only, has no health awareness, and caches results in-memory for 5 minutes. This spec keeps Prowlarr as the config source-of-truth but gives Arrflix a **mirror entity** to hang three things on that the pass-through can't support: runtime **health**, per-indexer **stats**, and a durable, shared **search cache**. All three converge on a single search-execution path — that convergence is the spine of this spec.

## TL;DR

- **Prowlarr stays the config source-of-truth.** Trackers are added, credentialed, and tested in Prowlarr (bundled, `localhost:<PROWLARR_PORT>`). Arrflix CRUD proxies to Prowlarr; it does not reimplement tracker management.
- **New: a mirror `indexer` table.** A lightweight row per Prowlarr indexer (`prowlarr_id`, name, protocol, implementation, enabled, priority, `capabilities`, tags) plus Arrflix-owned columns (health triple, stats projection, manual-override flag). It gives us a stable FK target, a home for health/stats, and resilience if Prowlarr is reset.
- **Hybrid sync.** CRUD writes through to Prowlarr and refreshes the mirror immediately; the health worker's periodic Prowlarr round-trip doubles as a drift reconcile. Rows referenced by `download_job` history are **tombstoned**, not deleted.
- **Health via the [connectivity-health pattern](../../patterns/connectivity-health/README.md), passively driven.** Status is derived primarily from real search outcomes, backstopped by a light Prowlarr-reachability probe. Extended statuses: `degraded`, `rate_limited`.
- **Soft gate.** Unlike libraries/downloaders, an unhealthy indexer does not block a want. Search proceeds on the healthy set and only escalates to "no sources" when *all* relevant indexers are down — fan-out semantics demand it.
- **Search quality is request-side.** Pass TMDB/TVDB/IMDB IDs through to Prowlarr with **capability-aware dispatch** (ID lookup where the indexer supports it, text fallback otherwise) and **category scoping**. The result model is essentially unchanged.
- **Per-indexer stats are a second reading of the health signal.** Passive health and stats both consume one `indexer_search_outcome` stream; the `indexer` row carries a denormalized projection for fast display.
- **The search cache moves here from [acquisition](../acquisition/README.md).** DB-backed, shared by both the autonomous worker and manual search. Acquisition becomes a consumer that calls invalidation hooks.

## The Prowlarr relationship

Arrflix wraps Prowlarr through the `golift.io/starr` client (`internal/service/indexer.go`) and a search adapter (`internal/indexer/prowlarr/adapter.go` behind the `IndexerSource` interface in `internal/indexer/source.go`). Prowlarr runs as a bundled s6 service; URL and API key come from config (`PROWLARR_PORT`, `PROWLARR_API_KEY`).

What stays in Prowlarr:

- **Tracker definitions and credentials** — the per-indexer login, API keys, base URLs. Arrflix never stores these; the add/edit UI is a schema-driven proxy to Prowlarr's `/indexer/schema` and CRUD endpoints.
- **The actual scrape** — Prowlarr fans a single search out to every enabled tracker and returns the aggregate. Arrflix does not query trackers individually.
- **Test mechanics** — `TestIndexerContext` / `/testall`.

What moves to (or newly lives in) Arrflix:

- **The mirror entity** (below) — so health, stats, scoping, and FK integrity have a home.
- **The search cache** — durable, shared (below).
- **Capability-aware search dispatch and result aggregation** — the search-execution path.

This division means a Prowlarr reset wipes tracker config (recoverable by re-adding) but Arrflix retains the *names and identity* of indexers referenced by historical download jobs and decision-log rows.

## The mirror entity

A new `indexer` table. Each row corresponds to one Prowlarr indexer and carries two classes of field.

**Mirrored from Prowlarr** (read-mostly; refreshed on sync, never authoritative here):

- **`prowlarr_id`** — Prowlarr's stable int64 ID. The join key for everything; `download_job.indexer_id` already stores it (today as a bare int64 with no FK).
- **`name`** — display name.
- **`protocol`** — `torrent` or `usenet`.
- **`implementation`** — Prowlarr's indexer type (e.g. `Nyaa`, `PirateBay`).
- **`enabled`** — mirrors Prowlarr's enable flag.
- **`priority`** — Prowlarr's priority; used as a tiebreak input downstream.
- **`capabilities`** — JSONB snapshot of what the indexer supports: which search params (ID types: tmdbid/tvdbid/imdbid), which categories. **This is the field that makes capability-aware dispatch decidable** — see [Search quality](#search-quality).
- **`tags`** — Prowlarr tag IDs.

**Owned by Arrflix** (Prowlarr has no equivalent):

- **`status` / `status_checked_at` / `status_last_transitioned_at`** — the [connectivity-health](../../patterns/connectivity-health/README.md) triple.
- **Stats projection** — denormalized counters for fast settings-table display (searches answered, avg results contributed, last-success timestamp, grab counts). Recomputed from `indexer_search_outcome`; see [Stats](#per-indexer-stats).
- **`manual_override`** — operator-asserted health, overriding the probe (see [Runtime health](#runtime-health)).
- **`tombstoned_at`** — set when the Prowlarr indexer is gone but `download_job` history still references the row.

The mirror is deliberately thin. It is **not** a second config store: editing an indexer still edits Prowlarr. The mirror exists for the columns in the second list.

### Sync — hybrid

Two write paths keep the mirror aligned with Prowlarr:

1. **Write-through on CRUD.** When Arrflix proxies a create/update/delete/toggle to Prowlarr and it succeeds, it immediately upserts the corresponding mirror row. The common case stays instantly consistent.
2. **Reconcile on the health round-trip.** The health worker already calls `GetIndexersContext()` every cadence tick — that one call returns the full indexer list *and* is the liveness signal. The worker diffs the returned set against the mirror and reconciles drift. This catches edits made directly in the Prowlarr UI on `:<PROWLARR_PORT>`, which the write-through path can't see.

So a single Prowlarr round-trip produces three outputs: liveness (health), config drift reconcile (sync), and the indexer list itself. We do not run a separate sync worker.

**Reconciliation rules:**

- **New in Prowlarr** → insert a mirror row on next reconcile. Status starts `unknown`.
- **Changed in Prowlarr** (name, enabled, capabilities, …) → update the mirrored fields. Arrflix-owned columns are untouched.
- **Gone from Prowlarr, unreferenced** → delete the mirror row.
- **Gone from Prowlarr, referenced by `download_job`** → **tombstone** (`tombstoned_at` set), don't delete. The audit pattern values history; decision-log and activity views need the indexer's name to render "grabbed from RARBG" long after RARBG is gone. Tombstoned rows are excluded from search dispatch and the active settings list.

## The search-execution path

The spine of this spec. Every search — autonomous (the [AcquisitionWorker](../acquisition/README.md)) or manual (the candidate-search UI) — flows through one execution path in the indexer service. That path does three things in sequence:

1. **Cache check / write** — consult `indexer_search_cache` for the query; on miss, query Prowlarr and store the result blob.
2. **Capability-aware dispatch** — when querying Prowlarr, choose ID-based vs text search per indexer from the `capabilities` snapshot, and scope by category.
3. **Outcome recording** — write one `indexer_search_outcome` row per indexer that participated, capturing result count, error, and latency. This stream feeds *both* health and stats.

Co-locating these is the whole architectural point of the mirror: **health, stats, and cache are not three features bolted on — they are three byproducts of running a search.** Pulling search ownership out of acquisition (where the cache was provisionally parked) and into the indexer module is what makes that convergence possible, and gives manual search the same caching and instrumentation the autonomous worker gets for free.

### Search cache (moved from acquisition)

The [acquisition spec](../acquisition/README.md#search-cache-restructured) provisionally owned `indexer_search_cache` because no indexers spec existed yet. It now lives **here**, because the cache has two consumers — the autonomous worker and manual search — and the indexer module is the natural shared home. Acquisition keeps owning the *decision-log* (which records search-run metadata); indexers owns the *cache* and the *search execution*.

- **DB-backed**, so autonomous workers share cache state across restarts and across worker processes (the current in-memory, per-instance, 5-minute cache loses everything on restart and 404s candidates the user didn't enqueue in time).
- **Keyed by** `(media_item_id or episode_id, query_hash)`, storing the raw indexer result blob with an expiry.
- **TTL** ~1 hour for autonomous searches — internal tuning, **not** a user-facing setting.
- **Invalidation hooks**, called by consumers (acquisition owns when these fire):
  - a release is grabbed (so a re-search doesn't re-offer the just-grabbed release),
  - an indexer's config changes (sync detected a change),
  - a manual "search now" request.
- **In-flight dedup** — two wants for the same query must not trigger two simultaneous Prowlarr searches. A short-lived advisory lock keyed on `query_hash` (or the cache row's pending state) collapses them.

Schema detail (column types, blob shape) is deferred to the data-shape iteration, consistent with how acquisition parked it.

## Search quality

All request-side. Better inputs to Prowlarr produce better results; the parsed-result model (`indexer.SearchResult` → `model.DownloadCandidate`) is essentially unchanged, which keeps this low-risk.

**ID-based search.** Today Arrflix sends free-text only — `"Title Year"` for movies, `"Title S##E##"` for episodes — even though it already holds the TMDB/TVDB/IMDB IDs at search time and Prowlarr's search API accepts `tmdbid` / `tvdbid` / `imdbid`. Wiring the IDs through eliminates a class of misses (wrong movie same title, year-off-by-one). Dispatch is **capability-aware**: per indexer, use ID lookup when the `capabilities` snapshot advertises support, otherwise fall back to text. Because dispatch is per-indexer, a single search can use IDs against capable trackers and text against the rest in the same fan-out.

**Category scoping.** Map media type to Newznab/Torznab category IDs so searches are scoped to movie vs. TV categories, cutting junk results. Categories are already returned on results but unsurfaced; scoping uses them on the request side.

**What stays the same.** The download-URL fallback chain (Prowlarr URL → magnet from GUID → magnet from infohash), the empty-title/no-URL filtering at the adapter boundary, and the torrent/usenet protocol split all carry over unchanged.

## Runtime health

Indexers are a producer of the [connectivity-health pattern](../../patterns/connectivity-health/README.md). The pattern owns the status triple, transition emission, hysteresis, and the admin-action audit hook. This section covers only the indexer-specific contract.

### Probe — passive primary, light backstop

Indexers fail *partially* — one tracker can be down while the rest are fine — and the obvious "test every indexer on a cadence" approach is self-defeating: it hammers trackers and risks triggering the very rate-limits we're trying to detect. So health is **passively driven**:

- **Primary signal: real search outcomes.** Every search already records per-indexer results in `indexer_search_outcome`. An indexer that errors or returns zero results across the last N searches degrades; one that answers recovers. This is a truer signal than a synthetic ping and costs nothing extra.
- **Backstop: a light Prowlarr-reachability probe.** The health worker's periodic `GetIndexersContext()` confirms Arrflix↔Prowlarr liveness and gives idle indexers (no recent searches) a baseline. It does **not** test each tracker.

The probe stays cheap and side-effect-free per the pattern. We deliberately do **not** run Prowlarr's `/testall` on a cadence — that's an on-demand operator action only.

### Extended statuses

Beyond the pattern's base (`healthy` / `unreachable` / `unknown`):

| Value          | Meaning                                                                                              |
| -------------- | ---------------------------------------------------------------------------------------------------- |
| `degraded`     | Answering some searches but erroring or returning empty on others, or slow. Still usable.            |
| `rate_limited` | The indexer (or Prowlarr on its behalf) returned 429 / rate-limit signals. Back off for a cooldown.  |

### Consumer-behavior mapping

Per the [pattern's vocabulary](../../patterns/connectivity-health/README.md#consumer-gating). The defining trait here is that **search is fan-out**, so indexer health gates *softly* — it shapes which indexers participate, it does not hold the want:

| Status         | Search dispatch (this module)                          | [Acquisition](../acquisition/README.md) want outcome                          |
| -------------- | ------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `healthy`      | include normally                                       | `proceed`                                                                      |
| `degraded`     | include, deprioritized; surface in UI                  | `proceed` — degraded sources still contribute                                  |
| `rate_limited` | skip for the cooldown window                           | `proceed` — other indexers cover; the want is not blocked on one tracker       |
| `unreachable`  | skip                                                   | `proceed` if any relevant indexer is healthy; **escalate to "no sources" only when all are down** |
| `unknown`      | include (treat as `degraded` per pattern default)      | `proceed`                                                                      |

This is intentionally softer than libraries/downloaders, where a `blocked` resource holds pending work. A want only fails for lack of sources when the *entire* relevant indexer set (matching the want's protocol and any future profile scoping) is unavailable — that is the loud, notification-worthy case.

### Manual override

Per the pattern's open question #5, operators can mark an indexer healthy to override the probe — useful for a private tracker that fails synthetic checks but works fine for real searches. The `manual_override` column asserts health regardless of derived status; setting it is an admin-action audit event. Override is opt-in per indexer.

### Hysteresis & cadence

Standard asymmetric hysteresis (2 consecutive negative signals to degrade, 1 positive to recover) per the pattern. Cadence for the backstop probe follows the pattern's indexer guidance (1–5 min). Passive signals update continuously as searches happen.

### Audit & transitions

Health transitions emit on the `indexer.health` [realtime](../realtime/README.md) channel and write `indexer.health_transitioned` rows to the [admin-action audit stream](../users/README.md#admin-action-audit), exactly as the pattern prescribes.

## Per-indexer stats

Stats and passive health are **two readings of one stream**, not two features. Every search writes per-indexer outcome rows; health reads the recent window, stats reads the time-windowed aggregate.

- **`indexer_search_outcome`** — one row per `(search_run, indexer)`: result count, error (if any), latency. This *is* the rolling window passive health consumes, and the durable substrate for time-windowed stats. Single source of truth.
- **Denormalized projection on the `indexer` row** — current status plus cheap counters (searches answered, average results contributed, last-success timestamp, grab counts) for fast settings-table reads, recomputed from the outcome stream.

This unlocks the "are my indexers earning their keep?" settings view — surfacing, e.g., "this tracker has contributed 0 results and 0 grabs in 90 days, consider removing." For self-hosters who accumulate 10–20 indexers with no visibility into which matter, that's a concrete win, and it costs almost nothing because the outcome stream already exists for health.

> Note: feeding **grab outcomes** (a `download_job` from indexer X that never starts — dead torrent, 404, no peers) back into health and stats is a strong negative signal that search-only data misses. It is deferred to **v2** to keep v1 scoped to the search path; the `indexer_search_outcome` shape leaves room for a sibling grab-outcome stream later.

## Operations

CRUD proxies to Prowlarr; search and stats are Arrflix-native. All routes JWT-gated; permission scoping lives in [users](../users/README.md). Existing endpoints (from the current implementation) are retained; new rows are marked.

| OperationID                | Method | Path                                       | Notes                                                                    |
| -------------------------- | ------ | ------------------------------------------ | ------------------------------------------------------------------------ |
| `indexers-list-configured` | GET    | `/api/v1/indexers/configured`              | List indexers from the mirror, with health + stats projection (new fields). |
| `indexers-get-schema`      | GET    | `/api/v1/indexers/schema`                  | Prowlarr indexer definitions for the add wizard.                          |
| `indexer-get`              | GET    | `/api/v1/indexer/{id}`                      | Single indexer config (proxied from Prowlarr).                            |
| `indexer-save`             | POST   | `/api/v1/indexer`                           | Create (id=0) or update; write-through to Prowlarr + mirror upsert.       |
| `indexer-delete`           | DELETE | `/api/v1/indexer/{id}`                      | Delete in Prowlarr; mirror row deleted or tombstoned per references.      |
| `indexer-toggle`           | PUT    | `/api/v1/indexer/{id}/toggle`               | Enable/disable; write-through.                                            |
| `indexer-action`           | POST   | `/api/v1/indexer/action/{name}`             | Opaque Prowlarr action proxy (schema option fetches, etc.).               |
| `indexer-test`             | POST   | `/api/v1/indexer/{id}/test`                 | On-demand test of a saved indexer (active probe; operator-triggered).     |
| `indexer-test-config`      | POST   | `/api/v1/indexer/test`                      | Test an unsaved config pre-save.                                          |
| `indexers-test-all`        | POST   | `/api/v1/indexers/testall`                  | Test all indexers (heavy; operator-triggered only, never on cadence).     |
| `indexer-override` *(new)* | PUT    | `/api/v1/indexer/{id}/health-override`      | Set/clear `manual_override`. Admin-action audited.                        |

Permissions (defined in [users](../users/README.md)):

- `indexers.read` — list / get / schema / stats
- `indexers.write` — save / delete / toggle / health-override
- `indexers.test` — the test endpoints (separable, mirroring `downloaders.test`)

Search itself is not a public indexer endpoint — it is reached through the [download-candidates](../acquisition/README.md) surface (`/movie/{id}/candidates`, `/series/{id}/candidates`), which calls this module's search-execution path.

## Validation

Because tracker config is validated by Prowlarr, Arrflix-side validation is light:

1. **Save** proxies to Prowlarr; Prowlarr's validation errors are surfaced as typed [errors](../../patterns/errors/README.md) (`BadGateway` when Prowlarr is unreachable, `Validation` for rejected config).
2. **Mirror upsert** happens only after Prowlarr confirms the write.
3. **Health-override** validates that the target indexer exists and is not tombstoned.

There is no live tracker check at save time beyond what Prowlarr's own test does on demand.

## Integration points

| Consumer / neighbor                                   | How it relates to indexers                                                                                              |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| **[Acquisition](../acquisition/README.md)**           | Primary consumer. The AcquisitionWorker and manual search both run through the search-execution path; acquisition fires the cache invalidation hooks and writes the decision-log from search-run metadata. |
| **[Quality profiles](../quality-profiles/README.md)** | References an indexer allow/deny set (the spec's "indexer scoping" open question). v1 mirror gives it a stable entity to reference; the scoping behavior itself is owned there. |
| **[Connectivity-health pattern](../../patterns/connectivity-health/README.md)** | Indexers are a producer; this spec declares the passive probe, extended statuses, and soft-gate mapping. |
| **[Realtime](../realtime/README.md)**                 | `indexer.health` transition events; settings UI live-updates health badges.                                             |
| **[Users](../users/README.md)**                       | `indexers.*` permissions; owns the admin-action audit stream health transitions and overrides write to.                |
| **[Audit pattern](../../patterns/audit/README.md)**   | Search-run metadata (which indexers responded) feeds acquisition's decision-log; health transitions are admin-action audit events. |
| **[Notifications](../notifications/README.md)**       | Subscribes to `failed`-tier transitions — here, the all-indexers-down case — for operator alerts.                       |
| **Frontend (`IndexersSettings.vue`)**                 | Existing CRUD + test UI; v1 adds health badges, the stats ("earning their keep") view, and capability surfacing.        |

## What indexers does NOT own

- **Tracker definitions and credentials** — Prowlarr's, always.
- **The "what to grab" decision** — gating and scoring live in [quality profiles](../quality-profiles/README.md).
- **The download-job state machine** — [acquisition](../acquisition/README.md).
- **The decision-log / search-run audit rows** — written by acquisition; indexers provides the search-run metadata.
- **Which downloader/library/template a grab routes to** — [routing](../routing/README.md).
- **Indexer scoping policy** (per-profile allow/deny) — referenced from here, owned by [quality profiles](../quality-profiles/README.md).
- **Grab-outcome-driven health** — deferred to v2.

## Open questions

1. **Search cache scope granularity.** Per `(media_item, query)` or finer? Series searches differ from episode searches, and both should cache. Inherited from [acquisition's open question](../acquisition/README.md). Lean: key on `(media_item_id | episode_id, query_hash)` where `query_hash` folds in the resolved search params (IDs vs text, categories). Pin in the data-shape iteration.
2. **Passive-health window size (N).** How many recent searches define the rolling window, and is it count-based or time-based? Lean: time-based (e.g. last 24h of outcomes) so a rarely-searched indexer isn't judged on stale data; revisit once we see real outcome volume.
3. **`rate_limited` cooldown duration.** Fixed, or backoff-escalating per consecutive 429? Lean: start with a fixed cooldown (a few minutes), add escalation only if flapping appears.
4. **`indexer_search_outcome` retention.** The stream grows with every search. Lean: trim aggressively (the denormalized projection holds the long-term summary; raw outcomes only need to cover the health window + a short stats tail). Concrete TTL in the data-shape iteration.
5. **Capability snapshot staleness.** `capabilities` is refreshed on sync, but a tracker's supported params can change between syncs. Lean: trust the snapshot; a wrong guess degrades gracefully (ID search that the indexer silently ignores still returns text-equivalent results). Re-fetch on the reconcile tick is enough.
6. **Stats view depth.** How much does the "earning their keep" view show — just counters, or per-indexer result-quality (how often its results actually got grabbed)? Grab attribution edges into the v2 grab-outcome work. Lean: counters + last-success for v1; grab-rate when the v2 loop lands.
7. **Tombstone retention.** Do tombstoned rows live forever (for historical decision-log rendering) or get pruned with the audit retention window? Lean: tie to the audit retention policy — once no live audit row references a tombstoned indexer, it can be pruned.
8. **Manual-override expiry.** Does an override stick until manually cleared, or auto-expire after a window? Lean: sticky until cleared, but surface it prominently in the UI so it isn't forgotten.
9. **Indexer scoping ownership handshake.** Quality profiles want a per-profile allow/deny indexer set. This spec provides the entity; does the *dispatch filtering* (restrict a search's fan-out to a profile's allowed set) live here or in the profile engine? Lean: the search-execution path accepts an optional allowed-indexer set as a parameter; the profile engine supplies it. Confirm when quality-profiles iterates.

   > **Pending — quality-profiles UI deferral (noted 2026-06).** The quality-profiles admin UI (phase 5) ships **without** an indexer-scoping control, because the entity this open question promises doesn't exist yet. `QualityProfile.Indexers` is typed `[]uuid.UUID`, but there is no UUID-keyed indexer source today: `/api/v1/indexers/configured` returns Prowlarr `int64` ids, and the routing `candidate.indexer` field matches on the **name** string. So nothing can populate a faithful picker. The profile editor round-trips the stored `indexers` value untouched (empty = all) and leaves a placeholder where the control will go. **Come back to this when the mirror entity lands**, and settle the FK target as part of it: either the mirror exposes a stable **UUID PK** (and a list endpoint the picker reads, with `QualityProfile.Indexers` referencing that PK), or scoping references `prowlarr_id` and `QualityProfile.Indexers` migrates `[]uuid.UUID` → `[]int64`. This open question and #10 (`download_job.indexer_id` FK) should be decided together — both hinge on what the mirror's stable id is.
10. **`download_job.indexer_id` FK.** Today it's a bare int64 with no foreign key. With a mirror table, should it become a real FK (with tombstones satisfying historical references)? Lean: yes — add the FK to the mirror, which is exactly why tombstoning exists. Migration detail deferred.

## What we're explicitly not deciding here

- The concrete `indexer_search_cache` / `indexer_search_outcome` column schemas (data-shape iteration).
- Whether grab outcomes feed health/stats (v2).
- Per-profile indexer scoping *behavior* (lives with [quality profiles](../quality-profiles/README.md)).
- Rate-limit budget / proactive throttling (a v2 seam; passive `rate_limited` detection is the v1 floor).
- Infohash dedup / cross-indexer release collapsing (helps manual search; auto-selection is the primary path — deferred).
- The shared `health.Worker` abstraction (deferred across all connectivity-health producers per the pattern).
- UI redesign specifics for the health badge and stats view.
- Anything about a non-Prowlarr indexer backend — Prowlarr is the only source in v1.

## Doc neighbors

- [Acquisition](../acquisition/README.md) — primary consumer; owns the decision-log and the cache invalidation triggers
- [Quality profiles](../quality-profiles/README.md) — consumes search results; owns indexer-scoping policy
- [Downloaders](../downloaders/README.md) — sibling external-connection module; same CRUD + connectivity-health shape, different role (transfer vs discovery)
- [Connectivity-health pattern](../../patterns/connectivity-health/README.md) — the runtime-health contract indexers implement (passive variant)
- [Routing](../routing/README.md) — dispatches grabbed releases; indexer choice is upstream of it
- [Users](../users/README.md) — `indexers.*` permissions; owns the admin-action audit stream
- [Audit pattern](../../patterns/audit/README.md) — search-run metadata feeds the decision-log
- [Notifications](../notifications/README.md) — alerts on the all-indexers-down (`failed`-tier) case
- [Realtime](../realtime/README.md) — `indexer.health` transition channel
