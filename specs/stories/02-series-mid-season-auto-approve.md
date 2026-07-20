# Story 2 — Series mid-season: friend requests an in-flight show with auto-approve

**Status:** Draft (triaged against the code)

A trusted family member requests a currently-airing series — two complete seasons behind it, season three four episodes deep — with auto-approve. Over the next few hours the back catalogue lands. The remaining unaired episodes then trickle in over the following weeks as they air. No admin intervention.

This is what makes Arrflix **ongoing** rather than a delivery service, and it's the first story that exercises [tracking](../modules/tracking/README.md) as a live artifact rather than a bookkeeping row.

> **Triage note.** The structural claims here held up unusually well — season packs, the many-wants-per-download relationship, per-episode want fulfilment, scope, and the airing-aware search cadence are all real. What did not survive: the worker names and event mechanics, the notification grouping, and every media-server step. Mechanism has been replaced with the requirements it served, per [the stories contract](./README.md). This story also predates the [autonomy dial](./08-autonomy-and-proposals.md), which now governs how much of this happens without asking.

## Cast

- **Friend** — a non-admin user holding the grants to request HD series and have those requests auto-approved. Push enabled.
- **Admin** — passive. An HD series profile exists; indexers and a downloader are configured and healthy.
- **The system** — Arrflix.

## Preconditions

- The series is not in any library and nobody is tracking it.
- TMDB has the full episode tree.
- **S1** (9 episodes) and **S2** (10 episodes): complete, season packs widely available.
- **S3**: airing weekly. E01–E04 aired, E05–E10 not yet.
- Acceptable HD releases exist for the aired material.

## Flow

### Phase 1 — Discovery (T+0)

**User-visible:** the series page shows a season grid distinguishing aired from upcoming episodes, and the request affordance says what's about to happen:

> *23 episodes already aired (S1 × 9, S2 × 10, S3 × 4). 6 more arriving weekly through July.* ~58 GB.

**This pre-flight summary does not exist.** It matters more than it looks: a series request is the one request where the user has little idea of its size, and "add show" can mean 6 GB or 600.

### Phase 2 — Request (T+1s)

**User-visible:** Friend picks how much of the show they want and taps Request. The pill reads *"Subscribing… syncing episode list"*. Toast: *"Request submitted — we'll notify you as episodes arrive."*

**The requester makes two choices: tier, and scope** — how much of the show. Scope includes future episodes; there is no separate follow-new-episodes toggle. Everything else about *how* releases get picked is operator policy, per [autonomy and proposals](./08-autonomy-and-proposals.md).

**Behind the scenes:** the request is auto-approved and spawns a [tracking](../modules/tracking/README.md) for the series, which becomes the durable home for everything that follows.

### Phase 3 — Episode structure sync (T+1s → T+~15s)

**User-visible:** the pill advances — *"Syncing episode list (29 episodes)…"* → *"Queueing 23 episodes…"*.

**Behind the scenes:** the full season and episode tree is pulled from TMDB and stored, **including the six unaired episodes** — they exist as records with future air dates and no file. Scope is then evaluated against that tree: 29 episodes in scope, 23 lacking files, so 23 wants are created.

> Unaired episodes existing as real rows is the foundation everything downstream rests on. Without them there is nothing to schedule against, nothing to show in a grid, and no way to say "six more are coming."

### Phase 4 — Search and grab (T+~15s → T+~3 min)

**User-visible:** the pill aggregates — *"Searching releases… 23 episodes queued"* → *"Grabbed 2 season packs + 4 episodes · Downloading"*. The season grid shows S1 and S2 each as a single download, S3E01–E04 individually, and the unaired episodes as greyed cells reading *"Airs Fri"*.

**Behind the scenes:** three outcomes emerge from the same search-gate-score-pick flow.

- **S1 and S2** — a complete season pack wins for each. **One acquisition covers nine wants**, then ten. This is the case that forces the many-wants-per-download relationship, and it is real.
- **S3E01–E04** — no pack exists yet, so each episode is picked and grabbed individually.

### Phase 5 — Download and import (T+~3 min → T+~hours)

**User-visible:** aggregate progress with a per-download drill-down.

**Behind the scenes:** as each download completes, its files are imported into the library under the series naming convention. **The crucial difference from a movie:** one season-pack download produces many files, and each must be matched to exactly one episode — and therefore to exactly one want. That matching is what lets a nine-episode pack satisfy nine separate intents.

Each want becomes available when its file is verified on disk. Availability never waits on a media server.

### Phase 6 — Notification (T+~hours, rolling)

**User-visible:** grouped pushes — *"Severance: 9 episodes from Season 1 are ready to watch."* Then Season 2. Then the four from Season 3.

**Grouping is the requirement, and it is unbuilt.** Today one notification fires per requester per want, so this exact scenario produces **twenty-three separate pushes** — precisely the flood the original story was written to prevent.

### Phase 7 — The weekly trickle (T+days)

**Behind the scenes:** wants for unaired episodes wait until their air date, then search hard during the window when a release is most likely, backing off if nothing appears.

**This works, and it is the model the movie path should copy.** Wants for future episodes are created up front and simply become due at air time. (Note this resolves the original story's open question in the opposite direction to its own lean — eager creation with deferred scheduling, not lazy creation at air time. Eager turned out better: the episode is visible as pending work rather than invisible until it airs.)

**User-visible:** a push per newly-available episode — individually noteworthy, unlike the back-catalogue flood — and the greyed cell fills in.

### Phase 8 — Eventual completion

When the series ends upstream and everything in scope is acquired, the tracking has nothing left to do and should stand down — unless it is still watching for [upgrades](./06-upgrades-and-divergent-tiers.md), which is the default. **Neither side of this exists:** nothing detects completion, and nothing archives.

## Postconditions

- One request, frozen; one tracking, active; the full episode tree stored including unaired episodes.
- 23 wants, ending available, plus 6 waiting on air dates.
- Six acquisitions covering those 23 wants — two season packs and four singles — demonstrating that acquisitions and wants are not one-to-one.
- A record of what was considered for each, so "why this release?" is answerable later.

## What must be true (foundation requirements)

Requirements carry stable ids per [the stories contract](./README.md#conventions). [The happy path](./01-happy-path-auto-approve.md) covers the per-want pipeline; these are what a series adds.

### Ongoing intent

- **REQ-SERIES-001** — A series request must create durable intent that keeps producing work as new episodes air, not a one-off delivery. *Currently true* — this is tracking's whole reason to exist.
- **REQ-SERIES-002** — Episodes that have not aired must exist as visible future work, not as absences. *Currently true*, and it's what makes a grid, a schedule, and an expectation possible.
- **REQ-SERIES-003** — Work for an unaired episode must not be attempted before it airs. *Currently true for series* — and notably **not** true for movies, per [asking for something that isn't out yet](./09-not-out-yet.md).
- **REQ-SERIES-004** — A tracking that has acquired everything in scope must stand down unless it is still watching for upgrades. **Neither side is built.**

### One acquisition, many wants

- **REQ-SERIES-005** — A single acquisition must be able to satisfy many wants, and each file it produces must be matched to exactly one of them. *Currently true.*
- **REQ-SERIES-006** — A pack that fails to cover every want it was grabbed for must return the uncovered wants to the queue rather than leaving them stranded. *Currently true.*

### The user can comprehend it

- **REQ-SERIES-007** — Before requesting, a user must be able to see the size of what they are asking for — how many episodes, roughly how much storage. **Unbuilt**, and this is the request where it matters most.
- **REQ-SERIES-008** — Series-level progress must be presentable without composing it from per-episode state. **Unbuilt** — there is no aggregate, so every surface derives its own summary from individual events, which is the same divergence problem described in [realtime](../modules/realtime/README.md).
- **REQ-SERIES-009** — Acquiring a back catalogue must not produce one notification per episode. **Not currently true:** it produces exactly that.
- **REQ-SERIES-010** — A newly-aired episode arriving *is* individually noteworthy and should be notified as such. The distinction from REQ-SERIES-009 is the point — same event, different volume, different treatment.

### Time targets (UX commitments)

- Tap → confirmation: **< 1s**
- Request → tracking exists and structure sync begins: **< 1s**
- Structure sync → wants exist: **< 30s** typical, under two minutes for very long-running shows
- A newly-aired episode: searched hard during the window when a release is most likely

## Out of scope (variant stories)

- **Approval instead of auto-approve** — [pending approval queue](./03-pending-approval-queue.md)
- **Someone else already tracks it** — [multi-requester, scope union](./05-multi-requester-scope-union.md)
- **Who picks the release** — [autonomy and proposals](./08-autonomy-and-proposals.md)
- **A better release later** — [upgrades and divergent tiers](./06-upgrades-and-divergent-tiers.md)
- **Changing their mind mid-flight** — [a requester cancels](./07-requester-cancels.md)
- **No acceptable release for some episodes** — [failed search, eventual recovery](./04-failed-search-recovery.md)
- **Scope other than the whole show** — a single season, or future-only. The model supports more shapes than the request interface currently offers.
- **Already partially in the library** — scope evaluation finds existing files and skips those episodes.
- **Series revival** — an ended show returns; the tracking must come back to life.

## Open questions

1. **How is the pre-flight count produced?** A scope-aware episode count needs the episode tree, which is exactly what hasn't been synced yet at the moment the user is deciding. Options: sync on page view (slow), pre-sync popular series (storage), or show a coarser warning and refine after. **Needs picking before REQ-SERIES-007 can be built.**

2. **What is the right grouping for back-catalogue notifications?** Per season is the obvious unit. The open part is the window — if one season finishes at 3am and the next at 4am, is that one message or two? **Lean:** group per season, with a short debounce.

3. **Where does aggregate progress come from?** A server-computed summary, or composed client-side from per-episode events? **Lean:** server-computed — it's the same argument as the per-title projection, and composing on the client is what produces surfaces that disagree.

4. **Eager or lazy wants for unaired episodes — resolved, opposite to the original lean.** Wants are created eagerly with deferred scheduling. Eager won because an episode that exists as pending work can be displayed, counted, and reasoned about; a lazily-created one is invisible until it airs.

5. **What happens when a pack covers more or fewer files than expected?** A pack with an extras disc, or a pack arriving mid-season that covers episodes already acquired individually. **Lean:** skip the pack when everything it covers is already satisfied; otherwise treat it as an upgrade decision.

6. **Where does the user see the schedule?** *"We'll look for S03E05 shortly after Friday's air time"* is genuinely useful. On the episode cell, the series card, or a [calendar](./09-not-out-yet.md)? Related to the calendar surface that story raises.

7. **Mid-flight scope change.** Narrowing scope while a pack is downloading. **Lean:** let in-flight downloads finish, cancel work not yet started — symmetric with [a requester cancels](./07-requester-cancels.md).
