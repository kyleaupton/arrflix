# Story 3 — Pending approval: two requests, one approved, one denied

**Status:** Draft

Two requesters submit two requests within seconds of each other. Neither has auto-approve for the relevant tier — Friend doesn't have `requests.auto_approve:movie:4k`, Cousin is a brand-new user with no auto-approve granted yet. Both land in `pending`. Admin gets notified, opens the approval queue, sees both with pre-flight context, approves Friend's 4K request, denies Cousin's with a reason. The approved request flows through a compressed Story 1 pipeline at 4K. Cousin gets an in-app notification with the denial reason.

This is the **approval-queue story** — the first one to exercise the human-in-the-loop bottleneck Story 1 deliberately bypassed. It validates the admin-side queue UX, both decision paths (approve and deny), denial-reason flow back to the requester, and the visibility-scoping rules that decide who sees what.

Follows the [Story 1](./01-happy-path-auto-approve.md) template.

## Cast

- **Friend** — familiar from prior stories. Updated config for this story:
  - `requests.create:movie:hd: true`, `requests.create:movie:4k: true`
  - `requests.auto_approve:movie:hd: true`, **`requests.auto_approve:movie:4k: false`**
  - PWA installed, push enabled.
- **Cousin** — new user, joined via invite three days ago. Default permissions:
  - `requests.create:movie:hd: true`, `requests.create:movie:4k: false`
  - **No auto-approve permissions yet** — admin's onboarding policy gives new users approval-required for 30 days.
  - PWA installed; push subscription registered but only opted into the `my_requests` bundle.
- **Admin** — familiar from [Story 10](./10-indexer-health-degraded-and-recovery.md). Holds `requests.approve` + `requests.deny`. Subscribed to `admin_alerts` bundle on push + in-app per [notifications](../modules/notifications/README.md#preferences-and-bundles) defaults.
- **The system** — Arrflix.
- **External** — TMDB, Prowlarr, qBittorrent, Plex.

## Preconditions

- Neither requested movie is in the library.
- Both movies exist in TMDB.
- Indexers are healthy (this story is not about search failure).
- Downloader has free slots.
- Admin's push subscription is registered.
- Cousin's push subscription is registered but they have not enabled push for any bundles (in-app only).
- No other pending requests in the queue (clean slate).

## Flow

### Phase 1 — Friend submits 4K request (T+0s)

**User-visible:**

- Friend opens PWA, searches "Mickey 17", taps the result.
- Focus page loads. The tier picker shows two options: **HD** (auto-approves) and **4K** (requires approval).
- Friend picks 4K. Below the button, pre-flight text updates: _"This will need admin approval. Typically reviewed within a day."_
- Friend taps "Request". Button morphs to a pill: **"Awaiting approval"** (yellow, not the green spinner of Story 1).
- Toast confirms: "Request submitted — we'll notify you when it's reviewed."

**Behind the scenes:**

- `POST /requests { tmdb_id, type: "movie", tier: "4k" }`
- Request service:
  1. Validates `Friend.requests.create:movie:4k` → ok.
  2. Quota check: under the cap → eligible if permission held (binary quota, iteration 2).
  3. Auto-approve check: `Friend.requests.auto_approve:movie:4k` → **not held** → auto-approve does not fire.
  4. Writes `request` row: `{ id, requester_id: friend, tmdb_id, type: movie, tier: 4k, status: pending, auto_approved: false }`.
  5. Emits `request.pending_review` event ([notifications](../modules/notifications/README.md), audience: `admin`).
  6. Audit row written: `request.submitted { requester, tier }` per [audit](../patterns/audit/README.md).

**Notifications:**

- Push fan-out to all admins per [notifications](../modules/notifications/README.md#three-audiences). In this household: one admin → one outbox row for push, one for in-app.

### Phase 2 — Cousin submits HD request (T+10s)

**User-visible:**

- Cousin (new to the household, exploring the PWA) searches "Longlegs", taps the result.
- Focus page shows HD as the only tier option (Cousin lacks `create:movie:4k`).
- Below the button: _"This will need admin approval (new accounts are reviewed for 30 days)."_
- Cousin taps "Request". Same yellow pill: **"Awaiting approval"**.
- Toast: "Request submitted."

**Behind the scenes:**

- Same flow as Phase 1. Cousin's request lands as `pending`.
- Emits another `request.pending_review` event.

**Notifications (the grouping question):**

- This is the second admin notification in 10 seconds. Per [notifications](../modules/notifications/README.md) dedup, a `dedup_key` like `admin.pending_review_batch` would collapse both into one push if applied within a debounce window. Without it, two pushes fire.
- **Lean per Story 10's experience**: producer-owned batching is the right tool. The Request service emits per-request events, but a small `pending_review_throttle` adapter (similar to hygiene's digest) coalesces multiple events within a short window (say 60s) into a single push: _"2 requests need review."_ The two underlying outbox rows still exist for history; the push channel uses the digest payload.
- See [Open question #1](#open-questions).

### Phase 3 — Admin reviews the queue (T+15 min)

**User-visible (Admin):**

- Admin's phone shows a push: **"2 requests need review"** (or two separate pushes — see [Open question #1](#open-questions)). Tap → opens app to the approval queue.
- The queue view shows two rows:

  | Requester | Title     | Tier | Storage est. | Requester history                                     | Library state  |
  | --------- | --------- | ---- | ------------ | ----------------------------------------------------- | -------------- |
  | Friend    | Mickey 17 | 4K   | ~80GB        | 2 pending, 12 approved, 0 denied (30d)                | Not in library |
  | Cousin    | Longlegs  | HD   | ~12GB        | 0 pending, 0 approved, 0 denied (new — joined 3d ago) | Not in library |

- Each row is tappable for detail. Drill-in shows the full request context: requester notes (none in this story), pre-flight breakdown, _"other requesters who asked for this"_ (none), _"previously denied / cancelled requests for this title"_ (none for Mickey 17; see [Open question #2](#open-questions) for whether cross-user history is shown).

**Behind the scenes:**

- Approval queue is a query against `request WHERE status = 'pending' AND <visible to viewer>`. Visibility per [requests](../modules/requests/README.md#visibility-scoping): admin sees all pending requests.
- Pre-flight data is computed on render: storage estimate from media metadata, requester history from request counts, library state from media_item presence.

### Phase 4 — Admin decides (T+~16 min)

**Friend's 4K request — approved:**

- Admin reviews Friend's request. Familiar requester, healthy history, library has room.
- Admin taps **Approve**. Optional note field. Admin types: _"Approved — try HD next time, 4K is heavy on storage."_
- Confirmation modal: _"Approving Mickey 17 (4K, ~80GB)? Friend will be notified."_
- Admin confirms.

**Behind the scenes (approval):**

- Request service:
  1. Validates admin holds `requests.approve` → ok.
  2. Transactionally: request `status: pending → approved`, writes `approved_by: admin, approved_at: <now>, approver_note: "Approved — try HD..."`.
  3. Spawn step (per [requests](../modules/requests/README.md#mapping-requests-to-downstream-state)): no existing want for `(media_item, 4K)` → create new `want` with tier 4K.
  4. Request transitions: `approved → spawned`, writes `spawned_want_ids: [<id>]`.
  5. Emits `want.created` (wakes AcquisitionWorker), `request.decision_made` event (audience: `user`, recipient: Friend).
  6. Audit rows: `request.approved { actor: admin, note }`, `request.spawned { want_ids }`.

**Cousin's HD request — denied:**

- Admin reviews Cousin's request. New user, unfamiliar with household conventions. Admin doesn't host horror.
- Admin taps **Deny**. Required reason field. Admin types: _"Household doesn't host horror titles. Happy to discuss exceptions — message me. Welcome to the family!"_
- Confirmation modal: _"Deny Longlegs? Cousin will be notified with this reason."_
- Admin confirms.

**Behind the scenes (denial):**

- Request service:
  1. Validates admin holds `requests.deny` → ok.
  2. Transactionally: request `status: pending → denied`, writes `denied_by: admin, denied_at: <now>, denied_reason: "Household doesn't host horror..."`.
  3. Emits `request.decision_made` event (audience: `user`, recipient: Cousin).
  4. Audit row: `request.denied { actor: admin, reason }`.

**Admin's queue:** both rows disappear from `pending`. Queue is now empty. The admins' in-app `request.pending_review` entries **resolve**: on decision, the request service calls `notifications.Resolve("request.<id>.pending_review")`, flipping the active rows to `resolved` (cleared from the bell's attention count, kept as history) and cancelling any not-yet-sent push. Per the [resolution lifecycle](../modules/notifications/README.md#resolvable-notifications-resolution-lifecycle).

### Phase 5 — Friend's approved request flows through (T+~16 min → T+~25 min)

The compressed Story 1 tail at the 4K tier:

- AcquisitionWorker picks up `want.created`. Search → quality gate (against the 4K profile, which has different cutoffs than HD) → score → pick → routing → download_job.
- qBittorrent downloads.
- ImportWorker hardlinks into the library per the 4K name template.
- Plex confirms via webhook → want `available`.

**Notifications:**

- 📱 Push to Friend: **"Mickey 17 (4K) is ready to watch."** Plus an in-app entry: **"Approved by Admin — note: 'try HD next time, 4K is heavy on storage'"** that links to the original request (now `spawned`).
- Friend's pill on the request page: **"Awaiting approval"** → **"Approved • Downloading"** → **"Available on Plex — watch now"**.

### Phase 6 — Cousin's denied request resolves (T+~16 min, same moment)

- 🔔 In-app notification to Cousin: **"Your request for Longlegs was denied. Reason: 'Household doesn't host horror titles. Happy to discuss exceptions — message me. Welcome to the family!'"**
  - No push fires — Cousin opted in only to `my_requests` in-app, not push. (Per the [notifications](../modules/notifications/README.md) preference matrix; the bundle covers `request.decision_made` events.)
  - Even if push were on, the denial is non-urgent; lean is in-app default for denials, push opt-in. See [Open question #4](#open-questions).
- Cousin's "my requests" page shows Longlegs with a red **Denied** tag and the reason inline. The original request is in terminal `denied` state — read-only.

> Dev note: that's a dumb example lol

## Postconditions

- **2 `request` rows**:
  - Friend's: `status: spawned`, `auto_approved: false`, `approved_by: admin`, `approver_note: "..."`, `spawned_want_ids: [<id>]`. Ultimately `fulfilled` semantically (fulfillment state is derived from the spawned want's lifecycle).
  - Cousin's: `status: denied`, `denied_by: admin`, `denied_reason: "Household doesn't host horror..."`. Terminal.
- **1 `want` row** (Friend's, ending `available` per Story 1's tail).
- **0 `want` rows** for Cousin's denied request — denial doesn't spawn anything.
- **1 `media_item`, 1 `media_file`** for Mickey 17 (4K).
- **~6 audit rows**: `request.submitted` × 2, `request.approved`, `request.spawned`, `request.denied`, plus the standard acquisition rows for Friend's want.
- **Notification outbox**:
  - `request.pending_review` × 2 admin events (or 1 batched — see [Open question #1](#open-questions)), delivered.
  - `request.decision_made` to Friend (push + in-app), delivered.
  - `request.decision_made` to Cousin (in-app only), delivered.
  - `want.available` to Friend (push + in-app), delivered.
- **Friend's "my requests" page** shows Mickey 17 as fulfilled with the approver's note attached.
- **Cousin's "my requests" page** shows Longlegs as denied with the reason inline.

## What must be true (foundation requirements)

Most requirements are already declared in [requests](../modules/requests/README.md), [users](../modules/users/README.md), and [notifications](../modules/notifications/README.md). Story-driven additions:

### Approval queue UI

- **Queue view** — list of `pending` requests visible to the viewer per [requests visibility scoping](../modules/requests/README.md#visibility-scoping). Sortable by submitted time (default), tier, requester. Filterable by requester.
- **Per-row pre-flight columns**: requester, title, tier, storage estimate, requester history (counts over 30d), library state. The data per [requests pre-flight visibility](../modules/requests/README.md#pre-flight-visibility) is required.
- **Drill-in detail page** — full request, requester notes, prior request history for this title (lean: across all users for admins, see [Open question #2](#open-questions)), approver UI.
- **Decision actions** — Approve (with optional note) and Deny (with required reason). Both produce confirmation modals.
- **Empty state** — when the queue is empty: "Nothing waiting. Last 5 decisions: …" with quick links to recent admin-action audit entries.

### Notification grouping for pending reviews

- Per Phase 2: the producer side should batch `request.pending_review` events within a short debounce window (lean: 60s) to avoid push storms when multiple requests arrive in close succession.
- Implementation: small adapter at the producer that buffers + emits a single aggregated event after the debounce window. The underlying outbox rows still exist per-request for accurate history.
- Single-request submissions skip batching (fire immediately after a brief wait). Multi-request batches use a different template: "_2 requests need review_" with deep link to the queue.

### Notification resolution on decision

- `request.pending_review` is declared **`resolvable`** with `dedup_key = "request.<id>.pending_review"`. When the request transitions to `approved` or `denied`, the request service calls `notifications.Resolve(dedup_key)`: in-app rows flip to `resolved` (kept as history, dropped from the attention count), and any not-yet-sent push is cancelled. Admins don't see stale "needs review" bell entries for already-decided requests.
- This is the same [resolution lifecycle](../modules/notifications/README.md#resolvable-notifications-resolution-lifecycle) Stories [4](./04-failed-search-recovery.md) and [10](./10-indexer-health-degraded-and-recovery.md) use — three stories, one mechanism, now settled in the notifications spec.

### Decision visibility & notification routing

- Per [requests](../modules/requests/README.md#visibility-scoping): denied request's reason is visible only to the requester + anyone with `requests.view:all` (admins). Cousin sees their reason; another non-admin user would not, even if they could see that a denial occurred.
- Requester's optional submission note (none in this story) follows the same rule — visible to the approver and admins.
- `request.decision_made` event payload includes the decision (`approved` / `denied`), the note/reason, the actor. Template renders only what's appropriate per channel.

### Audit rows

- Per [requests](../modules/requests/README.md#audit): `submitted`, `approved` / `denied`, `spawned` are all separate rows in the decision-artifact stream.
- Per [users](../modules/users/README.md#admin-action-audit): a separate admin-action audit row is **not** written for the per-request decision — those are decision-artifact events. Admin-action audit only fires for changes to permissions, roles, policies (e.g., if Admin granted Cousin `requests.auto_approve:movie:hd` mid-flight).

### Time targets (UX commitments)

- Submission → "Awaiting approval" pill: <1s (same as Story 1's submission target).
- Submission → admin push: <60s typically (within the batching debounce window).
- Approval → `want.created` event: <1s (same transaction).
- Denial → requester in-app notification: <30s.
- The end-to-end human-bottleneck time (submit → decision) is unbounded — pinned at 14–30 days by the request expiration ([requests open question #5](../modules/requests/README.md#open-questions)) before the request auto-expires.

## Out of scope (variant stories)

- **Over-quota rejection** — user holds auto-approve but is at or over their hard quota cap; submission is rejected at submit with a structured 422 (iteration 2 has no soft-threshold band — quota is binary). Brief variant. Pre-flight still shows the running count ("this user is at 4/5 weekly HD") so the wall isn't a surprise.
- **Approve-with-modification** — admin drops the tier from 4K to HD before approving Friend. Currently spec says no, deny-with-reason is the workaround. Variant story would re-evaluate.
- **Auto-expiration** — Friend's 4K request sits in queue for 14+ days because admin doesn't notice. Auto-transitions to `expired` with a notification to Friend. Edge case; brief variant.
- **Cancellation by requester before decision** — Friend cancels their pending 4K request because they decided HD is fine and submitted that separately (which auto-approves). The cancel cascade is in the requests spec; deserves a brief story.
- **Re-request after denial** — Cousin requests Longlegs again 5 minutes later (or 3 days later). Per [requests open question #14](../modules/requests/README.md#open-questions): no model-level gate; pre-flight should warn "you were denied this 3 days ago — sure?". Variant story exercises that warning.
- **Bulk operations** — admin selects 5 pending requests and approves all at once. UX-heavy variant; the model supports it trivially.
- **Multi-admin concurrent decision race** — Admin1 is reading the queue while Admin2 approves the same request. Admin1's decision attempt fails with a 409 (already decided). Edge case; brief variant.
- **Cousin re-requests with explanation** — denial reason invited "discuss exceptions"; Cousin sends a new request with a note explaining why they want it. Exercises the requester-note → approver-context flow that this story sets up but doesn't trigger.
- **Denial cascade for series tracking** — if the request had been for a series and a tracking already existed with other requesters, denying Cousin's request would remove them from the tracking's requester set. Series-specific variant.
- **Awkward edge: quota changes mid-pending** — Friend's request submitted within budget, but by the time admin reviews, Friend has used quota up. Approve still works (the request was within quota at submission and the approver explicitly overrode any check). Worth nailing in the requests spec; not exercised here.
- **Visibility for the queue across non-admin viewers** — a co-admin with `requests.approve:movie` but not `requests.approve` sees only movie requests. Story 03 doesn't exercise scope-qualified approve permissions.

## Open questions

1. **Pending-review notification batching window.** Two requests in 10 seconds: one push or two? Lean: 60-second producer-side debounce, single push within the window with a "_N requests need review_" template, underlying per-request outbox rows preserved for history. Pin in [notifications](../modules/notifications/README.md).

2. **Cross-user prior-request history in the drill-in.** When an admin reviews Cousin's request for Longlegs, should the detail page show that Friend has _also_ previously requested Longlegs (and was approved/denied/cancelled)? Lean: yes for admins (it's useful context), no for non-admin co-approvers (privacy across users). The requests spec touches this in [pre-flight visibility](../modules/requests/README.md#pre-flight-visibility) but doesn't pin who sees what across users.

3. **Notification resolution on decision — resolved.** Option (c): the `pending_review` rows flip to `resolved` (kept as history, cleared from the attention count) via the [notifications resolution lifecycle](../modules/notifications/README.md#resolvable-notifications-resolution-lifecycle), not auto-dismissed (which would lose history). Same mechanism as Stories 4 and 10. The remaining nicety — whether the resolved entry reads "resolved: approved by you" with the deciding actor — is a payload/template detail, not a mechanism question.

4. **Denial push policy.** Denial is non-urgent and potentially face-saving; pushing it feels heavy. Lean: in-app default, push opt-in. Approval pushes (the good news) stay on by default. Pin in [notifications](../modules/notifications/README.md) bundle defaults for `request.decision_made`.

5. **Denial reason templates / shortcuts.** Admins denying frequently want canned reasons ("library full", "indexer coverage poor", "household policy"). Lean: small per-install set of reason templates, free-text override per decision. Defer to v2 if implementation cost is high.

6. **Approver-note visibility on approval.** Friend sees the approver's note "_try HD next time_" inline on their request page. Is the same note visible to other users who can see the request (e.g., another co-admin browsing fulfilled requests)? Lean: yes for admins, no for non-admin co-viewers. Symmetric with denial reason visibility.

7. **What "fulfilled" means for an approved-then-spawned request.** Per the request lifecycle the request is `spawned` once the want is created — the want then has its own lifecycle. The UI says "fulfilled" once the want reaches `available`. Is that derived in the UI, or does the request entity grow a `fulfilled_at` timestamp updated by the want? Story 1's open question #1 flagged this; Story 3 doesn't add new information but inherits the ambiguity.

8. **Cousin's request expiration.** If admin had _not_ acted, Cousin's request would auto-expire. Per [requests open question #5](../modules/requests/README.md#open-questions), the window is 14 or 30 days. For a brand-new user this might want to be shorter (so they get fast feedback). Per-user-policy override is the model; pin the default + per-policy behavior.

9. **First-decision UX for new admins.** This is potentially the admin's first manual decision in the app. Pre-flight columns and the approve/deny flow should be self-explanatory; consider a brief one-time intro overlay ("Approving sends X to the user; denying requires a reason"). UX detail; not blocking.

10. **Audit row for "decision made on behalf of household."** When an admin denies on a household-policy basis ("we don't host horror"), the audit row captures the reason text. Worth indexing/searching this in the activity view for cross-decision pattern visibility ("admin has denied 3 horror requests this month — maybe add a household preference filter at submit time?"). Lean: nice-to-have; not v1 required, but the audit row supports the future feature.
