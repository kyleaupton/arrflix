# Realtime — SSE transport, broker, and producer API

**Status:** Draft, iteration 1

This doc defines how Arrflix delivers in-flight, ephemeral events from server to browser: the SSE transport choice, the multiplexed-subscription model, the per-user broker, the typed producer API (`realtime.Emit`), the reliability primitives (heartbeats, `Last-Event-ID` resume, bounded replay), and the relationship to the [notifications](../notifications/README.md) system. It pins the contract around an existing working broker — most of the model is "evolve and harden" rather than "design from scratch." It does **not** pin column types, exact Go signatures, or per-feature event types — those are implementation or feature-spec concerns.

## TL;DR

- **SSE, not WebSocket.** One-way server→client is what we actually need. SSE is HTTP-native, the browser handles reconnect, `Last-Event-ID` resume is in the spec, and the proxy story is one `proxy_buffering off` line instead of `Upgrade: websocket` everywhere. The codebase already has SSE running.
- **Single stream endpoint + side-channel control plane.** Client opens one long-lived `GET /api/v1/events` and stays there. Subscribe/unsubscribe to topics happens via REST (`POST /api/v1/events/subscriptions`, `DELETE /api/v1/events/subscriptions/<topic>`). Multiplexing without dropping the connection; no WebSocket needed.
- **Per-user scoping at the broker.** Every event carries a recipient (`user_id`, `admins`, or `broadcast`). The broker filters per subscriber before send — no client-side filter, no event ever crosses into a session that shouldn't see it.
- **`Last-Event-ID` + bounded replay buffer.** Per-subscriber ring buffer holds the last 5 minutes / 200 events. Clean resume across tab-sleep, brief drops, and redeploys. Beyond the window, the client refetches snapshots.
- **Typed producer API: `realtime.Emit(ctx, sse_events.X(...))`.** Mirrors the notifications constructor pattern. Each event has a Go constructor with a typed payload. The set of constructors is the event registry; grep finds it; the compiler enforces shape.
- **Two distinct producer APIs, on purpose.** `realtime` is ephemeral (scan progress, download progress, transient state) — no DB. [`notifications`](../notifications/README.md) is durable (want available, hygiene errors) — owns the outbox. They coexist; the notification system's `in_app` channel adapter is a thin shim that emits a generic `notification.delivered` SSE event after writing the outbox row.
- **State delivery is per-feature.** Snapshot+delta (inline payload, mutate cache via `setQueryData`) for high-frequency, small, simple-auth state. Notify-and-refetch (kick + `invalidateQueries`) for low-frequency, mutated-via-REST state. The spec gives the heuristic; per-feature owners pick.
- **TanStack Query is the default consumer.** API-shaped state lives in TanStack Query; SSE handlers mutate or invalidate query keys. Pinia stays the home for non-API global state (connection status, auth claims, dialog state, layout flags).

## Why this is its own spec

The codebase already has a working SSE pipeline: an [in-process broker](../../backend/internal/sse/broker.go), a [stream handler](../../backend/internal/http/handlers/events.go) with 9 declared event types, a [frontend Pinia store](../../web/src/stores/events.ts) with a pub/sub listener API, and a generated client from `@hey-api/openapi-ts`. It works, but it has known gaps:

- **No per-user scoping** — every connected client sees every event.
- **No resume semantics** — `Last-Event-ID` is unused; brief disconnects cause missed events.
- **Hardcoded event registry** — events are added by editing the handler file; no producer API; no compile-time enforcement of recipient or payload shape.
- **Known infra debt** documented in `SSE_TRANSPORT_FINDINGS.md`: nginx buffering, stale-closure race in the frontend events store, retry-attempt counter that never resets after a successful connection.

Without a spec, the system grows by editing the handler file every time a feature wants a new event type, and each addition reinvents whatever wire shape the author happened to think of. Worse, the moment we add per-user notifications (already pinned by the [notifications](../notifications/README.md) spec), naive global-broadcast becomes a correctness bug — admins see notifications meant for users, users see admin alerts.

This spec captures the contract the existing system should evolve into: same transport, same broker shape, but with per-user scoping, resume, a typed producer API, and the integration seam with notifications cleanly defined.

## The model

### Transport

**SSE (`text/event-stream`)** over HTTP/1.1. The choice is deliberate:

| Reason                                                                            | Trade-off                                                                                         |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| HTTP-native; works through any HTTP-aware proxy with one config line              | One-way only; client→server is REST                                                               |
| Browser handles reconnect automatically via the `EventSource` reconnect semantics | We've replaced the browser default with a richer client; the spec-level guarantee is still useful |
| `Last-Event-ID` resume is in the spec; no custom protocol needed                  | Requires per-event IDs (we'll generate them)                                                      |
| No `Upgrade: websocket` proxy gymnastics                                          | Cloudflare needs gzip disabled for SSE responses (one header)                                     |

WebSocket was considered and rejected: bidirectional capability isn't needed (REST handles all client→server), and the proxy and reconnect-logic burden is real.

### Endpoint shape

```
GET    /api/v1/events                          → SSE stream
POST   /api/v1/events/subscriptions            → add topics to current session
DELETE /api/v1/events/subscriptions/<topic>    → remove a topic
```

**The stream endpoint** is long-lived. JWT auth happens at chi middleware level (existing). On connect the server:

1. Allocates a `session_id` (UUID).
2. Resolves the user's eligibility (which `admins`/`broadcast` recipients apply).
3. Loads initial subscription set from query params (back-compat with current `?type=foo` filter syntax).
4. Emits a `ready` event whose payload includes the `session_id`.
5. Begins the broker-subscribed loop.

**The subscription endpoints** are stateful REST calls that mutate broker state for an existing session. They take a `session_id` (header or cookie) and a topic name. The broker validates the user has eligibility for that topic before adding it. Returns 204 on success.

Topic-name shape: `<domain>.<event>[:<scope>]`. Examples: `scan.progress:library_42`, `download_jobs.updated`, `notifications.delivered`. The scope qualifier is optional; topics without scope match all events of that type the user can see.

**Why REST for the control plane, not WebSocket-style messages-on-the-stream:** SSE is one-way. Putting subscribe/unsubscribe on a side channel keeps the transport pure, makes the operations standard REST (auditable, retryable, idempotent), and avoids inventing a control-message protocol just for this.

### Session and subscription model

A session is a single connected SSE stream. The broker maintains, per session:

| Field                | Meaning                                                                     |
| -------------------- | --------------------------------------------------------------------------- |
| `session_id`         | UUID                                                                        |
| `user_id`            | The JWT subject                                                             |
| `subscribed_topics`  | Set of topic strings                                                        |
| `replay_buffer`      | Bounded ring of the last 200 events (or 5 minutes, whichever first)         |
| `outbound_channel`   | Bounded Go channel; backpressure drops oldest if the consumer can't keep up |
| `last_event_id_seen` | Bumped on each successful send                                              |
| `connected_at`       | For metrics + debugging                                                     |

Sessions are entirely in-process. If the API restarts, all sessions are gone; clients reconnect and re-subscribe. No persistence.

A single user can have many sessions (multiple tabs, mobile + desktop). Each is independent. Events targeted to the user are delivered to **all** their active sessions.

### Per-user scoping

Every event carries a `recipient` field. The broker filters per-session before emit:

| Recipient   | Meaning                                                             | Delivery                                          |
| ----------- | ------------------------------------------------------------------- | ------------------------------------------------- |
| `user:<id>` | One specific user                                                   | Only that user's sessions                         |
| `admins`    | All users with the relevant admin permission, resolved at emit time | Every active session belonging to a current admin |
| `broadcast` | Everyone                                                            | Every active session                              |

`broadcast` is intentionally rare — it's for things like a `system.maintenance_mode` flag. Most events are `user:<id>` or `admins`.

**No client-side filter.** The frontend events store sees only events for its user. This eliminates a class of bug where the wrong-tab-active sees data leaking from another user (today's broker fans out everything; even with HTTPS that's wrong on principle).

**Recipient resolution at emit time, not delivery time.** Same shape as the [notifications](../notifications/README.md) spec — newly-promoted admins don't get backfilled; newly-demoted admins still see events in flight.

### The producer API

The producer surface mirrors notifications: a Go package exporting typed constructors, one per event type:

```go
// Ephemeral, in-flight; no DB record beyond what the producer already keeps.
realtime.Emit(ctx, sse_events.ScanProgress(sse_events.ScanProgressPayload{
    ScanID:           scanID,
    LibraryID:        libraryID,
    FilesSeen:        seen,
    MediaItemsCreated: created,
}))

realtime.Emit(ctx, sse_events.DownloadJobUpdated(sse_events.DownloadJobUpdatedPayload{
    JobID:   jobID,
    Summary: summary,
}))
```

Each constructor declares:

1. **Event-type string** (`"scan.progress"`) — the topic name producers and subscribers reference
2. **Recipient resolution** (which user IDs, or `admins`, or `broadcast` — usually derived from the payload)
3. **Payload type** (compile-time-enforced)
4. **State pattern hint** (`SnapshotDelta` vs `NotifyRefetch` — documentary; affects whether the payload carries data or is a kick)

The constructor file is the event registry — same convention as notifications. One file per producing domain (`events_scan.go`, `events_downloads.go`, `events_notifications.go`). Grep finds the registry; no init-time string maps.

**Adding a new event type** is a structural change: add a constructor, add a payload type, callers compile against the new shape, the OpenAPI spec regenerates with the new event in the discriminated union. No string-keyed lookups, no silent-drop on typos.

### Event payload shape

On the wire, each SSE event has:

```
id: <ulid>                  // monotonically increasing, for Last-Event-ID resume
event: <topic-name>         // "scan.progress" etc.
data: <json>                // the typed constructor's serialized payload
```

The `id:` is a [ULID](https://github.com/ulid/spec) (or any sortable, unique string). Monotonicity matters for resume — the replay buffer is ordered by ID, and a client's `Last-Event-ID` header tells the broker where to resume.

JSON payloads use the same conventions as the REST API (camelCase fields, ISO-8601 timestamps). The OpenAPI spec declares the per-event payload type; the generated TypeScript client gets type-safe handlers.

## Reliability

### Heartbeats

A `ping` event fires every 15 seconds when no other event has been sent. Heartbeats serve two purposes:

1. Prevent intermediate proxies from idling out the connection (nginx default `proxy_read_timeout` is 60s).
2. Let the client distinguish "quiet but healthy" from "actually disconnected" — three missed heartbeats triggers reconnect.

Heartbeats carry a tiny payload (`{"ts": <unix-seconds>}`) so the client can sanity-check clock skew if needed.

### `Last-Event-ID` and bounded replay

The browser sends the `Last-Event-ID` header on reconnect automatically. The broker:

1. Looks up the session's replay buffer (or, for a fresh session reconnecting to the same `session_id`, the prior session's buffer if still in memory).
2. If `Last-Event-ID` matches a buffered event, sends every event with `id > last_event_id` in order.
3. If `Last-Event-ID` is older than the buffer's oldest entry, sends a `resume.gap` event indicating the client should refetch snapshots.
4. If the session is brand new or the buffer is empty, no replay — just start streaming live.

**Buffer sizing**: 5 minutes / 200 events per subscriber, whichever is hit first. The 5-minute window covers laptop sleep, network blips, and brief redeploys. Beyond that, the client refetches and re-syncs. Bigger buffers don't add real value at self-hosted scale.

**Buffer scope**: per-session, not per-user. Each tab maintains its own buffer because each tab's subscribed-topics set may differ. Memory cost is bounded (small JSON payloads × 200 × small N of sessions).

### Backpressure and slow consumers

Each session has a bounded outbound channel (say, 256 events deep). On overflow:

1. The broker logs the session ID and event type that overflowed.
2. The broker closes the channel and tears down the session.
3. The client's reconnect logic fires; it reconnects, replays via `Last-Event-ID`, possibly hits the `resume.gap` path and refetches.

Failing loud beats falling behind silently. A slow consumer can't backpressure the broker into delaying events for other sessions.

### Proxy hardening

Three settings are non-negotiable for SSE through any reverse proxy:

| Setting                                             | Purpose                                                                                        |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `proxy_buffering off;` (nginx) / equivalent         | Otherwise the proxy holds events in its buffer until the buffer fills or the read timeout hits |
| `Cache-Control: no-cache` (response header)         | Otherwise intermediate caches may try to cache the stream                                      |
| Long read timeout (nginx `proxy_read_timeout 24h;`) | Otherwise the proxy idles the connection at default 60s despite heartbeats                     |

The backend also sends `X-Accel-Buffering: no` on the SSE response as belt-and-suspenders — nginx honors this without needing the location-block config, useful for setups where the user hasn't customized nginx.

Cloudflare additionally requires disabling gzip on `text/event-stream` responses; SSE responses do not gzip well and Cloudflare's default compression breaks them. Documented in deployment notes; the backend sets `Content-Encoding: identity` for SSE.

### Retry semantics on the client

The client's reconnect strategy:

1. On connection loss, wait 500ms before first retry.
2. Exponential back-off up to 5s max.
3. **Reset the attempt counter on successful connection** (the bug in `SSE_TRANSPORT_FINDINGS.md` finding #3).
4. On reconnect, the browser sends `Last-Event-ID` automatically.

The current `@hey-api/openapi-ts`-generated SSE client doesn't reset on success and is regenerated on every `just web-genclient`, which makes direct fixes painful. The implementation note (open question #6) is to either pass tight `sseDefaultRetryDelay`/`sseMaxRetryDelay` config (bounds the damage) or replace the generated client with a hand-rolled one we own.

## State delivery patterns

A feature emitting realtime events picks one of two patterns. Both are first-class; the choice is per-feature.

### Pattern A — Snapshot + delta (payload carries state)

The server emits an initial snapshot when the subscription is established, then per-change deltas:

```
T+0  ↓  download_jobs.snapshot  → [{id: 1, progress: 30}, {id: 2, progress: 80}, ...]
T+1  ↓  download_jobs.updated   → {id: 1, progress: 35}
T+2  ↓  download_jobs.updated   → {id: 2, progress: 82}
```

Frontend integration with TanStack Query:

```ts
useEventsStore().on("download_jobs.snapshot", (data) => {
  queryClient.setQueryData(["download-jobs"], data);
});
useEventsStore().on("download_jobs.updated", (delta) => {
  queryClient.setQueryData(["download-jobs"], (old) => applyDelta(old, delta));
});
```

**Use when:**

- The data is small (≤1000 items, ≤a few KB per update)
- The broker has the data in memory anyway
- Updates are frequent (per-second)
- Per-user filtering is simple (event is already user-scoped)

### Pattern B — Notify-and-refetch (payload is a kick)

The server emits a "something changed" ping; the frontend reacts by invalidating its TanStack Query cache:

```
T+0  ↓  wants.invalidated     → {scope: "user"}
T+1  ↓  (frontend calls queryClient.invalidateQueries(['wants']))
T+2  ↑  GET /api/v1/wants     → fresh data
```

**Use when:**

- The data is large or expensive to recompute (full library page, complex joins)
- The REST endpoint already does the right auth filtering
- Update frequency is low (per-minute or less)
- Multiple views consume the same data — invalidation updates all of them automatically

Both patterns can coexist on the same SSE connection. The producer picks based on the data shape and frequency.

### Mixing

A feature can mix both. Example: download jobs page uses pattern A (live progress bars). Library content page uses pattern B (when a `library.changed` event fires, invalidate `['library', libraryID]`). Same SSE connection serves both.

## Relationship to notifications

The [notifications](../notifications/README.md) spec defines a durable, multi-channel event system with an outbox, preferences, templates, and channel adapters. Realtime defines an ephemeral, single-channel (SSE) event system with a broker, replay buffer, and typed producer API.

They are **separate systems** with **one explicit seam**: the notifications system's `in_app` channel adapter emits a single, generic SSE event (`notification.delivered`) after writing the outbox row.

```go
// In the notifications in_app channel adapter:
func (a *InAppAdapter) Deliver(ctx context.Context, row OutboxRow) error {
    realtime.Emit(ctx, sse_events.NotificationDelivered(sse_events.NotificationDeliveredPayload{
        Recipient:      row.RecipientUserID,
        NotificationID: row.ID,
        EventType:      row.EventType,
        Title:          row.RenderedTitle,
        Body:           row.RenderedBody,
        CreatedAt:      row.CreatedAt,
    }))
    return nil
}
```

The producer of a notification doesn't touch the realtime package directly. Calling `notifications.NewWantAvailable(...)` writes the outbox row; the `in_app` channel adapter handles the realtime emission as part of "delivery." The realtime system never sees notification-specific event types — only the generic `notification.delivered`.

**The bell-icon UI is fed by the outbox query**, not by listening to the SSE event in isolation:

1. Page load → REST query against `notification_outbox` (filtered to this user, `in_app` channel) → renders the list
2. SSE `notification.delivered` event fires → TanStack Query invalidates the bell-icon query → it refetches and updates

This is pattern B for the bell icon. The outbox row is the durable record; SSE is just the live kick. If the user is offline, they miss the SSE event but the row is still there on next page load.

**`realtime.Emit(...)` is for everything that isn't notification-shaped**: scan progress, download progress, subscription updates that aren't durable events. These never write to any database; they're transient by design.

## Frontend consumer model

**TanStack Query is the default store for API-shaped state.** Lists, items, paginated views — all live as TanStack queries. SSE handlers mutate (`setQueryData`) or invalidate (`invalidateQueries`) query keys based on the chosen pattern.

**Pinia stays the home for non-API global state**:

| Pinia store  | What it holds                                                                                       |
| ------------ | --------------------------------------------------------------------------------------------------- |
| Events store | SSE connection state (`connected`/`reconnecting`/etc.), subscription set, pub/sub listener registry |
| Auth store   | JWT claims, current user, role membership                                                           |
| Layout store | Sidebar collapsed/expanded, theme, viewport hints                                                   |
| Dialog store | Open-dialog stack                                                                                   |
| App store    | Global flags, build info                                                                            |

API-derived state that's currently in Pinia (e.g., `downloadJobs.ts`) is a directional target for migration to TanStack Query under this guideline — not pinned as a hard requirement here, but the new-feature default.

The events store exposes a thin pub/sub:

```ts
useEventsStore().on('scan.progress', (data) => { ... })   // returns an unsubscribe fn
useEventsStore().off('scan.progress', cb)
useEventsStore().subscribe(['scan.progress', 'scan.completed'])    // calls POST /events/subscriptions
useEventsStore().unsubscribe(['scan.progress'])                    // calls DELETE /events/subscriptions/scan.progress
```

Composables wrap this for common patterns:

```ts
useSSEMutation(["download-jobs"], "download_jobs.updated", applyDelta);
// Sugar over: events.on(...) → queryClient.setQueryData(...)

useSSEInvalidation(["wants"], "wants.invalidated");
// Sugar over: events.on(...) → queryClient.invalidateQueries(...)
```

Component code rarely touches the events store directly; it composes against the higher-level hooks.

## Does NOT own

- **The notification system** — outbox, preferences, templates, push subscriptions, email delivery. [Notifications spec](../notifications/README.md) owns all of that. Realtime is just one transport the in-app channel uses.
- **Auth on the stream** — JWT verification happens at chi middleware before this spec's handler runs. The handler only reads `ClaimsFromContext`.
- **State persistence** — realtime is ephemeral by definition. No tables, no migrations.
- **Per-event-type ACL evaluation beyond recipient tags** — finer-grained scoping (e.g., "this scan progress event is for library 42, only users with `library.read:42` should see it") is built into the recipient resolution at constructor level, not in the broker. The broker trusts the constructor.
- **Frontend state architecture beyond the connection layer** — TanStack Query vs Pinia for specific features is per-feature; this spec gives the default guideline.

## Interactions

| Neighbor                                                                        | How realtime interacts                                                                                                                                                     |
| ------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[Notifications](../notifications/README.md)**                                 | Notifications' `in_app` channel adapter calls `realtime.Emit(notification.delivered)` after writing the outbox row. Realtime never sees notification-specific event types. |
| **[Users](../users/README.md)**                                                 | Recipient resolution for `admins` consults the permission system to enumerate current admins at emit time. JWT subject identifies the user on the stream.                  |
| **[Scan](../scan/README.md)**                                                   | Primary producer of `scan.*` events (started/progress/completed/failed). Pattern A — payload carries progress data.                                                        |
| **[Acquisition](../acquisition/README.md)**                                     | Producer of `download_jobs.snapshot` (on connect) and `download_jobs.updated` (per change). Pattern A.                                                                     |
| **[Hygiene](../hygiene/README.md)**                                             | Producer of `hygiene.audit_started` / `hygiene.audit_completed` and possibly per-finding deltas. Pattern depends on UI needs.                                              |
| **[Connectivity-health pattern](../../patterns/connectivity-health/README.md)** | Producer of `<resource>.health` events on transitions. Pattern A — payload carries new status.                                                                             |
| **[Matching](../matching/README.md)**                                           | Producer of `match.dropped_in` events for live unmatched-files UI. Pattern A or B per feature owner.                                                                       |
| **[Audit pattern](../../patterns/audit/README.md)**                             | Audit pattern's activity view consumes `audit.row_created` events for live-updating the feed. Pattern B — refetch the activity view.                                       |

## Open questions

1. **Session ID transport on control-plane endpoints.** Should the `session_id` be sent as a header (`X-Realtime-Session`), a cookie set when the stream opens, or a URL param? Header is cleanest; cookie is automatic but cross-tab-leaky; URL param is awkward for `DELETE`. Lean: header.
2. **Topic eligibility model.** Subscribing to `scan.progress:library_42` requires the user to have `library.read:42`. Where does this check live — in the control-plane endpoint, or in the broker on each emit? Lean: at subscribe time (cheap, one-shot); on permission change, all active subscriptions for that user re-validate.
3. **Replay buffer scope.** Per-session (each tab has its own) vs per-user (shared across the user's tabs). Per-session is simpler and matches how `Last-Event-ID` semantics actually work in browsers. Lean: per-session.
4. **Reconnect-to-same-session.** When the client reconnects within seconds, should it reuse its prior `session_id` (preserving subscription set and replay buffer) or always allocate fresh? Reusing saves a re-subscribe roundtrip. Lean: allow client to send `?session=<id>` on reconnect; broker honors it if the session is still in memory, otherwise allocates fresh.
5. **`broadcast` events — do we actually need them?** Currently no real producer uses them. Maintenance-mode toggles, version-update banners are the candidates. Lean: keep the recipient kind in the model but don't ship a v1 producer; first concrete need drives it.
6. **Replace the generated SSE client?** `@hey-api/openapi-ts`'s generated SSE client has the never-reset-on-success retry bug. Options: (a) keep it and bound the damage with tight `sseDefaultRetryDelay`/`sseMaxRetryDelay`, (b) replace with a hand-rolled client we own, (c) contribute the fix upstream. Lean: bound it for v1, contribute upstream as a follow-up.
7. **Side-channel subscribe failure handling.** If `POST /events/subscriptions` fails (server restart, network blip), the stream keeps running with stale subscriptions. Does the client retry, refetch the current subscription set on reconnect, or fail loud? Lean: refetch on reconnect via a `GET /events/subscriptions` introspection endpoint.
8. **Migration from current event types to typed constructors.** Existing 9 event types are hardcoded; migration converts them to the constructor pattern. Concrete pass — touches `events.go`, frontend consumers, and OpenAPI spec. Lean: do it in one PR alongside this spec's first implementation phase.
9. **Backpressure threshold tuning.** 256-event outbound channel is a guess. Real number depends on observed worst-case (large download job storm, scan with thousands of files). Lean: ship 256, instrument the overflow path, tune from real data.
10. **`resume.gap` UX.** When the replay buffer is exhausted and the client gets a `resume.gap` event, what does the UX look like? Brief toast ("Reconnected — refreshing data") or silent refetch? Lean: silent refetch; the toast wastes attention on a routine network event.
11. **Heartbeat cadence.** 15s is the current value. Some setups (especially behind aggressive proxies) might want 10s; others might want 30s to save bandwidth. Lean: keep 15s; expose as a setting if the support load demands.
12. **Cross-browser-tab coordination.** Multiple tabs from the same user each open their own session. Should the events store coordinate via `BroadcastChannel` to share a single underlying connection? Saves connections; complicates the model. Lean: defer — one connection per tab is fine at self-hosted scale.

## What we're explicitly not deciding here

- Exact Go signatures of `realtime.Emit` and the typed constructors — directional only
- The exact `sse_events.X` package layout (one file vs many)
- The ULID library choice or alternative ID schemes
- The exact replay buffer data structure (ring buffer vs deque)
- Per-event-type wire shapes — those are owned by each producing feature's spec
- The full nginx / Cloudflare / Traefik configuration matrix — covered in deployment docs
- Whether to support HTTP/2 server push as a future alternative (premature; the SSE story is the answer)
- Frontend testing strategy for SSE-consuming components — implementation
- Observability beyond log messages (metrics, traces) — out of v1 scope; covered separately

## V1 must-fix infrastructure debt

Pulled forward from `SSE_TRANSPORT_FINDINGS.md`. These ship alongside the spec's first implementation phase:

1. **Nginx buffering.** Add the SSE-specific location block to `docker/s6/s6-rc.d/nginx/default.conf` and `default.dev.conf`. Set `X-Accel-Buffering: no` on the SSE response as backup.
2. **Retry-counter reset.** Bound `sseDefaultRetryDelay` and `sseMaxRetryDelay` from the events store as a near-term fix; consider replacing the generated client (open question #6) as the durable fix.
3. **Stale-closure race in events store.** Add the generation-counter pattern to `web/src/stores/events.ts`'s status writes so a torn-down connection can't overwrite a fresh connection's status.

## Doc neighbors

- [Notifications](../notifications/README.md) — sibling system; its `in_app` channel adapter emits `notification.delivered` via this spec's `realtime.Emit`
- [Users](../users/README.md) — JWT subject identifies the user on the stream; `admins` recipient resolution consults the permission system
- [Scan](../scan/README.md) — primary realtime producer for in-flight scan progress (pattern A)
- [Acquisition](../acquisition/README.md) — primary realtime producer for download-jobs snapshot and updates (pattern A)
- [Connectivity-health pattern](../../patterns/connectivity-health/README.md) — emits `<resource>.health` events on transitions, consumed by admin dashboards
- [Audit pattern](../../patterns/audit/README.md) — sibling cross-cutting system; activity view consumes a realtime event to live-update (pattern B)
- [Errors pattern](../../patterns/errors/README.md) — typed errors flow through both REST and SSE; clients handle them uniformly
- `backend/internal/sse/broker.go`, `backend/internal/http/handlers/events.go`, `web/src/stores/events.ts` — the existing implementation this spec evolves
- `specs/modules/realtime/SSE_TRANSPORT_FINDINGS.md` — investigation of the three v1 must-fix issues
