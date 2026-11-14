---
status: v1
---

# Tracking — the ongoing-intent primitive

> **Beyond what's built:** the lifecycle's auto-transitions (auto-archive, `archived → active`) and manual pause/resume, `upgrade_behavior` (stored, always `none`, inert), the `fixed` schedule strategy as a user choice, and §Smart scheduling's refinements (per-series learning, indexer-health gating, exponential back-off) describe unbuilt design. Series stand-down (REQ-SERIES-004) is deferred with upgrades; V1's slice is defined in [V1.md](../../V1.md#basic-series-tracking--series-mid-season).

This doc defines **tracking**: how Arrflix models the ongoing intent to keep a series current. It captures _what tracking is_, _why it's its own primitive_, _what it owns_, and _how it interacts with the rest of the system_. Table and column names appear where they're built (`tracking`, `tracking_requester`, `want`); wire formats are out of scope.

Tracking has two axes. **Scope** answers *which episodes*; **[acquisition autonomy](#acquisition-autonomy--who-picks-the-release)** (`auto` / `propose` / `manual`, set per work segment — **backfill** vs **ongoing**) answers *who picks the release*. Full automation isn't the only posture: the manual-control admin who hand-picks backfill releases but wants new episodes handled automatically has a home in the model (Sonarr's answer, the unmonitored flag, makes those episodes *invisible*, which is worse). `upgrade_behavior` is the same trichotomy applied to upgrades.

## TL;DR

- Three distinct primitives — **request**, **tracking**, **want** — with separate lifecycles and responsibilities.
- **Tracking** is the **universal ongoing-intent primitive** — one record per requested media item, series _or_ movie — holding the ongoing config (scope, quality, upgrade behavior, schedule). It is the _producer_ of wants. A movie is the degenerate case: a single-atom tracking that produces one want.
- **Wants** are the leaf-level work items (one per movie file, one per episode file). They flow through the pipeline defined in [Story 1](../../stories/01-happy-path-auto-approve.md). **Invariant: every want has exactly one tracking parent.**
- **Requests** are the user-facing approval layer. Every approved request spawns (or joins) exactly one tracking (single-atom for a movie, scoped for a series), which produces the wants. Operators add content through the same request path (auto-approved); a manual grab is the one path that creates a tracking without a request.
- Multi-user safe: one tracking record per media item, requesters union their scopes, scope narrows on departure only if no one else needs it.
- Smart scheduling is the differentiator — search frequency is biased by time-since-air (movies anchor the same curve on `release_date`), not a fixed RSS interval.
- **Tracking means "the system pays attention" — not "the system downloads."** Structure sync, the missing-episode ledger, and change notifications are the floor; whether the system also *grabs* is a per-tracking dial — **acquisition autonomy**: `auto` / `propose` / `manual`, set separately for **backfill** (atoms aired before the tracking existed) and **ongoing** (atoms airing after). `propose` — the system searches and picks but holds for one-tap approval — is the trust ladder's middle rung, the mode Sonarr/Radarr never had.
- Auto-archives when the intent is complete — a series `Ended` upstream with all wanted episodes acquired, or a movie's one want acquired at-cutoff — and nothing is watching for upgrades. **Unbuilt:** nothing sets `archived` today.

## Why a separate primitive

Sonarr models monitoring as flags on rows: `series.monitored`, `season.monitored`, `episode.monitored`. It works, but it has well-known limitations:

1. **Provenance is lost.** Once a flag is set, you can't tell _why_ something is monitored or _who_ asked for it. Was it admin-initiated? A user request? An automatic rule? Sonarr doesn't know.
2. **"Series intent" and "file intent" are conflated.** "I want this series" (ongoing, indefinite) and "I need this episode file" (concrete, one-shot) have very different lifecycles. Flagging them the same way works but obscures the difference.
3. **Multi-user is awkward.** If two people both want a series, with different scope preferences, the flag model can't represent that.
4. **Completion is manual.** When a series ends and you've acquired everything, you have to manually unmonitor. Sonarr keeps polling forever otherwise.

Splitting into three primitives — request, tracking, want — gives each concern a clear home:

| Primitive    | Concept                                                       | Lifecycle                                                                                     |
| ------------ | ------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| **Request**  | "a user asked for this; gated by approval"                    | `pending → approved → spawned` _or_ `pending → denied / canceled`                           |
| **Tracking** | "keep this media item current per these rules (a movie is a single atom)"            | `active / paused / archived / canceled`                                                       |
| **Want**     | "acquire this specific atom" (one movie file, or one episode) | `pending → searching → grabbed → downloading → imported → available` (+ `failed`, `canceled`) |

For movies: **request → tracking (single-atom) → one want**.
For series: **request → tracking → wants per episode**.
A manual grab skips the request layer but still creates (or reuses) the movie's tracking, with no requester row.

## What tracking owns

A tracking record encodes the following concerns:

1. **What is being tracked** — a reference to the media item (series or movie). See [Movies under this model](#movies-under-this-model) for the single-atom case.
2. **Scope** — which episodes count as "wanted." See [Scope: rule + overrides](#scope-rule--overrides).
3. **Quality profile** — which profile applies. See [the quality system](#interactions) for how this resolves to actual releases.
4. **Acquisition autonomy** — who picks the release for each want: `auto`, `propose`, or `manual`, one value per segment (backfill / ongoing). See [Acquisition autonomy](#acquisition-autonomy--who-picks-the-release).
5. **Upgrade behavior** — `auto` (replace existing files when a better release appears), `propose` (queue an upgrade as a [notification](../notifications/README.md) for one-tap approval), or `none`. The same trichotomy as acquisition autonomy, applied to the third class of want-work — see [Relationship to upgrade behavior](#relationship-to-upgrade-behavior). **Unbuilt:** the column exists and is always `none`; nothing reads it.
6. **Schedule strategy** — `smart` (time-since-air bias, default) or `fixed` (poll every N minutes regardless). Every tracking is born `smart`; `fixed` is a 6h placeholder with no API to select it.
7. **Requesters** — the set of [`tracking_requester` association rows](#multi-requester-semantics), one per user, each carrying that user's live per-requester intent (scope + per-episode overrides, tier). Drives both lifecycle (zero rows → paused) and effective scope (union across rows). Retention is not per-requester intent — it's a library-wide [hygiene](../hygiene/README.md) policy — and "whole series" scope already includes future episodes, so there's no `monitor_future` flag.
8. **State** — `active / paused / archived / canceled`, plus a reason + timestamp on the last transition for auditability. **Unbuilt:** the reason and transition timestamp (only `updated_at` exists).

## Scope: rule + overrides

Scope answers "which episodes does this tracking want?" The model is **one rule + per-episode overrides**. (For a movie tracking, scope is the implicit single atom — the rules below are series-only and a no-op for movies.)

**Preset rules** (cover ~95% of cases):

| Rule                        | Meaning                                           |
| --------------------------- | ------------------------------------------------- |
| `all`                       | Every episode that exists or ever will exist      |
| `future_only`               | Episodes airing after the tracking was created    |
| `season(N)`                 | Only season N                                     |
| `pilot`                     | Only the first episode                            |
| `latest_season_plus_future` | The most recently aired season + everything after |

**Per-episode overrides** handle the long tail ("I want all of S1 and S3+, but not S2"):

- Explicit include — force-include an episode the rule would exclude
- Explicit exclude — force-exclude an episode the rule would include

**Composition:** an episode is wanted iff `rule says yes` _and not_ explicitly excluded, _or_ explicitly included. Rule + overrides is universal — even `rule: all` + a long exclude list expresses anything.

Scope and its overrides are **per-requester** — they live on each requester's [`tracking_requester` row](#multi-requester-semantics) (`scope_rule`, `scope_season`, `scope_overrides`), not globally on the tracking. Overrides key on the internal `media_episode` UUID. The pure resolver is `internal/scope`. All five presets resolve there, but a request can only seed `all` or `future_only` (see [requests](../requests/README.md#scope-series-only)), and no API edits a requester's scope or overrides after spawn. The tracking's effective scope is the union across requesters. This is what lets a departure correctly narrow scope, and lets per-episode notification audiences resolve to "the requesters whose scope covers this episode."

We deliberately avoid a full rules DSL. Five presets + overrides is enough; richer rules can be added later as new presets if real demand emerges.

## Acquisition autonomy — who picks the release

Scope decides *which* atoms are wanted. **Autonomy decides who picks the release that fulfills each want.** Auto-searching and auto-grabbing every want would bake in an assumption the product shouldn't make: that every user trusts the system's taste on day one. Tracking's floor is *attention* (structure sync, the have/lack ledger, "a new episode exists" notifications); grabbing is a dial on top of that attention. An admin who hand-picks every release is still fully a tracking user — they're at the bottom of the dial.

| Behavior  | Meaning                                                                                                                                                                                    |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `auto`    | The system searches on the schedule, gates / scores / picks, and grabs.                                                                                                                     |
| `propose` | The system searches and picks exactly as `auto` does, but stops before the grab: the winner is held as a **proposal** for one-tap approval. Approve → grab; decline → keep looking.          |
| `manual`  | The system creates and tracks the wants but never searches for them. They surface as "needs your pick"; [interactive search](../acquisition/README.md) is one tap away.                     |

**Heuristic:** `auto` is "I trust the system," `propose` is "show me your work first," `manual` is "just tell me what I'm missing."

### Two segments, two dials

A tracking carries one autonomy value per **segment** (`autonomy_backfill`, `autonomy_ongoing`; each want's `segment` is stamped at creation). An atom is **backfill** if its air date (episode) or release date (movie) precedes the tracking's creation; otherwise it is **ongoing**. The classification is a pure, immutable function of `(air_date, tracking.created_at)` — an atom never migrates between segments. Atoms with no known air date are ongoing (they'll air later or never).

The split isn't cosmetic — the trust economics genuinely differ:

- **Backfill** is a batch decision: season packs, tens of GB at once, quality trade-offs across a whole back-catalog. Exactly where a human plausibly wants the wheel.
- **Ongoing** is a low-stakes drip — one episode at a time, a bounded mistake — where automation shines.

The canonical mixed config (`backfill: manual, ongoing: auto`) is "I'll curate the back-catalog myself; keep me current automatically" — a posture Sonarr cannot express at all. Note that "skip the backfill entirely" is **not** an autonomy value — it's scope (`future_only`). The axes stay orthogonal in the model; the add-a-series UI may present them side by side ("How much? / Backfill: how? / New episodes: how?").

### Propose mechanics

`propose` is `auto` minus the grab. The [SearchScheduler](../acquisition/README.md#searchscheduler-new) drives it on the same smart cadence, and the full gate / score / pick runs unchanged — the divergence is one step: instead of creating a download_job, the AcquisitionWorker writes a **proposal** (`ProposalService`) and holds the want (`want.hold = 'proposed'`).

- **A proposal is per pick, not per want.** A season-pack pick proposes once, listing the N wants it covers; approving grabs once for all of them (the existing `download_job ↔ want` M:N carries it from there).
- **Approve** → the grab proceeds through the identical path `auto` would have taken. The decision log records the disposition as an approved proposal (with the approving user), not a `manual_override` — the *system* picked; the human ratified.
- **Decline** → the want is re-armed to `pending` with the declined release excluded from future picks (`want_release_exclusion`, reason `declined`). This is the same per-want release-exclusion mechanism failed-download recovery needs; one blocklist serves both.
- **Supersede** → a later search that finds a strictly better candidate *updates* the open proposal (one live proposal per want-set, not a stack). Delivery rides [notifications](../notifications/README.md)' resolvable lifecycle — an approved, declined, or superseded proposal resolves rather than lingering in the bell. **Unbuilt:** notification delivery; proposals surface through the title page and realtime updates only.
- **Staleness** → a release can die between proposal and approval (seeders gone, indexer pruned). Approval re-validates the release; if it's no longer grabbable, the system re-searches and re-proposes rather than failing the approval. **Unbuilt:** approving a proposal whose wants are no longer grabbable is a 409; no release re-validation or re-search happens.

`propose` is the **trust ladder**: users start at `manual`, watch the system's proposals match what they'd have picked (the decision log is the receipts), and graduate to `auto` per tracking. It is also the mode the *arr ecosystem never built — its automation is all-or-nothing, which is precisely why cautious users never turn it on.

### Manual mechanics

`manual` is **not** Sonarr's unmonitored flag, and the difference is the point. Unmonitored episodes are invisible — no ledger, no status, no reminder. A `manual` want is a first-class work item (held with `want.hold = 'needs_pick'`, which bars the worker from claiming it): it appears in the have/lack ledger, renders a status pill, and collects in a "needs your pick" surface. The user's pick flows the [interactive path](../acquisition/README.md#interactive-vs-autonomous-shared-components-divergence) — routed, imported, re-gated identically to an auto grab.

`manual` is also not `paused`: a paused tracking stops producing wants; a manual tracking keeps its ledger current and keeps producing wants — it just never searches. Manual wants are unscheduled (no back-off state, no search counters) and never auto-transition. They block auto-archive like any other unacquired want: completion means acquired, however picked.

### Who sets it

Autonomy is **tracking-level, operator-owned** — not per-requester. Requesters never see it: the request keeps its two knobs (tier + scope, per the [requests spec](../requests/README.md#what-the-requester-chooses)), and proposals route to users holding the operator/grab permission, never to requesters. Setting autonomy and approving or declining proposals are gated on `jobs.manage`. A per-tracking value is settable at add time (on an auto-approved request that creates the tracking) or any time after (`POST /api/v1/tracking/{id}/autonomy`); a manually approved request spawns `auto` / `auto`. **Unbuilt:** admin-configured tracking defaults. A requester joining an existing tracking never changes its autonomy.

### Relationship to upgrade behavior

There are three classes of want-work — **backfill**, **ongoing**, and **upgrades** — and one trichotomy across them. `upgrade_behavior` (`auto` / `propose` / `none`) is the same dial applied to the third class; its third value differs (`none`, not `manual`) because an upgrade has no standing want until the system goes looking — "manually hunting upgrades" is just interactive search, which always exists. Whether to unify the vocabulary is an [open question](#open-questions).

### Ownership

Tracking **declares** autonomy, exactly as it declares its schedule strategy; the [acquisition](../acquisition/README.md) worker **implements** it (grab vs propose-and-hold vs skip-scheduling). The proposal's delivery, one-tap resolution, and supersede lifecycle ride [notifications](../notifications/README.md). The want's held status while a proposal is open is an acquisition-owned annotation (the [status axes](../acquisition/README.md#want-status-the-two-axes) already model "not advancing, here's why").

## Lifecycle

```
                ┌──────────┐  user pauses / last requester leaves
                │  active  │ ─────────────────────────────────────┐
       ┌───────►│          │◄─────┐                              │
       │        └─────┬────┘ user │                              ▼
   new │              │ resumes   │                        ┌──────────┐
episode│       all    │           └──────────────────────  │  paused  │
upstream       wanted │ episodes acquired                  └─────┬────┘
       │       AND series.status=Ended                           │ user
       │              ▼                                          │ cancels
       │        ┌──────────┐                                     ▼
       └────────│ archived │                              ┌──────────┐
                └─────┬────┘                              │ canceled │
                      │ user cancels                      └──────────┘
                      └─────────────────────────────────────► (terminal)
```

**State semantics:**

- **`active`** — searches run, wants are generated as episodes are wanted but not yet acquired.
- **`paused`** — no new searches, no new wants generated. Existing in-flight wants continue (they belong to the want lifecycle, not tracking's). User can resume.
- **`archived`** — (unbuilt: never set) completion criteria met (TMDB marks series `Ended` _and_ all wanted episodes are at-cutoff quality). No further action expected, but state is retained for queries ("show me everything I'm tracking") and for the `archived → active` re-activation path if a new episode appears upstream.
- **`canceled`** — terminal. User explicit cancel (`POST /api/v1/tracking/{id}/cancel`, gated `tracking.cancel.own` / `.any`). In-flight wants are also canceled. A later re-request or `retry` re-arms the wants and returns the tracking to `active`, so in practice it isn't terminal.

**Auto-transitions** (unbuilt except the pause on last-requester departure):

- `active → archived` when the tracking is **complete** AND not watching for upgrades:
  - _Series:_ TMDB marks the series `Ended` AND all in-scope episodes are acquired at or above cutoff.
  - _Movie:_ the single want is acquired at or above cutoff (a movie is inherently "ended").
  - In both cases, a tracking with `upgrade_behavior != none` stays `active` (running low-frequency upgrade searches) rather than archiving — archive means "done and not looking for better."
- `archived → active` when TMDB sync detects a new in-scope episode (series), or — rare — a movie tracking is reactivated for upgrade watching.
- `active → paused` when the last requester unsubscribes from non-admin tracking. Admin tracking does not auto-pause. (Built: withdrawing the last requester's request cancels the non-terminal wants and pauses the tracking.)

**Manual transitions:** any user with appropriate permissions can pause / resume / cancel. **Unbuilt:** pause and resume; cancel and `retry` (re-drive failed/canceled wants, back to `active`) are the built manual actions.

## Multi-requester semantics

When two users both want the same media item (series _or_ movie), we use one tracking record, not two (`UNIQUE (media_item_id)` on `tracking`). The tracking owns multi-requester state for both types, uniformly.

- **Requesters set** — represented as **`tracking_requester` association rows**, one per `(tracking, user)`. Each row holds that requester's _live, mutable_ per-requester intent: their scope (+ per-episode overrides) and tier. An operator's own request gives them a `tracking_requester` row like anyone else's; a manual-grab tracking has none.
- **`tracking_requester` is the source of truth for per-requester intent; the [request](../requests/README.md) is the frozen origin.** At spawn, the requester's `tracking_requester` row is _seeded from_ their request's intent flags. Thereafter the request is immutable (audit history — "what Alice originally asked for"), and the association row is what the user edits when they change their mind ("what Alice wants now"). The split is deliberate: it keeps requests clean audit artifacts while giving per-requester intent a live home.
- **Effective scope** — the union of every `tracking_requester` row's scope. The reconciler computes it live from the association rows on each pass; nothing is cached on the tracking (`tracking.scope` is an inert `'self'` placeholder). The association rows are authoritative. (This resolves [open question #1](#open-questions).)
- **Joining** — a user requests a media item that's already tracked → a new `tracking_requester` row is inserted (seeded from their request); the effective scope is recomputed and widens to include their scope.
- **Leaving** — a user cancels → their `tracking_requester` row is removed; the effective scope is recomputed from the _surviving_ association rows and narrows accordingly (no narrowing if their scope was already covered by others), with the reconciler canceling wants that fell out of scope. Because recompute reads live association rows — not frozen requests — it stays correct even after other requesters have edited their scope. **Unbuilt:** the cancel path doesn't trigger a reconcile, so narrowing waits for the next one.
- **Zero requesters** — tracking moves to `paused` (not `canceled`, so admins can revive it). For purely admin-initiated tracking with no human requester, this only happens if the admin explicitly removes themselves; otherwise tracking is admin-anchored.

This avoids:

- Redundant searches (two tracking records for the same series)
- The "Alice canceled and now nobody is monitoring this" footgun
- The inverse: "Bob canceled but Alice still wants it" silently dropping Alice's data

## Smart scheduling

Sonarr's `rss_sync_interval` polls every X minutes regardless of when episodes air. We can do better: bias search frequency by **time-since-air**. The curve lives in the pure `internal/cadence` package; `SchedulerService` resolves the anchor and strategy and feeds it.

Default schedule strategy (`smart`), per in-scope episode:

| Time since air     | Search frequency                             | Rationale                                   |
| ------------------ | -------------------------------------------- | ------------------------------------------- |
| Not yet aired      | None                                         | Nothing to find                             |
| 0–1h after air     | Low (~hourly)                                | Releases rarely up yet                      |
| 1–6h after air     | High (every ~15 min)                         | Peak release window for most release groups |
| 6–24h after air    | Medium (~hourly)                             | Late drops                                  |
| 1–7d after air     | Low (~6 hours)                               | Catch-up window                             |
| >7d after air      | Very low (~daily), then exponential back-off (unbuilt: flat daily) | Cold case                   |
| Acquired at cutoff | None                                         | Done                                        |

Air and release dates are day-precision, so the sub-day tiers only apply to wants anchored on their creation time (the fallback for an undated episode or movie).

Refinements that fall out naturally (unbuilt except the movie anchor):

- **Per-series learning** — record observed "release lag" per series; bias the peak window accordingly. A show that always lands 3h after air gets searched at 3h, not blindly at 1–6h.
- **Indexer health gating** — don't bother searching while all relevant indexers are unhealthy; resume on recovery.
- **Failure back-off** — on consecutive empty searches for the same episode, exponential back-off within each frequency tier.
- **Movie anchor** — a movie tracking has no air date; it anchors the same curve on the movie's `release_date` (or the want's creation time if no release date is known), so a just-released movie gets the same peak-window urgency as a just-aired episode. [Story 4](../../stories/04-failed-search-recovery.md)'s recovery flow rides on this.

Tracking _declares_ its schedule strategy. The search scheduler _implements_ it. Tracking doesn't run searches; it's just the configuration root.

The schedule drives `auto` and `propose` wants identically — `propose` searches on the same cadence and stops one step later, at the pick. `manual` wants are not scheduled at all (see [Acquisition autonomy](#acquisition-autonomy--who-picks-the-release)).

`fixed` strategy exists for users who want predictable behavior (or for debugging). It does what Sonarr does: poll every N minutes regardless. (N is a fixed 6h; see [What tracking owns](#what-tracking-owns).)

## Movies under this model

**Every movie gets a tracking record too** — it is just the degenerate, single-atom case. A movie request spawns a tracking whose scope is the one movie; that tracking produces exactly one want, which flows through [Story 1](../../stories/01-happy-path-auto-approve.md)'s pipeline to `available`. If the movie is found at-cutoff immediately and nothing is watching for upgrades, the tracking auto-archives right away.

Why universal rather than the old "movie → want directly" short-circuit:

- **Scheduling has one home.** A just-released or not-yet-available movie needs search retries and back-off ([Story 4](../../stories/04-failed-search-recovery.md)). That schedule lives on the tracking (anchored on `release_date`), exactly like an episode's lives on its tracking. No separate "non-tracking want" retry path.
- **Upgrade watching is free.** "I have it in HD, grab 4K when it appears" is just `upgrade_behavior: propose` on the movie's tracking — not a special record conditionally spawned after the fact.
- **Pre-release monitoring is free.** A movie with no release yet is simply an `active` tracking whose want is in long back-off until something surfaces.
- **Multi-requester is uniform.** Two people wanting the same movie join the same tracking's requester set, exactly as for a series — the tracking, not the want, is the dedup boundary.
- **Autonomy is uniform.** The backfill/ongoing classification is the same date comparison: an already-released movie is a backfill atom (its one want obeys the backfill dial); an unreleased movie's want is ongoing. "Propose me a release when this movie drops" is the same mechanism as "propose me tonight's episode."

The cost is one tracking row per movie, including one-off manual grabs (which create a requester-less tracking whose want is born `grabbed`; auto-archive on import is unbuilt). That is a deliberate trade: one cheap row in exchange for a single code path and a clean invariant — **every want has exactly one tracking parent.**

## What tracking does NOT own

To keep scope tight, these adjacent concerns live elsewhere:

- **Search execution** — the search scheduler reads tracking config and runs searches. Tracking itself runs no code. The want's runtime status (state × annotation) is likewise an acquisition concern — see [Want status](../acquisition/README.md#want-status-the-two-axes).
- **Proposal orchestration** — `propose` mode's search-pick-hold flow runs in the [acquisition](../acquisition/README.md) worker (tracking only declares the behavior); proposal delivery, one-tap resolution, and supersede lifecycle belong to [notifications](../notifications/README.md).
- **Quality scoring** — defined by [quality profiles](../quality-profiles/README.md), not tracking. Tracking just references a profile by ID.
- **Decision log** — every accept/reject the [acquisition](../acquisition/README.md) pipeline makes is logged via the [decision-artifact pattern](../../patterns/audit/README.md). Tracking is _not_ the home for that data, though tracking-generated wants link to it.
- **Notification routing** — when a tracking event matters to the user (new episode imported, upgrade available, tracking auto-archived), [notifications](../notifications/README.md) decides how to deliver it. Tracking emits the event; it doesn't route.
- **Exact data shapes** — column types, indexes, foreign keys, API contracts. Deliberately deferred to a later iteration.

## Interactions

| Neighbor                                                          | How tracking interacts                                                                                                                                   |
| ----------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **TMDB series sync**                                              | Must know every episode that exists, with air dates, to drive scope evaluation and scheduling. A series spawn syncs the season/episode structure first; the reconciler only creates wants for dated episodes. |
| **Wants**                                                         | Tracking _produces_ wants when an in-scope episode lacks a file (at-cutoff upgrades are unbuilt). The relationship is 1:N. Each want carries a back-reference to its tracking. |
| **[Acquisition](../acquisition/README.md)**                       | Reads the tracking's autonomy for each want's segment: `auto` grabs, `propose` stops at the pick and writes a proposal, `manual` skips scheduling. An approved proposal re-enters the same grab path. |
| **Requests**                                                      | A request (movie or series) spawns a new tracking record or adds the requester to an existing one.                                                       |
| **Quality profile**                                               | Referenced by ID. Tracking doesn't define profiles, just selects one.                                                                                    |
| **Decision log**                                                  | Read-only consumer in the UI: "for this tracked series, here's why nothing has grabbed in the last 24h." Unbuilt (V1.1 search history).                 |
| **Watch state** _(future)_                                        | Optional input — "stop tracking once I finish the series" requires Plex/Jellyfin webhook integration. Deferred until that integration lands.             |
| **Library scan**                                                  | Existing media files satisfying an in-scope episode mean no want needs to be created. Tracking respects what scan finds.                                 |

## UI naming

Internal model name: **tracking**.
User-facing UI strings will likely say **"monitoring"** or **"following"** depending on context — those are localization/UX choices, not model concerns. Stay consistent in code with `tracking`; let the UI layer pick the right word.

## Open questions

1. **Scope storage shape — resolved.** Per-requester scope lives on `tracking_requester` association rows (the live source of truth, seeded from each request at spawn); the tracking caches the union and recomputes it from those rows on join/leave/edit. Chosen over "recompute from the request rows" because requests are frozen after spawn — reading them for a recompute would miss any post-spawn scope edit. See [multi-requester semantics](#multi-requester-semantics). **Answered by the code:** Per-requester scope lives on `tracking_requester` (`0016_tracking.up.sql:82-96`).
2. **Scope rule presets — are these the right five?** "Specific episode ranges" came up in conversation but isn't in the preset list; it's expressible via `all` + excludes but unwieldy if a user wants S05E10–S05E15. May need a sixth preset, or surface "specific episodes" in the UI even though it's just sugar over overrides. *Frozen — beyond V1.*
3. **Anime numbering.** Anime has dual numbering (per-season vs absolute) and the scope rules implicitly assume per-season. Anime support is out of scope for now, but the model should at least not preclude it. Worth a sanity-check pass when anime is on the table. *Frozen — beyond V1.*
4. **Per-episode overrides under TMDB renumbering.** If TMDB renumbers a series (rare but happens), per-episode overrides become wrong silently. Need to decide whether overrides key on a stable TMDB episode ID or on (season, episode) numbers — the former is robust but harder to surface in UI. **Answered by the code:** Overrides key on episode UUIDs, not (season, episode) numbers (`0016_tracking.up.sql:95`).
5. **"Pause on storage low"** — should tracking auto-pause when free disk is below a threshold? Probably yes, but the trigger lives in the storage-intelligence subsystem, not tracking itself. Cross-cutting concern; revisit when that subsystem is designed. *Frozen — beyond V1.*
6. **Tracking deletion vs cancellation.** Is `canceled` actually deletion, or does it preserve a tombstone for "previously tracked"? Tombstones help the "you've already tried this show and removed it" UX. Probably worth keeping; flesh out in data-shape iteration. **Answered by the code:** `canceled` is a kept state, re-armed in place (`0016_tracking.up.sql:41`, `service/request.go:415-419`).
7. **Re-activation noisiness.** `archived → active` when a new episode appears could be surprising ("why is this show downloading? I thought I was done with it"). Notify on re-activation? Require confirmation? Probably notify-then-resume by default; let users opt for confirm-then-resume. *Frozen — beyond V1.*
8. **Cross-tracking dedup edge cases.** If admin tracks `season(2)` and a user requests `season(3)`, are these one tracking with scope `seasons 2-3`, or one tracking with two separate scope rules? The "rule + overrides" model assumes one rule per tracking. May need to relax to "list of rules, OR'd together" if real cases emerge. *Frozen — beyond V1.*
9. **Internal name vs the universal scope.** `tracking` now spans movies and one-off manual grabs, where the word reads slightly series-flavored. Rename candidates if it confuses contributors: `subscription`, `intent`, `monitor`. Leaning: keep `tracking` internally (the UI already says "following"/"monitoring"); revisit in the data-shape iteration. **Answered by the code:** `tracking` is the name throughout (`0016_tracking.up.sql`, `service/tracking.go`).
10. **Proposal expiry.** How long does an unanswered proposal wait? A stale proposal (release dead, or the episode acquired another way) should resolve itself, but a merely-ignored one shouldn't nag forever either. Lean: no hard expiry — supersede-on-better-candidate plus re-validate-on-approve covers correctness, and the notification simply ages; revisit if real queues get noisy. *Frozen — beyond V1.*
11. **Proposal audience in multi-operator installs.** Two admins both hold the grab permission — do both get the proposal push, first-to-act wins (with the Story 3-style 409 on the race)? Lean: yes, same pattern as the approval queue; confirm when a real multi-admin install exists. *Frozen — beyond V1.*
12. **Unify `upgrade_behavior` with acquisition autonomy?** Same trichotomy, one divergent value (`none` vs `manual` — see [Relationship to upgrade behavior](#relationship-to-upgrade-behavior)). Options: keep two fields with near-identical vocabularies, or fold into a single per-class autonomy map (`backfill` / `ongoing` / `upgrade`). Lean: fold at data-shape time — three values of one enum keyed by want-class reads cleaner than two fields — but don't block the propose build on it. *Frozen — beyond V1.*
13. **Decline granularity.** Declining a proposal excludes that release from the want's future picks — but should a decline optionally teach the profile ("never propose this release group again")? Lean: no for v1; decline is per-release, per-want. Profile edits are deliberate acts, not side effects of a dismissal. *Frozen — beyond V1.*
14. **Per-tracking autonomy vs per-requester.** Autonomy is tracking-level and operator-owned by design (see [Who sets it](#who-sets-it)). If a future install wants requester-visible proposals ("your request found a candidate — approve?"), that's a permission grant routing proposals to a requester, not a per-requester autonomy value. Flagged so the data shape doesn't preclude it. *Frozen — beyond V1.*

## What we're explicitly not deciding here

- Exact table names, columns, indexes
- API endpoint shapes
- The search scheduler's implementation
- The proposal record's data shape and the held-want annotation name (acquisition's, at its data-shape pass)
- Quality profile structure
- Notification routing rules (lives in [notifications](../notifications/README.md))
- Storage thresholds, retention policies

Each of those gets its own pass (or its own spec) once this model holds up against more user stories.
