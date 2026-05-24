# Requests — the user-facing intent layer

**Status:** Draft, iteration 1

A request is what a user says when they want something added to the library. It is **not** the work itself, and not the long-term library state — it's the intent statement, gated by approval, that translates into the system's actual machinery ([tracking](../tracking/README.md) for series, [wants](../acquisition/README.md) for movies). This doc covers the request entity, lifecycle, approval flow, intent flags, quota enforcement, and the seams to tracking, acquisition, and the users/permissions spec.

The request entity is deliberately thin. The richness comes from a small set of intent flags + smart defaults + a presets layer over those flags, designed to beat Overseerr's rigidity without exposing complexity to the 80% case.

## TL;DR

- A **request** is a per-user intent statement: "I want this media at this tier, with these lifecycle preferences." It is **distinct** from the library state (tracking) and the work items (wants).
- One entity, a small set of intent flags: **`retention`**, **`tier_floor` / `tier_ceiling`**, **`scope`** (series only), **`monitor_future`** (series only). Defaults cover the 80% case; presets ("Watch this weekend", "I'm a fan") cover the next 15%.
- Lifecycle: `pending → approved → spawned`, _or_ `pending → denied / cancelled / expired`. After `spawned`, downstream state lives on tracking/wants — the request itself becomes a frozen artifact.
- Approval is permission-driven: `requests.auto_approve:<type>:<tier>` auto-approves at request time; otherwise the request waits for someone holding `requests.approve` (see [users](../users/README.md#permissions)).
- Quotas live on `user_policy` from the [users spec](../users/README.md#user_policy). Pre-flight visibility ("this will be your 3rd of 5 movies this week") is a first-class affordance, not an afterthought.
- Multi-requester semantics are **owned by tracking** for series and by the want for movies; requests stay 1:1 with users.
- Auto-approve is not a free pass: a soft-gating band can flip an auto-eligible request to manual review when quota thresholds are approached.
- **Hardlink-aware retention** is the killer differentiator: the same file can serve a `watch_once` request from one user and a `keep_forever` request from another, simultaneously.

## Why this is its own spec

Three concerns sit just adjacent to requests but don't belong inside them:

- **Identity, roles, permissions, quotas** — owned by [users](../users/README.md). Requests consume the permission vocabulary (`requests.create:*`, `requests.approve`, `requests.auto_approve:*`) and quota envelope but do not define them.
- **The ongoing-intent primitive** — owned by [tracking](../tracking/README.md). A series request _spawns_ tracking; it does not _replace_ it. Series scope semantics, multi-requester union, smart scheduling all live on tracking.
- **The work itself** — owned by [acquisition](../acquisition/README.md). Approval produces a want (movie) or a tracking + wants (series); from there it's the pipeline's problem.

Pulling requests out as its own spec makes those seams explicit and lets each spec stay coherent. Without this split, the users spec balloons (auth, RBAC, quotas, request lifecycle) and the tracking spec inherits approval UX it shouldn't care about.

## The request primitive

A request encodes a single user's intent for a single media item at a single moment in time. Concretely:

- **Subject** — a reference to a media item (movie or series). For series, the request may optionally narrow to a specific season or episode via the `scope` flag; the request entity itself is uniform, granularity is a property of its flags.
- **Requester** — the user submitting the request.
- **Tier** — the requested quality tier (e.g., `hd`, `4k`), validated against the requester's `requests.create:<type>:<tier>` permission.
- **Intent flags** — see [Intent flags](#intent-flags) below.
- **Notes** — optional free-text reason from the requester; optional free-text reason from the approver on approve or deny.
- **Status** — see [Lifecycle](#lifecycle).
- **Timestamps** — created, decided, spawned, terminal.
- **Decision metadata** — who approved or denied, when, why; whether the decision was automatic or manual.
- **Spawn links** — references to the tracking and/or want(s) produced when this request was approved.

What a request is _not_:

- It is not the library state. ("Does the library have this at 4K?" is a question for [tracking](../tracking/README.md) / media-item state, not for any request.)
- It is not the work item. ("Why hasn't this downloaded yet?" is a question for [wants](../acquisition/README.md), not for the originating request.)
- It is not perpetual. Once a request has spawned tracking or a want, the request is frozen. Subsequent changes — cancel monitoring, upgrade tier, swap scope — act on tracking/wants, not on the original request.

## Intent flags

The flags are what make the system flexible without piling on entity types. Most users never set them by hand; they're exposed through [presets](#intent-presets). The four flags:

| Flag                              | Meaning                                                                                                                                                                       | Default                            | Applies to |
| --------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------- | ---------- |
| **`retention`**                   | How long the content should stay in the library after fulfillment                                                                                                             | `keep_forever`                     | All        |
| **`tier_floor` / `tier_ceiling`** | Acceptable range of quality tiers; the system aims for `ceiling`, will accept `floor`                                                                                         | `floor = ceiling = requested tier` | All        |
| **`scope`**                       | For series, which episodes are wanted (defers to [tracking](../tracking/README.md#scope-rule--overrides) for the rule grammar)                                                | `all`                              | Series     |
| **`monitor_future`**              | For series, whether to subscribe to future episodes as they air                                                                                                               | `true` if scope includes future    | Series     |

### `retention`

The defining flag — this is what makes "watch this weekend" and "I'm a fan of this show" the same entity with different settings. Values:

| Value                 | Meaning                                                                                                                                                                            |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `keep_forever`        | Default. Once acquired, the file stays in the library until manually removed.                                                                                                      |
| `cleanup_after_watch` | The file stays until the requester has watched it (via Plex/Jellyfin watch-state), then is eligible for cleanup.                                                                   |
| `keep_for_days(N)`    | The file is eligible for cleanup N days after fulfillment, watched or not.                                                                                                         |
| `pinned`              | Like `keep_forever`, but additionally exempt from tier-downgrade and storage-pressure cleanup heuristics. Explicit user-pinned.                                                    |

> **Watch-state coupling, by design.** `cleanup_after_watch` is the one retention mode that genuinely needs a media server — "watched" is only knowable from the player (Plex/Jellyfin watch-state). This coupling is legitimate and unavoidable; it degrades gracefully (no media server → it never fires, behaving like `keep_forever`, optionally paired with `keep_for_days(N)`). Note this is _watch-state_ coupling, not _availability_ coupling: a file being **in the library** is Arrflix's own truth and never depends on a media server (see [acquisition → Media-server propagation](../acquisition/README.md#media-server-propagation-decoupled-from-available)).

**Hardlink-aware multi-retention.** The same underlying file can be hardlinked by multiple requests with different retention policies. Cleanup runs per-request; a file is eligible for actual deletion only when _all_ requests pointing at it have been satisfied or expired. This is unique to Arrflix — Overseerr can't model "watch once" alongside "keep forever" for the same media because they don't own the filesystem strategy. It's also the conceptual underpinning for the [hygiene](../hygiene/README.md) cleanup worker, which reads request retention state to decide what's safe to remove.

The request itself runs no code. Cleanup eligibility is a function the hygiene worker evaluates over the request's flags + watch state + age.

### `tier_floor` / `tier_ceiling`

Most requests pin both to the same tier ("I want HD"). The split exists for two cases:

1. **Graceful degradation** — "I'd love 4K, but HD is fine if 4K isn't around." `floor=hd, ceiling=4k`. The acquisition pipeline tries the ceiling first; if nothing meets quality-profile criteria, falls back toward the floor.
2. **Auto-upgrade** — paired with tracking's `upgrade_behavior`. If `ceiling > current library tier`, future releases at the ceiling can replace the current file. Owned by tracking; the request just expresses the preference.

Both values are validated at submit time against `requests.create:<type>:<tier>`. The ceiling drives the permission check; the floor only needs the lower permission. (Asking for a 4K-ceiling when you can't request 4K → submission rejected.)

### `scope` (series only)

Pulled from tracking's [preset list](../tracking/README.md#scope-rule--overrides). The request declares its scope preference; if the request spawns _new_ tracking, that becomes the tracking's scope rule. If the request _joins_ existing tracking, the requester's scope is unioned in per the [multi-requester semantics](../tracking/README.md#multi-requester-semantics). The request stores its own (pre-union) scope, not the effective one.

### `monitor_future` (series only)

Strictly speaking, this is `scope: all` vs. `scope: <bounded>`. We surface it as a separate flag because "keep monitoring for new episodes" is the question users ask, not the underlying scope-rule choice. Internally it collapses to a scope decision on tracking.

## Intent presets

The flags are powerful but most users should never see them. The UI surfaces a small set of named presets, each of which pins the underlying flags. Suggested starter set:

| Preset                | retention             | tier                 | scope (series) | monitor_future |
| --------------------- | --------------------- | -------------------- | -------------- | -------------- |
| "Watch this weekend"  | `cleanup_after_watch` | requested tier       | n/a            | n/a            |
| "Add to library"      | `keep_forever`        | requested tier       | `all`          | true           |
| "I'm a fan"           | `keep_forever`        | floor=hd, ceiling=4k | `all`          | true           |
| "Just this season"    | `keep_forever`        | requested tier       | `season(N)`    | false          |
| "Pilot only"          | `keep_forever`        | requested tier       | `pilot`        | false          |
| "Advanced…"           | (user customizes)     | (user customizes)    | (user customizes) | (user customizes) |

Presets are UI sugar over the flag combinations. The server-side request stores the **resolved flags**, not the preset name — presets are not stable identifiers, flags are. A future preset rename or removal doesn't break historical requests.

## Lifecycle

```
                    auto-approve permission
                    + within quota envelope
                ┌──────────────────────────────┐
                │                              ▼
        ┌──────────┐                     ┌──────────┐    spawn       ┌──────────┐
  ──►   │  pending │ ──────approve───►   │ approved │ ──────────►    │ spawned  │
        └────┬─────┘                     └──────────┘                └──────────┘
             │                                                              │
             ├──────deny──────►   ┌────────┐                                 │
             │  (approver)        │ denied │                                 │
             │                    └────────┘                                 │
             │                                                               │
             ├──────cancel────►   ┌───────────┐                              │
             │  (requester)       │ cancelled │                              │
             │                    └───────────┘                              │
             │                                                               │
             └──────expire───►    ┌─────────┐                                │
                 (timeout)        │ expired │                                │
                                  └─────────┘                                │
                                                                             │
                                ┌────────────────────────────────────────────┘
                                │
                                ▼  (read-only artifact)
                          tracking and/or wants
                          carry downstream state
```

**State semantics:**

- **`pending`** — submitted, awaiting decision. The only state in which a request mutates.
- **`approved`** — decision made (manual or auto); the request is committed, but the spawn step (creating tracking / want) has not completed yet. Brief intermediate state. Approved-but-not-spawned exists so the spawn step is atomic and observable in audit.
- **`spawned`** — tracking and/or wants now exist; their IDs are recorded on the request. The request is read-only history.
- **`denied`** — decision: no. Carries a reason (visible to the requester).
- **`cancelled`** — requester withdrew before a decision was made. Terminal.
- **`expired`** — pending request timed out (configurable, see [Open questions](#open-questions)). Terminal.

**Important:** the request entity does **not** track download / import / availability state. Once spawned, those questions belong to the want lifecycle ([acquisition](../acquisition/README.md)). The UI can _join_ a request to its spawned wants for display ("your request is now downloading"), but the request itself is frozen.

**Reversibility:**

- `approved → spawned` is automatic and irreversible. To undo a fulfilled request, cancel the resulting tracking/wants.
- `denied → pending`: admin re-opens. See [open question #4](#open-questions); lean is to require a new request linked back to the original.
- `cancelled` and `expired` are terminal.

## Approval

Approval is the gate between `pending` and `approved`. Two paths.

### Auto-approval

If the requester holds `requests.auto_approve:<type>:<tier>` at request time **and** the request passes [quota gating](#quota-gating), the request is created and transitioned `pending → approved → spawned` in one transaction. The audit row records the decision as automatic.

Auto-approve is **per-tier**: a user can have `auto_approve:movie:hd` but require manual review for `:4k`. This is the killer differentiator vs. Overseerr's single-flag auto-approve.

### Manual approval

If auto-approve does not fire, the request sits in `pending`. Any user holding `requests.approve` (or a scope-qualified variant) can transition it to `approved` or `denied`, with an optional reason.

Approver permissions (defined in [users](../users/README.md#permissions)):

- `requests.approve` — approve any pending request
- `requests.approve:movie` / `requests.approve:series` — approve by media type
- `requests.deny` — deny any pending request

The approve / deny split exists because some admin patterns want different sets of users gating each side. Deny is the heavier action (it ends the request); approve is the routine action.

**Approve-with-modification** — out of scope for v1. (Approvers can deny with a reason like "request again at HD" rather than silently mutating intent.)

### Quota gating

Auto-approval has a third condition beyond the permission: the request must fit within the requester's quota envelope defined in their `user_policy` (see [users](../users/README.md#user_policy)).

The quota check is not binary. Three outcomes:

| Condition                                                              | Outcome                                                                                          |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| Under all applicable quotas                                            | Auto-approve fires (assuming permission held)                                                    |
| Within soft-threshold band (e.g., 70–100% of any quota)                | Auto-approve **does not fire**, request falls into manual review even with permission held      |
| Over any hard quota                                                    | Submission rejected with a structured 422 (see [errors](../../patterns/errors/README.md))        |

The soft-threshold band is configurable per `user_policy`. This gives admins a way to let power users self-serve normal volume while keeping themselves in the loop on outliers — no all-or-nothing trust decision.

Quota dimensions, reset windows, and "what counts against quota" are defined in the users spec under [user_policy](../users/README.md#user_policy).

## Mapping requests to downstream state

Approval doesn't acquire content; it _spawns_ the entities that do.

### Movies

A movie request's spawn step:

1. Resolve or create the `media_item` row for the movie (existing matching pipeline).
2. Check whether an open want exists for this `(media_item, tier)`.
   - **Yes** — link this request to the existing want; the user joins the want's multi-requester set. No new want is created.
   - **No** — create a new want with the request's tier.
3. Record audit rows.

The user sees "1 other person is also waiting on this" in their UI when joining an open want.

### Series

A series request's spawn step:

1. Resolve or create the `media_item` row for the series.
2. Check whether tracking exists for this series.
   - **Yes** — the requester joins the existing tracking's requester set per [multi-requester semantics](../tracking/README.md#multi-requester-semantics). The requester's scope is unioned in. Tracking's effective `upgrade_behavior` may shift based on the new tier_ceiling.
   - **No** — create a new tracking record with this request's scope, tier, and monitor preferences.
3. Tracking's scheduler eventually emits wants per [tracking](../tracking/README.md).
4. Record audit rows.

### Cancellation cascade

When a request is cancelled (or hypothetically re-denied):

- **Movie request linked to a want** — the user is removed from the want's requester set. If they were the last requester, the want is cancelled.
- **Series request linked to tracking** — the user is removed from the tracking's requester set per tracking's leaving semantics. The tracking may pause or narrow scope.

The request stays in the database as audit history; the cascade only touches downstream entities.

### Watch state and retention

For requests with `retention=cleanup_after_watch`, the hygiene cleanup worker watches for the requester's watch-state event from Plex/Jellyfin, marks the request as `satisfied` (a sub-state on `spawned`, not a separate lifecycle state), and includes the underlying file in its cleanup-eligibility evaluation. The file is removed only when all requests on it agree it's safe to remove.

## Multi-requester semantics — owned elsewhere

The requests spec does **not** define how multiple requesters merge. That belongs to tracking (for series) and to the want (for movies). Requests stay 1:1 with the user who submitted them; downstream entities are the deduplication boundary.

What requests owns:

- The per-user artifact ("here's what I asked for, when, why")
- The per-user audit and quota accounting
- The intent flags _before_ they're merged with anyone else's

What requests does not own:

- The merged scope ("the library wants seasons 1–3 because Alice asked for S1 and Bob asked for S2–3")
- The effective tier ceiling across requesters
- The "who's still wanting this?" lifecycle

Different requesters can have different intents pointing at the same downstream entity. The downstream entity arbitrates between them.

## Visibility scoping

Request visibility is governed by the [users spec activity-scoping predicate](../users/README.md#activity-visibility-scoping). Concretely:

- A user always sees their own requests across all states.
- A user with `requests.view:all` sees every request system-wide (the admin / co-admin view).
- A user with `requests.view:org` sees requests within their org (the family / group view, if orgs exist).
- A denied request's _reason_ is visible only to the requester and to anyone with `requests.view:all`. (Privacy: avoid leaking "Alice was denied because she asked for too much 4K" to siblings.)
- The requester's _notes_ on a request are visible to anyone who can view the request; the approver's notes follow the same rule.

The default user-facing surface is the requester's own request list. "All requests" is a deliberate admin view, not the home page.

## Mutability

The request entity is sparse on mutation paths to keep the audit trail clean.

- **While `pending`**: requester may edit notes, change tier (re-validated against permissions and quotas), change retention, change scope (series), or cancel. Approver may approve or deny.
- **After `approved` / `spawned`**: read-only. To change downstream behavior, act on tracking/wants directly.
- **`denied`, `cancelled`, `expired`**: read-only, terminal.

Any mutation while `pending` resets the auto-approve check. Changing the tier from HD to 4K after submission may flip a request from auto-eligible to manual-required (and vice versa).

## Audit

Requests are decision artifacts: every approve, deny, auto-approve, and cancel is a structured event. They flow into the [decision-artifact audit pattern](../../patterns/audit/README.md).

Per-request audit rows:

- `submitted` — initial creation, with the requester and resolved flags
- `mutated` — pending-state edits (tier change, scope change, notes)
- `auto_approved` / `approved` / `denied` — the decision, with actor and reason
- `spawned` — references the resulting tracking and/or want IDs
- `cancelled` — requester withdrew
- `expired` — system timeout fired

Admin-action audit (separate stream, owned by [users](../users/README.md#admin-action-audit)):

- A user's quota policy is changed → logged
- A user's `requests.auto_approve:*` permission is granted or revoked → logged
- A user's pending requests are bulk-denied (e.g., during user suspension) → logged

The two streams answer different questions: decision audit answers "why was this request approved?", admin audit answers "why does this user have auto-approve for 4K?".

## Pre-flight visibility

A first-class feature, not an afterthought. Submission and approval surfaces must show:

**For the requester (before submit):**

- Quota state ("this will be your 3rd of 5 movies this week")
- Storage estimate ("~8GB based on tier and runtime")
- Auto-approve preview ("this will be approved automatically" / "this will need admin review because…")
- Duplicate detection ("you already requested this in HD last month"; "Bob also requested this last week")
- Library state ("we already have season 1 at HD — this would add seasons 2-5")

**For the approver (before decide):**

- Requester's recent request history ("Alice has 2 pending, 4 approved this month, 1 denied")
- Storage impact ("this series is ~120GB across all seasons")
- Other open requests for the same media ("Bob also requested this in HD")
- Current quota state for the requester
- Library state, as above

Overseerr's UX makes you guess. Ours shows the math. The data is all derivable; the work is in the UX.

## What requests does NOT own

- The acquisition pipeline ([acquisition](../acquisition/README.md))
- Tracking semantics, multi-requester merge, scope-rule grammar ([tracking](../tracking/README.md))
- The permission vocabulary, role catalog, user_policy schema, activity-scoping predicate ([users](../users/README.md))
- The quality-profile catalog and tier registry ([quality profiles](../quality-profiles/README.md))
- The decision-artifact schema ([audit pattern](../../patterns/audit/README.md))
- Cleanup execution and storage-pressure heuristics ([hygiene](../hygiene/README.md))
- Notification delivery (lives in [notifications](../notifications/README.md))
- Library state ("is this in the library?") and want state ("has it downloaded?") — both joined for UI but not owned here
- Watchlist semantics ("I might want this someday") — separate concept, deliberately not in this spec

## Interactions

| Neighbor                                              | How requests interact                                                                                                                |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| **[Users / permissions](../users/README.md)**         | Consumes `requests.*` permissions and `user_policy` quotas. Subject to the activity-scoping predicate.                               |
| **[Tracking](../tracking/README.md)**                 | Series request approval spawns or joins tracking. Cancellation removes the requester from tracking's set.                            |
| **[Acquisition / wants](../acquisition/README.md)**   | Movie request approval spawns or joins a want. Series wants flow through tracking.                                                   |
| **[Quality profiles](../quality-profiles/README.md)** | Tier names come from the tier registry. The request's `tier_ceiling` / `tier_floor` map to profile selection at spawn time.          |
| **[Audit pattern](../../patterns/audit/README.md)**   | Every decision (auto-approve, approve, deny, cancel, expire) writes a structured audit row.                                          |
| **[Errors](../../patterns/errors/README.md)**         | Submission and approval errors use the typed-error model (forbidden, conflict, quota-exceeded, validation).                          |
| **[Metadata](../metadata/README.md)**                 | Subject resolution at submit time goes through the media-item / TMDB sync surfaces.                                                  |
| **[Hygiene](../hygiene/README.md)**                   | Reads request retention flags + watch state when deciding cleanup eligibility. Hardlink-aware multi-retention is enforced here.      |
| **[Notifications](../notifications/README.md)**       | Emits events on pending-needs-review, decision-made, request-fulfilled.                                                              |

## Open questions

1. **Watchlist as a separate concept.** Users want to bookmark "I might want this someday" without actually requesting. Is a watchlist a `pending` request that never gets approved, a separate entity, or just a UI affordance over media-item state? Lean: separate concept, not in this spec.
2. **`pinned` retention.** Worth modeling as a separate value or fold into `keep_forever`? The distinction is whether tier-upgrade / storage-pressure cleanup heuristics can act on the file. Lean: keep separate.
3. **Approve-with-modification.** Should approvers be able to drop the tier or scope before approving ("approve, but at HD not 4K")? Currently out of scope; deny-with-reason is the workaround. Reconsider if real users hit this repeatedly.
4. **Re-opening a denied request.** Model as a state transition (`denied → pending`) or require a new request linked to the original via a `re_request_of` back-reference? Lean: new request with back-reference — cleaner for audit.
5. **Expiration window.** Default for pending requests sitting un-decided. Probably 14 days or 30 days, configurable per `user_policy`. Pin in iteration 2.
6. **Soft-threshold band shape.** Single percentage (e.g., 70–100%)? Per-quota override ("over 80% of weekly movies but not yet over storage")? Lean: single percentage to start, complicate if needed.
7. **Bundle requests / movie nights.** "Approve these 3 movies as one decision." Useful UX, but adds entity complexity. Defer to iteration 2 unless real demand surfaces.
8. **Group / shared requests.** "Movie night for 4 people — bills against the booker's quota, notifies all four when ready." Probably v2; want to confirm the data model leaves room.
9. **Tier upgrade as a request.** If Alice has HD and asks for 4K, is that a new request (with `tier_ceiling=4k` and duplicate-detection flagging it as an upgrade) or a mutation of the existing? Lean: new request, with pre-flight surfacing the upgrade intent. Worth sanity-checking against real flows.
10. **Cancellation race.** Requester cancels at the moment auto-approve is mid-flight. Lean: optimistic concurrency, cancelled-takes-precedence — but pin the rule explicitly in iteration 2.
11. **Notes visibility.** Approver's denial reason is visible to requester. Requester's submission note — visible to approver only, or to other viewers? Lean: approver only by default.
12. **Bulk operations.** Bulk approve, bulk deny, bulk cancel. Cosmetic API additions on top of the model; defer until UI demands.
13. **What "watched" means for shared retention.** If Alice's request is `cleanup_after_watch` and Bob's is `keep_forever`, the file stays. But what if Alice and Bob both have `cleanup_after_watch` — does the file go after _either_ watches, or _both_? Lean: both. Pin in iteration 2.
14. **Re-request after denial cooldown.** Prevent a user from spamming the same denied request? Lean: not in the model — handle as a soft-gate in pre-flight ("you were denied this 2 days ago; sure you want to ask again?").

## What we're explicitly not deciding here

- Exact table names, columns, indexes, constraints
- API endpoint shapes, request/response formats, status code matrices
- The quota schema on `user_policy` (lives in users spec)
- The cleanup worker's implementation, scheduling, and storage-pressure heuristics (lives in hygiene)
- Notification routing rules and delivery channels (lives in [notifications](../notifications/README.md))
- UI component layouts, preset copy, error wording
- Backfill / migration ordering relative to the users spec rollout
- Relative ordering of pre-flight checks (permission → quota → duplicate → submit) — the spec mandates all of them, not their order

## Doc neighbors

- [Users](../users/README.md) — identity, roles, permissions, `user_policy`, activity-visibility scoping
- [Tracking](../tracking/README.md) — series ongoing-intent primitive, multi-requester semantics
- [Acquisition](../acquisition/README.md) — the pipeline that turns approved requests into library files
- [Quality profiles](../quality-profiles/README.md) — tier registry, profile selection
- [Hygiene](../hygiene/README.md) — reads retention flags and watch state to drive cleanup
- [Audit pattern](../../patterns/audit/README.md) — decision-artifact stream this spec writes into
- [Errors](../../patterns/errors/README.md) — typed error model for submission/approval failures
- [Story 1](../../stories/01-happy-path-auto-approve.md) — pressure-tests this spec end-to-end
- [Notifications](../notifications/README.md) — surfaces request events to users
