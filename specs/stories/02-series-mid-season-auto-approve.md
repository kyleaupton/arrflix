# Story 2 — Series mid-season: friend requests an in-flight show with auto-approve

**Status:** Draft

A trusted family member opens the PWA, requests a currently-airing series (Severance — two complete seasons in the can, season 3 four episodes deep) with auto-approve, and over the next few hours the back catalog lands on Plex. The remaining unaired episodes of S3 then trickle in over the following weeks as they air. No admin intervention. This story is what makes Arrflix _ongoing_, not just a delivery service — and it's the first one that exercises [tracking](../modules/tracking/README.md) as a runtime artifact.

Follows the template established by [Story 1](./01-happy-path-auto-approve.md): Cast → Preconditions → Flow → Postconditions → What must be true → Out of scope → Open questions.

## Cast

- **Friend** — a non-admin user. Their config:
  - `requests.create:series:hd: true`, `requests.create:series:4k: false`
  - `requests.auto_approve:series:hd: true`
  - PWA installed, push notifications enabled
- **Admin** — passive in this story. Prior config:
  - An HD quality profile exists and is configured as the default for series
  - At least one indexer + one downloader configured and healthy
  - Plex integration connected (URL + token), `library.new` webhook subscribed
  - `TMDBSeriesSyncWorker` and `SearchScheduler` are running (foundation)
- **The system** — Arrflix: web UI, API, background workers
- **External** — TMDB, Prowlarr, qBittorrent, Plex

## Preconditions

- "Severance" is not in any library
- No existing [[tracking]] for the series
- TMDB has metadata for the series and all aired episodes
- Series state per TMDB:
  - **S1** (9 episodes): complete, aired 2022, season pack widely available
  - **S2** (10 episodes): complete, aired 2025, season pack widely available
  - **S3**: `Returning Series`, currently airing weekly Fridays
    - **E01–E04**: aired (individual WEB-DL releases available)
    - **E05–E10**: not yet aired (E05 airs in ~3 days)
- Indexers carry at least one HD release that passes the quality policy for each aired episode (or a season pack covering it)
- Downloader has free slots
- Friend's push subscription is registered on the server

## Flow

### Phase 1 — Discovery (T+0s)

**User-visible:**

- Friend opens PWA, searches "Severance", taps the series result.
- Series focus page loads with TMDB metadata (poster, overview, 3-season picker, season grid showing which episodes have aired vs. upcoming).
- Below the poster: a **"Request" button** with subtitle "Add to library in HD" (reflecting Friend's tier).
- Below the button: **pre-flight** summary — "_23 episodes already aired (S1 × 9, S2 × 10, S3 × 4). 6 more arriving weekly through July._" Storage estimate ~58GB.

**Behind the scenes:**

- TMDB search proxied through the existing TMDB service.
- Focus page calls media service: "do we have this? is it tracked? what's my tier? what's the scope-aware count?"
- API answers: not in library, no tracking, viewer's tier is HD, 23 aired + 6 upcoming.

### Phase 2 — Request (T+1s)

**User-visible:**

- Friend taps "Request". Button morphs into a status pill: **"Subscribing… syncing episode list"** with a spinner.
- Toast confirms: "Request submitted — we'll notify you as episodes arrive."

**Behind the scenes:**

- `POST /requests { tmdb_id, type: "series", tier: "HD", preset: "add_to_library" }`
- Preset resolves to flags: `retention: keep_forever`, `tier_floor: hd`, `tier_ceiling: hd`, `scope: all`, `monitor_future: true`.
- Request service:
  1. Checks `Friend.requests.create:series:hd` → ok
  2. Quota check: well under soft threshold → eligible for auto-approve
  3. Evaluates approval: `requests.auto_approve:series:hd` is true → auto-approve fires
  4. Transactionally writes `request` row: `{ id, requester_id, tmdb_id, type: series, tier_floor: hd, tier_ceiling: hd, retention: keep_forever, scope: all, monitor_future: true, status: approved, auto_approved: true }`
  5. Spawn step: no existing tracking for this series → create new [[tracking]] row: `{ id, media_item_id, scope_rule: all, quality_profile_id: <hd>, upgrade_behavior: propose, schedule_strategy: smart, requesters: [Friend], state: active }`
  6. Updates request: `status: spawned, spawned_tracking_id: <tracking_id>`
  7. Emits `tracking.created` event ([realtime](../modules/realtime/README.md))

**Notifications:** none yet — in-app pill is sufficient.

### Phase 3 — Series sync (T+1s → T+~15s)

**User-visible:**

- Pill updates: **"Syncing episode list (29 episodes)…"** then **"Queueing 23 episodes…"**

**Behind the scenes:**

- `tracking.created` wakes the [TMDBSeriesSyncWorker](../modules/metadata/README.md#series-structure-sync-the-foundation-gap) for an **immediate full sync** (per metadata spec's "tracking activation" trigger).
- Worker fetches series-level + seasons + full episode tree from TMDB (with `external_ids` appended).
- Upserts:
  - 1 `media_item` (or refreshes existing stub)
  - 3 `media_season` rows
  - 29 `media_episode` rows (including the 6 unaired — they exist as records with future `air_date` and no `media_file`)
  - TMDB / TVDB / IMDB external IDs into `media_item_external_id` and `media_episode_external_id`
  - Raw payload into `media_metadata_source`
- On sync complete, **TrackingService** evaluates scope (`rule: all` + no overrides) against the freshly synced episode tree:
  - 29 episodes in scope
  - 23 aired and not present in library → needs wants
  - 6 unaired → no wants yet (smart scheduling will create them when they air)
- **WantService** creates 23 `want` rows, each: `{ tracking_id, media_episode_id, quality_profile_id: <snapshot>, status: pending }`
- Emits 23 `want.created` events.

### Phase 4 — Auto-select & grab (T+~15s → T+~3 min)

**User-visible:**

- Pill aggregates: **"Searching releases… 23 episodes queued"** → **"Grabbed 2 season packs + 4 episodes • Downloading"**
- "My subscriptions" page (new) now lists Severance with a season grid: S1 + S2 each show a single download bar (one job covering many episodes); S3E01–E04 show individual progress; S3E05–E10 show **"Airs Fri"** with greyed cells.

**Behind the scenes:**

- 23 `want.created` events wake the **AcquisitionWorker**. It processes wants with the standard search → gate → score → pick → route flow per [acquisition](../modules/acquisition/README.md). Three distinct outcomes emerge:

  **(a) S1 wants — season pack chosen:**
  - First S1 want triggers an indexer search; in-flight dedup (or short advisory lock) holds the other 8 S1 searches.
  - Quality engine sees `Severance.S01.COMPLETE.1080p.WEB-DL` matches all 9 S1 episodes → wins on the bin-first selection.
  - AcquisitionWorker identifies all 9 in-flight S1 wants → creates **one** `download_job`, links to all 9 wants via `download_job_want` M:N.
  - 9 wants transition `pending → searching → grabbed`.
  - 9 audit rows written (one per want, all referencing the same `download_job` and `search_run`).
  - Emits `want.grabbed` × 9.

  **(b) S2 wants — same pattern:** one season pack, one `download_job`, 10 wants linked, 10 audit rows.

  **(c) S3E01–E04 wants — no season pack yet:**
  - Per-episode searches return individual WEB-DLs.
  - Quality engine picks the best release per episode.
  - 4 `download_job` rows, each linked to a single want.
  - 4 wants transition `pending → searching → grabbed`.

- All quality decisions (including rejected runners-up) persist via the [audit pattern](../patterns/audit/README.md).
- Net: **6 `download_job` rows**, **23 wants in `grabbed` state**, ready for the download worker.

### Phase 5 — Download → Import (T+~3 min → T+~hours)

**User-visible:**

- Pill aggregates downloader progress: **"Downloading 23 episodes • ~2h 40m remaining"** (ETA derived from qBit's aggregate speed/size).
- Per-job progress visible on a drill-down.

**Behind the scenes:**

- DownloadJobWorker hands each torrent to qBittorrent; polls; broadcasts progress via [realtime](../modules/realtime/README.md).
- As each job completes, ImportWorker hardlinks files into the configured series library path via the series name template (likely `Severance/Season 0X/Severance - S0XE0Y - <Title>.<ext>`).
- **Crucial difference from Story 1**: each season-pack download produces **N `import_task` rows, one per file**, and the matcher (see [matching](../modules/matching/README.md)) assigns each file to exactly one want based on parsed `(season, episode)` → `media_episode_external_id`. Each `import_task` carries the resolved `want_id`.
- Per file imported:
  - 1 `media_file` created, linked to its `media_item` + `media_episode`
  - The corresponding want transitions `downloading → imported`, then `imported → available` once VerifyStep confirms it on disk. **Available is Arrflix's own truth — no media server required.**
  - Media-server sync nudges Plex to partial-refresh (debounced — one refresh per batch, not 23), non-blocking.

**Notifications:** the per-season grouped notifications fire as wants reach `available` (Phase 6), gated on verification, not on Plex.

### Phase 6 — Notify + media-server sync (T+~hours, rolling)

**Behind the scenes:**

- Wants reach `available` as each file is verified on disk (Phase 5) — this is what drives the notifications below. None of it waits on Plex.
- Independently and rolling: Plex scans and fires `library.new` webhooks (batched by Plex); the handler records propagation + rating key per `media_file` and emits `media_file.propagated`. Missed webhooks are backfilled by reconciliation. Propagation flips the in-app "Open in Plex" affordance; it does not affect want state.

**Notifications:**

- 📱 **Grouped push** to Friend (debounce window ~10 min): **"Severance: 9 episodes from Season 1 are ready to watch."** Deep link resolves to the Arrflix show page, which opens in Plex once propagation confirms.
- Followed shortly by: **"Severance: 10 episodes from Season 2 are ready."** and **"Severance: 4 episodes from Season 3."**
- In-app: "My subscriptions" → Severance now shows S1 + S2 + S3E01–E04 as **"Available"** (with **"Open in Plex"** appearing per episode as propagation confirms); the show as a whole shows **"Up to date — next episode airs Fri"**.

### Phase 7 — Smart scheduling kicks in for unaired (T+days)

**Behind the scenes:**

- **SearchScheduler** holds wants for S3E05–E10 in a deferred state ("not yet aired → no search"). Per [tracking](../modules/tracking/README.md#smart-scheduling):
  - Until `air_date` passes, no search runs.
  - In the **1–6h post-air window**, searches fire every ~15 min (peak release window).
  - On consecutive empty results within a tier, exponential back-off.
- Friday at air time, S3E05's `air_date` passes. SearchScheduler enters the peak window:
  - First search at +1h: indexer returns nothing yet. Empty audit rows written. Back-off slightly.
  - Search at +2h: a `Severance.S03E05.1080p.WEB-DL` release surfaces. Quality picks it; AcquisitionWorker creates a single-want `download_job`.
  - Want flows through the same `grabbed → downloading → imported → available` pipeline.

**User-visible:**

- Friend gets a push: **"Severance S03E05 'Cold Harbor' is ready to watch."**
- The greyed S3E05 cell in the subscriptions view fills in.
- Repeats weekly for E06–E10.

### Phase 8 — Eventual auto-archive (out of scope, but flagged)

- When TMDB eventually marks Severance `Ended` **and** all 29 in-scope episodes are acquired at-cutoff, tracking auto-transitions `active → archived` per the tracking spec. A `tracking.archived` notification fires.
- Not exercised in this story since S3 is still mid-air. Covered by a future variant story.

## Postconditions

- **1 `request` row** — `status: spawned`, `auto_approved: true`, references `tracking_id`
- **1 `tracking` row** — `state: active`, `scope: all`, `requesters: [Friend]`, `schedule_strategy: smart`
- **29 `media_episode` rows** — 23 with associated `media_file`, 6 awaiting air
- **23 `want` rows** initially, ultimately ending at `available` as Plex confirms each. Future S3E05–E10 wants are created lazily by SearchScheduler at air time, not up-front.
- **6+ `download_job` rows** — 2 season packs (covering 19 wants via M:N) + 4 single-episode jobs in this phase; +1 per future episode as they air
- **23 `media_file` rows** in the configured series library, hardlinked from the downloader's complete dir
- **One decision_log row per release considered**, per (want, search_run) — large for season-pack searches, since the pack covers many wants
- **Plex** has the show; library section reflects 23 episodes
- **Friend's "my subscriptions" page** shows Severance as an active subscription with episode grid

## What must be true (foundation requirements)

Story 1 covered the per-want pipeline. Story 2 adds the **persistent intent** and **multi-want orchestration** layers. Net new requirements beyond Story 1:

### Data primitives

- **`tracking` entity** per [tracking](../modules/tracking/README.md): `{ id, media_item_id, scope_rule, scope_overrides, quality_profile_id, upgrade_behavior, schedule_strategy, requesters, state }` plus lifecycle timestamps + last-transition reason.
- **`media_season` and `media_episode` rows** for the full episode tree, **including unaired** episodes (the foundation gap called out in [metadata](../modules/metadata/README.md#series-structure-sync-the-foundation-gap)).
- **`media_episode_external_id`** registry — TMDB canonical, TVDB populated for indexer searches.
- **`download_job_want`** M:N intermediate — one season-pack job covers many wants.
- **`import_task.want_id`** FK — each imported file fulfills exactly one want.
- **Per-want quality_profile snapshot** — the want freezes the profile ID at creation so mid-flight profile edits don't change in-flight decisions ([acquisition open question #1](../modules/acquisition/README.md#open-questions)).

### Services / workers

- **[TMDBSeriesSyncWorker](../modules/metadata/README.md#series-structure-sync-the-foundation-gap)** — pulls full season/episode tree on tracking activation, then on cadence. Required before tracking can produce wants for a newly tracked series.
- **TrackingService** — owns scope evaluation; produces wants for in-scope episodes that lack at-cutoff files.
- **WantService** — creates wants in batch when tracking spawns; carries `tracking_id` back-reference.
- **[SearchScheduler](../modules/acquisition/README.md#searchscheduler-new)** — wakes wants per the time-since-air bias; respects air dates for unaired episodes; back-off on empty results.
- **AcquisitionWorker (Story 1) extended** — must handle the season-pack case: identify all in-flight wants a chosen release fulfills, link via `download_job_want`, write per-want audit rows.
- **ImportWorker (Story 1) extended** — per-file `want_id` assignment via matcher; debounced Plex refresh per library section, not per file.

### UI surfaces

- **Series focus page** — season grid showing aired vs. upcoming; pre-flight summary on the Request button ("23 aired + 6 upcoming, ~58GB").
- **"My subscriptions" page** — replaces / extends "my requests" for ongoing tracking artifacts. Lists tracked series with per-episode status grid (available / downloading / scheduled / awaiting-air).
- **Subscription detail** — per-episode drill-down with audit log access ("why hasn't S03E05 grabbed yet?").

### Realtime / messaging

- Per-tracking aggregate progress events ("23 wants → 19 grabbed / 4 searching") so the UI doesn't have to compose from N individual want events.
- The internal event bus carries the new events: `tracking.created`, `tracking.archived`, and the per-want events from Story 1.

### Notifications

- **Grouped delivery** for the back-catalog flood — one notification per (subscription, season) rather than 23 individual pushes. Lives in [notifications](../modules/notifications/README.md); the producer (here: AcquisitionWorker or PlexIntegrationService) batches by `(tracking_id, season_number)` within a debounce window.
- **Per-episode delivery** for newly-aired episodes (the trickle case in Phase 7) — those are individually noteworthy and Friend wants them.

### Time targets (UX commitments)

- Click → "Subscribing…" toast: <1s
- Request → tracking + first sync trigger: <1s
- Series sync (cold) → wants spawned: <30s for a typical series; <2 min worst-case for very long-running shows
- First grab attempt on a freshly aired episode: within the 1–6h peak window, with first search at +1h
- ETA honesty: never claim "available" until the file is verified on disk (independent of Plex); aggregate ETA derived only from `downloading` wants

## Out of scope (variant stories)

- **Tier mismatch / 4K gating** — Friend can only request HD. Story 3 (pending) covers the 4K request path.
- **Manual approval queue** — Friend has `auto_approve`. Manual approval flow is its own story.
- **Scope ≠ `all`** — Friend uses the "Add to library" preset (everything). Variant stories: `latest_season_plus_future`, `pilot`, per-episode overrides.
- **Multi-requester** — Bob requests Severance mid-acquisition while Friend's tracking is in flight. Owned by tracking's [multi-requester semantics](../modules/tracking/README.md#multi-requester-semantics); deserves its own story (the proposed Story 07).
- **Already partially in library** — Severance S1 already imported (e.g., from a different request or manual drop-in) when Friend subscribes. Scope evaluation finds existing files and skips those wants.
- **Failed grab for some episodes** — pack found for S1, but S2 has no acceptable release. Wants stay `searching`; back-off kicks in; admin sees nothing landed for that subset. Needs the "couldn't find" notification UX (proposed Story 04).
- **Upgrade flow** — Friend has S1 in 720p, a 1080p pack becomes available, `upgrade_behavior: propose` fires. Story 06 in the proposed sequence.
- **Cancellation mid-flight** — Friend cancels the request; tracking moves to `paused` (sole requester left); in-flight downloads cancel.
- **Auto-archive** — series ends upstream + all wanted episodes acquired → `tracking.archived` notification. Not triggered here.
- **`active → archived → active` revival** — Severance gets renewed for S4 a year later; TMDB sync detects new episodes; tracking re-activates with a notification.
- **Indexer health degraded mid-flight** — connectivity-health gating halts SearchScheduler; recovery resumes work. Proposed Story 10.

## Open questions

1. **Pre-flight scope visibility.** The Request button claims "23 aired + 6 upcoming, ~58GB". This needs a fast scope-aware count: either we sync metadata at focus-page-view time (slow), or we pre-sync popular series proactively (storage cost), or we show a coarser "an entire show" warning and let post-spawn UI refine it. Pick before the focus page is built.

2. **Notification grouping window.** Phase 6 batches per `(tracking_id, season_number)` within a debounce window. What's the right window — 10 min? 1 hour? Per-user-config? And what if S1 finishes at 3am and S2 finishes at 4am — does Friend wake up to two pushes or one merged? Lean: 10-min debounce, per-season grouping, but pin in the notifications spec.

3. **Aggregate progress event shape.** The UI needs `23 wants → 19 grabbed / 4 searching`. Is that a per-tracking aggregate event the [realtime](../modules/realtime/README.md) broker computes and emits, or does the client compose from 23 individual events? Aggregate is friendlier (less chatty) but more producer work. Lean: produce a `tracking.progress` event on transitions; client renders from that.

4. **Wants for unaired episodes — eager or lazy?** Option A: create 29 wants up front, with status `awaiting_air` for the unaired 6. Option B: create only the 23 aired wants now and let SearchScheduler create unaired wants at air time. Option A is simpler to reason about; Option B keeps the `want` table clean and matches "wants are real work items." Lean: B, but pin.

5. **Subscription page identity.** Story 1's "my requests" page becomes confused when requests spawn tracking. Is it now "my subscriptions"? "Requests" for movies + one-shot + "Subscriptions" for series? Or a single unified "My library activity"? UX call.

6. **Pack-overflow for misaligned counts.** What if `Severance.S01.COMPLETE.1080p.WEB-DL` contains 10 files because the release group included an extras disk, but TMDB shows 9 episodes? Per [acquisition](../modules/acquisition/README.md#overflow--under-coverage) the extras flow to unmatched_files (or "extras") — but the user-facing message ("9 of 10 files imported; 1 unmatched") needs UX.

7. **Pack-undercoverage during in-flight pack.** S3 mid-air: a `Severance.S03.COMPLETE` pack surfaces after E04 but before E05 airs — covering 4 episodes that are already grabbed individually. Does the system prefer the pack (re-grab + replace) or skip (cheaper)? Lean: skip if every covered want is already at-cutoff; upgrade only if the pack quality strictly beats current files via the upgrade path.

8. **SearchScheduler visibility for the user.** "We'll search for S03E05 starting 1 hour after Friday's air time" is helpful UX. Where does that surface — on the subscription card, the episode cell, both? And do we let users force-search early?

9. **Mid-flight scope change.** Friend changes the preset to "Just this season" 5 minutes after submitting, while S1 + S2 packs are still downloading. Do those downloads cancel (out-of-scope now) or complete (already paid the bandwidth cost)? Lean: let in-flight downloads complete, cancel `searching` wants, narrow future scope. Pin in tracking.

10. **First-import refresh latency — resolved by decoupling.** Wants reach `available` on disk-verify, so a slow Plex scan no longer holds them in `imported`. The "waiting on Plex" condition becomes a per-file _propagation_ badge ("syncing to your server…") layered on an already-`available` want — a secondary signal, not a want state. The open part is purely cosmetic: how prominently to surface propagation-pending in the grid. (See [acquisition → Media-server propagation](../modules/acquisition/README.md#media-server-propagation-decoupled-from-available).)
