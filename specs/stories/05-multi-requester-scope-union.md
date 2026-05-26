# Story 5 — Two requesters, one series: scope union, widening, and departure

**Status:** Draft

Alice is already tracking Severance, but only Season 3 — she came in mid-run and doesn't care about the back catalog. Bob discovers the show and requests the whole thing. Instead of creating a second tracking record, Bob **joins** Alice's existing tracking; the effective scope **widens** from `season(3)` to `all`, and 19 new wants spawn for Seasons 1 and 2. Both requesters coexist on one tracking. Days later Bob cancels; the effective scope **narrows** back to `season(3)`, in-flight S1/S2 wants cancel, but already-acquired files persist (untracked, not deleted).

This is the **multi-requester story** — the one that proves the three primitives ([request](../modules/requests/README.md), [tracking](../modules/tracking/README.md), [want](../modules/acquisition/README.md)) compose the way the specs claim: requests stay 1:1 with users, tracking collapses to one-per-series and unions scope, wants dedupe across requesters. It directly forces [tracking open question #1](../modules/tracking/README.md#open-questions) (scope storage shape) and the scope-narrowing-on-departure semantics.

Follows the [Story 1](./01-happy-path-auto-approve.md) template.

## Cast

- **Alice** — existing user. Came to Severance mid-Season-3.
  - `requests.create:series:hd: true`, `requests.auto_approve:series:hd: true`.
  - Already has an active tracking for Severance, scope `season(3)`.
- **Bob** — another non-admin user. Wants the whole show.
  - `requests.create:series:hd: true`, `requests.auto_approve:series:hd: true`.
  - PWA installed, push enabled.
- **The system** — Arrflix.
- **External** — TMDB, Prowlarr, qBittorrent, Plex.

## Preconditions

- **Alice's tracking already exists**: Severance, `state: active`, `scope_rule: season(3)`, `requesters: [Alice]`, `quality_profile: hd`, `schedule_strategy: smart`.
- The episode tree is already synced (from Alice's tracking activation per [Story 2](./02-series-mid-season-auto-approve.md)): 29 `media_episode` rows across 3 seasons, S3E05–E10 still airing weekly.
- **In the library**: S3E01–E04 (acquired under Alice's scope). Possibly S3E05+ as they've aired.
- **Not in the library**: all of S1 (9 episodes) and S2 (10 episodes) — outside Alice's `season(3)` scope, so no wants were ever created for them.
- Bob has no existing request or tracking for Severance.
- Indexers + downloader healthy; S1 and S2 season packs are available.

## Flow

### Phase 1 — Bob discovers a partially-tracked series (T+0s)

**User-visible (Bob):**

- Bob searches "Severance", taps the result.
- Focus page loads with the season grid. It reflects **current library + tracking state**:
  - S3E01–E04: green check ("in library")
  - S3E05–E10: "airing — tracked"
  - S1, S2: greyed, "not in library"
- The Request button subtitle reads: _"Season 3 is already being followed. Requesting will add Seasons 1–2 and keep following new episodes."_
- Pre-flight summary: _"You'd add 19 episodes (S1 × 9, S2 × 10), ~46GB."_

**Behind the scenes:**

- Focus page calls media service: "do we have this? is it tracked? by whom? what's in scope?"
- API answers: tracking exists (scope `season(3)`), S3 partially in library, S1/S2 absent. The scope-aware diff ("what would my `all` request add?") is computed against the synced episode tree.
- Note: Bob is **not** told Alice by name unless visibility scoping allows it — see [Open question #1](#open-questions). The default phrasing is requester-agnostic ("already being followed").

### Phase 2 — Bob requests the whole show (T+1s)

**User-visible:**

- Bob picks "whole series", taps Request.
- Pill: **"Subscribing… adding Seasons 1–2"**.
- Toast: "Request submitted — we'll grab the back catalog and keep following."

**Behind the scenes:**

- `POST /requests { tmdb_id, type: "series", tier: "hd", scope: "all" }` — the two request choices (`scope: all` includes future episodes; no separate `monitor_future` flag).
- Request service:
  1. Validates `Bob.requests.create:series:hd` → ok.
  2. Quota check under the cap; `requests.auto_approve:series:hd` held → auto-approve fires.
  3. Writes Bob's `request` row: `{ requester_id: bob, tier: hd, scope: all, status: approved, auto_approved: true }`. **Bob's request stores its own pre-union scope (`all`)** per [requests](../modules/requests/README.md#scope-series-only).
  4. Spawn step: tracking for this series **already exists** → Bob **joins** it (per [requests → series mapping](../modules/requests/README.md#series)), rather than creating a second tracking.
  5. Bob's request transitions `approved → spawned`, recording `spawned_tracking_id: <existing tracking>`.

### Phase 3 — Join, scope union, and widening (T+1s → T+~10s)

**Behind the scenes:**

- TrackingService applies [multi-requester semantics](../modules/tracking/README.md#multi-requester-semantics):
  - `requesters: [Alice]` → `[Alice, Bob]`
  - **Effective scope** = union of each requester's scope = `union(season(3), all)` = **`all`**.
  - The tracking's effective scope widens from `season(3)` to `all`.
- TrackingService re-evaluates scope against the episode tree:
  - Previously in-scope: S3 (10 episodes) — wants already exist (Alice's).
  - Now in-scope: all 29 episodes.
  - **Delta**: S1 (9) + S2 (10) = 19 episodes newly in scope, none in the library, no existing wants.
- WantService creates **19 new wants** for S1 + S2. The S3 wants are untouched — no duplication, because the want already exists and now simply has two requesters' tracking behind it.
- Emits 19 `want.created` events.

**The scope-storage question, made concrete:**

- The tracking could store only the computed union (`all`), or each requester's scope separately. Story 05 makes the trade-off visible: **on Bob's later departure (Phase 7), the union must be recomputed** — which requires knowing Alice's _current_ `season(3)`.
- Resolution this story commits to: **per-requester scope lives on a [`tracking_requester` association row](../modules/tracking/README.md#multi-requester-semantics)** — one per `(tracking, user)`, seeded from the request at spawn and _mutable_ thereafter. Tracking caches the union and recomputes it from the surviving association rows on join/leave/edit. The [request](../modules/requests/README.md) keeps its original scope as frozen audit history but is **not** the recompute source — because requests are immutable after spawn, recomputing from them would miss any post-spawn scope edit (e.g. Alice narrowing her scope). This satisfies [tracking open question #1](../modules/tracking/README.md#open-questions).

**User-visible:**

- Bob's "my subscriptions" now lists Severance with S1/S2 showing "queued" and S3 showing current state.
- **Alice's experience is unchanged** — she wanted S3, she still gets S3. No notification fires to Alice by default (her scope and her wants are unaffected). See [Open question #2](#open-questions).

### Phase 4 — Acquisition of the back catalog (T+~10s → T+~hours)

Compressed [Story 2](./02-series-mid-season-auto-approve.md#phase-4--auto-select--grab-t15s--t3-min) flow:

- AcquisitionWorker processes the 19 new wants.
- S1 season pack chosen → one `download_job` linked to all 9 S1 wants via `download_job_want` M:N.
- S2 season pack chosen → one `download_job` linked to all 10 S2 wants.
- Downloads → imports → per-file want fulfillment → Plex confirms → wants reach `available`.

**Notifications:**

- 📱 Grouped push to **Bob**: "Severance: 9 episodes from Season 1 are ready" then "…10 from Season 2."
- **Alice** gets nothing — these episodes were never in her scope. The notification audience for `tracking.episode_imported` is resolved to the requesters **whose scope includes that episode** — not every requester on the tracking. See [Open question #3](#open-questions).

### Phase 5 — Steady state: two requesters, one tracking (T+~hours → days)

- Tracking: `requesters: [Alice, Bob]`, effective scope `all`, `state: active`.
- New S3 episodes continue to air; both Alice and Bob want S3, so S3 episode-imported notifications go to **both**.
- New S1/S2 content is moot (complete seasons), but upgrade-watching now applies to S1/S2 as well (the tracking's `upgrade_behavior` covers them).
- The library has all 29 episodes (modulo unaired S3). One physical copy of each; both requesters "want" the S3 ones, only Bob "wants" the S1/S2 ones.

### Phase 6 — What is (and isn't) per-requester (illustrative)

This phase illustrates the per-requester intent distinction without changing state:

- **Scope and tier are per-requester.** Each requester's scope (Alice `season(3)`, Bob `all`) and tier live independently on their `tracking_requester` row and coexist on the one tracking. If they wanted different tiers (Alice HD, Bob 4K) those would coexist too — that's the divergent-tier / upgrade case, [out of scope here](#out-of-scope-variant-stories) (Story 06).
- **Retention is *not* per-requester.** Iteration 1 modeled a per-request `retention` flag plus a hardlink-aware "watch-once for Alice, keep-forever for Bob on one file" scheme. Iteration 2 removed it: keeping and cleanup are now a single library-wide [hygiene](../modules/hygiene/README.md) policy, and per-user multi-retention is parked for a later storage iteration. So there is no per-requester retention divergence to arbitrate — the shared S3 file is governed by one library policy regardless of who tracks it.

### Phase 7 — Bob departs: scope narrows (T+~days)

**User-visible (Bob):**

- Bob decides he only cared about Season 3 after all (or is cleaning up his subscriptions). He cancels his Severance request / unsubscribes.
- His "my subscriptions" removes Severance. Toast: "Unsubscribed from Severance."

**Behind the scenes:**

- Per [requests cancellation cascade](../modules/requests/README.md#cancellation-cascade) → [tracking leaving semantics](../modules/tracking/README.md#multi-requester-semantics):
  1. Bob's `tracking_requester` row is removed: requesters `[Alice, Bob]` → `[Alice]`.
  2. Effective scope **recomputed** from the surviving association rows: `union(Alice: season(3))` = **`season(3)`**.
  3. Tracking's effective scope narrows `all` → `season(3)`.
  4. Tracking stays `active` (Alice still wants it) — not paused, because an association row remains.
- TrackingService re-evaluates against the narrower scope:
  - S1, S2 are now **out of scope**.
  - **In-flight S1/S2 wants** (if any remain in `searching`/`pending`) → canceled (no requester wants them anymore). Example: if S2E10 was still searching when Bob left, that want cancels.
  - **Already-acquired S1/S2 files** → **persist**. They are not deleted. They become "in library, untracked" — the tracking simply stops monitoring them for new episodes (moot; complete) and for upgrades.
- Bob's `request` row → terminal `cancelled`. It remains as audit history.

**The "what happens to acquired files" decision, committed:**

- Scope narrowing affects what the tracking **wants going forward** (future acquisition + upgrade-watching), **not** what's already on disk. Deleting acquired content because a requester left would be hostile and surprising.
- The S1/S2 files are now governed only by the library-wide [hygiene](../modules/hygiene/README.md) retention policy — there's no per-request retention to lose when Bob leaves (retention isn't a request concern in iteration 2). Whether they eventually become cleanup-eligible is hygiene's call, not tracking's. See [Open question #5](#open-questions).

**User-visible (Alice):**

- Nothing. Her scope (`season(3)`) and her wants are exactly as before. The narrowing returned the tracking to her original intent.

## Postconditions

- **2 `request` rows** (frozen origin/audit, never the recompute source):
  - Alice's: `status: spawned`, original `scope: season(3)`, references the tracking.
  - Bob's: `status: cancelled` (after Phase 7), original `scope: all`, references the same tracking.
- **`tracking_requester` rows** (live per-requester intent; what the union recompute reads):
  - During Phases 3–6: 2 rows — Alice `season(3)`, Bob `all`.
  - After Phase 7: 1 row — Alice `season(3)`. Bob's row removed on departure.
- **1 `tracking` row** throughout (never two): effective scope `season(3)` after Phase 7 (cached union of the surviving association rows), `state: active`. During Phases 3–6 the effective scope was `all`.
- **`want` rows**:
  - S3 wants: untouched throughout (Alice's; Bob's join and departure never affected them).
  - 19 S1/S2 wants: created in Phase 3, most reaching `available`. On Phase 7 departure, any still in-flight cancel; the rest leave their `media_file`s in place.
- **`media_file` rows**: S1/S2 files acquired before Phase 7 persist in the library, now untracked.
- **Audit rows**: Bob's `request.submitted`, `request.spawned` (join), `request.cancelled`; tracking requester-set transitions; want cancellations on departure.
- **No duplicate tracking, no duplicate wants** at any point.

## What must be true (foundation requirements)

Most of [Story 2](./02-series-mid-season-auto-approve.md)'s requirements carry over (tracking, series sync, season-pack M:N, SearchScheduler). Net new for multi-requester:

### Tracking requester-set + scope recomputation

- **`tracking_requester` association** — one row per `(tracking, user)`, holding that user's live per-requester intent (scope + per-episode overrides, tier). Insert on join, remove on leave, mutate on edit. (Iteration 1 also stored retention + `monitor_future` here; retention is now a library-wide hygiene policy and `monitor_future` is subsumed by scope.)
- **Effective-scope recomputation** on every join/leave/edit, sourced from the surviving association rows. Tracking caches the union but recomputes from the association rows — _not_ from the frozen request rows, which would miss post-spawn edits. This is the concrete resolution to [tracking open question #1](../modules/tracking/README.md#open-questions).
- **Scope-delta evaluation** — on a widening, spawn wants for newly in-scope episodes lacking files; on a narrowing, cancel in-flight wants for newly out-of-scope episodes and leave acquired files in place.

### Request → tracking join/leave wiring

- Series request spawn must detect an existing tracking and **join** rather than create. (Per [requests](../modules/requests/README.md#series); Story 5 is the first to exercise the join branch.)
- Cancellation cascade must remove the requester, trigger scope recomputation, and handle the want-cancellation + file-persistence split.

### Notification audience scoping for shared tracking

- `tracking.episode_imported` (and similar per-episode events) must resolve recipients to **the requesters whose scope includes that episode**, not all requesters on the tracking. Bob's S1/S2 imports notify Bob, not Alice; S3 imports notify both. This is a more precise audience resolution than "all requesters." See [Open question #3](#open-questions).

### Pre-flight scope-diff on the focus page

- "What would my request add?" computed against current library + tracking state: the scope-aware diff (Bob's `all` minus what's already covered = S1 + S2). Per [requests pre-flight visibility](../modules/requests/README.md#pre-flight-visibility), extended to the shared-tracking case.

### Per-requester intent independence

- Each requester's **scope and tier** intent is stored on **their `tracking_requester` row** and carried independently (seeded from their request, then live). Retention is no longer per-requester — it moved to a library-wide [hygiene](../modules/hygiene/README.md) policy in iteration 2, and per-user multi-retention (the same-file-different-lifetime idea) is parked. So the association carries scope + tier; there is no per-requester retention to arbitrate.

### Time targets

- Join + scope widening + new-want spawn: < a few seconds (same transaction class as Story 2's spawn).
- Departure + scope narrowing + want cancellation: < a few seconds.
- No user-visible latency difference between "first requester" and "joining requester" on the request action.

## Out of scope (variant stories)

- **Divergent tiers across requesters** — Alice wants HD, Bob wants 4K. The tracking's effective tier rises to 4K; existing HD files become upgrade candidates. This is upgrade territory (proposed Story 06); deliberately kept same-tier here.
- **Per-user multi-retention** — the parked idea where one hardlinked file serves a "watch-once" intent for one user and "keep-forever" for another. Removed from the per-request model in iteration 2 (retention is now a single library-wide [hygiene](../modules/hygiene/README.md) policy); revisit as a storage feature in a later iteration. A future story exercises watch-based cleanup once hygiene's `lifecycle/*` rules and watch-state land.
- **Last requester leaves** — if Alice (not Bob) had left and Bob remained, fine; but if **both** leave, the tracking moves to `paused` (not canceled), per [tracking lifecycle](../modules/tracking/README.md#lifecycle). Brief variant.
- **Admin-anchored tracking** — admin added the tracking; a user joins then leaves. The tracking doesn't pause on the user's departure because the admin anchors it. Variant.
- **Movie duplicate** — two users want the same movie; the second joins the existing single-atom **tracking**'s requester set (the tracking, not the want, is the dedup boundary — uniform with the series case). Worth a short companion story or a paragraph.
- **Join during in-flight acquisition** — Bob joins while Alice's S3 wants are mid-download. Bob's join adds S1/S2 but also makes Bob a requester on the in-flight S3 wants. Edge timing; mostly covered but worth validating the S3-want requester-set update.
- **Per-episode overrides interacting with union** — Alice has `season(3)` + explicit-exclude S3E07; Bob has `all`. Does the union re-include S3E07 (Bob wants it) or honor Alice's exclude? Per the model, union means Bob's inclusion wins. Variant worth pinning.
- **Pending approval on the joining request** — if Bob lacked auto-approve, his join would wait in the approval queue (Story 3), and the scope wouldn't widen until approved. Composition of Story 3 + Story 5; brief variant.
- **Re-widening after narrowing** — Bob re-subscribes a week after canceling. Scope widens again; S1/S2 files still on disk (never deleted) → no re-acquisition needed, just re-tracking. Nice efficiency case; brief variant.

## Open questions

1. **Requester identity disclosure in pre-flight.** Bob's focus page says "already being followed" — does it name Alice? Lean: no by default (privacy across users); admins with `requests.view:all` may see "tracked by Alice." Pin against the [requests visibility scoping](../modules/requests/README.md#visibility-scoping) predicate.

2. **Does a widening notify existing requesters?** When Bob joins and scope widens to `all`, does Alice get any signal? Her scope is unaffected, so lean: no notification. But if Bob's join changed something Alice cares about (e.g., raised the tier ceiling, triggering upgrades on her S3 files), she might. Lean: notify only when an existing requester's effective experience changes. Pin the rule.

3. **Per-episode notification audience on shared tracking.** `tracking.episode_imported` should notify only requesters whose scope includes that episode. This requires resolving, per episode, which requesters' scopes cover it — a per-event scope evaluation against the requester set. Confirm this is the model (vs. the simpler "notify all requesters") and pin in [notifications](../modules/notifications/README.md) + [tracking](../modules/tracking/README.md).

4. **Scope storage — resolved.** Per-requester scope lives on a `tracking_requester` join table (live, mutable, seeded from each request); the tracking caches the union and recomputes from those rows on join/leave/edit. We deliberately do _not_ recompute from the request rows: requests are frozen after spawn, so they'd miss post-spawn scope edits. The "duplication" with the request's original scope is intentional — request = frozen origin, association = live state. See [tracking open question #1](../modules/tracking/README.md#open-questions).

5. **Acquired files after scope narrowing.** This story commits to "files persist; scope narrowing only affects future wants + upgrade-watching." But who governs the orphaned S1/S2 files' eventual fate? With Bob gone, the files are governed by the library-wide [hygiene](../modules/hygiene/README.md) retention policy (there's no per-request retention to lose in iteration 2). Do they persist indefinitely, become `lifecycle/*` cleanup candidates, or surface as "untracked content" for review? Lean: persist indefinitely unless an explicit cleanup policy claims them; hygiene may surface them as "untracked content" for optional review.

6. **In-flight want cancellation on narrowing — partial packs.** If a season pack covering S2E01–E10 is mid-download when Bob leaves, do we cancel the download (wasting the bandwidth already spent) or let it complete (acquiring files no one is tracking)? Lean: let in-flight downloads complete (bandwidth already committed), cancel only `searching`/`pending` wants. Symmetric with [Story 2 open question #9](./02-series-mid-season-auto-approve.md#open-questions).

7. **Per-episode overrides under union — resolved.** Alice excludes S3E07; Bob wants `all`. Union says Bob's inclusion wins (S3E07 stays in scope). On Bob's departure, S3E07 falls back out (Alice excluded it). This works because overrides live **per-requester on the `tracking_requester` row** (seeded from the request, mutable thereafter), so recomputation from the surviving association rows is correct.

8. **Want requester-set vs tracking requester-set.** A want (especially a shared S3 want) conceptually has requesters too (for notification + cancellation). Is the want's requester set derived from the tracking's (all requesters whose scope covers this episode), or stored explicitly? Lean: derived — a want's requesters are the `tracking_requester` rows whose scope (incl. overrides) covers this episode, a query rather than a stored set. Pin in acquisition.

9. **"Untracked content" surfacing.** After Phase 7, S1/S2 are in the library but no tracking wants them. Should the UI distinguish "tracked" vs "in library but untracked" content? Useful for the user to understand why new S1 special editions won't auto-upgrade. Lean: yes, a subtle badge; not v1-blocking.

10. **Concurrent join race.** Bob and a third user Carol both request Severance within the same second, both joining Alice's tracking. Two concurrent scope-recomputations must not lose an update. Lean: serialize requester-set mutations per tracking — association-row inserts/deletes plus the cache recompute under one lock (row or advisory lock on the tracking id). Pin in tracking.
