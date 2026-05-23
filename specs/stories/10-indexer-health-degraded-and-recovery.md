# Story 10 — Indexers degrade mid-acquisition: graceful fallback, admin alert, partial recovery

**Status:** Draft

The admin has three indexers configured under Prowlarr. Over the course of an hour, one degrades into `rate_limited`, then a second goes `unreachable`, then the third also fails — leaving the system with no healthy indexers and Friend's open wants stuck. The admin gets a `connectivity.failed` push. An hour later, two of the three indexers recover; in-flight wants resume and one of them grabs cleanly. The third indexer stays unhealthy — the admin investigates separately, but the system functions on degraded capacity.

This is the **first admin-centric story** (Stories 1, 2, 4 were friend-centric) and the first to validate the [connectivity-health pattern](../patterns/connectivity-health/README.md) under real load — multi-resource hysteresis, per-consumer gating, admin-audience notifications, and partial recovery. It also forces the pending indexers spec to take shape: this story can't be fully specified until indexer entities have a defined model.

Follows the [Story 1](./01-happy-path-auto-approve.md) template: Cast → Preconditions → Flow → Postconditions → What must be true → Out of scope → Open questions.

## Cast

- **Admin** — primary actor. Their config:
  - Holds all admin permissions; subscribed to the `admin_alerts` bundle on push + in-app (per [notifications](../modules/notifications/README.md#preferences-and-bundles) defaults).
  - PWA installed, push enabled.
- **Friend (and other passive users)** — has open wants from prior flows:
  - Story 2's Severance subscription: `want` for S03E05 is in its `1–6h post-air` peak window, expecting to grab within the hour.
  - Story 4's _Sentinel_ request: `want` is in the long back-off phase (next retry at +6h from last attempt).
- **The system** — Arrflix: AcquisitionWorker, SearchScheduler, the indexer health worker, the notifications outbox + delivery worker.
- **External** — Prowlarr managing three indexer connections:
  - **Indexer-A** — a public source, rate-limit-sensitive.
  - **Indexer-B** — a semi-private tracker.
  - **Indexer-C** — another public source.

## Preconditions

- All three indexers are configured, enabled, and currently `healthy`.
- The HD quality profile's `allowed_indexer_set` includes all three (covering both Sentinel's and Severance's wants).
- The indexer health worker is running at the recommended cadence (per [connectivity-health](../patterns/connectivity-health/README.md#cadence-guidance): 1–5 min for indexers).
- Hysteresis configured per pattern: 2 consecutive failures to degrade, 1 success to recover.
- The admin has `notification_preference` rows enabling push + in-app for the `admin_alerts` bundle.
- At least one other admin exists (so fan-out to multiple recipients is exercised) — or, if not, the single admin case is the trivial reduction.
- A downloader is healthy and has free slots (this story is about indexer failure, not downloader).

## Flow

### Phase 1 — Steady state (T+0)

- All 3 indexers: `status: healthy`, `status_checked_at: <recent>`, `status_last_transitioned_at: <hours ago>`.
- Severance S03E05's want is in `searching`; SearchScheduler has it on the ~15-min cadence for the post-air peak window.
- Sentinel's want is in `searching` with the next retry scheduled at T+~6h.
- Admin's dashboard shows three green health badges in the Indexers panel.

### Phase 2 — Indexer-A rate-limited (T+~15 min)

**Behind the scenes:**

- The health worker's scheduled probe of Indexer-A hits the upstream and gets a 429 response. The probe maps this to the extended status `rate_limited` per the indexers spec (pending — see [What must be true](#what-must-be-true-foundation-requirements)).
- Per [hysteresis](../patterns/connectivity-health/README.md#hysteresis--debouncing), this single failure does **not** transition. `status_checked_at` is updated; `status` stays `healthy`. A flap-suppression counter increments.
- Next probe (5 min later): also 429. Counter reaches 2 → transition fires.
- Indexer-A row updates: `status: rate_limited`, `status_last_transitioned_at: <now>`.
- A [realtime](../modules/realtime/README.md) event fires on the `indexer.health` channel:
  ```json
  { resource_id: "<A>", resource_type: "indexer",
    from_status: "healthy", to_status: "rate_limited",
    transitioned_at: "<ts>", reason: "429 from upstream" }
  ```
- An admin-action audit row is written: `indexer.health_transitioned { from, to, reason, indexer_id }` per [admin-action audit](../modules/users/README.md#admin-action-audit).
- AcquisitionWorker's consumer-behavior mapping reads `rate_limited → degraded`: keep using Indexer-A for queries that already have results, but **prefer** the other healthy indexers for new searches. No new searches are routed to Indexer-A until it recovers.
- The notifications system does **not** fire — `rate_limited` maps to `degraded`, which is not a `failed`-tier event.

**User-visible:**

- Admin's indexer dashboard updates live via SSE: Indexer-A's green dot turns amber. A small "rate-limited; reduced usage" badge appears.
- No push, no in-app notification, no email.
- Friend sees nothing — Severance's want continues being searched against B and C.

### Phase 3 — Indexer-B unreachable (T+~30 min)

**Behind the scenes:**

- Indexer-B's network path fails (maybe a tracker outage). The health worker's probe times out.
- Two consecutive timeout probes → transition `healthy → unreachable`.
- Same machinery as Phase 2: row update, SSE event, audit row.
- Consumer-behavior mapping reads `unreachable → blocked`. AcquisitionWorker stops routing new searches to Indexer-B; in-flight searches against B (if any) error with `BadGateway` per [errors](../patterns/errors/README.md), back-off rather than retry.
- Severance S03E05's next scheduled search executes during this window: AcquisitionWorker queries A (degraded, allowed) and C (healthy). Returns no results yet. Decision_log writes a `search_run` row noting which indexers were queried, which were skipped.

**User-visible:**

- Admin's dashboard: Indexer-B's dot turns red. Banner at top: "1 indexer unreachable, 1 rate-limited. Searches continuing on Indexer-C."
- Still no push — the system can still function, even if barely.

### Phase 4 — Indexer-C unreachable: full block (T+~45 min)

**Behind the scenes:**

- Indexer-C also starts failing (perhaps a Prowlarr restart or Prowlarr-host hiccup affecting all sources). Two consecutive failures → transition `healthy → unreachable`.
- AcquisitionWorker now sees: A `degraded` (rate-limited, deprioritized), B `blocked`, C `blocked`. The profile's `allowed_indexer_set` has zero healthy members.
- AcquisitionWorker's behavior at this point per [acquisition](../modules/acquisition/README.md) + the connectivity-health [consumer gating](../patterns/connectivity-health/README.md#consumer-gating) recommendation: **defer searches**, do not enqueue new search runs against fully-blocked infrastructure. Wants stay in `searching`; SearchScheduler holds them with a `paused: indexer_health` annotation (distinct from normal back-off — see [Open question #4](#open-questions)).
- Severance S03E05's next-scheduled search (~T+50 min) is supposed to run but the scheduler skips it, logging "deferred: no healthy indexers in profile's allowed set."
- The connectivity-health system fires `connectivity.failed` for the **system overall** — see [Open question #1](#open-questions) for the "which entity is failed" question.

**Notification fan-out:**

- The `connectivity.failed` event has audience `admin`. Per [notifications](../modules/notifications/README.md#three-audiences), recipients are resolved at enqueue time: every user holding the relevant admin permission.
- For each (admin, channel) pair the admin has enabled for `admin_alerts`, an outbox row is written. In this scenario: 2 admins × (push + in_app) = 4 outbox rows.
- Push delivery fires: **"All indexers unavailable — acquisition paused."** Tap → admin dashboard.
- In-app rows appear in the bell icon for both admins.

**User-visible (Admin):**

- Push notification arrives.
- Dashboard banner escalates to red: "**All indexers unavailable.** Acquisition is paused. _[Diagnose] [Retry now]_"
- The Indexers panel shows: A (amber, rate-limited), B (red, unreachable for ~15 min), C (red, unreachable for ~1 min).

**User-visible (Friend):**

- Severance's want pill changes from "Searching for HD release" to **"Paused — waiting for indexers"** (the new wait reason replaces the back-off reason).
- Sentinel's pill is unchanged (it was already in a long back-off; the next scheduled retry is hours away).
- No push fires to Friend — this is admin-domain.

### Phase 5 — Admin investigation (T+~50 min)

**User-visible (Admin):**

- Admin taps the push → arrives at the Indexers panel.
- Per-indexer detail page available: shows status timeline (the audit history rendered as a recent transitions list), most recent probe attempt + error, **"Test now"** button.
- Admin taps Test on Indexer-B → triggers a probe-on-demand (per [connectivity-health open question #6](../patterns/connectivity-health/README.md#open-questions): a uniform `POST /indexers/{id}/probe` endpoint). Probe returns timeout. Status unchanged; no transition (still `unreachable`).

(Investigation continues out-of-band — admin is looking at Prowlarr logs, etc. The story doesn't follow that thread.)

### Phase 6 — Recovery cascade (T+~1h → T+~1h 15m)

**Behind the scenes:**

- T+~1h: Indexer-A's rate-limit window resets. The next scheduled probe succeeds.
  - Asymmetric hysteresis: 1 success is enough to recover. Transition fires: `rate_limited → healthy`.
  - Row update + SSE event + audit row.
  - AcquisitionWorker re-enables Indexer-A.

- T+~1h 5m: SearchScheduler sees that Severance S03E05's want now has a healthy indexer available. It clears the `paused: indexer_health` annotation and schedules the next search at the normal peak-window cadence (next slot ~10 min away).

- T+~1h 10m: Indexer-B's upstream comes back. Next probe succeeds → transition `unreachable → healthy`.

- T+~1h 15m: Severance S03E05's deferred search runs against A and B. Returns a 1080p WEB-DL of the episode (about an hour after air, well within the typical release window). Quality picks it; AcquisitionWorker creates a `download_job`; want transitions `searching → grabbed`.

**Notification handling on recovery:**

- The `connectivity.failed` notification fires its inverse: `connectivity.recovered` (per [notifications](../modules/notifications/README.md), this event exists alongside `connectivity.failed`). Audience: same as the failed event.
- Outbox rows for `connectivity.recovered` are written. Push fires: **"Indexers recovered — acquisition resumed."**
- The earlier `connectivity.failed` in-app entry: see [Open question #6](#open-questions) for whether it auto-dismisses (notification supersedes), gets marked as resolved, or stays as a historical entry.

**User-visible (Admin):**

- Dashboard banner clears or downgrades to a yellow "Indexer-C still unreachable" note.
- Indexer-A and Indexer-B both show green; Indexer-C still red.
- Push: "Indexers recovered — acquisition resumed."

**User-visible (Friend):**

- Severance pill: "Paused — waiting for indexers" → "Grabbed release • Queued" (transitioned through `searching` instantly because the SearchScheduler immediately picked up and the result was waiting).
- The normal Story-1 tail plays out: download → import → Plex confirms → push to Friend: "Severance S03E05 'Cold Harbor' is ready to watch."

### Phase 7 — Persistent failure (T+~1h 30m and beyond)

**Behind the scenes:**

- Indexer-C remains `unreachable`. Its `status_last_transitioned_at` is stable at the Phase 4 timestamp.
- Per [connectivity-health open question #4](../patterns/connectivity-health/README.md#open-questions): the pattern leaves it to each implementer's spec to decide when `unreachable` escalates from `blocked` to `failed`. The pending indexers spec must answer: if an indexer is unreachable for, say, 24 hours, does it auto-escalate to `failed` (loud notification with "this looks like a config issue") or stay quiet?
- Story 10 leaves this open: the admin already got a notification at Phase 4 about the system being blocked; they're aware Indexer-C is the holdout. Whether a second escalation fires at T+24h is the pending indexers spec's call.
- The other two indexers handle ongoing load fine. Sentinel's want eventually retries (per Story 4's back-off) and continues to find nothing — but that's Story 4's failure mode, not Story 10's.

## Postconditions

- **3 `indexer` rows** with the three connectivity-health columns:
  - Indexer-A: `healthy`, last_transition was the rate_limit recovery at T+~1h
  - Indexer-B: `healthy`, last_transition was the recovery at T+~1h 10m
  - Indexer-C: `unreachable`, last_transition was the Phase 4 failure at T+~45 min
- **~6 admin-action audit rows** for the transitions (3 down + 2 up + Indexer-C still down).
- **Notification outbox**:
  - `connectivity.failed` rows: 2 admins × 2 channels = 4 rows, all `delivered`.
  - `connectivity.recovered` rows: same count, all `delivered`.
  - In-app entries visible in admins' bell icons (with whatever supersede behavior is chosen — see [Open question #6](#open-questions)).
- **Severance S03E05**: imported, available on Plex, Friend has a push. The want's audit trail includes a `search_run` row marked `deferred: indexer_health` for the missed slot during the outage.
- **Sentinel**: still in its long back-off; no change attributable to this story.
- **Admin dashboard**: 2 green + 1 red indexer; persistent banner about Indexer-C.

## What must be true (foundation requirements)

Story 10 is the first to fully exercise the [connectivity-health pattern](../patterns/connectivity-health/README.md). Most requirements are already declared in that pattern + the [notifications](../modules/notifications/README.md) spec. The story-driven additions:

### Indexers spec (pending — Story 10 forces it)

The pending indexers spec must declare, at minimum:
- The **indexer entity** — whether each indexer-under-Prowlarr is its own Arrflix-side row (Story 10 assumes yes) or whether Prowlarr-as-a-whole is the single resource. The richer model is per-indexer; Story 10 cannot be specified without it.
- The **probe** — what success and failure look like (HTTP GET against a search endpoint? Prowlarr's `/api/v1/indexer/{id}/test`? something else?).
- The **extended status values** and consumer-behavior mappings:
  - `rate_limited` → `degraded` (for AcquisitionWorker: deprioritize; still usable for in-flight queries)
  - `unreachable` → `blocked` (skip entirely; defer scheduled searches if no alternates are healthy)
  - `degraded` (e.g., consistently-slow responses) → `degraded` (proceed, surface in UI)
- The **escalation TTL** — when `unreachable` becomes `failed` (per [connectivity-health open question #4](../patterns/connectivity-health/README.md#open-questions)).

### AcquisitionWorker consumer behavior

- Must consult the indexer health status before enqueueing a search run.
- Must filter the profile's `allowed_indexer_set` by current health before deciding to run.
- If all allowed indexers are `blocked`, must defer the search (not fail it). The want stays `searching`; SearchScheduler holds it with a distinguishable reason.
- On a transition event for a relevant indexer (via SSE subscription on `indexer.health`), must re-evaluate paused wants and resume them when appropriate. Edge case: many wants paused; on recovery, don't fire a thundering herd — see [Open question #5](#open-questions).

### SearchScheduler

- Annotate paused wants with the **reason** for the pause: `back_off` (normal failure cadence) vs `indexer_health` (deferred due to no healthy indexers). These are distinct and need separate UI surfacing.
- On health-recovery events, the scheduler resumes `indexer_health`-paused wants immediately (not on their original schedule); back-off-paused wants are unaffected.

### Notifications — `connectivity.failed` and `connectivity.recovered`

- Both events declared in [notifications](../modules/notifications/README.md). Audience: `admin`. Default bundle: `admin_alerts`.
- Payload includes the affected resource(s) — for indexer-failure, either a single indexer or a list (when multiple have failed in a window).
- Dedup key: probably `(resource_type, resource_id, status)` so the same indexer going up-down-up-down doesn't generate N pushes (per [notifications open question #4](../modules/notifications/README.md#open-questions): dedup drops or replaces — Story 10 needs at least "drop while recovered is in-flight").

### Realtime SSE channel `indexer.health`

- Per [connectivity-health](../patterns/connectivity-health/README.md#transition-emission), the channel name is `<resource_type>.health`. For indexers, that's `indexer.health`.
- Both the admin dashboard UI and consumer subsystems (AcquisitionWorker, SearchScheduler) subscribe.
- Events emit on transitions only — no heartbeats, no repeated unhealthy events.

### Admin dashboard UI

- Indexers panel showing per-indexer status + transition timestamps.
- Per-indexer detail page with audit history (rendered from `indexer.health_transitioned` rows) and a probe-on-demand button.
- Top-of-dashboard banner that aggregates: "1 unreachable, 1 rate-limited" → "All indexers unavailable" → cleared, all derived from current statuses.

### Time targets

- Probe cadence: 1–5 min per [connectivity-health](../patterns/connectivity-health/README.md#cadence-guidance) for indexers.
- Time to detect + signal: 2 × probe cadence due to hysteresis (≤ 10 min for indexers).
- Time to recover: 1 probe cycle (≤ 5 min for indexers).
- Notification delivery: per the notifications outbox retry curve — usually seconds.

## Out of scope (variant stories)

- **Downloader fails mid-download.** Different consumer (DownloadJobWorker), different failure shape (in-flight torrents stuck, not just enqueuing blocked), different recovery semantics. Its own story.
- **Library goes read-only or low-space.** Already a documented behavior in [libraries](../modules/libraries/README.md). Its own story would exercise import-time gating + the `low_space` extended status.
- **Plex itself goes down.** Affects availability confirmation (Story 1 Phase 5). Would exercise the future Plex spec's health probe + the `version_mismatch` extended status.
- **Auth-failed on a downloader.** Story-shape would be: admin rotated qBit's password and didn't update Arrflix; downloader probe returns `auth_failed`; this maps directly to `failed` tier (not `blocked`) per the connectivity-health spec, with a "fix your credentials" admin alert. Its own story.
- **Single-indexer install.** The trivial reduction of Story 10: only one indexer configured; when it fails, there's no degraded state — straight to blocked. Worth a brief variant note.
- **Indexer healthy but returning garbage.** Probe says healthy, but every result is malformed / unparseable. Different failure mode (not connectivity); belongs to quality-detection or release-parser robustness.
- **Profile-specific indexer outage.** Indexer-A is healthy but the profile only allows Indexer-B and C, both of which are down. Subtly different from Story 10: the system overall is fine, just this profile is stuck. The fix is "use a different profile" rather than "wait for recovery." Probably a variant.
- **Prowlarr itself unreachable** (vs. individual sources within Prowlarr). Single point of failure — all indexers go red simultaneously. The story is similar to Phase 4 of this one but with a different root cause. The pending indexers spec needs to decide whether Prowlarr-as-a-whole is also a probed resource.
- **Many-indexer install (10+ indexers).** Notification fan-out and UI density both change shape. Variant.
- **Manual mark-healthy override.** Per [connectivity-health open question #5](../patterns/connectivity-health/README.md#open-questions): admin clicks "I just fixed it" to skip the next probe wait. Useful variant to validate the override + audit-row + temporary-trust behavior.

## Open questions

1. **What is the "failed entity" of a system-wide block?** When all indexers are `blocked`, is the `connectivity.failed` event about a single indexer (the most-recently-transitioned one) or about "the indexer subsystem"? Lean: a system-wide event distinct from per-resource transitions — fires when the AcquisitionWorker's first deferred-due-to-no-healthy-indexers search occurs. Resource identifier is the profile or the subsystem name, not a single row.

2. **Profile-aware health gating.** A profile with `allowed_indexer_set = [B, C]` is fully blocked when only A is healthy, even though "the system" has 1 healthy indexer. Should the per-profile blocked state fire a separate notification (less alarming than "system blocked")? Lean: yes for v2 — start with system-level events in v1 and pin profile-aware semantics later.

3. **Per-indexer probe-on-demand UX.** The pattern recommends a uniform `POST /<resource>/{id}/probe` endpoint ([connectivity-health open question #6](../patterns/connectivity-health/README.md#open-questions)). For indexers, what does a "Test now" button actually do — call upstream's `/test`? Run the same probe the worker runs? Both produce a status decision, but the test endpoint may be heavier (real search vs. shallow ping). Lean: same probe the worker runs; document that it's a lightweight check.

4. **`paused: indexer_health` annotation on wants.** Distinct from `paused: back_off`. This affects the want's pill ("Paused — waiting for indexers" vs "Still searching"), the SearchScheduler's resume logic (immediate on health recovery vs. wait for back-off slot), and the audit trail. Pin in [acquisition](../modules/acquisition/README.md).

5. **Thundering-herd on recovery.** 50 wants paused for indexer health. Indexer-A comes back healthy. Do all 50 wants immediately fire searches against A? That's both a stampede on A (likely getting it rate-limited again immediately) and wasteful (A may have nothing for most of them). Lean: stagger by reading the back-off curve from each want's last attempt, not by treating recovery as a "search all now" event. Pin in acquisition.

6. **`connectivity.recovered` superseding `connectivity.failed`.** When recovery fires, the failed-event in-app entry could: (a) stay as historical record, (b) get auto-dismissed (notification supersede mechanism — see [Story 4 open question](./04-failed-search-recovery.md#open-questions)), (c) get marked as "resolved" but remain visible. Lean: option (c) — resolved + visible. The history is useful for "when did this happen earlier today?" Pin in notifications.

7. **Persistent-unreachable escalation.** Indexer-C is still down at T+1h 30m. At what point does the system escalate beyond the original "all indexers unavailable" push? Per-indexer "still down at 24h" reminder? Different bundle? Silent until the admin acts? Pin in the pending indexers spec per the pattern's [open question #4](../patterns/connectivity-health/README.md#open-questions).

8. **In-flight search results from an indexer that just went unhealthy.** AcquisitionWorker fires a search against B at T+~29 min (just before B's transition). B responds at T+~31 min (just after). Are those results trusted (it answered) or discarded (the status flipped)? Lean: trust them — the response is the response, regardless of what happened to status between request and response.

9. **Multi-admin notification fan-out de-duplication.** Two admins each get a push, each get an in-app entry. Both look at the dashboard; both see the same red banner. Are there cases where admins would want a "team-wide" single notification (e.g., one admin handles it, others see it as resolved)? Lean: defer — the per-admin model is correct; ownership / acknowledgement workflows are v2.

10. **What if the health worker itself stalls?** If the probe scheduler is stuck (deadlock, hung goroutine), statuses stay frozen at their last known values. Per [connectivity-health open question #7](../patterns/connectivity-health/README.md#open-questions), startup state is `unknown` — but a frozen worker leaves stale `healthy` claims undetected. Lean: a watchdog on `status_checked_at` (UI warns if "last checked > 3× cadence"). Not v1 required, but worth flagging.
