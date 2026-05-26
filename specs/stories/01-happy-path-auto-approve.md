# Story 1 — Happy path: friend requests a movie with auto-approve

**Status:** Draft

A trusted family member opens the PWA, requests a movie, and watches it on Plex ~10 minutes later. No admin intervention. This is the bread-and-butter flow Arrflix has to nail; if this story isn't delightful, nothing else matters.

This story is also the **template** for stories 2–4. The shape (Cast → Preconditions → Flow → Postconditions → What must be true → Out of scope → Open questions) is what each subsequent story should follow.

## Cast

- **Friend** — a non-admin user. Their config:
  - `can_request_movie: true`, `can_request_4k: false`
  - `auto_approve_movie_hd: true`
  - PWA installed, push notifications enabled
- **Admin** — passive in this story. Their relevant prior config:
  - An HD quality profile exists (the friend's request will use it)
  - At least one indexer + one downloader are configured and healthy
  - Plex integration connected (URL + token), `library.new` webhook subscribed
- **The system** — Arrflix: web UI, API, background workers
- **External** — TMDB, Prowlarr, qBittorrent, Plex

## Preconditions

- Movie is **not** in any library
- Movie is **not** already a [[want]] (no in-flight request from anyone)
- TMDB has metadata for the movie
- Indexers have at least one HD release that passes the quality policy
- Downloader has free slots
- Friend's push subscription is registered on the server

## Flow

### Phase 1 — Discovery (T+0s)

**User-visible:**

- Friend opens PWA on phone, lands on home / browse.
- Searches for "Dune Part Two", taps the result card.
- Movie focus page loads with TMDB metadata (poster, overview, runtime, year).
- Below the poster: a **"Request" button** with subtitle "Available in HD" (reflecting their tier).

**Behind the scenes:**

- TMDB search proxied through the existing TMDB service.
- Focus page calls media service: "do we have this? is it wanted? what's my tier?"
- API answers: not in library, no existing want, viewer's tier is HD.

### Phase 2 — Request (T+1s)

**User-visible:**

- Friend taps "Request". Button morphs into a status pill: **"Searching for HD release..."** with a spinner.
- Toast confirms: "Request submitted — we'll notify you when it's ready."

**Behind the scenes:**

- `POST /requests { tmdb_id, type: "movie", tier: "HD" }`
- Request service:
  1. Checks `Friend.can_request_movie` → ok
  2. Evaluates approval policy: `auto_approve_movie_hd` is true → auto-approve
  3. Creates a **request** row: `{ id, requested_by, tmdb_id, type, tier, status: approved, approved_by: system, auto_approved: true }`
  4. Spawn step: no existing tracking for this movie → create a single-atom **tracking** row: `{ id, media_item_id, scope: self, quality_profile_id: <hd>, upgrade_behavior, schedule_strategy: smart, requesters: [Friend], state: active }`. Request → `spawned`, recording `spawned_tracking_id`.
  5. Tracking produces one **want** row: `{ id, tracking_id, type: movie, quality_profile_id: <snapshot>, status: pending }`. Emits `want.created` (event bus + [realtime](../modules/realtime/README.md)).

**Notifications:** none pushed — the in-app pill is sufficient when the user is here.

### Phase 3 — Search & grab (T+3s → T+30s)

**User-visible (PWA still open):**

- Status pill updates via [realtime](../modules/realtime/README.md):
  `Searching for HD release...` → `Grabbed release • Queued` → `Downloading 12% • ~7 min`
- ETA is honest: derived from the downloader's reported speed/size.

**Behind the scenes:**

- Auto-select worker picks up the want **event-driven** (no polling delay).
- Calls indexer service → Prowlarr → results.
- [[decision-log]]: quality engine scores every result, rejects ineligible ones with reasons.
- Picks the top scorer, creates a `download_job` linked to the want.
- Want status: `pending` → `searching` → `grabbed` → `downloading`
- Downloader service hands the magnet/torrent to qBittorrent.
- Download worker polls qBit; broadcasts progress via [realtime](../modules/realtime/README.md).

### Phase 4 — Download → Import → Verify (T+~7 min)

**User-visible:**

- If PWA closed: nothing yet.
- If still watching: pill updates **"Importing..."** then **"Available — in your library"**.

**Behind the scenes:**

- qBit reports complete.
- Import service hardlinks file from downloads dir into the configured library path via the name template.
- Creates `media_file` row, links to `media_item`, writes `media_file_state`.
- Want status: `downloading` → `imported`.
- **VerifyStep** confirms the file is present and readable on disk (stat/size check). Want status: `imported` → `available`. **This is the terminal happy state — Arrflix has the file and has verified it, with no dependency on any media server.**
- In parallel and non-blocking: the media-server sync nudges Plex to partial-refresh the affected section. This does not gate anything above.

**Notifications:** the `available` event fires _now_ (Phase 5), gated on Arrflix's own verification — not on Plex.

### Phase 5 — Notify + media-server sync (T+~7 min → rolling)

**Behind the scenes:**

- On `want → available` (Phase 4's verify), [notifications](../modules/notifications/README.md) fires the `available` event immediately. Nothing waits on Plex.
- Independently, the media-server sync resolves propagation: when Plex's scan completes it fires `library.new`; the handler records a `(media_file, media_server)` propagation row + rating key and emits `media_file.propagated`. If the webhook never arrives, a reconciliation poll backfills the same record. Either way the want was never blocked.

**Notifications:**

- 📱 Push to Friend: **"Dune Part Two is ready to watch."** The deep link points at the Arrflix media page, which shows "syncing to your server…" and flips to **"Open in Plex"** the moment propagation confirms (usually seconds). If propagation is already done when the push fires, the link goes straight to Plex.
- In-app status pill: **"Available"** → **"Open in Plex"** once propagated.

### Phase 6 — Watch

**User-visible:** Friend taps the push, opens Plex, watches.

**Behind the scenes:** (deferred) Plex play webhook → arrflix watch state → feeds future recommendations + cleanup.

## Postconditions

- One `request` row, status `spawned` (frozen artifact once tracking + want exist)
- One single-atom `tracking` row — `active` while searching, auto-archived once the want is `available` at-cutoff (a movie is inherently complete)
- One `want` row (parented to the tracking via `tracking_id`), status `available` — reached when the file was verified on disk, independent of any media server
- One `media_item`, one `media_file`, one `media_file_state`
- A `(media_file, Plex)` propagation record, `visible` once Plex's scan completed (or backfilled by the reconciliation poll)
- One `download_job`, status `completed`
- One or more decision_log rows (every release considered by quality + the routing decision)
- Friend's "my requests" page shows the request as fulfilled with a Plex link

## What must be true (foundation requirements)

These are the things this story assumes exist. Each is a foundation requirement that drives a spec.

### Data primitives

- **User policy fields** (likely a `user_policy` table joined off `user`): `can_request_movie`, `can_request_4k`, `auto_approve_movie_hd`, `auto_approve_movie_4k`, `auto_approve_series_hd`, `auto_approve_series_4k`, plus quotas like `max_pending_requests`.
- **`request` entity**: `{ id, requested_by, tmdb_id, type, tier, status, approved_by, approved_at, auto_approved, denied_reason, spawned_tracking_id }`. Lifecycle: `pending → approved → spawned` or `pending → denied` (see [requests](../modules/requests/README.md#lifecycle)).
- **`tracking` entity**: the universal ongoing-intent primitive — one per requested media item (single-atom for a movie). Produces wants. See [tracking](../modules/tracking/README.md).
- **`want` entity**: `{ id, tracking_id, type, quality_profile_id, status, created_at }` — every want has a tracking parent (movies included). Lifecycle: `pending → searching → grabbed → downloading → imported → available` plus terminals `failed`, `canceled`.
- **`decision_log` entity** per `(want, search_run)`: every considered release with score, accept/reject + reason.
- **media-server propagation record** — a per-`(media_file, media_server)` row that stores the server's `external_ref` (Plex `rating_key`, Jellyfin item id) once known. The rating key lives **here**, not on `media_file` — `media_file` is Arrflix's own truth and stays server-agnostic. See [media-server](../modules/media-server/README.md#the-propagation-record).
- **`push_subscription`** per user (VAPID endpoint, keys, ua/device label) — see [notifications](../modules/notifications/README.md).
- **`notification_preference`** per user, per event-type, per channel (push, in-app, future email) — see [notifications](../modules/notifications/README.md).

### Services / workers

- **Request service** — validates permissions, evaluates approval policy, creates the want on approve.
- **AcquisitionWorker** — event-driven trigger on `want.created`; searches indexers, scores via the quality profile, evaluates routing rules, grabs.
- **Decision logging** — every accept/reject persisted with reason via the system-wide [decision-artifact pattern](../patterns/audit/README.md) (powers the "why didn't this download?" debugger).
- **Plex integration** — outbound partial-refresh after import; inbound `library.new` webhook receiver that correlates to our `media_file`.
- **[Notifications](../modules/notifications/README.md)** — per-user, per-event-type routing across channels.
- **Web Push delivery** — VAPID-signed push to registered subscriptions.

### UI surfaces

- Movie focus page aware of viewer tier; shows the right CTA.
- "Request" button that morphs into a status pill, fed by [realtime](../modules/realtime/README.md).
- "My requests" page (in-flight + historical).
- Push permission flow at the right moment (see open questions).

### Realtime / messaging

- [Realtime](../modules/realtime/README.md) channel scoped per user, filtered to events relevant to their requests/wants.
- An internal event bus so the AcquisitionWorker reacts to `want.created` immediately rather than polling.

### Time targets (UX commitments)

- Click → "request submitted" toast: <1s
- Request → tracking + want created: <1s (same tx)
- Want → first indexer search: <30s
- ETA honesty: never claim "available" until the file is verified present in the library — this no longer depends on Plex (media-server visibility is a separate, non-blocking signal)

## Out of scope (variant stories)

- **Approval queue** — Story 2
- **Quality tier mismatch / 4K gating** — Story 3
- **Series subscription with future episodes** — Story 4
- Movie already in library at requested tier → variant
- Movie already wanted by someone else (dedup) → variant
- Upgrade request (have HD, request 4K) → variant
- Cancellation → variant
- Failed grab / no eligible releases → variant; needs notification UX
- Friend has not enabled push → variant; falls back to in-app + future email

## Open questions

1. **Plex correlation by path vs rating key — resolved.** This is now a pure **propagation** concern (which Plex item maps to our `media_file`), not an availability gate — it no longer blocks the want. The [media-server](../modules/media-server/README.md#correlation-mapping-a-server-item-back-to-our-media_file) spec resolves it: correlate **path-primary** (we wrote the file via a deterministic name template) with an optional per-`(library, media_server)` path-mapping override for container-mount differences, falling back to basename matching; the rating key is the stored result, not the join key.
2. **Push permission UX timing.** Ask on first PWA visit, on first request submit, or after first available-notification _would_ have fired? Probably first request submit: "we'll notify you when it's ready — enable notifications?"
3. **"Already auto-approved" UI.** Does the friend even know it was auto-approved? Pro: transparency. Con: extra cognitive load on the happy path. Probably skip in v1; surface in request history.
4. **Tier picker vs auto-default.** If Friend has both `can_request_4k` and `can_request_movie`, do they pick tier at request time, or default to the highest allowed? Likely default to highest with explicit override. Story 3 locks this in.
5. **`library.new` reliability — resolved by decoupling.** Availability no longer depends on the webhook: the want reaches `available` on disk-verify (Phase 4). A missed webhook only delays _media-server propagation_, which a reconciliation poll backfills. No want is ever stuck waiting on Plex. (Mechanics live in the pending media-server spec.)
6. **ETA fidelity by phase.** We have no signal during `searching`. Generic "usually <1 min" or just a spinner? Probably show phase only, drop ETA except during `downloading`.
