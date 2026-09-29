# Story 1 — Happy path: friend requests a movie with auto-approve

**Status:** Draft (triaged against the code)

A trusted family member opens the PWA, requests a movie, and watches it ten minutes later. No admin intervention. This is the bread-and-butter flow Arrflix has to nail; if this story isn't delightful, nothing else matters.

> **Triage note.** This story predates most of the implementation and was written as combined product-and-architecture. Its entity model held up remarkably well; its permission vocabulary, worker mechanics, and media-server tail did not. Mechanism has been removed in favour of links to the specs that own it, per [the stories contract](./README.md). Unbuilt steps are now marked as such rather than narrated in the present tense.

## Cast

- **Friend** — a non-admin user who holds the grants to request HD movies and to have those requests auto-approved (`requests.create:movie:hd`, `requests.auto_approve:movie:hd` — see [users](../modules/users/README.md)). No 4K grant. PWA installed, push notifications enabled.
- **Admin** — passive here. Their relevant prior setup: an HD quality profile exists, at least one indexer and one downloader are configured and healthy, and a media server is connected.
- **The system** — Arrflix: web UI, API, background workers.
- **External** — TMDB, the indexer, the downloader, the media server.

## Preconditions

- The movie is not in any library, and nobody else has requested it.
- TMDB has metadata for it.
- At least one release exists that passes the HD profile.
- Friend's push subscription is registered.

## Flow

### Phase 1 — Discovery (T+0s)

**User-visible:** Friend searches for the film, taps the result, and lands on its page. Below the poster is a **Request** button showing the tier they'd get.

The page knows three things before it renders: whether the library already has it, whether anyone is already getting it, and which tiers this viewer may ask for. The tier picker offers only tiers the viewer holds a grant for.

### Phase 2 — Request (T+1s)

**User-visible:** Friend taps **Request**. The button becomes a status pill — *"Searching for an HD release…"*. A toast confirms: *"Request submitted — we'll notify you when it's ready."*

**Behind the scenes:** the request is checked against Friend's grants, auto-approved without a human, and spawns the machinery that actually does the work — a [tracking](../modules/tracking/README.md) for the film, which produces one [want](../modules/acquisition/README.md). The request itself is frozen at that point; everything after is want state. See [requests § mapping requests to downstream state](../modules/requests/README.md#mapping-requests-to-downstream-state).

**Notifications:** none. The in-app pill is sufficient while the user is here.

### Phase 3 — Search and grab (T+3s → T+30s)

**User-visible:** the pill advances — *Searching* → *Grabbed* → *Downloading 12% · ~7 min*. The ETA is honest, derived from the downloader's reported speed and size.

**Behind the scenes:** the acquisition worker picks the want up, queries indexers, scores every result against the profile, picks the best eligible one, and hands it to the downloader. Progress is reported live.

### Phase 4 — Download → import → verify (T+~7 min)

**User-visible:** if the PWA is still open, the pill moves through *Importing…* to *Available*.

**Behind the scenes:** the file is hardlinked into the library under the configured name template, recorded, and **verified present and readable on disk**. That verification is what marks the want available.

> **This is the terminal happy state.** Availability depends on Arrflix's own check, not on any media server. That decoupling is deliberate and is implemented as described.

### Phase 5 — Notify and sync (T+~7 min → rolling)

**User-visible:** a push arrives — *"Dune Part Two is ready to watch."*

**Behind the scenes:** the notification fires off the verified-available transition, immediately, waiting on nothing external. Separately and non-blockingly, the media server is nudged to pick up the new file; when it confirms, the deep link resolves to the media server rather than to Arrflix.

> **Unbuilt.** There is no media-server integration — no library refresh, no webhook receiver, no propagation record, no deep link. The available-notification half is built; everything media-server-facing in this phase is aspirational. See [media-server](../modules/media-server/README.md).

### Phase 6 — Watch

**User-visible:** Friend taps the push and watches.

**Behind the scenes:** deferred — watch state feeding recommendations and cleanup is a later concern.

## Postconditions

- The request is a frozen historical artifact, recorded as having spawned its tracking.
- One single-atom tracking for the film; one want, available, reached by disk verification.
- The film exists as one library file, recorded and verified.
- Friend's requests view shows it as fulfilled.

## What must be true (foundation requirements)

Most of what this story needs is owned by other stories now; those are linked rather than restated. What remains is what the happy path uniquely asserts.

- **REQ-FULFIL-001** — A request from a user holding the matching create and auto-approve grants must complete without any human action.
- **REQ-FULFIL-002** — A want must reach *available* on Arrflix's own verification that the file is present and readable, never on confirmation from a media server. *Currently true* — the decoupling is implemented, and it is the requirement that keeps a missed webhook from stranding a request forever.
- **REQ-FULFIL-003** — The requester must be told when the film is ready, through a channel that works when the app is closed.
- **REQ-FULFIL-004** — The status a requester sees must reflect where the work actually is, without a reload.
- **REQ-FULFIL-005** — A viewer must only be offered tiers they hold a grant for. *Currently true.*
- **REQ-FULFIL-006** — Once available, the requester must be able to get from the notification to watching it. **Unbuilt** — there is no media-server integration and nothing populates a deep link, though the notification payload reserves a field for one.

### Time targets (UX commitments)

- Tap → *request submitted* confirmation: **< 1s**
- Request → tracking and want exist: **< 1s**
- Want created → first indexer search begins: **< 30s**
- Never claim availability before the file is verified on disk

> The third target is a *requirement*, not a mechanism. The original story specified an event bus so the worker would react instantly; the implementation polls on a short interval, which satisfies the target. Either is fine — the commitment is the latency, not how it's achieved.

### Owned elsewhere

- Per-user realtime scoping and live status delivery — [realtime](../modules/realtime/README.md)
- Notification delivery, preferences, and push — [notifications](../modules/notifications/README.md)
- Approval when auto-approve does *not* apply — [pending approval queue](./03-pending-approval-queue.md)
- What happens when no release exists — [failed search, eventual recovery](./04-failed-search-recovery.md)
- Media-server propagation and deep links — [media-server](../modules/media-server/README.md)

## Out of scope (variant stories)

- **Approval queue** — [pending approval queue](./03-pending-approval-queue.md)
- **Series with future episodes** — [series mid-season, auto-approve](./02-series-mid-season-auto-approve.md)
- **Someone else already requested it** — [multi-requester, scope union](./05-multi-requester-scope-union.md)
- **Wanting it better than what arrived** — [upgrades and divergent tiers](./06-upgrades-and-divergent-tiers.md)
- **Changing their mind** — [a requester cancels](./07-requester-cancels.md)
- **It isn't out yet** — [asking for something that isn't out yet](./09-not-out-yet.md)
- **No eligible release** — [failed search, eventual recovery](./04-failed-search-recovery.md)
- Movie already in the library at the requested tier → variant
- Friend has not enabled push → variant; falls back to in-app

## Open questions

1. **Media-server correlation — resolved in spec, unbuilt in code.** Correlating a server's item back to our file is a *propagation* concern, not an availability gate, and [media-server § correlation](../modules/media-server/README.md#correlation-mapping-a-server-item-back-to-our-media_file) resolves the approach. Nothing implements it.

2. **Push permission timing.** Ask on first visit, on first request submit, or after the first notification would have fired? **Lean:** first request submit — *"we'll notify you when it's ready — enable notifications?"* — because that's the moment the value is obvious.

3. **Does the user know it was auto-approved?** Transparency versus cognitive load on the happy path. **Lean:** don't surface it inline; show it in request history.

4. **Tier picker versus auto-default — resolved by implementation.** The picker is explicit, offering only tiers the viewer holds grants for.

5. **Webhook reliability — resolved by decoupling.** Availability no longer depends on a webhook; a missed one delays only media-server propagation.

6. **ETA fidelity by phase.** There is no useful signal while searching. **Lean:** show the phase only, and give an ETA solely while downloading.
