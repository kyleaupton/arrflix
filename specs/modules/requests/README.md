# Requests — the user-facing intent layer

**Status:** Draft, iteration 2

A request is what a user says when they want something added to the library. It is **not** the work itself, and not the long-term library state — it's the intent statement, gated by approval, that translates into the system's actual machinery: a [tracking](../tracking/README.md) record (single-atom for a movie, scoped for a series) that produces [wants](../acquisition/README.md). This doc covers the request entity, lifecycle, approval flow, the two things a requester actually chooses, quota enforcement, and the seams to tracking, acquisition, and the users/permissions spec.

The request entity is deliberately thin. A requester makes at most two choices — **which tier** and (for a series) **how much of it** — and everything else is a smart default or owned by a system that actually understands the trade-off.

> **What changed in iteration 2.** Iteration 1 piled lifecycle preferences onto the request: a `retention` flag (`keep_forever` / `cleanup_after_watch` / …), a `tier_floor`/`tier_ceiling` range, a `monitor_future` toggle, and a presets layer to hide all of it. Those are gone. **Retention is no longer a request concern** — it's a library/storage policy owned by [hygiene](../hygiene/README.md), which is what gets media-server watch-state out of the request path entirely (it becomes one optional, additive cleanup rule). The tier *range* collapses to a single tier; `monitor_future` folds into scope; presets disappear because there's no longer enough complexity to hide. See [Where retention went](#where-retention-went).

## TL;DR

- A **request** is a per-user intent statement: "I want this media, at this tier" (+ for a series, "this much of it"). It is **distinct** from the library state (tracking) and the work items (wants).
- A requester chooses **at most two things**: **`tier`** (a single value from the admin's tier catalog) and, for series, **`scope`**. Both have sane defaults; many requests are a single tap.
- Lifecycle: `pending → approved → spawned`, _or_ `pending → denied / cancelled / expired`. After `spawned`, downstream state lives on tracking/wants — the request itself becomes a frozen artifact.
- Approval is permission-driven: `requests.auto_approve:<type>:<tier>` auto-approves at request time; otherwise the request waits for someone holding `requests.approve` (see [users](../users/README.md#permissions)).
- Quotas live on `user_policy` from the [users spec](../users/README.md#user_policy) and are a **binary hard cap**: under the cap proceeds, over the cap is rejected at submit. Pre-flight visibility ("this will be your 3rd of 5 movies this week") is a first-class affordance.
- Multi-requester semantics are **owned by tracking** for both movies and series (tracking is the dedup boundary); requests stay 1:1 with users.
- **Retention / cleanup is not here.** A request says *what to acquire*, not *how long to keep it*. Keeping and cleanup are a library-wide policy owned by [hygiene](../hygiene/README.md).

## Why this is its own spec

Three concerns sit just adjacent to requests but don't belong inside them:

- **Identity, roles, permissions, quotas** — owned by [users](../users/README.md). Requests consume the permission vocabulary (`requests.create:*`, `requests.approve`, `requests.auto_approve:*`) and quota envelope but do not define them.
- **The ongoing-intent primitive** — owned by [tracking](../tracking/README.md). A request (movie or series) _spawns_ tracking; it does not _replace_ it. Scope semantics, multi-requester union, smart scheduling all live on tracking.
- **The work itself** — owned by [acquisition](../acquisition/README.md). Approval produces a tracking, which produces the want(s); from there it's the pipeline's problem.

Pulling requests out as its own spec makes those seams explicit and lets each spec stay coherent. Without this split, the users spec balloons (auth, RBAC, quotas, request lifecycle) and the tracking spec inherits approval UX it shouldn't care about.

## The request primitive

A request encodes a single user's intent for a single media item at a single moment in time. Concretely:

- **Subject** — a reference to a media item (movie or series). For series, the request may narrow via `scope`; the request entity itself is uniform, granularity is a property of its scope.
- **Requester** — the user submitting the request.
- **Tier** — the requested quality tier (see [Tier](#tier)). Validated against the requester's `requests.create:<type>:<tier>` permission.
- **Scope** (series only) — see [Scope](#scope-series-only).
- **Notes** — optional free-text reason from the requester; optional free-text reason from the approver on approve or deny.
- **Status** — see [Lifecycle](#lifecycle).
- **Timestamps** — created, decided, spawned, terminal.
- **Decision metadata** — who approved or denied, when, why; whether the decision was automatic or manual.
- **Spawn links** — references to the tracking and/or want(s) produced when this request was approved.

What a request is _not_:

- It is not the library state. ("Does the library have this at 4K?" is a question for [tracking](../tracking/README.md) / media-item state, not for any request.)
- It is not the work item. ("Why hasn't this downloaded yet?" is a question for [wants](../acquisition/README.md), not for the originating request.)
- It is not a retention policy. ("How long do we keep this?" is a [hygiene](../hygiene/README.md) / library-policy question, not a per-request choice.)
- It is not perpetual. Once a request has spawned tracking or a want, the request is frozen. Subsequent changes — cancel monitoring, upgrade tier, swap scope — act on tracking/wants, not on the original request.

## What the requester chooses

There are only two knobs, and most requests touch neither (they accept the defaults).

### Tier

The requested quality tier — a single value from the admin-curated tier catalog ([quality profiles](../quality-profiles/README.md#tiers)). A tier is a human-meaningful label ("HD", "4K", "3D"); it resolves at want-spawn to a profile per media type, and the requester never sees the profile machinery.

- **The tier set is whatever the admin has configured.** A casual install has one tier (`HD`) and the request UI shows *no* tier selector at all — one option is no choice. An enthusiast install adds `4K`; a home-theater install might add `3D`. New device needs become new admin-defined tiers, not new request concepts.
- **Permission-gated.** The tier is validated at submit against `requests.create:<type>:<tier>`. Asking for a tier you can't request → submission rejected. (Story 3 owns the tier-mismatch UX — hide vs. disabled-with-reason vs. route-to-approval.)
- **Default** — the tier catalog's default tier (typically `HD`).

Note the deliberate omissions: there is no `tier_floor`/`tier_ceiling` *range* on the request. "I'd take HD but prefer 4K" is **upgrade behavior**, owned by [tracking](../tracking/README.md) (`upgrade_behavior`) and the [profile](../quality-profiles/README.md), not a request-time knob. And there is no "give me both 4K and HD" — that's two requests (and runs into the unsolved [multiple-versions question](#open-questions)).

### Scope (series only)

For a series, how much of it the requester wants. Pulled from tracking's [preset list](../tracking/README.md#scope-rule--overrides), surfaced to the requester as a short choice:

- **Whole series** (default) — every episode that exists or will exist. This *includes future episodes* — there is no separate `monitor_future` flag; "keep getting new episodes" is just what "whole series" means.
- **A specific season** — `season(N)`.
- **Pilot only** — to try a show before committing.

Per-episode overrides and the richer scope rules (`future_only`, `latest_season_plus_future`) are **not** request-time choices — they're managed on the [tracking](../tracking/README.md) after spawn, by a user who wants that level of control.

At spawn, the request's scope seeds the requester's [`tracking_requester` row](../tracking/README.md#multi-requester-semantics) — the live, mutable home for their per-requester scope. The request keeps its original scope as a frozen snapshot (audit); the association row is the source of truth thereafter. The tracking's effective scope is the union across all requesters.

## Where retention went

Iteration 1 made `retention` a per-request flag whose marquee value, `cleanup_after_watch`, depended on media-server watch-state to fire. That made Plex/Jellyfin **load-bearing** for a request feature — the one place in the system that violated the rule established everywhere else (a media server enriches and accelerates; it never gates).

Iteration 2 removes retention from the request:

- **Keeping and cleanup are a library-wide policy**, owned by [hygiene](../hygiene/README.md). The operator configures it once (e.g. "propose cleanup of watched movies older than N days"); a casual requester is never asked to reason about a file's eventual deletion.
- **Watch-state becomes purely additive.** Its only consumer is now an *optional* hygiene cleanup rule. With no media server, the rule simply never has a signal to act on — and nothing about requests, tracking, or availability is affected. This is the additive-not-load-bearing posture the rest of the system already follows.
- **The differentiator survives, in the right home.** "Auto-clean watched content" is still something Overseerr can't do (it doesn't own the filesystem). It just moves from a per-request user knob to an operator policy — where storage lifecycle actually belongs.

The hardlink-aware "watch-once for Alice, keep-forever for Bob on the same file" idea is parked for a future iteration; it belongs to the storage/hygiene layer, not the request entity.

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
- **`approved`** — decision made (manual or auto); the request is committed, but the spawn step (creating tracking / want) has not completed yet. Brief intermediate state, kept so the spawn step is atomic and observable in audit.
- **`spawned`** — tracking and/or wants now exist; their IDs are recorded on the request. The request is read-only history.
- **`denied`** — decision: no. Carries a reason (visible to the requester).
- **`cancelled`** — requester withdrew before a decision was made. Terminal.
- **`expired`** — pending request timed out (configurable, see [Open questions](#open-questions)). Terminal.

**Important:** the request entity does **not** track download / import / availability state, and (unlike iteration 1) carries **no post-spawn sub-state** — there is no `satisfied` flag fed back from watch-state, because retention isn't a request concern anymore. Once spawned, all downstream questions belong to the want lifecycle ([acquisition](../acquisition/README.md)). The UI can _join_ a request to its spawned wants for display ("your request is now downloading"), but the request itself is frozen.

**Reversibility:**

- `approved → spawned` is automatic and irreversible. To undo a fulfilled request, cancel the resulting tracking/wants.
- `denied → pending`: admin re-opens. See [open question #3](#open-questions); lean is to require a new request linked back to the original.
- `cancelled` and `expired` are terminal.

## Approval

Approval is the gate between `pending` and `approved`. Two paths.

### Auto-approval

If the requester holds `requests.auto_approve:<type>:<tier>` at request time **and** the request is [under quota](#quota-gating), the request is created and transitioned `pending → approved → spawned` in one transaction. The audit row records the decision as automatic.

Auto-approve is **per-tier**: a user can have `auto_approve:movie:hd` but require manual review for `:4k`. This is the killer differentiator vs. Overseerr's single-flag auto-approve.

### Manual approval

If auto-approve does not fire (no permission, or over quota), the request sits in `pending`. Any user holding `requests.approve` (or a scope-qualified variant) can transition it to `approved` or `denied`, with an optional reason.

Approver permissions (defined in [users](../users/README.md#permissions)):

- `requests.approve` — approve any pending request
- `requests.approve:movie` / `requests.approve:series` — approve by media type
- `requests.deny` — deny any pending request

The approve / deny split exists because some admin patterns want different sets of users gating each side. Deny is the heavier action (it ends the request); approve is the routine action.

**Approve-with-modification** — out of scope for v1. (Approvers can deny with a reason like "request again at HD" rather than silently mutating intent.)

### Quota gating

Auto-approval has a second condition beyond the permission: the request must fit within the requester's quota envelope defined in their `user_policy` (see [users](../users/README.md#user_policy)).

The quota check is **binary** (iteration 1's soft-threshold band is gone — its shape was never settled, and an all-or-nothing cap is enough for v1):

| Condition            | Outcome                                                                                   |
| -------------------- | ----------------------------------------------------------------------------------------- |
| Under the quota cap  | Proceeds. Auto-approves if the auto-approve permission is held; otherwise manual review.   |
| At or over the cap   | Submission rejected with a structured 422 (see [errors](../../patterns/errors/README.md)). |

Quota dimensions, reset windows, and "what counts against quota" are defined in the users spec under [user_policy](../users/README.md#user_policy). The pre-flight surface still shows the running count ("this will be your 3rd of 5 this week") — that affordance is unchanged; only the banding logic is removed.

## Mapping requests to downstream state

Approval doesn't acquire content; it _spawns_ the entities that do.

### Movies

A movie request's spawn step (uniform with series — a movie is a single-atom tracking):

1. Resolve or create the `media_item` row for the movie (existing matching pipeline).
2. Check whether tracking exists for this movie.
   - **Yes** — insert a `tracking_requester` row for this requester (seeded from the request's tier). No new tracking or want is created; the user sees "1 other person is also waiting on this."
   - **No** — create a new single-atom tracking plus the requester's `tracking_requester` row (seeded from the request's tier). The tracking produces the one want.
3. Record audit rows.

### Series

A series request's spawn step:

1. Resolve or create the `media_item` row for the series.
2. Check whether tracking exists for this series.
   - **Yes** — insert a `tracking_requester` row for this requester (seeded from the request) per [multi-requester semantics](../tracking/README.md#multi-requester-semantics); the effective scope recomputes and unions in their scope.
   - **No** — create a new tracking record plus the requester's `tracking_requester` row (seeded from the request's scope and tier).
3. Tracking's scheduler eventually emits wants per [tracking](../tracking/README.md).
4. Record audit rows.

### Cancellation cascade

When a request is cancelled (or hypothetically re-denied):

- **Movie request** — the user is removed from the tracking's requester set. If they were the last requester, the (single-atom) tracking pauses and its in-flight want is cancelled.
- **Series request** — the user is removed from the tracking's requester set per tracking's leaving semantics. The tracking may pause or narrow scope.

The request stays in the database as audit history; the cascade only touches downstream entities.

## Multi-requester semantics — owned elsewhere

The requests spec does **not** define how multiple requesters merge. That belongs to tracking, for both movies and series. Requests stay 1:1 with the user who submitted them; the tracking is the deduplication boundary.

What requests owns:

- The per-user artifact ("here's what I asked for, when, why")
- The per-user audit and quota accounting
- The intent _as originally requested_ (the frozen origin: tier + scope). The _live_ per-requester copy — editable after spawn — lives on the [`tracking_requester` association](../tracking/README.md#multi-requester-semantics), seeded from these.

What requests does not own:

- The merged scope ("the library wants seasons 1–3 because Alice asked for S1 and Bob asked for S2–3")
- The effective tier across requesters
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

- **While `pending`**: requester may edit notes, change tier (re-validated against permissions and quotas), change scope (series), or cancel. Approver may approve or deny.
- **After `approved` / `spawned`**: read-only. To change downstream behavior, act on your [`tracking_requester` association](../tracking/README.md#multi-requester-semantics) — your live per-requester intent (scope, overrides, tier) — not the frozen request. (Want-level actions like cancel act on the wants.)
- **`denied`, `cancelled`, `expired`**: read-only, terminal.

Any mutation while `pending` resets the auto-approve check. Changing the tier from HD to 4K after submission may flip a request from auto-eligible to manual-required (and vice versa).

## Audit

Requests are decision artifacts: every approve, deny, auto-approve, and cancel is a structured event. They flow into the [decision-artifact audit pattern](../../patterns/audit/README.md).

Per-request audit rows:

- `submitted` — initial creation, with the requester, tier, and scope
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
- The quality-profile catalog, tier registry, and tier→profile resolution ([quality profiles](../quality-profiles/README.md))
- The decision-artifact schema ([audit pattern](../../patterns/audit/README.md))
- **Retention and cleanup** — keeping policy, watch-based cleanup, storage-pressure heuristics ([hygiene](../hygiene/README.md)). A request says what to acquire, never how long to keep it.
- **Watch-state** — ingested by [media-server](../media-server/README.md), consumed by hygiene's cleanup policy. Requests neither read nor depend on it.
- Notification delivery (lives in [notifications](../notifications/README.md))
- Library state ("is this in the library?") and want state ("has it downloaded?") — both joined for UI but not owned here
- Watchlist semantics ("I might want this someday") — separate concept, deliberately not in this spec

## Interactions

| Neighbor                                              | How requests interact                                                                                                                |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| **[Users / permissions](../users/README.md)**         | Consumes `requests.*` permissions and `user_policy` quotas. Subject to the activity-scoping predicate.                               |
| **[Tracking](../tracking/README.md)**                 | Request approval spawns or joins tracking, seeding the requester's association row from tier + scope. Cancellation removes the requester. |
| **[Acquisition / wants](../acquisition/README.md)**   | Movie request approval spawns or joins a want. Series wants flow through tracking.                                                   |
| **[Quality profiles](../quality-profiles/README.md)** | Tier names come from the tier registry. The request's tier resolves to a profile (per media type) at spawn time.                     |
| **[Audit pattern](../../patterns/audit/README.md)**   | Every decision (auto-approve, approve, deny, cancel, expire) writes a structured audit row.                                          |
| **[Errors](../../patterns/errors/README.md)**         | Submission and approval errors use the typed-error model (forbidden, conflict, quota-exceeded, validation).                          |
| **[Metadata](../metadata/README.md)**                 | Subject resolution at submit time goes through the media-item / TMDB sync surfaces.                                                  |
| **[Hygiene](../hygiene/README.md)**                   | Owns retention/cleanup as a library policy. Requests do not carry retention; hygiene decides what's safe to remove.                  |
| **[Media-server](../media-server/README.md)**         | Supplies watch-state to hygiene's cleanup policy. No coupling to requests.                                                            |
| **[Notifications](../notifications/README.md)**       | Emits events on pending-needs-review, decision-made, request-fulfilled.                                                              |

## Open questions

1. **Watchlist as a separate concept.** Users want to bookmark "I might want this someday" without actually requesting. Is a watchlist a `pending` request that never gets approved, a separate entity, or just a UI affordance over media-item state? Lean: separate concept, not in this spec.
2. **Scope granularity at request time.** Is `whole series / season(N) / pilot` the right starting set, or does v1 need even less (whole-series + pilot only)? The richer rules and per-episode overrides live on tracking post-spawn regardless.
3. **Re-opening a denied request.** Model as a state transition (`denied → pending`) or require a new request linked to the original via a `re_request_of` back-reference? Lean: new request with back-reference — cleaner for audit.
4. **Expiration window.** Default for pending requests sitting un-decided. Probably 14 or 30 days, configurable per `user_policy`. Pin in a later iteration.
5. **Bundle requests / movie nights.** "Approve these 3 movies as one decision." Useful UX, but adds entity complexity. Defer unless real demand surfaces.
6. **Group / shared requests.** "Movie night for 4 people — bills against the booker's quota, notifies all four when ready." Probably later; want to confirm the data model leaves room.
7. **Tier upgrade as a request.** If Alice has HD and asks for 4K, is that a new request (with duplicate-detection flagging it as an upgrade) or a mutation of the existing? Lean: new request, with pre-flight surfacing the upgrade intent. Note this overlaps the unsolved multiple-versions question below.
8. **Multiple versions of the same item.** Requesting (and acquiring) two resolutions of one movie — e.g. 4K for the home theater and 1080p for the phone — is **not yet modeled.** The want/tracking layer currently assumes one file per atom; manual dual-version DB rows are possible but Arrflix does not plan to acquire two versions. Parked pending a tracking/want model decision.
9. **Cancellation race.** Requester cancels at the moment auto-approve is mid-flight. Lean: optimistic concurrency, cancelled-takes-precedence — pin the rule explicitly later.
10. **Notes visibility.** Approver's denial reason is visible to requester. Requester's submission note — visible to approver only, or to other viewers? Lean: approver only by default.
11. **Bulk operations.** Bulk approve, bulk deny, bulk cancel. Cosmetic API additions on top of the model; defer until UI demands.
12. **Re-request after denial cooldown.** Prevent a user from spamming the same denied request? Lean: not in the model — handle as a soft-gate in pre-flight ("you were denied this 2 days ago; sure you want to ask again?").

## What we're explicitly not deciding here

- Exact table names, columns, indexes, constraints
- API endpoint shapes, request/response formats, status code matrices
- The quota schema on `user_policy` (lives in users spec)
- Retention / cleanup policy, the cleanup worker, storage-pressure heuristics (lives in [hygiene](../hygiene/README.md))
- Notification routing rules and delivery channels (lives in [notifications](../notifications/README.md))
- UI component layouts, error wording, the tier-mismatch UX (Story 3)
- Backfill / migration ordering relative to the users spec rollout
- Relative ordering of pre-flight checks (permission → quota → duplicate → submit) — the spec mandates all of them, not their order

## Doc neighbors

- [Users](../users/README.md) — identity, roles, permissions, `user_policy`, activity-visibility scoping
- [Tracking](../tracking/README.md) — series ongoing-intent primitive, multi-requester semantics
- [Acquisition](../acquisition/README.md) — the pipeline that turns approved requests into library files
- [Quality profiles](../quality-profiles/README.md) — tier registry, tier→profile resolution
- [Hygiene](../hygiene/README.md) — owns retention/cleanup policy (where watch-state lands)
- [Media-server](../media-server/README.md) — ingests watch-state; supplies it to hygiene
- [Audit pattern](../../patterns/audit/README.md) — decision-artifact stream this spec writes into
- [Errors](../../patterns/errors/README.md) — typed error model for submission/approval failures
- [Story 1](../../stories/01-happy-path-auto-approve.md) — pressure-tests this spec end-to-end
- [Notifications](../notifications/README.md) — surfaces request events to users
