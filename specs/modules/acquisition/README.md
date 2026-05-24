# Acquisition — orchestrating the get-it-into-the-library flow

**Status:** Draft, iteration 1

This doc defines the **acquisition pipeline**: the orchestration layer that takes a want from "we intend to acquire this" to "the file is in the library and verified." Surfacing that file in a downstream media server (Plex, Jellyfin) is a separate, non-blocking concern — see [Media-server propagation](#media-server-propagation-decoupled-from-available). It is the spine that ties together [tracking](../tracking/README.md), [quality profiles](../quality-profiles/README.md), [routing](../routing/README.md), [matching](../matching/README.md), and the existing download/import machinery.

Acquisition is a **code module, not a settings surface.** It has no user-facing configuration of its own — every configurable knob the flow exposes lives in a sibling spec (quality profiles, routing rules, tracking defaults, indexers, downloaders, user permissions). What lives here is plumbing: the worker, search execution, the decision log writer, the event bus, the season-pack linkage.

## TL;DR

- Acquisition is **orchestration**, not selection. Picking is owned by [quality profiles](../quality-profiles/README.md); post-pick decisions are owned by [routing](../routing/README.md). This doc owns the chain that connects them.
- The pipeline: **want → search → quality (gate + score + pick) → routing (decide where) → download_job → import_task → media_file → verify → available**. A want reaches `available` when Arrflix verifies the file on disk — **not** when Plex sees it. Media-server sync (Plex/Jellyfin) happens _after_ `available` and never gates it.
- The pipeline writes audit rows following the system-wide [decision-artifact pattern](../../patterns/audit/README.md). Retention is centralized there, not here.
- A single download_job can fulfill **many wants** (season packs). Linkage is M:N.
- Interactive search shares all components with the autonomous flow but bypasses hard-gating and tags the entry as a manual override.
- New workers: **AcquisitionWorker** (event-driven on `want.created`), **SearchScheduler** (drives tracking's recurring searches), **TMDBSeriesSyncWorker** (foundation gap for series — see [metadata](../metadata/README.md#series-structure-sync-the-foundation-gap)). The existing download/import workers are unchanged.
- No settings page. Configurable elements live in the sibling specs listed above.

## Why this doc exists

Two reasons:

1. **Acquisition isn't a single component.** It's a chain: search → gate → score → pick → route → grab → download → import → match → notify. The sibling specs each own pieces of it; this doc owns the chain itself.
2. **The architecture is shifting from user-driven to mixed.** Today, nothing happens until a user clicks. The new world has autonomous workers acting on persistent intent. Interactive search remains, but it's no longer the only path. The pipeline needs to serve both cleanly, and that shared orchestration is the substance of this spec.

## The pipeline: today vs new

### Today (user-driven, single path)

```
user clicks "grab"
  → DownloadCandidatesService.EnqueueCandidate(media_id, candidate_id)
    → indexer search (5min in-memory cache)
    → policy.Engine.Evaluate(context)          ← routing decisions
    → create download_job (+ media_item if new)
    → return trace + job                        ← trace is ephemeral
  → DownloadJobWorker polls download_job
    → hands torrent to qBittorrent
    → marks downloading → completed
  → ImportWorker reads completed jobs
    → creates import_task per file
    → hardlinks files into library, applies name template
    → creates media_file rows
```

The trace is returned in the API response and forgotten. There is no record of _which other releases were considered or rejected_.

### New (mixed: autonomous + interactive)

**Autonomous path** (monitoring / approved request):

```
tracking (single-atom for a movie, per-episode for a series) creates want(s)
  → emit want.created
AcquisitionWorker picks up want
  → indexer search (cache lookup, longer TTL, DB-backed)
  → quality profile: hard-gate filter         ┐
  → quality profile: score survivors           │ writes audit rows
  → pick best (bin-first → score → tiebreak)   ┘   for every release seen
  → if no eligible release: bump search counters, schedule retry, emit want.search_failed
  → if eligible: routing rules evaluate
  → create download_job linked to want(s)
  → emit want.grabbed
DownloadJobWorker (existing) → completed
ImportWorker (existing, with want linkage) → media_file
VerifyStep (new) → presence-verify on disk → emit want.available   ← Arrflix's own authority
MediaServerSync (new, downstream, non-blocking)
  → nudge each configured server to index (Plex partial-refresh / Jellyfin scan)
  → on webhook OR reconciliation poll: record propagation, emit media_file.propagated
```

**Interactive path** (the manual workflow today):

```
user opens movie/series → clicks search
  → indexer search (cache lookup)
  → display all results; quality-profile scoring shown but not gating
  → user picks a release
  → routing rules evaluate
  → create download_job (and a synthetic want if one doesn't exist yet)
  → write audit row marked manual_override
  → existing download/import flow
```

### What stays the same

The **back half** of the pipeline is unchanged. Once a download_job exists, the existing DownloadJobWorker and ImportWorker do their thing. Hardlinks, name templates, media_file creation — all stay. The new world is about the **front half**: persistent intent + a richer selection layer + an orchestrator that wakes on events instead of clicks.

## Components

### Indexer service (see [indexers](../indexers/README.md))

The search-execution path — Prowlarr query, capability-aware dispatch, result aggregation, the search cache, and per-indexer health/stats — is **owned by the [indexers module](../indexers/README.md)**. Acquisition is a consumer: it calls search, passes results to the quality profile, and fires the cache's invalidation hooks.

### Search cache (owned by indexers; invalidated here)

The DB-backed search cache **lives in [indexers](../indexers/README.md#search-cache-moved-from-acquisition)** (it has two consumers — this worker and manual search — so it belongs in the shared search subsystem, not here). It replaces the legacy in-memory, per-instance, 5-minute cache with a DB-backed store shared across restarts and worker processes, ~1-hour TTL for autonomous searches.

Acquisition's responsibility is to **fire the invalidation hooks** at the right moments:

- A new release is grabbed (so re-search doesn't see the just-grabbed release as still available)
- An indexer's config changes
- A manual "search now" request

The cache TTL is **not a user-configurable setting** — it's internal tuning.

### Quality profile engine (called, not owned here)

Defined in [quality profiles](../quality-profiles/README.md). At the pipeline level it exposes:

- `Gate(release, profile) → (passed, reject_reason)`
- `Score(release, profile) → score`
- `Pick(releases, profile, current_file?) → (winner, all_decisions)`

`Pick` returns both the chosen release and a structured list of every decision made (grabbed / runner_up / rejected, with reason), so the orchestrator can write audit rows in one go.

### Routing rules engine (called, not owned here)

Defined in [routing](../routing/README.md). Runs **after** the quality profile picks a release:

- Input: the picked release + media metadata (the `RoutingEvaluationContext` shape)
- Output: a `RoutingEvaluation` with the chosen downloader, library, name template

Routing's audit row sits alongside the quality-profile audit rows in the same decision log.

### Decision logging

Acquisition produces one of the three audit-trail streams in Arrflix. The mechanism, retention, and read surfaces (Activity view) are defined in the [decision-artifact pattern](../../patterns/audit/README.md).

Acquisition-specific contributions to that pattern:

- One audit row per release considered, per `(want, search_run)`.
- `decision`: `grabbed` | `runner_up` | `rejected` | `manual_override`.
- `reason`: structured (gate name + threshold + actual) + human summary.
- `score` (if scored).
- Release identity (indexer, guid / infohash, title).
- Search run metadata (when the search ran, what query, which indexers responded).
- For `manual_override`: which user picked it.

**Read patterns:**

- "For this want, why hasn't it grabbed yet?" → all decisions for the latest search run
- "For this grabbed file, what else was considered?" → entries for the search_run that produced the grab
- "For this profile, what's the rejection breakdown over the last week?" → aggregate by reason

**Retention:** see the [audit spec](../../patterns/audit/README.md#retention). Not configured here.

### AcquisitionWorker (new)

Event-driven on `want.created` and on scheduled retries from the SearchScheduler. For one want:

1. Look up the want's quality_profile_id (resolved at want creation from tier or tracking).
2. Build the search query (release name + year for movies; series title + S/E for episodes).
3. Cache-check; if miss, query indexers per profile's allowed indexer set.
4. Pass results to the quality profile's `Pick`.
5. If a winner exists:
   - Evaluate routing rules.
   - Create download_job linked to this want (and any other open wants the chosen release covers — see [Season packs](#season-packs)).
   - Persist all audit rows (quality + routing decisions).
   - Emit `want.grabbed`.
6. If no winner:
   - Persist rejection rows.
   - Schedule next search per the tracking's schedule strategy. Every want has a tracking parent (movies included, as a single-atom tracking), so there is no separate non-tracking retry path.
   - Emit `want.search_failed`.

Concurrency: process wants in parallel, but **never two simultaneous searches for the same query** (in-flight dedup via the search cache or a short-lived advisory lock).

### SearchScheduler (new)

A background worker that, for each active tracking, decides when to run the next search per the tracking's `schedule_strategy`. Implements the time-since-air bias described in [tracking](../tracking/README.md#smart-scheduling). Does **not** run searches itself — it produces "due to search" signals that wake up the AcquisitionWorker.

Smart scheduling rules summarized:

- Don't search before air time (episodes) or `release_date` (movies — a movie tracking anchors the same curve on its release date).
- Bias high-frequency searches to the 1-6h post-air window.
- Back off exponentially after consecutive empty results.
- Stop entirely when a want is `available` at-cutoff or tracking is `archived` / `paused`.

### TMDBSeriesSyncWorker (foundation gap)

Owned by [metadata](../metadata/README.md#series-structure-sync-the-foundation-gap). Mentioned here because acquisition depends on it: without an up-to-date series structure (including unaired episodes), tracking can't generate the wants the AcquisitionWorker is supposed to act on. Listed under [What gets added](#what-gets-added-summary) below as a dependency.

## Interactive vs autonomous: shared components, divergence

A core design goal: **interactive and autonomous flows share the gate/score/route components, but diverge on (a) gating enforcement, (b) audit-row decoration, and (c) who initiates.**

| Step               | Autonomous                                       | Interactive                                                                                                              |
| ------------------ | ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| Search             | Triggered by SearchScheduler or want.created     | Triggered by user click                                                                                                  |
| Indexer query      | Same cache, same indexer service                 | Same cache, same indexer service                                                                                         |
| Hard gates         | **Enforced** — failing releases rejected         | **Informational** — failing releases shown but marked                                                                    |
| Soft scores        | Used for ranking + auto-pick                     | Shown as a sortable column / sort default                                                                                |
| Selection          | Pipeline picks best                              | User picks                                                                                                               |
| Routing rules      | Evaluated against the auto-picked release        | Evaluated against the user-picked release                                                                                |
| Audit row          | One per release considered, normal decision type | One row marked `manual_override` for the picked release; can optionally also log all visible results for full visibility |
| Want               | Always exists (acquisition drives it)            | May not exist; synthesize one on grab so the rest of the pipeline is uniform                                             |

The shared infrastructure is the quality profile engine + routing rules engine + audit log + download_job + import_task. Both paths produce the same output: a download_job + linkages + audit rows.

**Sub-question to resolve in iteration 2:** should the interactive view _show_ the rejected releases by default (marked "would auto-reject: low seeders")? Probably yes — it's a teaching surface that helps admins understand what their profile does.

## Season packs

A single torrent can satisfy multiple wants. Examples:

- `Series.Name.S03.COMPLETE.1080p.BluRay` covers S03E01 through S03E10.
- `Movie.Name.1080p.x264-GROUP` (collection torrent) might cover several movies in a series.

The data model needs to support **1 download_job → N wants**.

### Linkage

- `download_job` ↔ `want` is M:N (intermediate table `download_job_want`, or similar).
- When AcquisitionWorker picks a release that covers multiple wants:
  - Identify all in-flight wants (`pending` or `searching` state) the release could fulfill — typically via parsed metadata (season number, episode range).
  - Link them all to the new download_job.
  - Transition all linked wants to `grabbed`.

### Overflow & under-coverage

- **Overflow**: the season pack has more episodes than the wants we have. Excess files are imported normally and create episode wants if scope rule includes them, otherwise become unmatched_files (or are stored as "extras" — TBD).
- **Under-coverage**: the season pack has fewer episodes than wanted (e.g., S03 pack on a show that aired S03E11 after the pack was published). Wants that _weren't_ covered stay in `searching`; the scheduler keeps looking.

### Import → want fulfillment

- `import_task` carries `want_id` (single) — each imported file fulfills exactly one want.
- The matcher (see [matching](../matching/README.md)) determines which want each file fulfills, by season+episode for series or by media_item for movies.
- On import completion: corresponding want transitions `downloading → imported`.

## Want status: the two axes

A want's runtime status has **two orthogonal axes**, and conflating them is what makes status UI sprawl. Keeping them separate gives every surface — the request pill, the subscription grid, the realtime events — one vocabulary to render from instead of per-screen strings.

**Axis 1 — `state` (the lifecycle).** The canonical state machine, unchanged:

`pending → searching → grabbed → downloading → imported → available` (+ terminals `failed`, `canceled`).

**Axis 2 — `annotation` (why it's holding).** An optional, structured reason explaining why a want isn't actively advancing right now. The bare state ("searching") doesn't distinguish "actively querying indexers" from "waiting out a back-off" from "blocked because every indexer is down" — but the user _and_ the scheduler's resume logic need that difference. The vocabulary is small and fixed (extensible as new holds emerge):

| Annotation         | Meaning                                                                  | Typical state                     | Set by                                                                 |
| ------------------ | ------------------------------------------------------------------------ | --------------------------------- | ---------------------------------------------------------------------- |
| _(none)_           | Actively advancing — searching now, or downloading now                   | any                               | —                                                                      |
| `awaiting_air`     | Episode hasn't aired yet; scheduler won't search                         | pending / searching               | SearchScheduler                                                        |
| `awaiting_release` | Movie not yet released; scheduler won't search                           | pending / searching               | SearchScheduler                                                        |
| `backing_off`      | Searched, nothing acceptable yet; in exponential back-off                | searching                         | SearchScheduler                                                        |
| `blocked`          | Can't proceed — required infrastructure is unhealthy (`blocked_on`: `indexer` / `downloader` / `library`) | searching / grabbed / downloading | SearchScheduler / pipeline, via [connectivity-health](../../patterns/connectivity-health/README.md) |

Plus a few **freshness / progress** fields the pill reads (data, not enum): `last_searched_at`, `search_attempt_count`, `considered_count` (cumulative releases evaluated — Story 4's "47 considered"), `next_attempt_at` (when the next search is due), and for the download phase `progress_pct` + `eta`.

**The pill is a pure render of `(state, annotation, freshness)`.** "Searching for HD release…", "Searching • last checked 4h ago", "Still searching • 47 considered", "Paused — waiting for indexers", "Downloading 12% • ~7 min" are all the _same_ render function over different status projections — not strings authored per story. Realtime want events ([realtime](../realtime/README.md)) carry this structured projection, not pre-rendered text, so copy stays consistent everywhere.

**Resume logic keys off the annotation, not the state.** A `blocked`(indexer) want resumes immediately on a `<resource>.health` recovery event; a `backing_off` want resumes on its `next_attempt_at` schedule. The scheduler distinguishes them by annotation — they're both `searching`, but they wake on different signals. (This is what [Story 10](../../stories/10-indexer-health-degraded-and-recovery.md) needs and what resolves its open question on distinguishing infra-blocked from backing-off wants.)

**Not a want annotation: media-server propagation.** Once a want is `available`, whether Plex/Jellyfin has indexed the file is a _separate_ per-`(media_file, server)` status (see [Media-server propagation](#media-server-propagation-decoupled-from-available)), surfaced as its own badge — not a want annotation. The want is done; propagation is downstream.

## Media-server propagation (decoupled from `available`)

**Arrflix owns the truth about its own library. A media server (Plex, Jellyfin) is a _downstream consumer_ of that library, not the authority on it.** The pipeline separates two orthogonal facts:

| Axis                      | Question                              | Owner                   | Gates the want?                         |
| ------------------------- | ------------------------------------- | ----------------------- | --------------------------------------- |
| **Acquisition state**     | Is the file in my library, verified?  | Arrflix (this pipeline) | **Yes** — this _is_ the want lifecycle  |
| **Media-server propagation** | Has Plex/Jellyfin indexed it yet?  | the media server        | **No** — enrichment only                |

### `available` means _verified on disk_, not _seen by Plex_

The last two transitions of the want lifecycle:

- **`imported`** — ImportWorker has hardlinked the file into the library path and written the `media_file` row. The file is _placed_.
- **`available`** — Arrflix has **verified the file is present and readable on disk** (a stat/size check; the same authority [scan](../scan/README.md)'s verify mode uses). The file is _confirmed_. This is the terminal happy state, and it is reachable with **no media server at all**.

For the common case the verify is an inline check microseconds after the hardlink. The state still earns its place: it catches the "hardlink reported success but the file is gone / the disk errored" case, and gives scan-verify a re-confirmation hook.

What this kills: the old `imported → available` transition was gated on a Plex `library.new` webhook. A missed webhook (Plex restart mid-scan, network blip) left the want stuck in `imported` **forever**, and Jellyfin / no-server users never reached a terminal state at all. That coupling is gone.

### Propagation is a separate, non-blocking axis

Once a file is `available`, Arrflix nudges each configured media server to index it (Plex partial-refresh, Jellyfin scan) and tracks the result as a **per-`(media_file, media_server)` propagation record** — server-agnostic, multi-server-ready:

- Populated by **(a)** the server's webhook when it fires (fast path), or **(b)** a best-effort reconciliation poll that queries the server's API (backfill when the webhook is missed).
- The media server is itself a [connectivity-health](../../patterns/connectivity-health/README.md) resource; reconciliation only runs while it's healthy.
- **It never gates the want.** Webhook missed → the want is already `available`; propagation simply stays `pending` until the next backfill catches it.

The full propagation model (record shape, reconciliation cadence, per-server webhook handling, `version_mismatch` health) lives in the **pending media-server spec** (a foundation gap). This section pins only the contract: _propagation enriches, it never gates._

### Notifications fire on `available`, links resolve lazily

"Ready to watch" fires the moment the want is `available` — Arrflix's own truth, never blocked on a server. Because Arrflix isn't a player (users watch _in_ Plex/Jellyfin), the notification's deep link resolves like this:

- Propagation already `visible` → embed the server deep link directly.
- Not yet → link to the **Arrflix media detail page**, which shows "syncing to your server…" and flips to "Open in Plex" the instant propagation confirms.

Never a dead link, never a stuck want. (An optional _grace window_ — hold the push up to ~60–120s when a healthy server is configured, so the deep link is usually live on first tap — is a UX nicety the media-server spec may add. The default without it is still correct.)

### Watch-state coupling is different — and legitimate

`cleanup_after_watch` retention ([requests](../requests/README.md#retention)) genuinely depends on a media server, because "has the user watched it" is only knowable from the player. That coupling is unavoidable and stays. It degrades gracefully: with no media server, `cleanup_after_watch` simply never triggers (treat as `keep_forever`, or pair with `keep_for_days`). Do not confuse it with _availability_ coupling, which we have removed.

## What stays the same

- Indexer service (Prowlarr wrapper)
- Downloader integrations (qBittorrent, etc.)
- Download_job state machine (`created` → `enqueued` → `downloading` → `completed` / `failed` / `cancelled`)
- Import_task state machine (`pending` → `in_progress` → `completed` / `failed` / `cancelled`)
- Name templates and their application during import
- Library scan + match
- Hardlink import
- TMDB enrichment for item-level metadata
- The error model and RFC 9457 wire format (see [errors](../../patterns/errors/README.md))

## What gets restructured

- **`DownloadCandidatesService` is split.** Today it does search + evaluate + enqueue. After:
  - The **search + display** half becomes part of the interactive flow surface.
  - The **evaluate + enqueue** half is generalized: the same orchestration (search → gate → score → pick → route → create job) becomes the AcquisitionWorker's loop, with an alternate user-pick entry point for interactive.
- **`download_job` grows `want` linkage.** Via an M:N intermediate, not a single FK.
- **`import_task` grows a `want_id` FK.** One want per imported file.
- **Search cache: in-memory → DB-backed**, longer TTL, with explicit invalidation hooks.
- **Media-server linkage stays off `media_item`.** Plex rating keys (and Jellyfin equivalents) live in a per-`(media_file, media_server)` propagation record, not a column on `media_item` — server-agnostic and multi-server-ready. Owned by the pending media-server spec; see [Media-server propagation](#media-server-propagation-decoupled-from-available).

## What gets added (summary)

New tables (owned by sibling specs unless noted):

- `quality_profile` and friends — see [quality profiles](../quality-profiles/README.md)
- `quality_tier` registry — see [quality profiles](../quality-profiles/README.md)
- `tracking` and friends — see [tracking](../tracking/README.md)
- `want` — see [tracking](../tracking/README.md)
- `request` — see users / permissions / approval (pending spec)
- Audit/decision tables — schema per producer; see [audit pattern](../../patterns/audit/README.md)
- `download_job_want` (M:N linkage) — **owned here**
- `indexer_search_cache` — **owned by [indexers](../indexers/README.md)**; acquisition is a consumer and fires its invalidation hooks
- (Optional iteration-2) `episode_release_pattern` for per-series search-timing learning — **owned here**
- `push_subscription` — see [notifications](../notifications/README.md)
- `notification_preference` — see [notifications](../notifications/README.md)

New services (owned by sibling specs unless noted):

- `RequestService`
- `TrackingService`
- `WantService`
- `QualityProfileService`
- `MediaServerService` (outbound refresh nudge + inbound webhook/reconciliation poll; Plex now, Jellyfin later) — see the pending media-server spec. **Never gates the want lifecycle.**
- `NotificationService` (with push channel) — see [notifications](../notifications/README.md)

New workers — **owned here**:

- `AcquisitionWorker`
- `SearchScheduler`

Dependency, owned by [metadata](../metadata/README.md):

- `TMDBSeriesSyncWorker` — without this, smart scheduling is blind to upstream changes.

## Event bus / messaging

The pipeline relies on events flowing between components. The [realtime](../realtime/README.md) module handles user-facing server-to-client updates well; we likely need an **internal event bus** in addition for worker-to-worker signaling. Concretely:

| Event                | Producer                                     | Consumers                                |
| -------------------- | -------------------------------------------- | ---------------------------------------- |
| `want.created`       | TrackingService (every want has a tracking parent — movies included) | AcquisitionWorker (wake-up), SSE (user)  |
| `want.grabbed`       | AcquisitionWorker                            | SSE, NotificationService                 |
| `want.search_failed` | AcquisitionWorker                            | SearchScheduler (back-off), SSE          |
| `want.imported`      | ImportWorker                                 | VerifyStep (presence-verify on disk)     |
| `want.available`     | VerifyStep (file verified on disk)           | NotificationService, SSE, MediaServerSync |
| `media_file.propagated` | MediaServerSync (webhook or reconciliation poll) | NotificationService (deep-link upgrade), SSE |
| `tracking.archived`  | TrackingService                              | SSE, NotificationService                 |
| `upgrade.proposed`   | AcquisitionWorker                            | NotificationService                      |

In-process pub/sub is sufficient for v1 — single container, all workers in the same Go process. If we ever split into separate processes, this graduates to a real bus.

## No settings page

Acquisition exposes no admin UI. Every knob in the flow lives in another spec:

| Concern                                  | Home                                                                          |
| ---------------------------------------- | ----------------------------------------------------------------------------- |
| Which releases qualify                   | [Quality profiles](../quality-profiles/README.md)                             |
| Where a grabbed release goes             | [Routing](../routing/README.md)                                               |
| When / how often to search               | [Tracking](../tracking/README.md) (per tracking) + tracking defaults (admin)  |
| Indexer connections + health             | Indexers spec (pending)                                                       |
| Downloader connections + eligibility     | Downloaders spec (pending)                                                    |
| User permissions / quotas / auto-approve | Users / permissions / approval spec (pending)                                 |
| Decision log retention                   | [Audit pattern](../../patterns/audit/README.md)                               |
| Activity / decisions timeline UI         | [Audit pattern](../../patterns/audit/README.md)                               |

If something here grows a knob worth surfacing, that knob lives in the appropriate sibling spec.

## Open questions

1. **Where does the resolved `quality_profile_id` live on a want?** Snapshot at want creation (so the want is stable even if the tier-profile binding changes mid-flight) or look up dynamically? Snapshot is safer; pin in iteration 2.
2. **Search cache scope.** Per `(media_item, query)` or finer? Series searches differ from episode searches; we want both to cache. Worth a small data-shape pass before implementing.
3. **Cancellation semantics.** If a want is canceled while a search is in flight, what happens? The search completes; results are discarded; no download_job is created. If a download_job is in flight, the existing cancel path covers it.
4. **Dedup against concurrent searches.** Two wants for the same media_item triggering simultaneous searches should share. The DB-backed cache + a short advisory lock should handle this, but worth nailing down.
5. **Synthetic wants for interactive grabs.** When the user manually grabs something with no pre-existing want, we synthesize one to keep the pipeline uniform. Lifecycle of synthetic wants: created in `grabbed` state directly? Or briefly `pending → grabbed`? Probably the former — no point in going through search-evaluation for a release the user has already picked. Since every want has a tracking parent, an interactive grab with no existing tracking synthesizes an admin-anchored single-atom tracking alongside the want; it auto-archives on import unless the user opts into upgrade-watching.
6. **Pre-grab dedup.** If a download_job already covers a want (in progress), the AcquisitionWorker should skip it. How is this enforced — query at the start of every Pick, or rely on a unique constraint that errors and we retry? Query-first is simpler; document it.
7. **Manual override + auto-upgrade interaction.** If a user manually grabs a 720p but the tracking is at 1080p profile, does auto-upgrade activate immediately? Probably yes (the manual grab doesn't override the profile, just the _current_ pick). Worth documenting in the upgrade-behavior section of [tracking](../tracking/README.md).

## What we're explicitly not deciding here

- Exact table schemas, columns, indexes for the acquisition-owned tables
- API endpoint shapes and request/response formats
- The custom-format DSL / scoring rule grammar (deferred to [quality profiles](../quality-profiles/README.md))
- Routing rule grammar (deferred to [routing](../routing/README.md))
- Audit retention policy (deferred to [audit pattern](../../patterns/audit/README.md))
- Worker concurrency limits, queue depths, back-off curves
- The event bus implementation (Go channel vs library)
- Migration ordering / data backfill plan when renames land
- Media-server propagation mechanics — record shape, reconciliation cadence, per-server webhook/correlation strategy. Decoupled from the want lifecycle here (see [Media-server propagation](#media-server-propagation-decoupled-from-available)); the details live in the pending media-server spec.

## Doc neighbors

- [Tracking](../tracking/README.md) — defines the ongoing-intent primitive that produces wants
- [Quality profiles](../quality-profiles/README.md) — defines how a release is gated and scored
- [Routing](../routing/README.md) — defines where a picked release goes
- [Matching](../matching/README.md) — turns imported files into identified content
- [Audit pattern](../../patterns/audit/README.md) — the decision-artifact pattern this pipeline writes into
- [Errors](../../patterns/errors/README.md) — the typed error model used throughout
- [Story 1](../../stories/01-happy-path-auto-approve.md) — concrete user flow that pressure-tests the pipeline end-to-end
