---
status: v1
---

# Acquisition — orchestrating the get-it-into-the-library flow

> **Beyond what's built:** The autonomous front half (claim → search → gate/score/pick → route → grab or propose), season-pack linkage, and the back-half want mirror through `available` are built. Unbuilt design: §Decision logging (no audit rows are written), the DB-backed search cache, the import-time re-gate, §Media-server propagation, the `want.*` event bus in §Event bus / messaging (only `want_updated` SSE exists), a separate SearchScheduler worker, and the `annotation` values in §Want status beyond the `hold` column's `needs_pick` / `proposed`. V1's slice is defined in [V1.md](../../V1.md).

This doc defines the **acquisition pipeline**: the orchestration layer that takes a want from "we intend to acquire this" to "the file is in the library and verified." Surfacing that file in a downstream media server (Plex, Jellyfin) is a separate, non-blocking concern — see [Media-server propagation](#media-server-propagation-decoupled-from-available). It is the spine that ties together [tracking](../tracking/README.md), [quality profiles](../quality-profiles/README.md), [routing](../routing/README.md), [matching](../matching/README.md), and the existing download/import machinery.

Acquisition is a **code module, not a settings surface.** It has no user-facing configuration of its own — every configurable knob the flow exposes lives in a sibling spec (quality profiles, routing rules, tracking defaults, indexers, downloaders, user permissions). What lives here is plumbing: the worker, search execution, the season-pack linkage, and (unbuilt) the decision log writer and event bus.

## TL;DR

- Acquisition is **orchestration**, not selection. Picking is owned by [quality profiles](../quality-profiles/README.md); post-pick decisions are owned by [routing](../routing/README.md). This doc owns the chain that connects them.
- The pipeline: **want → search → quality (gate + score + pick) → routing (decide where) → download_job → import_task → file → verify → available**. A want reaches `available` when Arrflix verifies the file on disk — **not** when Plex sees it. Media-server sync (Plex/Jellyfin) happens _after_ `available` and never gates it.
- The pipeline is designed to write audit rows following the system-wide [decision-artifact pattern](../../patterns/audit/README.md) (unbuilt — the pick is logged, not persisted). Retention is centralized there, not here.
- A single download_job can fulfill **many wants** (season packs). Linkage is M:N.
- Interactive search shares routing, the want spine, and the download/import back half with the autonomous flow, but skips the quality profile entirely — the user's pick is the selection.
- Workers: **AcquisitionWorker** (`jobs/acquisition`, polls runnable wants), with recurring-search timing resolved by **SchedulerService** + the pure `internal/cadence` curve rather than a separate SearchScheduler worker. Series structure sync runs inside the enrichment worker (`EnrichmentService.SyncSeriesStructure` — see [metadata](../metadata/README.md#series-structure-sync-the-foundation-gap)). The download and import workers carry the back half.
- No settings page. Configurable elements live in the sibling specs listed above.

## Why this doc exists

Two reasons:

1. **Acquisition isn't a single component.** It's a chain: search → gate → score → pick → route → grab → download → import → match → notify. The sibling specs each own pieces of it; this doc owns the chain itself.
2. **The architecture is mixed.** Autonomous workers act on persistent intent; interactive search remains, but it isn't the only path. The pipeline needs to serve both cleanly, and that shared orchestration is the substance of this spec.

## The pipeline

### Autonomous and interactive paths

**Autonomous path** (monitoring / approved request). Steps marked _(unbuilt)_ are design:

```
tracking (single-atom for a movie, per-episode for a series via ReconcileService) creates want(s)
AcquisitionWorker (3s poll) claims runnable wants: pending, no hold, next_run_at ≤ now
  (FOR UPDATE SKIP LOCKED, flips them to 'searching')
  → indexer search (free text; no cache)                      (DB-backed cache: unbuilt)
  → exclusion + relevance gates (blocklist, identity match)
  → quality profile: gate → score → pick, singles and packs separately
  → coverage-first choice between best single and best pack
  → if no eligible release: recheck at the cadence curve's next_run_at, want → pending
  → if eligible: routing.Dispatch resolves downloader / library / name template
  → tracking autonomy for the want's segment is 'propose'? → write a proposal, hold='proposed'
  → else: create download_job linked to want(s), want → grabbed (one tx)
  (audit rows for every release seen: unbuilt — the selection is logged only)
DownloadWorker → hands the release to the downloader, mirrors downloading, → completed
  → spawns import_task per file, fanned out onto linked wants
  → on downloader failure: exclude the release per want, want → pending (re-search)
ImportWorker
  → ffprobe (feeds the name template)
  → quality re-gate on the real file                          (unbuilt)
  → hardlink/copy → verify present + non-empty → file rows, want → imported (one tx)
  → want → available, enqueue want.available notification    ← Arrflix's own authority
MediaServerSync (downstream, non-blocking)                    (unbuilt)
  → nudge each configured server to index (Plex partial-refresh / Jellyfin scan)
  → on webhook OR reconciliation poll: record propagation, emit file.propagated
```

**Interactive path** (`DownloadCandidatesService`):

```
user opens movie/series → clicks search
  → indexer search; results held in a 5-minute in-memory cache keyed by (indexer_id, guid)
  → display all results (no quality-profile scoring)
  → user picks a release (optional preview: routing evaluation trace)
  → routing.Dispatch resolves downloader / library / name template
  → movie: ensure media_item + tracking, create or CAS the want straight to 'grabbed'
    (NULL quality profile), create download_job, link it
  → series: create download_job; link it to the episode's live want if one exists
  → same download/import back half
```

The routing trace is returned in the API response and not persisted; there is no record of _which other releases were considered_.

### What stays the same

The **back half** of the pipeline is shared by both paths. Once a download_job exists, the DownloadWorker and ImportWorker do their thing — hardlinks, name templates, file creation. The designed addition is the **import-time re-gate** (see below, unbuilt): before the file is placed, the profile is re-run against the file's asserted attributes.

## Import-time re-gate (asserted verification)

**Unbuilt.** The selection front-half decides the grab on **advertised** attributes (the [parsed](../parsing/README.md) release title — all we have pre-download). The back-half adds the mirror step: at import, before the file is placed, the [importer](../importer/README.md) probes the file with `ffprobe` and re-runs the same [quality profile](../quality-profiles/README.md#import-time-re-gate) against the **asserted** attributes. **Advertised gates the grab; asserted gates the keep.**

This is distinct from VerifyStep, and the two sit side by side in the back half answering different questions:

- **VerifyStep** asks _"is the file there?"_ — presence/size on disk (`imported → available`). Built, inline in the import worker.
- **The re-gate** asks _"is the file what we ordered?"_ — quality on the actual bytes (before placement).

Outcomes — the pass/penalize/hard-fail _logic_ is owned by [quality profiles](../quality-profiles/README.md#import-time-re-gate); the _orchestration_ is owned here:

- **Pass / soft-fail** → the file is placed. A soft-fail (over-advertised but acceptable — e.g., a false Atmos claim on a fine 1080p) is kept, its recorded score penalized, and a [`quality/advertised-mismatch`](../hygiene/README.md) finding logged at import. No user notification — soft mismatches are common.
- **Hard-fail** (the asserted file fails a gate `ffprobe` can verify — e.g., advertised 2160p, the stream is 1080p) → the import is rejected, the release is **blocklisted**, and the want returns to `searching` to re-grab. The AcquisitionWorker treats this like a failed search with the offending release excluded, so it can't re-pick the same bad file. Emits `want.regate_failed`.

The re-gate can only reject on attributes `ffprobe` asserts; source / edition / group stay advertised-trusted (see the [parsing taxonomy](../parsing/README.md#what-ffprobe-can-and-cannot-verify)). The design enforces this partition structurally — each gate carries an `ffprobe`-verifiable tag and the re-gate runs only the tagged subset ([quality profiles](../quality-profiles/README.md#open-questions)); only the soft-fail penalty magnitude remains open.

## Components

### Indexer service (see [indexers](../indexers/README.md))

The search-execution path — Prowlarr query, result aggregation, and (unbuilt) the search cache and per-indexer health/stats — is **owned by the [indexers module](../indexers/README.md)**. Acquisition is a consumer: it calls `IndexerSource.Search` and passes results to the quality profile.

### Search cache (owned by indexers; invalidated here)

**Unbuilt.** The autonomous path searches uncached; only the interactive path holds results, in `DownloadCandidatesService`'s 5-minute in-memory map. The DB-backed search cache **lives in [indexers](../indexers/README.md#search-cache-moved-from-acquisition)** (it has two consumers — this worker and manual search — so it belongs in the shared search subsystem, not here). It replaces the interactive path's in-memory, per-instance cache with a DB-backed store shared across restarts and worker processes, ~1-hour TTL for autonomous searches.

Acquisition's responsibility is to **fire the invalidation hooks** at the right moments:

- A new release is grabbed (so re-search doesn't see the just-grabbed release as still available)
- An indexer's config changes
- A manual "search now" request

The cache TTL is **not a user-configurable setting** — it's internal tuning.

### Quality profile engine (called, not owned here)

Defined in [quality profiles](../quality-profiles/README.md). `QualityProfileService` loads the want's profile and runs the pure `internal/qualityprofile` engine over one group of Subjects, returning the winner plus a `Selection` holding every candidate's evaluation (gate results, bin, score). The orchestrator runs it twice per want — once over singles, once over packs — and logs the selection; persisting it as audit rows is unbuilt.

### Routing rules engine (called, not owned here)

Defined in [routing](../routing/README.md). Runs **after** the quality profile picks a release:

- Input: the [Subject](../../patterns/rules/README.md#the-subject) at the `grab` moment (assembled here: picked release + media)
- Output: `RoutingService.Dispatch` returns the resolved downloader, library, and name template (every slot resolved, from a matching rule or the defaults) plus the evaluation trace

Routing's audit row is designed to sit alongside the quality-profile audit rows in the same decision log (unbuilt).

### Decision logging

**Unbuilt** — no decision rows are written today. Acquisition produces one of the three audit-trail streams in Arrflix. The mechanism, retention, and read surfaces (Activity view) are defined in the [decision-artifact pattern](../../patterns/audit/README.md).

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

A poll loop (`jobs/acquisition`, every 3s, up to 20 wants per tick) that claims runnable wants — `pending`, no `hold`, `next_run_at ≤ now` — with `FOR UPDATE SKIP LOCKED`, flipping them to `searching`, and hands each to `AcquisitionService.ProcessWant`. There is no `want.created` wake-up; a new want is picked up on the next tick. For one want:

1. Skip it if a sibling's season pack already grabbed it this tick.
2. Build the search query from the stored media item (title + year for movies; `<title> SxxEyy` for episodes, broadened to `<title> Sxx` when the want has ≥2 in-flight season siblings, so packs enter the pool). The query is free text across all indexers.
3. Drop candidates the want has excluded (`want_release_exclusion`) and candidates that fail the relevance gate (wrong title/year/IMDb id, or wrong episode).
4. Run the want's quality profile (the snapshotted `want.quality_profile_id`) over singles and packs separately; choose between the two winners coverage-first.
5. If a winner exists:
   - `routing.Dispatch` resolves the downloader, library, and name template.
   - If the tracking's autonomy for the want's segment is `propose`, write a [proposal](#season-packs) instead and park the want(s) at `hold = 'proposed'`.
   - Otherwise, in one transaction, CAS the want (and any pack-covered siblings) `searching → grabbed` and create the download_job linked to them.
6. If no winner: return the want to `pending` with `next_run_at` from `SchedulerService.NextSearchAt` (the cadence curve) and reset `attempt_count`. Every want has a tracking parent (movies included, as a single-atom tracking), so there is no separate non-tracking retry path.
7. On error: a retryable error (an indexer unreachable) returns the want to `pending` with exponential backoff capped at 1h and bumps `attempt_count` — it never fails an in-scope want. A non-retryable error fails the want.

Each tick first runs a reaper that resets wants stuck in `searching` for more than 5 minutes back to `pending` (the crash window between claim and a terminal transition).

Concurrency: wants in a tick are processed sequentially. **Unbuilt:** parallel processing with in-flight dedup so two wants never search the same query at once.

### SearchScheduler (new)

Built as a resolver, not a worker: `SchedulerService.NextSearchAt` reads a want's release anchor (episode `air_date`, movie `release_date`, else the want's `created_at`) and its tracking's `schedule_strategy`, and the pure `internal/cadence` package returns the next search time. The AcquisitionWorker writes that onto `want.next_run_at` after a search finds nothing; the claim query's `next_run_at ≤ now` is the "due to search" signal. Implements the time-since-air bias described in [tracking](../tracking/README.md#smart-scheduling).

Smart scheduling rules summarized:

- Don't search before air time (episodes: `ReconcileService` stamps a future episode's want with `next_run_at = air_date`). **Unbuilt** for movies: nothing holds a movie want until its obtainable date ([V1.md](../../V1.md#not-out-yet--not-out-yet)).
- The `smart` curve rechecks every 1h in the first hour after the anchor, every 15m from 1–6h, 1h from 6–24h, 6h up to a week, then daily. `fixed` is a flat 6h.
- Empty results don't back off exponentially — the curve alone sets the pace. (Errors back off exponentially, capped; see above.)
- Stop when a want is `available`. **Unbuilt:** tracking `paused` / `archived`, and upgrade-watching past `available`.

### TMDBSeriesSyncWorker (foundation gap)

Owned by [metadata](../metadata/README.md#series-structure-sync-the-foundation-gap); runs as `EnrichmentService.SyncSeriesStructure` inside the enrichment worker rather than as its own worker. Mentioned here because acquisition depends on it: without an up-to-date series structure (including unaired episodes), tracking can't generate the wants the AcquisitionWorker is supposed to act on. Listed under [What gets added](#what-gets-added-summary) below as a dependency.

## Interactive vs autonomous: shared components, divergence

A core design goal: **interactive and autonomous flows share the gate/score/route components, but diverge on (a) gating enforcement, (b) audit-row decoration, and (c) who initiates.**

| Step               | Autonomous                                       | Interactive                                                                                                              |
| ------------------ | ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| Search             | Triggered by a due `next_run_at`                 | Triggered by user click                                                                                                  |
| Indexer query      | Same indexer service, uncached                   | Same indexer service; 5-minute in-memory result cache                                                                    |
| Hard gates         | **Enforced** — failing releases rejected         | **Informational** (unbuilt — today no profile runs at all)                                                               |
| Soft scores        | Used for ranking + auto-pick                     | Shown as a sortable column / sort default (unbuilt)                                                                      |
| Selection          | Pipeline picks best                              | User picks                                                                                                               |
| Routing rules      | Evaluated against the auto-picked release        | Evaluated against the user-picked release                                                                                |
| Audit row          | One per release considered, normal decision type | One row marked `manual_override` for the picked release; can optionally also log all visible results (unbuilt, both paths) |
| Want               | Always exists (acquisition drives it)            | Movie: synthesized at `grabbed` if absent. Series: linked only if the episode already has a live want                    |

The shared infrastructure is the routing rules engine + the want spine + download_job + import_task (and, by design, the quality profile engine and audit log). Both paths produce the same output: a download_job + linkages.

**Open sub-question:** should the interactive view _show_ the rejected releases by default (marked "would auto-reject: low seeders")? Probably yes — it's a teaching surface that helps admins understand what their profile does.

## Season packs

A single torrent can satisfy multiple wants. Examples:

- `Series.Name.S03.COMPLETE.1080p.BluRay` covers S03E01 through S03E10.
- `Movie.Name.1080p.x264-GROUP` (collection torrent) might cover several movies in a series.

The data model needs to support **1 download_job → N wants**.

### Linkage

- `download_job` ↔ `want` is M:N (`download_job_want`).
- When AcquisitionWorker picks a release that covers multiple wants:
  - Identify the in-flight wants (`pending` or `searching`) of the same tracking and season the release covers — via the parsed season / episode range. A sibling that has excluded this release is carved out.
  - A pack is chosen over the best single only when it covers ≥2 in-flight wants (or no single qualifies).
  - Find-or-create the job by `(indexer_id, guid)` over in-flight jobs, CAS the covered wants to `grabbed`, and link the ones the CAS claimed.
- A `propose`-autonomy pick writes a `proposal` (+ `proposal_want` links, the same M:N shape) holding the fully resolved grab; approve replays it through the same grab path, decline excludes the release (`reason = 'declined'`) and re-arms the wants.

### Overflow & under-coverage

- **Overflow**: the season pack has more episodes than the wants we have. Today excess files get no import task and stay in the download directory. Design: they create episode wants if the scope rule includes them, otherwise become unidentified files (or are stored as "extras" — TBD).
- **Under-coverage**: the season pack has fewer episodes than wanted (e.g., S03 pack on a show that aired S03E11 after the pack was published). At import spawn, linked wants with no matching file are released back to `pending` and unlinked from the job; the scheduler keeps looking.

### Import → want fulfillment

- `import_task` carries `want_id` (single) — each imported file fulfills exactly one want.
- The download worker's import fan-out (not the [matcher](../matching/README.md)) determines which want each file fulfills: by parsed season+episode against the job's linked wants for series, the single linked want for movies.
- On import completion: the want transitions `downloading → imported → available`.

## Want status: the two axes

A want's runtime status has **two orthogonal axes**, and conflating them is what makes status UI sprawl. Keeping them separate gives every surface — the request pill, the subscription grid, the realtime events — one vocabulary to render from instead of per-screen strings.

**Axis 1 — `state` (the lifecycle).** The canonical state machine (`want.status`):

`pending → searching → grabbed → downloading → imported → available` (+ terminals `failed`, `canceled`).

**Axis 2 — `annotation` (why it's holding).** Built as the nullable `want.hold` column with two values: `needs_pick` (a manual-autonomy segment owns the want; the user picks the release) and `proposed` (a pick is parked as a proposal). Either bars the worker from claiming the want. The values in the table below are unbuilt design. An optional, structured reason explaining why a want isn't actively advancing right now. The bare state ("searching") doesn't distinguish "actively querying indexers" from "waiting out a back-off" from "blocked because every indexer is down" — but the user _and_ the scheduler's resume logic need that difference. The vocabulary is small and fixed (extensible as new holds emerge):

| Annotation         | Meaning                                                                  | Typical state                     | Set by                                                                 |
| ------------------ | ------------------------------------------------------------------------ | --------------------------------- | ---------------------------------------------------------------------- |
| _(none)_           | Actively advancing — searching now, or downloading now                   | any                               | —                                                                      |
| `awaiting_air`     | Episode hasn't aired yet; scheduler won't search                         | pending / searching               | SearchScheduler                                                        |
| `awaiting_release` | Movie not yet released; scheduler won't search                           | pending / searching               | SearchScheduler                                                        |
| `backing_off`      | Searched, nothing acceptable yet; in exponential back-off                | searching                         | SearchScheduler                                                        |
| `blocked`          | Can't proceed — required infrastructure is unhealthy (`blocked_on`: `indexer` / `downloader` / `library`) | searching / grabbed / downloading | SearchScheduler / pipeline, via [connectivity-health](../../patterns/connectivity-health/README.md) |

Plus a few **freshness / progress** fields the pill reads (data, not enum): `last_searched_at`, `search_attempt_count`, `considered_count` (cumulative releases evaluated — Story 4's "47 considered"), `next_attempt_at` (when the next search is due), and for the download phase `progress_pct` + `eta`. Of these, the want carries `next_run_at`, `attempt_count` (consecutive errors, reset on a successful search), and `last_error`; download progress rides the `title_progress` SSE event.

**The pill is a pure render of `(state, annotation, freshness)`.** The [title-status](../title-status/README.md) projection is the built form of this for requester surfaces. "Searching for HD release…", "Searching • last checked 4h ago", "Still searching • 47 considered", "Paused — waiting for indexers", "Downloading 12% • ~7 min" are all the _same_ render function over different status projections — not strings authored per story. Realtime want events ([realtime](../realtime/README.md) `want_updated`) carry the structured want row, not pre-rendered text, so copy stays consistent everywhere.

**Resume logic keys off the annotation, not the state.** A `blocked`(indexer) want resumes immediately on a `<resource>.health` recovery event; a `backing_off` want resumes on its `next_attempt_at` schedule. The scheduler distinguishes them by annotation — they're both `searching`, but they wake on different signals. (This is what [Story 10](../../stories/10-indexer-health-degraded-and-recovery.md) needs and what resolves its open question on distinguishing infra-blocked from backing-off wants.)

**Not a want annotation: media-server propagation.** Once a want is `available`, whether Plex/Jellyfin has indexed the file is a _separate_ per-`(file, server)` status (see [Media-server propagation](#media-server-propagation-decoupled-from-available)), surfaced as its own badge — not a want annotation. The want is done; propagation is downstream.

## Media-server propagation (decoupled from `available`)

**Unbuilt**, except that `available` is already decoupled from any media server. **Arrflix owns the truth about its own library. A media server (Plex, Jellyfin) is a _downstream consumer_ of that library, not the authority on it.** The pipeline separates two orthogonal facts:

| Axis                      | Question                              | Owner                   | Gates the want?                         |
| ------------------------- | ------------------------------------- | ----------------------- | --------------------------------------- |
| **Acquisition state**     | Is the file in my library, verified?  | Arrflix (this pipeline) | **Yes** — this _is_ the want lifecycle  |
| **Media-server propagation** | Has Plex/Jellyfin indexed it yet?  | the media server        | **No** — enrichment only                |

### `available` means _verified on disk_, not _seen by Plex_

The last two transitions of the want lifecycle:

- **`imported`** — ImportWorker has hardlinked the file into the library path and written the `file` row. The file is _placed_.
- **`available`** — Arrflix has **verified the file is present and non-empty on disk** (a stat/size check; the same authority [scan](../scan/README.md)'s verify mode uses). The file is _confirmed_. This is the terminal happy state, and it is reachable with **no media server at all**.

The verify is an inline check in the import worker right after the hardlink, run _before_ the file row commits: a missing or zero-length file fails the import task (retryably) instead of writing rows. `imported` and `available` are then written back to back. The check catches the "hardlink reported success but the file is gone / the disk errored" case, and gives scan-verify a re-confirmation hook.

Why not gate on the media server: a Plex `library.new` webhook gate means a missed webhook (Plex restart mid-scan, network blip) leaves the want stuck in `imported` **forever**, and Jellyfin / no-server users never reach a terminal state at all.

### Propagation is a separate, non-blocking axis

Once a file is `available`, Arrflix nudges each configured media server to index it (Plex partial-refresh, Jellyfin scan) and tracks the result as a **per-`(file, media_server)` propagation record** — server-agnostic, multi-server-ready:

- Populated by **(a)** the server's webhook when it fires (fast path), or **(b)** a best-effort reconciliation poll that queries the server's API (backfill when the webhook is missed).
- The media server is itself a [connectivity-health](../../patterns/connectivity-health/README.md) resource; reconciliation only runs while it's healthy.
- **It never gates the want.** Webhook missed → the want is already `available`; propagation simply stays `pending` until the next backfill catches it.

The full propagation model (record shape, reconciliation cadence, per-server webhook handling, `version_mismatch` health) lives in the [media-server spec](../media-server/README.md). This section pins only the contract: _propagation enriches, it never gates._

### Notifications fire on `available`, links resolve lazily

"Ready to watch" fires the moment the want is `available` — Arrflix's own truth, never blocked on a server. Because Arrflix isn't a player (users watch _in_ Plex/Jellyfin), the notification's deep link resolves like this:

- Propagation already `visible` → embed the server deep link directly.
- Not yet → link to the **Arrflix media detail page**, which shows "syncing to your server…" and flips to "Open in Plex" the instant propagation confirms.

Never a dead link, never a stuck want. (Built: the `want.available` notification is enqueued by the import worker on the `available` transition and links to the Arrflix title page; the deep-link upgrade is V1.1.) (An optional _grace window_ — hold the push up to ~60–120s when a healthy server is configured, so the deep link is usually live on first tap — is a UX nicety the media-server spec may add. The default without it is still correct.)

### Watch-state coupling is different — and legitimate

[Hygiene](../hygiene/README.md#retention--cleanup-the-home-for-request-retention)'s optional watch-based cleanup rule genuinely depends on a media server, because "has this been watched" is only knowable from the player. That coupling is legitimate and stays — but it's a *read* feeding an optional cleanup heuristic, never a gate. It degrades gracefully: with no media server, the watch-based rule simply never fires, and time-based cleanup (which needs no server) is unaffected. Do not confuse it with _availability_ coupling, which this design rules out. (Retention is owned by hygiene as a library-wide policy, not by requests — see [requests → where retention went](../requests/README.md#where-retention-went).)

## What stays the same

- Indexer service (Prowlarr adapter)
- Downloader integrations (qBittorrent, etc.)
- Download_job state machine (`created` → `enqueued` → `downloading` → `completed` / `failed` / `cancelled`; `enqueued → completed` is allowed when the torrent already exists in the client)
- Import_task state machine (`pending` → `in_progress` → `completed` / `failed` / `cancelled`)
- Name templates and their application during import
- Library scan + match
- Hardlink import
- TMDB enrichment for item-level metadata
- The error model and RFC 9457 wire format (see [errors](../../patterns/errors/README.md))

## What gets restructured

- **`DownloadCandidatesService` is the interactive surface; `AcquisitionService` is the autonomous loop.** The two share `RoutingService.Dispatch`, the want spine, and the download/import back half, but not a common orchestration entry point. **Unbuilt:** a single orchestration with an alternate user-pick entry point.
- **`download_job` has `want` linkage** via the M:N `download_job_want`, not a single FK.
- **`import_task` has a `want_id` FK.** One want per imported file.
- **Search cache: in-memory → DB-backed** (unbuilt), longer TTL, with explicit invalidation hooks.
- **Media-server linkage stays off `media_item`.** Plex rating keys (and Jellyfin equivalents) live in a per-`(file, media_server)` propagation record, not a column on `media_item` — server-agnostic and multi-server-ready. Owned by the [media-server spec](../media-server/README.md); see [Media-server propagation](#media-server-propagation-decoupled-from-available).

## What gets added (summary)

Tables (owned by sibling specs unless noted):

- `quality_profile` and friends (`custom_format`, `quality_profile_format`, `quality_size_default`) — see [quality profiles](../quality-profiles/README.md)
- `tier_binding` (tier → profile) — see [quality profiles](../quality-profiles/README.md)
- `tracking`, `tracking_requester` — see [tracking](../tracking/README.md)
- `want`, `want_release_exclusion` — see [tracking](../tracking/README.md)
- `request`, `user_policy` — see [requests](../requests/README.md)
- `download_job_want` (M:N linkage) — **owned here**
- `proposal`, `proposal_want` (the parked `propose`-autonomy grab) — **owned here**
- `push_subscription`, `notification_preference` — see [notifications](../notifications/README.md)
- **Unbuilt:** audit/decision tables (schema per producer; see [audit pattern](../../patterns/audit/README.md)); `indexer_search_cache` (**owned by [indexers](../indexers/README.md)**; acquisition fires its invalidation hooks); optional `episode_release_pattern` for per-series search-timing learning (**owned here**)

Services (owned by sibling specs unless noted):

- `RequestService`, `TrackingService`, `WantService`, `ReconcileService` (series want producer)
- `QualityProfileService`
- `AcquisitionService`, `ProposalService`, `SchedulerService` — **owned here**
- `NotificationService` (with push channel) — see [notifications](../notifications/README.md)
- **Unbuilt:** `MediaServerService` (outbound refresh nudge + inbound webhook/reconciliation poll; Plex first, Jellyfin later) — see the [media-server spec](../media-server/README.md). **Never gates the want lifecycle.**

Workers — **owned here**:

- `AcquisitionWorker` (`jobs/acquisition`); search timing is `SchedulerService` + `internal/cadence`, not a separate worker

Dependency, owned by [metadata](../metadata/README.md):

- Series structure sync (`EnrichmentService.SyncSeriesStructure`, run by the enrichment worker) — without it, smart scheduling is blind to upstream changes.

## Event bus / messaging

**Unbuilt** as described: no `want.*` bus exists. Workers find work by polling their tables (`next_run_at`, `FOR UPDATE SKIP LOCKED`), and the only want event is the `want_updated` SSE delta each transition emits, with a `title_status` re-announce alongside. The pipeline relies on events flowing between components. The [realtime](../realtime/README.md) module handles user-facing server-to-client updates (lossy, server→browser); worker-to-worker signaling is the opposite contract (durable, must-not-drop) and is owned by the [work-dispatch pattern](../../patterns/work-dispatch/README.md) — `LISTEN/NOTIFY` wake-up hints over the claim-queue. The "internal event bus" referenced below is that pattern; the `want.created` / `want.regate_failed` / due-signal wake-ups land there, not on the realtime broker. Concretely:

| Event                | Producer                                     | Consumers                                |
| -------------------- | -------------------------------------------- | ---------------------------------------- |
| `want.created`       | TrackingService (every want has a tracking parent — movies included) | AcquisitionWorker (wake-up), SSE (user)  |
| `want.grabbed`       | AcquisitionWorker                            | SSE, NotificationService                 |
| `want.search_failed` | AcquisitionWorker                            | SearchScheduler (back-off), SSE          |
| `want.imported`      | ImportWorker                                 | SSE                                      |
| `want.regate_failed` | ImportWorker (re-gate hard-fail)             | AcquisitionWorker (blocklist + re-search), SearchScheduler, SSE |
| `want.available`     | ImportWorker (file verified on disk)         | NotificationService, SSE, MediaServerSync |
| `file.propagated` | MediaServerSync (webhook or reconciliation poll) | NotificationService (deep-link upgrade), SSE |
| `tracking.archived`  | TrackingService                              | SSE, NotificationService                 |
| `upgrade.proposed`   | AcquisitionWorker                            | NotificationService                      |

All workers run in one Go process (single container). Durable, must-not-drop wake-ups (`want.created` and friends) use the [work-dispatch pattern](../../patterns/work-dispatch/README.md)'s `LISTEN/NOTIFY`-hint-over-claim-queue — the row is the truth, the notify is a latency hint. Purely ephemeral, co-located hand-offs with no durability need can stay a Go channel. If the deployment ever splits into separate processes, the durable path already routes through Postgres and needs no change; only the ephemeral channels would graduate to a real bus.

## No settings page

Acquisition exposes no admin UI. Every knob in the flow lives in another spec:

| Concern                                  | Home                                                                          |
| ---------------------------------------- | ----------------------------------------------------------------------------- |
| Which releases qualify                   | [Quality profiles](../quality-profiles/README.md)                             |
| Where a grabbed release goes             | [Routing](../routing/README.md)                                               |
| When / how often to search               | [Tracking](../tracking/README.md) (per tracking) + tracking defaults (admin)  |
| Indexer connections + health             | [Indexers](../indexers/README.md)                                             |
| Downloader connections + eligibility     | [Downloaders](../downloaders/README.md)                                       |
| User permissions / auto-approve          | [Users](../users/README.md)                                                   |
| Decision log retention                   | [Audit pattern](../../patterns/audit/README.md)                               |
| Activity / decisions timeline UI         | [Audit pattern](../../patterns/audit/README.md)                               |

If something here grows a knob worth surfacing, that knob lives in the appropriate sibling spec.

## Open questions

1. **Where does the resolved `quality_profile_id` live on a want?** Snapshot at want creation (so the want is stable even if the tier-profile binding changes mid-flight) or look up dynamically? Snapshot is safer; pin in iteration 2. — *Answered by the code: snapshotted onto `want.quality_profile_id` at creation.*
2. **Search cache scope.** Per `(media_item, query)` or finer? Series searches differ from episode searches; we want both to cache. Worth a small data-shape pass before implementing. *Frozen — beyond V1.*
3. **Cancellation semantics.** If a want is canceled while a search is in flight, what happens? The search completes; results are discarded; no download_job is created. If a download_job is in flight, the existing cancel path covers it. **Answered by the code:** `grabSingle` CASes `searching → grabbed` only while still searching, so a canceled want creates no job (`internal/service/acquisition.go:236-240`).
4. **Dedup against concurrent searches.** Two wants for the same media_item triggering simultaneous searches should share. The DB-backed cache + a short advisory lock should handle this, but worth nailing down. *Frozen — beyond V1.*
5. **Synthetic wants for interactive grabs.** When the user manually grabs something with no pre-existing want, we synthesize one to keep the pipeline uniform. Lifecycle of synthetic wants: created in `grabbed` state directly? Or briefly `pending → grabbed`? Probably the former — no point in going through search-evaluation for a release the user has already picked. Since every want has a tracking parent, an interactive grab with no existing tracking synthesizes an admin-anchored single-atom tracking alongside the want; it auto-archives on import unless the user opts into upgrade-watching. — *Answered by the code for the want: a movie manual grab creates (or CASes) the want directly at `grabbed` with a NULL profile. The auto-archive is unbuilt.*
6. **Pre-grab dedup.** If a download_job already covers a want (in progress), the AcquisitionWorker should skip it. How is this enforced — query at the start of every Pick, or rely on a unique constraint that errors and we retry? Query-first is simpler; document it. — *Answered by the code: both — `alreadyCoveredByPack` re-reads first, and the grab is a `searching → grabbed` CAS (packs also find-or-create the job on a partial UNIQUE `(indexer_id, guid)`), so a lost race is a benign no-op.*
7. **Manual override + auto-upgrade interaction.** If a user manually grabs a 720p but the tracking is at 1080p profile, does auto-upgrade activate immediately? Probably yes (the manual grab doesn't override the profile, just the _current_ pick). Worth documenting in the upgrade-behavior section of [tracking](../tracking/README.md). *Frozen — beyond V1.*

## What we're explicitly not deciding here

- Exact table schemas, columns, indexes for the acquisition-owned tables
- API endpoint shapes and request/response formats
- The custom-format DSL / scoring rule grammar (deferred to [quality profiles](../quality-profiles/README.md))
- Routing rule grammar (deferred to [routing](../routing/README.md))
- Audit retention policy (deferred to [audit pattern](../../patterns/audit/README.md))
- Worker concurrency limits, queue depths, back-off curves
- The event bus implementation (Go channel vs library)
- Migration ordering / data backfill plan when renames land
- Media-server propagation mechanics — record shape, reconciliation cadence, per-server webhook/correlation strategy. Decoupled from the want lifecycle here (see [Media-server propagation](#media-server-propagation-decoupled-from-available)); the details live in the [media-server spec](../media-server/README.md).

## Doc neighbors

- [Tracking](../tracking/README.md) — defines the ongoing-intent primitive that produces wants
- [Quality profiles](../quality-profiles/README.md) — defines how a release is gated and scored
- [Routing](../routing/README.md) — defines where a picked release goes
- [Matching](../matching/README.md) — turns imported files into identified content
- [Audit pattern](../../patterns/audit/README.md) — the decision-artifact pattern this pipeline writes into
- [Errors](../../patterns/errors/README.md) — the typed error model used throughout
- [Story 1](../../stories/01-happy-path-auto-approve.md) — concrete user flow that pressure-tests the pipeline end-to-end
