# Story 4 — Failed search, eventual recovery: friend requests something the indexers don't have yet

**Status:** Draft

Same friend as [Story 1](./01-happy-path-auto-approve.md), same auto-approve, same HD tier — but this time the movie is a brand-new theatrical release that just opened over the weekend, and the indexers don't have a usable release yet. The system searches, finds nothing, comes back later, finds only cam-quality rips (all rejected), backs off, keeps trying, and finally a 1080p WEB-DL surfaces about 30 hours later. The want resolves normally from there.

This is the **most common non-happy path** in real-world usage: the request is fine, the user is fine, the system is fine — the source material just isn't out there yet. Story 4 exists to pin down what the user sees during that gap, what the system writes to the decision log, and how the back-off and recovery actually behave.

Follows the [Story 1](./01-happy-path-auto-approve.md) template: Cast → Preconditions → Flow → Postconditions → What must be true → Out of scope → Open questions.

## Cast

- **Friend** — identical config to [Story 1](./01-happy-path-auto-approve.md):
  - `requests.create:movie:hd: true`, `requests.create:movie:4k: false`
  - `requests.auto_approve:movie:hd: true`
  - PWA installed, push notifications enabled
- **Admin** — passive. Same prior config as Story 1.
  - HD quality profile rejects `cam`, `ts`, `telesync`, and missing-codec releases via hard gates.
- **The system** — Arrflix: web UI, API, background workers.
- **External** — TMDB, Prowlarr, qBittorrent, Plex.

## Preconditions

- "Sentinel" (a hypothetical action movie that opened in theaters last weekend) is not in any library.
- No existing [[want]] for it.
- TMDB has metadata.
- Indexers are healthy.
- Downloader has free slots.
- The HD quality profile is configured such that **cam-quality releases hard-gate-reject** (not just score low).
- Friend's push subscription is registered.
- _The differentiator from Story 1_: nothing currently on indexers passes the profile. Specifically:
  - At T+0: indexers return zero results for the title.
  - At T+~2h: cam releases start appearing.
  - At T+~30h: a 1080p WEB-DL surfaces.

## Flow

### Phase 1 — Discovery & request (T+0s → T+1s)

Indistinguishable from [Story 1 Phases 1–2](./01-happy-path-auto-approve.md#phase-1--discovery-t0s). Friend taps Request. Button morphs to a pill: **"Searching for HD release…"** A `request` row is created (`auto_approved: true`); it spawns a single-atom **tracking** (anchored on the movie's `release_date`), which creates a `want` in `pending` and fires `want.created`.

The only difference from Story 1 is that the pre-flight summary on the Request button shows **"Note: this just released — may take a day or two to find a quality release"** based on a heuristic over `release_date` ≤ N days. (See [Open question #1](#open-questions).)

### Phase 2 — First search wave: zero results (T+~15s)

**User-visible:**

- Pill stays at **"Searching for HD release…"**

**Behind the scenes:**

- AcquisitionWorker picks up the `want.created` event.
- Builds the search query, hits the indexer service via Prowlarr.
- Indexers respond with **zero results** for all queried sources.
- AcquisitionWorker:
  - Writes a **search_run** audit row noting zero results across N indexers, per the [audit pattern](../patterns/audit/README.md).
  - Does **not** write per-release decision rows (there were no releases).
  - Emits `want.search_failed { reason: no_results, indexers_queried: [...], count: 0 }`.
  - Hands the want back to the **SearchScheduler**, which schedules retries per the movie tracking's smart schedule (anchored on `release_date` instead of an episode air date):
    - First retry: +30 min
    - Then: +1h, +2h, +4h, +8h, +24h, then daily — exponential within tiers, per [tracking smart scheduling](../modules/tracking/README.md#smart-scheduling). Same curve as an episode; only the anchor differs.

**Notifications:** none. A single empty search is not yet noteworthy.

### Phase 3 — Second wave: cam releases, all rejected (T+~2h)

**User-visible:**

- Pill updates: **"Searching for HD release • last checked 2h ago"**
- A drill-down (tap the pill) reveals: _"5 releases considered, all rejected. Tap for details."_ Drill in: a per-release table — title, indexer, score column (mostly N/A because gating happened first), reject reason ("cam-quality release; profile rejects cam"), seeders, age.

**Behind the scenes:**

- SearchScheduler triggers retry at +2h.
- AcquisitionWorker hits indexers; 5 results return — all variations of `Sentinel.2026.CAM.HDRip.XviD-RG` style.
- Quality profile engine:
  - Each release fails the hard gate on `source != cam` (or equivalent rule).
  - Returns 5 `(release, reject, reason)` tuples.
- AcquisitionWorker:
  - Writes one **decision_log** row per release (per [audit](../patterns/audit/README.md)): `decision: rejected`, structured reason (`gate: source_blocklist`, `value: cam`), release identity (indexer, guid, infohash, title), search_run reference.
  - Emits `want.search_failed { reason: all_rejected, count: 5, breakdown: {gate_source_blocklist: 5} }`.
  - Schedules next retry per the back-off.

**Notifications:** still none. Five rejected cam releases is not actionable; the user can't tell the indexers to make a 1080p release appear.

### Phase 4 — Back-off and the long wait (T+~2h → T+~30h)

**User-visible:**

- Pill cycles through retries: **"Searching for HD release • last checked 4h ago"**, **"…8h ago"**, **"…14h ago"**.
- "My requests" page shows Sentinel with a yellow "still searching" indicator (not red — yellow because the system is working, just waiting).
- Eventually (default: at the **+24h mark**, configurable), a **"still searching"** in-app notification is generated:

  > **"We're still looking for _Sentinel_."** It just released and quality releases sometimes take a day or two. We'll keep trying. _[See what we've tried]_ · _[Cancel request]_

  (Push delivery for this event is **off by default** — in-app only — to avoid push fatigue. See [Open question #2](#open-questions).)

**Behind the scenes:**

- Each retry adds rows to decision_log: more cam-quality rejects, the occasional `dvdscr` or `hdcam` reject, etc.
- The retry interval grows per the back-off curve. After ~6 attempts the cadence is daily.
- Friend taps **"See what we've tried"** → debug surface: per-search_run timeline, per-release decision rows, aggregate stats ("47 releases considered across 8 search runs over 28 hours; all rejected — primary reason: cam-quality (43), insufficient seeders (3), missing audio codec (1)"). This is the same [audit-view](../patterns/audit/README.md) UI surface as Story 1's "why did this grab?" answer, in inverse.

### Phase 5 — 1080p surfaces, normal pipeline resumes (T+~30h)

**User-visible:**

- Pill updates: **"Found a release • Queued"** → **"Downloading 8% • ~5 min"** (back to Story 1's tempo from here).

**Behind the scenes:**

- SearchScheduler triggers retry at the next daily slot.
- AcquisitionWorker hits indexers; 12 results return this time — most still cam, but **two 1080p WEB-DLs** appear.
- Quality profile engine:
  - Hard-gates: 10 cam/ts releases rejected.
  - 2 survivors scored normally.
  - Best scorer (higher seeders + better release group) picked.
- AcquisitionWorker:
  - Writes decision_log rows for all 12 (10 rejected, 1 runner_up, 1 grabbed).
  - Evaluates routing rules → picks downloader, library, name template.
  - Creates `download_job`, links to the want.
  - Want transitions: `searching → grabbed → downloading` (back-to-back).
  - Emits `want.grabbed` event.

**Notifications:**

- The pending in-app "still searching" notification is **superseded** (auto-dismissed when the want is no longer in a failing state).
- No push yet — push fires only on `available`, same as Story 1.

### Phase 6 — Download → Import → Plex confirms → Available (T+~30h → T+~30h 8min)

Indistinguishable from [Story 1 Phases 4–5](./01-happy-path-auto-approve.md#phase-4--download--import-t7-min). qBit downloads, ImportWorker hardlinks + renames, `import_task` carries `want_id`, `media_file` row created, want → `imported`. Plex partial-refresh + `library.new` webhook → want → `available`.

**Notifications:**

- 📱 Push to Friend: **"_Sentinel_ is ready to watch."** Tap → Plex deep link.
- In-app pill: **"Available on Plex"**.
- The "still searching" notification, if not already auto-dismissed, is dismissed now.

## Postconditions

- **1 `request` row**, `status: spawned` (frozen once tracking + want exist; the failure phase doesn't touch the request entity).
- **1 single-atom `tracking` row**, `active` throughout the 30h search (it only archives once the want is `available` at-cutoff). This is the scheduling home for the retries.
- **1 `want` row** (parented to the tracking), ending at `available`. Crucially, the want **never left `searching`** across the failure phase — it didn't transition to a `failed` terminal state. (See [Open question #3](#open-questions).)
- **1 `media_item`, 1 `media_file`, 1 `media_file_state`** — identical to Story 1.
- **1 `download_job`, status `completed`** — created only on the successful pick at T+~30h, not on the earlier failures.
- **~8–10 `search_run` rows** with timestamps spanning T+0 through T+~30h.
- **~50 `decision_log` rows total** — 47 rejected across the failure phase, plus 1 grabbed + 1 runner_up + 10 rejected from the final successful search.
- **1 in-app notification** (the "still searching" message), `dismissed_at` populated when the want recovered.
- Friend's "my requests" page shows Sentinel as fulfilled, with the back-history collapsed under a "took 30 hours, 47 rejected" detail toggle.

## What must be true (foundation requirements)

Most of Story 1's requirements carry over unchanged. Net new for Story 4:

### Back-off & scheduling

- **One scheduling home, for movies and episodes alike — resolved.** Every want has a tracking parent (movies are single-atom trackings, per the [universal-intent model](../modules/tracking/README.md#movies-under-this-model)), so there is no "non-tracking want" special case. A movie tracking reuses the smart-schedule curve anchored on the movie's `release_date` (option (a) from the earlier draft); a not-yet-released movie anchors on request time until a release date is known. This is what Story 4 forced, and the universal-intent change settles it.
- **Back-off resets on first new result.** If a search returns even one new release (regardless of whether it passes gates), the back-off may reset or relax — otherwise the system stays slow on long-running failures even after fresh indexer activity. Need to pick a heuristic.

### Audit & decision log

- **`search_run` row** even when zero releases returned, with `indexers_queried` and per-indexer `(count, latency_ms, error?)` so "we did try" is visible.
- **`decision_log.search_run_id` FK** — every per-release decision links to its search run, enabling "show me the timeline" UI.
- **Aggregate reject summary** — the UI needs a fast "47 cam-rejected" view without scanning every row. Either a derived materialized view, or a per-want rollup column, or just a fast query — implementation choice. Pin in audit data-shape iteration.
- **Decision_log retention for failed-then-recovered wants.** A want that fails for 30h and then succeeds has ~50 audit rows. Multiply by realistic load and the table grows fast. Per the [audit pattern](../patterns/audit/README.md), retention is centralized — but the recovery case may warrant separate retention (success rows kept longer, failure rows pruned faster, since they're noise once the want resolves).

### Notification semantics

- **A "still searching" event type**, audience: requester, channel default in-app. Producers: AcquisitionWorker after N consecutive failed search_runs OR M hours since `want.created` without a successful grab. Per [notifications](../modules/notifications/README.md) — typed constructor; payload includes the want, the aggregate reject breakdown, deep link to debug surface.
- **Auto-dismissal semantics** — when the want transitions to `grabbed`, in-app notifications referencing it should be dismissed (or marked superseded), not just left as stale "still searching" entries. Need a notification-supersede mechanism, or the producer emits a `notification.supersede(dedup_key)` paired event.
- **Push opt-out for this event by default** — confirmed in [Open question #2](#open-questions); this story's recommendation is that "still searching" is **in-app only by default** with an opt-in for push.

### UI surfaces

- **Want pill states** beyond Story 1's set:
  - `Searching for HD release…` (active)
  - `Searching for HD release • last checked 4h ago` (after first retry without grab)
  - `Still searching • 47 considered` (after the "still searching" threshold)
  - The differentiation is mostly tone (warmer / cooler / yellow indicator); the data is the same.
- **Drill-down debug view** — "see what we've tried" link from any want's pill. Renders the search_run timeline + decision rows + aggregate stats. This is the same [audit](../patterns/audit/README.md) surface as Story 1's "why did this grab?" — bidirectional.
- **"Force search now" affordance** — see [Open question #4](#open-questions). Lean: yes, but rate-limited.

### Time targets (UX commitments)

- First search attempt: <30s of `want.created` (unchanged from Story 1).
- "Still searching" notification: not before T+24h on default config. (Configurable.)
- Recovery latency: bounded by the back-off curve, which itself is bounded by the worst-case daily cadence after T+~12h. Force-search bypasses this.

## Out of scope (variant stories)

- **Terminal give-up flow** — the user wants to stop trying after N days, or the system declares the request unfulfillable. This is the structural fork from Story 4 — its own variant story. Open questions: how long is "give up," does the system propose cancellation or just keep retrying forever, what happens to the request entity.
- **All-rejected for a non-new release** — failure mode isn't "indexers haven't gotten there yet" but "user's quality profile is too strict for what exists." Different UX (the right answer is "loosen profile," not "wait"). Variant story.
- **Indexer down during the failure window** — overlaps with connectivity-health (proposed Story 10); the failure mode is "we can't tell whether nothing exists or we just can't see it." Different shape.
- **Tier-mismatch fallback** — `tier_floor: hd, tier_ceiling: 4k`: 4K never surfaces but HD does. Should the want grab HD per the floor, or wait for 4K? Owned by [requests](../modules/requests/README.md) intent flags; deserves a dedicated story.
- **User uses interactive search mid-failure** — Friend gets impatient, opens the manual search view, finds a 720p release, grabs it manually overriding the profile. Story 1 alluded to this; deserves a dedicated story exercising the interactive flow + audit row marked `manual_override`.
- **Series episode never surfaces** — Story 2's tracking + smart scheduling has built-in back-off but no terminal give-up. Variant story for the "this episode just never released" case (rare but real for cancelled / bootleg-only content).
- **Failure during download or import** — qBit reports failed download; or import fails on disk full / permission denied. Different failure axis (post-grab); its own story.
- **Hardlocked storage / quota cancellation** — admin's library is full; new wants can't even start. Connects to hygiene / connectivity-health.

## Open questions

1. **Pre-flight "may take a day or two" warning.** Worth showing on the Request button for newly-released titles? Heuristic candidates: `release_date` within last 14 days for movies; `air_date` within last few hours for episodes. Risk: false positives feel like the system is making excuses. Lean: yes for very recent releases (< 7 days), tone it down to a small footnote, don't block submission.

2. **"Still searching" delivery channel default.** Push vs. in-app vs. both. Push risks fatigue (most users won't act, and the "good news" push comes later anyway). In-app is invisible if the user isn't looking. Recommendation: in-app by default, push opt-in per user, configurable per `notification_preference`. Pin in [notifications](../modules/notifications/README.md).

3. **Does a long-failing want ever enter a `failed` terminal state?** Today's spec implies `failed` is reachable for non-recoverable conditions (download cancelled, import error). For "no releases" failures, the want stays `searching` indefinitely with back-off. Should there be a `failed` transition after N days of consistent emptiness? Lean: no — `failed` is for active failures, not patience. But pin the rule.

4. **"Force search now" button.** Useful for impatient users; risks indexer abuse. Lean: yes, rate-limited (one force per want per hour), prominently visible on the pill drill-down. Audit row written for the user-initiated search_run.

5. **Aggregate audit retention for the rejected-pile case.** A want with 47 cam-rejects on the way to success — does the system keep those 47 rows forever? They're noise once the want resolves; keeping them helps the "what didn't work?" forensic story; pruning them keeps the audit table lean. Lean: keep for 30 days after want resolution, then prune via [hygiene](../modules/hygiene/README.md). Pin in [audit](../patterns/audit/README.md).

6. **Cross-indexer dedup of identical rejected releases.** If 3 indexers all carry `Sentinel.2026.CAM.HDRip.XviD-RG` (same infohash, different sources), do we write 1 decision_log row or 3? Lean: 1 row keyed on infohash, with `indexers_seen` as an array. Reduces noise dramatically on popular releases.

7. **Back-off reset trigger.** If a search after a long back-off period returns _new_ releases (even if all still hard-gated), does the back-off reset? Without reset, the system stays slow even after fresh indexer activity. With reset, a flood of fresh-but-bad releases keeps the system busy on a futile search. Lean: reset only when at least one release passes hard gates (regardless of whether it wins the pick), not on raw new-result count.

8. **"What we've tried" UI scoping.** The drill-down can show: every search_run (chronological), or aggregated reject breakdown (grouped by reason), or both. UX call; both are valuable.

9. **Heuristic for detecting "this won't resolve on its own."** After ~7 days of all-cam-rejected, the situation is qualitatively different from "we just need to wait" — it's probably not coming. Worth a separate heuristic (e.g., transition the still-searching notification to "we're not finding HD; consider 720p?") with a suggested user action. Lean: yes for v2; out of scope for v1 spec.

10. **Search-run as audit citizen.** Story 4 treats `search_run` as a first-class audit-pattern record. Story 1 doesn't surface it (the search succeeds immediately so the search_run is invisible). Worth promoting to a documented audit row type — see [audit](../patterns/audit/README.md) — with the same retention / drill-down treatment as decision_log rows. Pin in audit iteration.
