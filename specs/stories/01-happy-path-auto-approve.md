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
  4. Creates a **want** row: `{ id, request_id, tmdb_id, type: movie, quality_tier: HD, status: pending }`
  5. Emits a `want.created` event (event bus + SSE)

**Notifications:** none pushed — the in-app pill is sufficient when the user is here.

### Phase 3 — Search & grab (T+3s → T+30s)

**User-visible (PWA still open):**

- Status pill updates via SSE:
  `Searching for HD release...` → `Grabbed release • Queued` → `Downloading 12% • ~7 min`
- ETA is honest: derived from the downloader's reported speed/size.

**Behind the scenes:**

- Auto-select worker picks up the want **event-driven** (no polling delay).
- Calls indexer service → Prowlarr → results.
- [[decision-log]]: quality engine scores every result, rejects ineligible ones with reasons.
- Picks the top scorer, creates a `download_job` linked to the want.
- Want status: `pending` → `searching` → `grabbed` → `downloading`
- Downloader service hands the magnet/torrent to qBittorrent.
- Download worker polls qBit; broadcasts progress on SSE.

### Phase 4 — Download → Import (T+~7 min)

**User-visible:**

- If PWA closed: nothing yet.
- If still watching: pill updates **"Importing..."** then **"Adding to Plex..."**

**Behind the scenes:**

- qBit reports complete.
- Import service hardlinks file from downloads dir into the configured library path via the name template.
- Creates `media_file` row, links to `media_item`, writes `media_file_state`.
- Want status: `downloading` → `imported`
- Import service calls Plex partial-refresh on the affected library section.

**Notifications:** none yet — we have not confirmed Plex visibility.

### Phase 5 — Plex confirms (T+~8 min)

**Behind the scenes:**

- Plex finishes scanning, fires `library.new` webhook.
- Webhook handler correlates the new Plex item back to our `media_file` (path or rating key — see [Open questions](#open-questions)).
- Want status: `imported` → `available`
- Notification service fires the `available` event.

**Notifications:**

- 📱 Push to Friend: **"Dune Part Two is ready to watch."** Tap → Plex deep link.
- In-app status pill: **"Available on Plex — watch now"** (button).

### Phase 6 — Watch

**User-visible:** Friend taps the push, opens Plex, watches.

**Behind the scenes:** (deferred) Plex play webhook → arrflix watch state → feeds future recommendations + cleanup.

## Postconditions

- One `request` row, status `fulfilled` (or `approved` with a `fulfilled_at` — TBD)
- One `want` row, status `available`
- One `media_item`, one `media_file`, one `media_file_state`
- One `download_job`, status `completed`
- One or more decision_log rows (every release considered by quality + the routing decision)
- Friend's "my requests" page shows the request as fulfilled with a Plex link

## What must be true (foundation requirements)

These are the things this story assumes exist. Each is a foundation requirement that drives a spec.

### Data primitives

- **User policy fields** (likely a `user_policy` table joined off `user`): `can_request_movie`, `can_request_4k`, `auto_approve_movie_hd`, `auto_approve_movie_4k`, `auto_approve_series_hd`, `auto_approve_series_4k`, plus quotas like `max_pending_requests`.
- **`request` entity**: `{ id, requested_by, tmdb_id, type, tier, status, approved_by, approved_at, auto_approved, denied_reason, fulfilled_at }`. Lifecycle: `pending → approved → fulfilled` or `pending → denied`.
- **`want` entity**: `{ id, request_id (nullable — admins can want without requesting), tmdb_id, type, quality_tier, status, created_at }`. Lifecycle: `pending → searching → grabbed → downloading → imported → available` plus terminals `failed`, `canceled`.
- **`decision_log` entity** per `(want, search_run)`: every considered release with score, accept/reject + reason.
- **`media_file` ↔ Plex correlation** — store Plex `rating_key` on `media_file` once known.
- **`push_subscription`** per user (VAPID endpoint, keys, ua/device label).
- **`notification_preference`** per user, per event-type, per channel (push, in-app, future email).

### Services / workers

- **Request service** — validates permissions, evaluates approval policy, creates the want on approve.
- **AcquisitionWorker** — event-driven trigger on `want.created`; searches indexers, scores via the quality profile, evaluates routing rules, grabs.
- **Decision logging** — every accept/reject persisted with reason via the system-wide [decision-artifact pattern](../patterns/audit/README.md) (powers the "why didn't this download?" debugger).
- **Plex integration** — outbound partial-refresh after import; inbound `library.new` webhook receiver that correlates to our `media_file`.
- **Notification service** — per-user, per-event-type routing across channels.
- **Web Push delivery** — VAPID-signed push to registered subscriptions.

### UI surfaces

- Movie focus page aware of viewer tier; shows the right CTA.
- "Request" button that morphs into a status pill, fed by SSE.
- "My requests" page (in-flight + historical).
- Push permission flow at the right moment (see open questions).

### Realtime / messaging

- SSE channel scoped per user, filtered to events relevant to their requests/wants.
- An internal event bus so the AcquisitionWorker reacts to `want.created` immediately rather than polling.

### Time targets (UX commitments)

- Click → "request submitted" toast: <1s
- Request → want created: <1s (same tx)
- Want → first indexer search: <30s
- ETA honesty: never claim "available" until Plex confirms

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

1. **Plex correlation by path vs rating key.** Plex `library.new` webhooks give rating keys, but our canonical link is the file path. Options: walk the Plex API on import to discover the rating key, or match by path on webhook receipt. Each has failure modes (path mapping mismatches; partial scans). Pick one before implementing the webhook handler.
2. **Push permission UX timing.** Ask on first PWA visit, on first request submit, or after first available-notification _would_ have fired? Probably first request submit: "we'll notify you when it's ready — enable notifications?"
3. **"Already auto-approved" UI.** Does the friend even know it was auto-approved? Pro: transparency. Con: extra cognitive load on the happy path. Probably skip in v1; surface in request history.
4. **Tier picker vs auto-default.** If Friend has both `can_request_4k` and `can_request_movie`, do they pick tier at request time, or default to the highest allowed? Likely default to highest with explicit override. Story 3 locks this in.
5. **`library.new` reliability.** If Plex misses a webhook (restart mid-scan, network blip), how do we eventually mark `available`? Fallback poller on `imported` wants? Manual "mark available" button?
6. **ETA fidelity by phase.** We have no signal during `searching`. Generic "usually <1 min" or just a spinner? Probably show phase only, drop ETA except during `downloading`.
