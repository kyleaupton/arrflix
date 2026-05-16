# Auto-select — the selection & grab pipeline

**Status:** Draft, iteration 1

This doc ties together the components that, given an intent to acquire something, turn that intent into a grabbed-and-imported file. It is the **keystone** spec: tracking, quality profiles, wants, requests, and the existing download/import machinery all meet here.

This doc:

- Disambiguates the **routing policy** (existing) from the **quality profile** ([new spec](../quality-profiles/README.md)) — they're not the same thing despite the legacy naming.
- Defines the new pipeline end-to-end, showing where each component plugs in.
- Captures what stays the same, what gets renamed, what gets restructured, and what's new.
- Defines the **decision log** as a first-class persisted artifact.
- Specifies how **interactive search** (the manual workflow) remains first-class alongside autonomous flows.

It does **not** pin down data shapes, exact APIs, or worker implementations. Those come later, after the model survives more user stories.

## TL;DR

- Today there's one thing called **`policy`**. It's a **routing policy** (picks downloader / library / name template). We're keeping it and renaming it.
- We're adding **`quality_profile`** — the _what-release-to-grab_ engine. It runs **before** routing in the pipeline. See the [quality profiles spec](../quality-profiles/README.md).
- The full pipeline is: **want → search → quality_profile (gate + score + pick) → routing_policy (decide where) → download_job → import_task → media_file → Plex notify → available**.
- **Decision log** is new and persistent: every release considered, with score and reason. Powers the "why didn't this download?" debugger.
- Interactive search stays first-class. It consumes the same scoring (for display) but bypasses gating; the user's pick is logged as a manual override.
- A single download_job can fulfill **many wants** (season packs). Linkage is M:N.
- New workers: **AutoSelectWorker** (event-driven on want creation), **SearchScheduler** (drives tracking's recurring searches), **TMDBSeriesSyncWorker** (keeps episode list current). The existing download/import workers are unchanged.

## Why this doc exists

Three reasons:

1. **A naming collision needs resolving.** The current `policy` table/service is a _routing_ policy. What we've been calling "policy engine" in the tracking and quality-profile design discussions is something different. Without disambiguation, every future doc and PR will be confusing.
2. **Auto-select isn't a single component.** It's a chain: search → gate → score → pick → route → grab. The tracking spec and quality-profile spec define pieces of it; this doc defines the chain itself.
3. **The architecture is shifting from user-driven to mixed.** Today, nothing happens until a user clicks. The new world has autonomous workers acting on persistent intent. Interactive search remains, but it's no longer the only path. The pipeline needs to serve both cleanly.

## The two policy concepts, disambiguated

Two things, two separate responsibilities, two different homes:

| Concept             | Question it answers                                 | Today               | Future name       | Spec                                                  |
| ------------------- | --------------------------------------------------- | ------------------- | ----------------- | ----------------------------------------------------- |
| **Quality profile** | Given many releases, which one (if any) do we grab? | Doesn't exist       | `quality_profile` | [../quality-profiles/](../quality-profiles/README.md) |
| **Routing policy**  | Given a release we're grabbing, where does it go?   | `policy` (existing) | `routing_policy`  | (back-fill, this doc covers the relabel)              |

They run in sequence:

- **Quality profile** runs against many candidate releases → picks one (or rejects all).
- **Routing policy** runs against the picked release → decides downloader, library, name template.

The routing policy is good and stays. It just needs its name to stop overlapping with the quality engine.

Why not collapse them into one "policy" engine with both quality rules and routing rules? Two reasons:

- The current rule grammar (operators over EvaluationContext) is great for routing — there are only a few discrete decisions to make. It would be awkward for quality scoring (which needs ranked lists, cutoffs, custom format scores with tie-breaks).
- They have different consumers. Quality profile is consumed by AutoSelectWorker, interactive search display, and the decision log. Routing policy is consumed at grab time only. Different concerns, different surfaces.

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
tracking (or movie request) creates want
  → emit want.created
AutoSelectWorker picks up want
  → indexer search (cache lookup, longer TTL, DB-backed)
  → quality_profile: hard-gate filter        ┐
  → quality_profile: score survivors          │ writes decision_log rows
  → pick best (bin-first → score → tiebreak)  ┘   for every release seen
  → if no eligible release: bump search counters, schedule retry, emit want.search_failed
  → if eligible: routing_policy.Evaluate
  → create download_job linked to want(s)
  → emit want.grabbed
DownloadJobWorker (existing) → completed
ImportWorker (existing, with want linkage) → media_file
PlexNotifier (new)
  → Plex partial-refresh
  → on library.new webhook: correlate, emit want.available
```

**Interactive path** (your manual workflow today):

```
user opens movie/series → clicks search
  → indexer search (cache lookup)
  → display all results; quality_profile scoring shown but not gating
  → user picks a release
  → routing_policy.Evaluate
  → create download_job (and a synthetic want if one doesn't exist yet)
  → write decision_log row marked manual_override
  → existing download/import flow
```

### What stays the same

The **back half** of the pipeline is unchanged. Once a download_job exists, the existing DownloadJobWorker and ImportWorker do their thing. Hardlinks, name templates, media_file creation — all stay. The new world is about the **front half**: persistent intent + a richer selection layer.

## Components

### Indexer service (existing, unchanged)

Thin Prowlarr wrapper. Search returns a list of raw indexer results. No scoring, no gating, no caching beyond what it does today. The cache becomes a separate concern (below).

### Search cache (restructured)

Today: in-memory, 5-minute TTL, per-service-instance.

New: **DB-backed** so autonomous workers share cache state across restarts and across worker processes. Longer TTL (probably ~1 hour) for autonomous searches, with explicit invalidation when:

- A new release is grabbed (so re-search doesn't see the just-grabbed release as still available)
- An indexer's config changes
- A manual "search now" request

Schema-wise: a table keyed by `(media_item_id or episode_id, query_hash)` storing the raw indexer result blob with an expiry. Out of scope here; covered in the data-shape iteration.

### Quality profile engine

See [quality-profiles/](../quality-profiles/README.md). At the pipeline level, it exposes:

- `Gate(release, profile) → (passed, reject_reason)`
- `Score(release, profile) → score`
- `Pick(releases, profile, current_file?) → (winner, all_decisions)`

`Pick` returns both the chosen release and a structured list of every decision made (grabbed / runner-up / rejected, with reason), so the caller can persist the decision log in one go.

### Routing policy engine (existing, renamed)

The current `policy.Engine` stays. Its inputs and outputs don't change. It runs **after** the quality profile picks a release:

- Input: the picked release + media metadata (the existing `EvaluationContext` shape, possibly enriched)
- Output: `EvaluationTrace` with the chosen downloader, library, name template

Two things to consider:

- **`stop_processing` action.** Today the routing policy can short-circuit. In the new pipeline, gating is the quality profile's job; the routing policy should probably _not_ be able to reject a release outright. Leave the action for now (don't break existing user configs), but document it as deprecated in favor of quality-profile gates.
- **Persisting routing decisions.** The trace is currently API-only. In the new pipeline, the routing decision should be persisted alongside the quality decision in the decision log (or in a sibling field on the download_job).

### Decision log (new)

A persistent, append-only record of every release the auto-select pipeline has considered.

**What gets logged:**

- One entry per release considered for a given (want, search_run) pair.
- `decision`: `grabbed` | `runner_up` | `rejected` | `manual_override`
- `reason`: structured (gate name + threshold + actual) + human summary
- `score` (if scored)
- Release identity (indexer, guid / infohash, title)
- Search run metadata (when the search ran, what query, which indexers responded)
- For `manual_override`: which user picked it

**Read patterns:**

- "For this want, why hasn't it grabbed yet?" → all decisions for the latest search run
- "For this grabbed file, what else was considered?" → entries for the search_run that produced the grab
- "For this profile, what's the rejection breakdown over the last week?" → aggregate by reason

**Write patterns:**

- High volume, mostly append. Each search run can produce dozens of entries.
- Bulk insert at end of `Pick`.

**Retention:**

- Likely tiered: keep entries for `grabbed` indefinitely (or until want is canceled); keep `rejected`/`runner_up` for a bounded window (30-90 days). Pin in retention iteration.

### AutoSelectWorker (new)

Event-driven on `want.created` and on scheduled retries from the SearchScheduler. For one want:

1. Look up the want's quality_profile_id (resolved at want creation from tier or tracking).
2. Build the search query (release name + year for movies; series title + S/E for episodes).
3. Cache-check; if miss, query indexers per profile's allowed indexer set.
4. Pass results to the quality profile's `Pick`.
5. If a winner exists:
   - Evaluate routing_policy.
   - Create download_job linked to this want (and any other open wants the chosen release covers — see [Season packs](#season-packs)).
   - Persist all decision_log entries.
   - Emit `want.grabbed`.
6. If no winner:
   - Persist rejection decisions.
   - Schedule next search per the tracking's schedule strategy (or one-off retry policy for non-tracking wants).
   - Emit `want.search_failed`.

Concurrency: process wants in parallel, but **never two simultaneous searches for the same query** (in-flight dedup via the search cache or a short-lived advisory lock).

### SearchScheduler (new)

A background worker that, for each active tracking, decides when to run the next search per the tracking's `schedule_strategy`. Implements the time-since-air bias described in [tracking](../tracking/README.md#smart-scheduling). Does **not** run searches itself — it produces "due to search" signals that wake up the AutoSelectWorker.

Smart scheduling rules summarized:

- Don't search before air time.
- Bias high-frequency searches to the 1-6h post-air window.
- Back off exponentially after consecutive empty results.
- Stop entirely when a want is `available` at-cutoff or tracking is `archived`/`paused`.

### TMDBSeriesSyncWorker (new — foundation gap)

For each tracked series, periodically fetches the upstream episode list + air dates from TMDB. Detects:

- New episodes that fit a scope rule → generates new wants.
- TMDB status changes (`Returning Series` → `Ended`) → triggers `active → archived` evaluation.
- Renumbering / restructuring → flagged for review (Sonarr famously struggles here).

Not part of the auto-select pipeline strictly speaking, but **required for it to work** for series. Without this worker, smart scheduling is blind to upstream changes.

## Interactive vs autonomous: shared components, divergence

A core design goal: **interactive and autonomous flows share the gate/score/route components, but diverge on (a) gating enforcement, (b) decision logging, and (c) who initiates.**

| Step               | Autonomous                                       | Interactive                                                                                                              |
| ------------------ | ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| Search             | Triggered by SearchScheduler or want.created     | Triggered by user click                                                                                                  |
| Indexer query      | Same cache, same indexer service                 | Same cache, same indexer service                                                                                         |
| Hard gates         | **Enforced** — failing releases rejected         | **Informational** — failing releases shown but marked                                                                    |
| Soft scores        | Used for ranking + auto-pick                     | Shown as a sortable column / sort default                                                                                |
| Selection          | Pipeline picks best                              | User picks                                                                                                               |
| Routing policy     | Runs against the auto-picked release             | Runs against the user-picked release                                                                                     |
| Decision log entry | One per release considered, normal decision type | One row marked `manual_override` for the picked release; can optionally also log all visible results for full visibility |
| Want               | Always exists (auto-select drives it)            | May not exist; synthesize one on grab so the rest of the pipeline is uniform                                             |

The shared infrastructure is the quality profile engine + routing policy + decision log + download_job + import_task. Both paths produce the same output: a download_job + linkages + a decision log entry.

**Sub-question to resolve in iteration 2:** should the interactive view _show_ the rejected releases by default (marked "would auto-reject: low seeders")? Probably yes — it's a teaching surface that helps admins understand what their profile does. Out of scope here.

## Season packs

A single torrent can satisfy multiple wants. Examples:

- `Series.Name.S03.COMPLETE.1080p.BluRay` covers S03E01 through S03E10.
- `Movie.Name.1080p.x264-GROUP` (collection torrent) might cover several movies in a series.

The data model needs to support **1 download_job → N wants**.

### Linkage

- `download_job` ↔ `want` is M:N (intermediate table `download_job_want`, or similar).
- When AutoSelectWorker picks a release that covers multiple wants:
  - Identify all in-flight wants (`pending` or `searching` state) the release could fulfill — typically via parsed metadata (season number, episode range).
  - Link them all to the new download_job.
  - Transition all linked wants to `grabbed`.

### Overflow & under-coverage

- **Overflow**: the season pack has more episodes than the wants we have. Excess files are imported normally and create episode wants if scope rule includes them, otherwise become unmatched_files (or are stored as "extras" — TBD).
- **Under-coverage**: the season pack has fewer episodes than wanted (e.g., S03 pack on a show that aired S03E11 after the pack was published). Wants that _weren't_ covered stay in `searching`; the scheduler keeps looking.

### Import → want fulfillment

- `import_task` carries `want_id` (single) — each imported file fulfills exactly one want.
- The matcher (existing scan/match logic) determines which want each file fulfills, by season+episode for series or by media_item for movies.
- On import completion: corresponding want transitions `downloading → imported`.

## What stays the same

- Indexer service (Prowlarr wrapper)
- Downloader integrations (qBittorrent, etc.)
- Download_job state machine (`created` → `enqueued` → `downloading` → `completed` / `failed` / `cancelled`)
- Import_task state machine (`pending` → `in_progress` → `completed` / `failed` / `cancelled`)
- Name templates and their application during import
- Library scan + match
- Hardlink import
- TMDB enrichment for item-level metadata
- The error model and RFC 9457 wire format

## What gets renamed

| Old                         | New                                | Notes                                        |
| --------------------------- | ---------------------------------- | -------------------------------------------- |
| `policy` (table)            | `routing_policy`                   | Migration renames; data preserved            |
| `policy.Engine`             | `routingpolicy.Engine`             | Package rename                               |
| `PoliciesService`           | `RoutingPoliciesService`           | Service rename                               |
| `/api/v1/policies/*`        | `/api/v1/routing-policies/*`       | Breaking; user is fine with breaking changes |
| `EvaluationContext`/`Trace` | `RoutingEvaluation{Context,Trace}` | Type rename for clarity                      |
| `policy.evaluate` (dry-run) | `routing-policies/evaluate`        | Endpoint rename                              |

## What gets restructured

- **`DownloadCandidatesService` is split.** Today it does search + evaluate + enqueue. After:
  - The **search + display** half becomes part of the interactive flow surface.
  - The **evaluate + enqueue** half is generalized: the same orchestration (search → gate → score → pick → route → create job) becomes the AutoSelectWorker's loop, with an alternate user-pick entry point for interactive.
- **`download_job` grows `want` linkage.** Via an M:N intermediate, not a single FK.
- **`import_task` grows a `want_id` FK.** One want per imported file.
- **Search cache: in-memory → DB-backed**, longer TTL, with explicit invalidation hooks.
- **`media_item` may grow `plex_rating_key`** for webhook correlation (see [Story 1 open questions](../stories/01-happy-path-auto-approve.md#open-questions)).

## What gets added (summary)

New tables:

- `quality_profile` (+ associated config tables — exact shape TBD)
- `quality_tier` registry
- `tracking` (+ `tracking_requester`, per-episode override)
- `want`
- `request`
- `decision_log`
- `download_job_want` (M:N linkage)
- `indexer_search_cache` (DB-backed cache)
- (Optional iteration-2) `episode_release_pattern` for per-series search-timing learning
- `push_subscription` (for PWA push)
- `notification_preference`

New services:

- `RequestService`
- `TrackingService`
- `WantService`
- `QualityProfileService`
- `DecisionLogService`
- `PlexIntegrationService` (outbound refresh + inbound webhook receiver)
- `NotificationService` (with push channel)

New workers:

- `AutoSelectWorker`
- `SearchScheduler`
- `TMDBSeriesSyncWorker`

## Event bus / messaging

The pipeline relies on events flowing between components. The existing SSE broker handles user-facing realtime updates well; we likely need an **internal event bus** in addition for worker-to-worker signaling. Concretely:

| Event                | Producer                                     | Consumers                                |
| -------------------- | -------------------------------------------- | ---------------------------------------- |
| `want.created`       | RequestService / TrackingService / Admin add | AutoSelectWorker (wake-up), SSE (user)   |
| `want.grabbed`       | AutoSelectWorker                             | SSE, NotificationService                 |
| `want.search_failed` | AutoSelectWorker                             | SearchScheduler (back-off), SSE          |
| `want.imported`      | ImportWorker                                 | PlexIntegrationService (trigger refresh) |
| `want.available`     | PlexIntegrationService                       | NotificationService, SSE                 |
| `tracking.archived`  | TrackingService                              | SSE, NotificationService                 |
| `upgrade.proposed`   | AutoSelectWorker                             | NotificationService                      |

In-process pub/sub is sufficient for v1 — single container, all workers in the same Go process. If we ever split into separate processes, this graduates to a real bus.

## Open questions

1. **Routing policy's `stop_processing` action.** Should we deprecate it? Quality gating belongs in the quality profile. Keeping `stop_processing` in routing makes the boundary fuzzy. Probably deprecate (warn but allow) and remove in a later iteration.
2. **Where does the resolved `quality_profile_id` live on a want?** Snapshot at want creation (so the want is stable even if the tier-profile binding changes mid-flight) or look up dynamically? Snapshot is safer; pin in iteration 2.
3. **Search cache scope.** Per `(media_item, query)` or finer? Series searches differ from episode searches; we want both to cache. Worth a small data-shape pass before implementing.
4. **Cancellation semantics.** If a want is canceled while a search is in flight, what happens? The search completes; results are discarded; no download_job is created. If a download_job is in flight, the existing cancel path covers it.
5. **Dedup against concurrent searches.** Two wants for the same media_item triggering simultaneous searches should share. The DB-backed cache + a short advisory lock should handle this, but worth nailing down.
6. **Interactive search showing rejected releases.** Default to showing them (marked) or hiding them with an opt-in toggle? Probably show by default for power users; toggle for the request-style UI.
7. **Routing decision persistence.** Should the routing trace also live in `decision_log`, or as a sibling JSON field on `download_job`? Probably the latter (one row per job, not per release), but raise this in the decision-log data-shape iteration.
8. **Synthetic wants for interactive grabs.** When the user manually grabs something with no pre-existing want, we synthesize one to keep the pipeline uniform. Lifecycle of synthetic wants: created in `grabbed` state directly? Or briefly `pending → grabbed`? Probably the former — no point in going through search-evaluation for a release the user has already picked.
9. **Pre-grab dedup.** If a download_job already covers a want (in progress), the AutoSelectWorker should skip it. How is this enforced — query at the start of every Pick, or rely on a unique constraint that errors and we retry? Query-first is simpler; document it.
10. **Manual override + auto-upgrade interaction.** If a user manually grabs a 720p but the tracking is at 1080p profile, does auto-upgrade activate immediately? Probably yes (the manual grab doesn't override the profile, just the _current_ pick). Worth documenting in the upgrade-behavior section of [tracking](../tracking/README.md).
11. **Routing policy enrichment.** The current `EvaluationContext` has release-parsed fields + media metadata. The new pipeline might want to pass _also_ the quality profile decision (quality bin, score, custom format hits). Should `EvaluationContext` grow, or should routing operate purely on the release+media as today?

## What we're explicitly not deciding here

- Exact table schemas, columns, indexes
- API endpoint shapes and request/response formats
- The custom-format DSL / scoring rule grammar (deferred to [quality profiles](../quality-profiles/README.md))
- The exact retention policy for decision_log
- Worker concurrency limits, queue depths, back-off curves
- The event bus implementation (Go channel vs library)
- Migration ordering / data backfill plan when renames land
- Plex correlation strategy (covered in Story 1 open questions)

Each gets its own pass once the shape here holds up against more stories.

## Doc neighbors

- [Tracking](../tracking/README.md) — defines the ongoing-intent primitive that produces wants
- [Quality profiles](../quality-profiles/README.md) — defines how a release is gated and scored
- [Story 1](../stories/01-happy-path-auto-approve.md) — concrete user flow that pressure-tests the pipeline end-to-end
- [Errors](../errors/README.md) — the typed error model used throughout
