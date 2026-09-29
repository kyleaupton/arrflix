# Story 5 — Two requesters, one series: scope union, widening, and departure

**Status:** Draft (triaged against the code)

Alice already follows a series, but only its current season — she came in mid-run and doesn't care about the back catalogue. Bob discovers the show and asks for all of it. Rather than starting a second effort, Bob **joins** Alice's. The effective scope **widens**, nineteen new episodes are queued, and both requesters coexist on one tracking. Days later Bob leaves; the scope **narrows** back, work not yet started is stood down, and everything already acquired stays exactly where it is.

This is the **multi-requester story** — the one proving that requests, tracking, and wants compose the way the specs claim: requests stay one-per-person, tracking collapses to one-per-title and unions intent, and work is never duplicated.

> **Triage note.** This is the most accurate of the original stories — the join, union, widen, and narrow spine is implemented essentially as described. Three things were wrong: the tracking does **not** cache the union (it recomputes live, which is simpler and better), per-requester intent is **not** editable after the fact, and the departure half of the story is **not** wired. Those are marked below.

## Cast

- **Alice** — an existing user, already following the series' current season only.
- **Bob** — another user, who wants the whole show. Push enabled.
- **The system** — Arrflix.

## Preconditions

- Alice's tracking exists and is active, scoped to season 3.
- The episode tree is synced: 29 episodes across three seasons, with season 3 still airing weekly.
- **In the library:** the season 3 episodes acquired under Alice's scope.
- **Not in the library:** all of seasons 1 and 2 — outside Alice's scope, so no work was ever created for them.
- Bob has no existing request for the series.

## Flow

### Phase 1 — Bob finds a partially-tracked series (T+0)

**User-visible (Bob):** the season grid reflects current reality — season 3 partly in the library and being followed, seasons 1 and 2 absent. The request affordance tells him what *his* request would add:

> *Season 3 is already being followed. Requesting will add Seasons 1–2 and keep following new episodes.* You'd add 19 episodes, ~46 GB.

**Bob is not told that Alice is the one following it.** Requester identity is not disclosed between requesters by default; "already being followed" is the honest ceiling.

> **Unbuilt.** The scope-aware diff — *what would my request actually add?* — does not exist, and it is the thing that makes joining an existing effort comprehensible rather than mysterious.

### Phase 2 — Bob requests the whole show (T+1s)

**Behind the scenes:** Bob's request is auto-approved and, finding an existing tracking, **joins it** rather than creating a second one. His request records that it joined, and keeps its own original scope as frozen history.

**The dedup boundary is the title, not the request.** Two people wanting one show produce one tracking, one set of wants, and one download per release — never two parallel efforts racing each other.

### Phase 3 — Join, union, and widening (T+1s → T+~10s)

**Behind the scenes:**

- The requester set becomes Alice and Bob.
- **Effective scope is the union of what each requester wants** — season 3, unioned with everything, is everything.
- Re-evaluating against the episode tree finds nineteen newly in-scope episodes with no files and no existing work. Nineteen wants are created.
- **Season 3's existing wants are untouched.** They already exist; they now simply have two people behind them.

**Where per-requester intent lives:** on the association between a requester and a tracking — seeded from their request, and the source of truth for recomputation. The request itself stays frozen as origin history and is deliberately *not* the recompute source, because a frozen row cannot reflect a later change of mind.

> **Correction to the original story.** It claimed the tracking *caches* the union and recomputes it on change. It does not — there is no cached union, and effective scope is recomputed live from the surviving associations on every evaluation. That's simpler and has no staleness to manage. The story was wrong; the implementation is better.

**User-visible:** Bob sees the back catalogue queued. **Alice sees nothing** — her scope and her episodes are unaffected, and nothing about her experience changed.

### Phase 4 — The back catalogue arrives (T+~10s → T+~hours)

A compressed [series mid-season](./02-series-mid-season-auto-approve.md) flow: season packs cover the two complete seasons, each one acquisition serving many wants, and the files land.

**Notification audience is where this gets interesting.** Bob should hear about the back catalogue arriving. **Alice should not** — those episodes were never in her scope and are not what she asked for.

> **Not currently true.** Every requester on a tracking is notified about every want that becomes available, with no scope filter. Alice would be told about all nineteen of Bob's episodes. This is the story's own open question, answered by the code in the way the story explicitly rejects.

### Phase 5 — Steady state

Two requesters, one tracking, one copy of each file. New episodes are wanted by both, so both hear about them. The back catalogue is wanted by Bob alone.

**Scope and tier are per-requester; the acquired files are shared.** There is exactly one copy of each episode on disk regardless of how many people wanted it.

> Divergent *tiers* across requesters — Alice at HD, Bob at 4K — is the case this story deliberately excludes and [upgrades and divergent tiers](./06-upgrades-and-divergent-tiers.md) owns. It is also, as that story records, silently broken today.

### Phase 6 — Bob departs (T+~days)

**User-visible (Bob):** he unsubscribes. Toast confirms; the series leaves his list.

**Behind the scenes:**

- Bob's association is removed. The requester set is Alice alone.
- **Effective scope recomputes** from the surviving association: back to season 3.
- Seasons 1 and 2 are now out of scope. **Work not yet started is stood down; anything already acquired stays.**
- The tracking remains active — Alice still wants it. It does not pause, because somebody is still there.

**The committed decision on acquired files:** narrowing scope changes what the tracking *will do*, never what is already on disk. Deleting acquired content because a requester left would be hostile and surprising. Those files become library content like any other, governed by [hygiene](../modules/hygiene/README.md).

> **Not currently true.** Departure removes the association but **never re-evaluates the tracking**, so work that only Bob justified keeps running. This is the phase's headline behavior and it is a latent gap — recorded as a requirement in [a requester cancels](./07-requester-cancels.md), which owns the withdrawal path in detail.

**User-visible (Alice):** nothing. The narrowing returned the tracking to exactly her original intent.

## Postconditions

- Two requests: Alice's active, Bob's terminally cancelled — both retained, neither the source of truth for what happens next.
- One tracking throughout — never two — with one surviving association and effective scope recomputed from it.
- Season 3's work untouched by Bob's arrival or departure.
- Seasons 1 and 2 on disk, no longer tracked, not deleted.

## What must be true (foundation requirements)

Requirements carry stable ids per [the stories contract](./README.md#conventions). Most of [series mid-season](./02-series-mid-season-auto-approve.md)'s requirements carry over.

### One effort per title

- **REQ-UNION-001** — Two requesters wanting the same title must produce one tracking and one set of wants, never parallel efforts. *Currently true*, with the title as the dedup boundary.
- **REQ-UNION-002** — A requester joining an existing tracking must never duplicate work that already exists for it. *Currently true.*

### Intent is per-requester and live

- **REQ-UNION-003** — Each requester's intent must be held independently, and effective scope must be the union of everyone currently attached. *Currently true.*
- **REQ-UNION-004** — Effective scope must be derived from live per-requester intent, never from the frozen request. *Currently true* — and the reason is that a request cannot reflect a later change of mind.
- **REQ-UNION-005** — A requester must be able to change their own intent after the fact. **Not currently true:** there is no way to alter scope once a request has spawned, so the "mutable thereafter" this model depends on exists only in principle. It also means most of this story's interesting variants cannot be exercised at all.

### Joining and leaving are safe

- **REQ-UNION-006** — Widening must queue only what is genuinely newly in scope and lacking a file. *Currently true.*
- **REQ-UNION-007** — Removing a requester must re-evaluate the tracking so work only they justified is stood down. **Not currently true** — see [a requester cancels](./07-requester-cancels.md).
- **REQ-UNION-008** — Narrowing must never delete acquired files. Scope governs future work, not existing content.
- **REQ-UNION-009** — A tracking must remain active while any requester remains, and stand down only when the last one leaves. *Currently true* via the withdrawal path, though [a requester cancels](./07-requester-cancels.md) records a way to bypass it.

### Nobody learns about what they didn't ask for

- **REQ-UNION-010** — A requester must only be notified about content their own intent covers. **Not currently true:** every requester is notified about every want on the tracking.
- **REQ-UNION-011** — A requester must not be told who else wants a title. *Currently true by omission* — nothing surfaces it, though nothing enforces it either.

## Out of scope (variant stories)

- **Divergent tiers** — one requester at HD, another at 4K. [Upgrades and divergent tiers](./06-upgrades-and-divergent-tiers.md).
- **The withdrawal path in detail** — what cancelling means at each stage. [A requester cancels](./07-requester-cancels.md).
- **The last requester leaving** — covered as the terminal case in that story.
- **Joining while acquisition is in flight** — Bob arrives mid-download. Mostly covered; the requester-set update on already-running work is worth validating.
- **Joining a request that needs approval** — the scope wouldn't widen until approved. Composition with [pending approval queue](./03-pending-approval-queue.md).
- **Re-joining after leaving** — the files are still on disk, so nothing is re-acquired. A pleasing efficiency case.
- **Per-user retention** — one file serving different lifetimes for different people. Removed from the model; retention is a library-wide [hygiene](../modules/hygiene/README.md) policy.

## Open questions

1. **Is a joining requester told the title is already followed, and how much?** **Lean:** yes that it is, never by whom — the scope diff is the useful part, the identity isn't.

2. **Does widening notify existing requesters?** Alice's scope is unaffected, so probably not. But if a join changed something she'd notice — raising the quality target, for instance — she plausibly should hear about it. **Lean:** notify only when an existing requester's own experience changes.

3. **Per-episode notification audience — resolved, and unimplemented.** A requester hears only about content their scope covers. The code notifies everyone. Captured as REQ-UNION-010.

4. **Where per-requester intent lives — resolved.** On the requester-tracking association, live and recomputed, with the request kept as frozen origin.

5. **Who governs files left behind after narrowing?** With Bob gone, seasons 1 and 2 are in the library and nothing wants them. **Lean:** they persist indefinitely unless a cleanup policy claims them, and are worth surfacing as untracked content rather than silently orphaning them.

6. **Should the interface distinguish tracked from merely-present content?** After Bob leaves, seasons 1 and 2 are in the library but nothing is watching them for new content or better releases. **Lean:** yes, quietly — it explains why they behave differently from the rest of the show.

7. **Concurrent joins.** Two people requesting the same title in the same second. Association writes are idempotent and there is no cached union to lose an update on, so the original concern is moot rather than solved.
