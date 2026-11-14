---
status: as-built
---

# Realtime — SSE transport, broker, and producer API

> **Beyond what's built:** topic-scoped delivery is plumbed but unused (no event carries a topic, and [topic eligibility](#topic-eligibility) admits any topic); subscribe-returns-snapshot, permission-change revalidation, [per-viewer payloads](#per-viewer-payloads), the [`notification.delivered` seam](#relationship-to-notifications), and the `useSSEMutation` / `useSSEInvalidation` composables describe unbuilt design.

This doc defines how Arrflix delivers in-flight, ephemeral events from server to browser: the SSE transport choice, the multiplexed-subscription model, the per-user session broker, the typed producer API (`realtime.Emit`), the reliability primitives (heartbeats, `Last-Event-ID` resume, bounded replay), and the relationship to the [notifications](../notifications/README.md) system. The **transport writer is owned outright**: the huma SSE adapter can't carry the sortable string IDs that `Last-Event-ID` resume requires, so the wire layer is a small forked `Register` we maintain (see [Transport](#transport)). This doc does **not** pin column types, exact Go signatures, or per-feature event types — those are implementation or feature-spec concerns.

**Scope boundary:** this module is **server→browser only**. Server→server work dispatch (a worker waking on new rows without polling) is a different reliability contract — it must not drop events — and lives in the [work-dispatch pattern](../../patterns/work-dispatch/README.md). The two are siblings, not the same bus; conflating them forces at-least-once semantics onto a deliberately lossy fan-out.

## TL;DR

- **SSE, not WebSocket.** One-way server→client is what we actually need. SSE is HTTP-native, the browser handles reconnect, `Last-Event-ID` resume is in the spec, and the proxy story is one `proxy_buffering off` line instead of `Upgrade: websocket` everywhere.
- **We own both ends of the SSE transport.** The huma SSE adapter (`humasse`) isn't used on the wire layer because its `Message.ID` is an `int` — it physically cannot carry the sortable UUIDv7 needed for restart-safe resume — and because it dispatches event names by Go reflection, forcing a distinct type per event. The replacement is a forked `Register` (`handlers/events_stream.go`) that keeps the OpenAPI `oneOf` generation (so payload types reach the TS client), emits string IDs, sets response headers, and reads the event name from a field. On the frontend, a small hand-rolled reader (`web/src/lib/sseClient.ts`) replaces the `@hey-api` generated SSE client, which resolves its URL and headers once and so can't inject `?session=` or a refreshed token on reconnect.
- **Single stream endpoint + side-channel control plane.** Client opens one long-lived `GET /api/v1/events` and stays there. Subscribe/unsubscribe to topics happens via REST (`POST /api/v1/events/subscriptions`, `DELETE /api/v1/events/subscriptions/<topic>`). Multiplexing without dropping the connection; no WebSocket needed. The control plane is built; nothing in the app subscribes yet.
- **Per-user scoping at the broker.** Every event carries a recipient (`user:<id>`, `capability:<key>`, or `broadcast`). The broker filters per subscriber before send — no client-side filter, no event ever crosses into a session that shouldn't see it.
- **`Last-Event-ID` + bounded replay buffer.** Per-session ring buffer holds the last 5 minutes / 200 events. Clean resume across tab-sleep, brief drops, and redeploys. Beyond the window, the client refetches snapshots.
- **Typed producer API: `realtime.Emit(ctx, broker, realtime.X(...))`.** Each event has a Go constructor with a typed payload, and **carries its event name as a field** (not via reflection on its Go type). `realtime.Registry` lists every event for spec-gen; runtime emit reads the name off the event, so the two can't drift. Grep finds it; the compiler enforces shape.
- **Two distinct producer APIs, on purpose.** `realtime` is ephemeral (scan progress, download progress, transient state) — no DB. [`notifications`](../notifications/README.md) is durable (want available, hygiene errors) — owns the outbox. They coexist. **Unbuilt:** a generic `notification.delivered` SSE kick from the `in_app` adapter; the bell polls instead.
- **Name events for what the UI renders, not what the DB stores.** An event named after a table pushes a join onto every client that receives it. Multiple surfaces doing that join independently will diverge — see [Naming](#naming-events-after-concepts-not-tables).
- **Payloads may be per-viewer, not just routed per-viewer.** Recipient scoping decides *who gets an event*; a per-viewer payload also changes *what it contains*. Bounded by topic scope — see [Per-viewer payloads](#per-viewer-payloads). No event uses one yet: `title_status` is a kick, and each client refetches its own per-viewer answer over REST.
- **State delivery is per-feature.** Delta (inline payload, mutate cache via `setQueryData`) for high-frequency, small, simple-auth state. Notify-and-refetch (kick + `invalidateQueries`) for low-frequency, mutated-via-REST state. The spec gives the heuristic; per-feature owners pick.
- **TanStack Query is the default consumer.** API-shaped state lives in TanStack Query; one bindings module (`web/src/realtime/bindings.ts`) mutates or invalidates query keys. The connection itself is a module singleton, not a Pinia store.

## Why this is its own spec

Realtime spans an in-process broker (`backend/internal/sse`), a typed producer package (`backend/internal/realtime`), the owned stream writer and control plane (`backend/internal/http/handlers/events*.go`), and a frontend connection and cache-binding layer (`web/src/realtime/`, `web/src/lib/sseClient.ts`). Without a spec, the system grows by each feature inventing its own wire shape, and per-user notifications and operator-only data turn naive global broadcast into a correctness bug — requesters see operator data, users see each other's events.

This spec captures the contract: an in-process fan-out broker with per-session recipient scoping, resume, a typed producer API, an owned wire writer (the huma adapter can't carry string IDs — see [Transport](#transport)), and the integration seam with notifications.

## The model

### Transport

**SSE (`text/event-stream`)** over HTTP/1.1. The choice is deliberate:

| Reason                                                                            | Trade-off                                                                                         |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| HTTP-native; works through any HTTP-aware proxy with one config line              | One-way only; client→server is REST                                                               |
| Browser handles reconnect automatically via the `EventSource` reconnect semantics | We use a fetch-based reader with its own reconnect loop (bearer auth needs headers `EventSource` can't send); the spec-level semantics still apply |
| `Last-Event-ID` resume is in the spec; no custom protocol needed                  | Requires per-event IDs (we generate them)                                                         |
| No `Upgrade: websocket` proxy gymnastics                                          | Cloudflare needs gzip disabled for SSE responses (one header)                                     |

WebSocket was considered and rejected: bidirectional capability isn't needed (REST handles all client→server), and the proxy and reconnect-logic burden is real.

#### The wire writer is ours, not huma's

The stream is registered by `registerEventStream` (`handlers/events_stream.go`), a fork of `humasse.Register`. Three concrete reasons, all from the library's design:

1. **`humasse.Message.ID` is an `int`**, written as `id: %d`. It cannot carry a sortable string. An int counter also resets to 0 on restart, so a reconnecting client's `Last-Event-ID` would point "into the future" and force a `resume_gap` refetch on every redeploy — defeating resume's purpose. We emit a **UUIDv7** (time-ordered, restart-safe, globally unique, no extra dependency).
2. **The handler can't set response headers.** huma's adapter sets `Content-Type` before calling our handler and hands us only the stdlib `context.Context`, not the `huma.Context` — so there's no hook for `X-Accel-Buffering: no` / `Cache-Control: no-cache`. Owning the writer makes these one-liners.
3. **Event-name dispatch is by `reflect.TypeOf`**, which forces a distinct Go type per event. Owning the writer lets the name be a field on the event and collapses to the single registry.

What we **keep**: huma's OpenAPI generation. The fork builds the `oneOf`-array schema from `realtime.Registry` (one schema per event, `data` derived from the payload sample, `id` typed as `string`), so payload types such as `ReadyPayload` and `TitleStatusPayload` land in the generated TypeScript types. The fork uses only public huma surface (`huma.Register`, `huma.StreamResponse`, `ctx.BodyWriter()`, `ctx.SetHeader`); the maintenance cost is re-checking it against that stable surface on `huma/v2` bumps.

The **frontend** transport is owned too. The `@hey-api/openapi-ts`-generated SSE client resolves its request URL and headers once and reuses them across reconnects, so it can't inject a freshly captured `?session=` (which replay needs) or a refreshed bearer token. `web/src/lib/sseClient.ts` is a minimal fetch-based reader that re-resolves both on every attempt, tracks `id:` and resends it as `Last-Event-ID`, and resets its backoff after a successful connect — see [Retry semantics](#retry-semantics-on-the-client).

### Endpoint shape

```
GET    /api/v1/events                          → SSE stream
GET    /api/v1/events/subscriptions            → list current session's topics (resync after reconnect)
POST   /api/v1/events/subscriptions            → add topics to current session
DELETE /api/v1/events/subscriptions/<topic>    → remove a topic
```

**The stream endpoint** is long-lived. JWT auth happens at chi middleware level. On connect the server:

1. Reattaches to the `?session=<id>` session if it is still in memory and owned by this user; otherwise allocates a fresh `session_id` (UUID).
2. Resolves the user's capability set (`AuthzService.EffectiveKeys`, the global keys they hold), once per attach.
3. Emits a `ready` event whose payload (`{ok, sessionId}`) carries the `session_id`.
4. Replays missed events, or emits `resume_gap` (see [Last-Event-ID](#last-event-id-and-bounded-replay)).
5. Begins the broker-subscribed loop.

A fresh session starts with no topics; there are no connect-time topic query parameters.

**The subscription endpoints** are stateful REST calls that mutate broker state for an existing session. The `session_id` travels in the **`X-Realtime-Session` request header** (decided: header over cookie — cookies leak across tabs, and each tab is its own session; over URL param — awkward for `DELETE`). The handler checks the user's eligibility for the topic before adding it (see [eligibility](#topic-eligibility)). `POST`/`DELETE` return 204; `GET` returns the current topic set so a reconnecting client can resync rather than blindly re-POST. A session id the user doesn't own is a 404.

**Subscribe returns its snapshot in the REST response, not on the stream.** **Unbuilt:** `POST` always returns 204, and clients seed from the relevant query's REST baseline. When a `POST /subscriptions` adds a topic whose feature uses [snapshot+delta](#pattern-a--snapshot--delta-payload-carries-state), the initial snapshot is the `POST`'s 200 body — *not* a separate event pushed onto the SSE stream. This sidesteps the ordering race (204 returns, but the snapshot would arrive async on a different channel the client has to correlate) and keeps snapshot production out of the broker. The stream then carries only deltas. Topics that are pure notify-and-refetch return 204 with no body.

Topic-name shape: `<domain>.<event>[:<scope>]`. Examples: `scan.progress:library_42`, `title.status:movie:42`. No producer sets a topic yet, so every built event is default-delivery. The scope qualifier is what makes dynamic subscription earn its keep — high-cardinality, page-bound streams (a single title's live state) can be subscribed and dropped on navigation without tearing down the long-lived stream.

**Subscribing is opt-in, never a filter.** An event either carries a topic or it doesn't. A topic-carrying event reaches only sessions subscribed to that exact topic; an event without one reaches every eligible session regardless of what it has subscribed to. So subscribing to a page-scoped stream can never blackhole the events a session gets by default. (An additive-to-restrictive filter, where an empty set means "everything" and the first subscription silently narrows the session, is the trap this avoids.)

**Why REST for the control plane, not WebSocket-style messages-on-the-stream:** SSE is one-way. Putting subscribe/unsubscribe on a side channel keeps the transport pure, makes the operations standard REST (auditable, retryable, idempotent), and avoids inventing a control-message protocol just for this.

### Session and subscription model

A realtime session (`sse.Session`) is one logical SSE subscription, distinct from the login [`user_session`](../users/README.md). It outlives a single TCP connection: when the connection drops the session is *detached* but kept, with its replay ring, for 5 minutes (`detachTTL`) so a reconnect can reattach; a sweeper evicts it after that. The broker maintains, per session:

| Field          | Meaning                                                                     |
| -------------- | --------------------------------------------------------------------------- |
| `ID`           | UUID                                                                        |
| `UserID`       | The JWT subject                                                             |
| `topics`       | Opt-in topic set                                                            |
| `capabilities` | Permission keys resolved at attach                                          |
| `replay`       | Bounded ring of the last 200 events (or 5 minutes, whichever first)         |
| `out`          | The current attachment's outbound channel (256 deep); nil while detached    |
| `kick`         | Signals the attached handler to tear down on overflow                       |
| `epoch`        | Bumped on every (re)attach so a stale handler's teardown can't kill a fresh connection |
| `connectedAt`  | For debugging                                                               |

Sessions are entirely in-process. If the API restarts, all sessions are gone; clients reconnect and refetch. No persistence.

A single user can have many sessions (multiple tabs, mobile + desktop). Each is independent. Events targeted to the user are delivered to **all** their active sessions.

### Per-user scoping

Every event carries a `recipient` field. The broker filters per-session before emit:

| Recipient          | Meaning                              | Delivery                                             |
| ------------------ | ------------------------------------ | ---------------------------------------------------- |
| `user:<id>`        | One specific user                    | Only that user's sessions                            |
| `capability:<key>` | Holders of an authz permission key   | Sessions whose connect-time key set contains it      |
| `broadcast`        | Everyone                             | Every active session                                 |

`broadcast` is intentionally rare — it's for things like a `system.maintenance_mode` flag. Most events are `user:<id>` or `capability:<key>`.

**Capability targeting, not an `admins` tag.** The permission model gates on grants, not role identity, so an "admins" tag has no honest implementation. A capability key is the same idea made checkable — `capability:jobs.read` names exactly the grant that gates the REST endpoint serving the same data, so the two cannot drift.

**The zero recipient targets nobody.** Delivery fails closed, so an event that forgot its recipient surfaces as a missing update rather than as a silent disclosure.

Capability keys match literally, which suits the flat keyspace. Resource-scoped eligibility — `library.read` on library 42 — belongs on the topic's scope qualifier, not the recipient.

**No client-side filter for scoping.** The frontend connection sees only events for its user. This eliminates a class of bug where one user's tab sees data leaking from another user. (Client-side *listener* filtering still exists for the orthogonal job of "which of my eligible topics is this page interested in" — that's cheap and unrelated to scoping correctness.)

**Recipient is a coarse tag; eligibility is resolved per-session at connect — not per-emit.** The event carries a recipient tag (`user:<id>` / `capability:<key>` / `broadcast`). Each *session* resolves its capability set **once, at attach**. The broker then filters by cheap set-membership — **no DB or permission-system call on the emit path.** This matters because high-frequency producers (scan progress per file, download progress per second) would otherwise hammer the permission system on every emit. The cost is bounded staleness: a granted permission reaches a session at its next (re)connect, and a revoked one keeps applying until then — reattach re-resolves, so a reconnect is enough. At self-hosted scale and reconnect cadence that window is negligible, and it matches the resolve-at-creation posture of the [notifications](../notifications/README.md) spec.

<a id="topic-eligibility"></a>**Topic eligibility is checked at subscribe time**, against the same connect-time eligibility set. Subscribing to `scan.progress:library_42` requires the user to hold `library.read:42`; the control-plane endpoint validates this once and admits the topic. On a permission change, the user's active sessions re-validate their subscription set (drop now-ineligible topics) — driven off the same signal that recomputes eligibility. The broker does not re-check eligibility on every emit; it trusts the admitted topic set. **Unbuilt:** the `canSubscribe` seam admits any topic for any authenticated user, and nothing re-validates on permission change.

Current recipients: `download_job_updated` and `import_task_updated` target `jobs.read`; `proposal_updated` targets `jobs.manage`; `scan_*`, `title_status` and `title_progress` target `library.read`. `want_updated` is broadcast — scoping it correctly needs the tracking's requesters resolved at emit, which is the per-viewer computation [title status](../title-status/README.md) does properly, so narrowing it here would be thrown away. `ready`, `ping` and `resume_gap` are written straight to one stream by the handler.

<a id="per-viewer-payloads"></a>

### Per-viewer payloads

Recipient scoping answers *who receives an event*. Some producers additionally need the **payload itself** to differ per viewer — a requester and an operator looking at the same title get different available actions, and the operator's payload carries release candidates and file paths that the requester's simply does not include.

This is a deliberate extension of the scoping model, and it inverts a cost assumption. A recipient tag lets one serialized payload fan out to N sessions; a per-viewer payload means **N payloads for one state change**.

It is admissible when **the topic is narrow enough that N is small**. A title-scoped topic is subscribed by the sessions actually looking at that title — usually one, in a household perhaps two. Fan-out is bounded by real viewers rather than by user count, and the cost is paid on transitions rather than on ticks.

Three rules keep it from becoming the default:

1. **Per-viewer payloads are for scoped topics only.** A global topic with a per-viewer payload is a fan-out multiplier with no bound, and is not admissible.
2. **The high-frequency channel stays viewer-independent.** Where a producer splits into a rare full event and a frequent tick, only the rare one may be per-viewer. Ticks carry numbers, and numbers are the same for everyone.
3. **Withhold by omission, not by convention.** If a viewer should not see a field, that field is absent from their payload. Sending it and trusting the client not to render it is not scoping — it is a rendering decision applied to data that already leaked.

The third rule is not hypothetical: a broadcast download-job payload carries candidate release titles, so a requester's cache would hold them even if the status card declined to display them. `download_job_updated` targets `jobs.read` for this reason.

### Naming events after concepts, not tables

**An event is named after what a consumer renders, not after the row that changed.**

An event named after a table carries no meaning on its own. `want_updated` tells a client that a row changed; it does not tell it what is now true about the thing the user is looking at. To get from one to the other, every consumer joins that event against whatever else it has cached — and consumers with different caches, written at different times, join differently.

The failure mode is not that one of them is wrong. It is that **they are independently derived, so they diverge**, and each divergence looks like its own unrelated bug. Fixing them one at a time never converges, because the mechanism that produces them is untouched.

The rule:

- ✅ `title_status` — what is true about this title, for this viewer, now
- ❌ `want_updated`, `tracking_updated`, `download_job_updated` — what row changed

**A row event is fine when exactly one surface consumes it** and that surface owns the row — an admin table of download jobs, for instance. It stops being fine the moment a second surface needs the same concept, because that is the point at which two derivations exist.

Two practical consequences:

- **The payload must be self-locating.** A consumer keyed on TMDB id cannot use an event whose only title reference is an internal media id; it would have to maintain its own mapping, which is another derivation. An event carries enough identity to be applied without a lookup.
- **Coalesce on the producer side.** A concept-level event fires when the concept changes, which may be once for nine underlying row changes. Coalescing belongs on the server, where the bound is known — a client-side debounce with no maximum wait can be starved indefinitely by exactly the burst it was added to smooth.

[Title status](../title-status/README.md) is the worked example: one projection replacing independent client-side derivations of the same concept. `internal/titlenotify` is its producer-side coalescer: callers announce "this media item changed", and it flushes at most one `title_status` per title every 250ms, backed by a 30s sweep that catches any write without an explicit call.

### The producer API

The producer surface is the `internal/realtime` package: typed constructors, one per event type, grouped by producing domain (`events_scan.go`, `events_downloads.go`, `events_titles.go`, …):

```go
// Ephemeral, in-flight; no DB record beyond what the producer already keeps.
realtime.Emit(ctx, broker, realtime.ScanProgress(realtime.ScanProgressPayload{ ... }))
realtime.Emit(ctx, broker, realtime.DownloadJobUpdated(job))
```

Each constructor returns a `realtime.Event` carrying:

1. **Event name** (`"scan_progress"`) — snake_case, emitted directly as the `event:` line (read from this field, **not** from `reflect.TypeOf`).
2. **Recipient tag** (`realtime.User(id)`, `realtime.Capability(key)`, or `realtime.Broadcast`). A coarse tag the broker filters against per-session eligibility; see [Per-user scoping](#per-user-scoping).
3. **Payload** — marshaled from a typed struct (compile-time-enforced).

`Emit` stamps a UUIDv7 ID and publishes onto the broker. `realtime.Registry` lists every event name with a zero-value payload sample; the stream writer enumerates it to build the OpenAPI `oneOf`. Runtime emit never consults the registry, so the two consumers can't drift.

The built events: `ready`, `ping`, `resume_gap`, `title_status`, `title_progress`, `download_job_updated`, `want_updated`, `proposal_updated`, `import_task_updated`, `scan_started`, `scan_progress`, `scan_completed`, `scan_failed`.

**Adding a new event type** is a structural change: add a constructor, add a payload type and a `Registry` entry, and the OpenAPI spec regenerates with the new event in the discriminated union. No string-keyed lookups, no silent-drop on typos.

### Event payload shape

On the wire, each SSE event has:

```
id: <uuidv7>                // time-ordered, for Last-Event-ID resume
event: <event-name>         // "scan_progress" etc.
data: <json>                // the typed constructor's serialized payload
```

The `id:` is a UUIDv7 (any sortable, unique string would do). Monotonicity matters for resume — the replay buffer is ordered by ID, and a client's `Last-Event-ID` header tells the broker where to resume.

JSON payloads use the same conventions as the REST API (camelCase fields, ISO-8601 timestamps). The OpenAPI spec declares the per-event payload type; the generated TypeScript client gets type-safe handlers.

## Reliability

### Heartbeats

A `ping` event fires every 15 seconds, whether or not other events were sent. Heartbeats serve two purposes:

1. Prevent intermediate proxies from idling out the connection (nginx default `proxy_read_timeout` is 60s).
2. Let the client distinguish "quiet but healthy" from "actually disconnected". **Unbuilt:** the client has no missed-heartbeat timeout; it reconnects only when the fetch stream ends or errors.

Heartbeats carry a tiny payload (`{"ts": <unix-seconds>}`) so the client can sanity-check clock skew if needed.

### `Last-Event-ID` and bounded replay

The client resends the last `id:` it saw as `Last-Event-ID` on every reconnect. The broker:

1. Looks up the `?session=` session's replay buffer, if the session is still in memory.
2. If `Last-Event-ID` matches a buffered event, sends every event after it in order.
3. If `Last-Event-ID` is older than the buffer's oldest entry, sends a `resume_gap` event indicating the client should refetch.
4. If the session is brand new or the buffer is empty, no replay — just start streaming live.

The ring buffers events even while the session is detached, so a briefly-gone client accumulates what it missed.

**Buffer sizing**: 5 minutes / 200 events per session, whichever is hit first. The 5-minute window covers laptop sleep, network blips, and brief redeploys. Beyond that, the client refetches and re-syncs. Bigger buffers don't add real value at self-hosted scale.

**Buffer scope**: per-session, not per-user. Each tab maintains its own buffer because each tab's subscribed-topics set may differ. Memory cost is bounded (small JSON payloads × 200 × small N of sessions).

**Reconnect-to-same-session**: on reconnect the client sends `?session=<id>` (the `session_id` from the `ready` event). The broker honors it if that session is still in memory and belongs to the same user — preserving its subscription set *and* replay buffer, and refreshing its capability set. If the session has been evicted (restart, 5-minute detach TTL), the broker allocates a fresh `session_id` and returns it in a new `ready`. The client fires a resync on every recovered drop, which invalidates every active query (`bindings.ts`).

### Backpressure and slow consumers

Each attachment has a bounded outbound channel, 256 events deep. The event is appended to the replay ring before the send is attempted. On overflow:

1. The broker logs the session ID and event type that overflowed (to stderr).
2. The broker kicks the handler, which ends the connection and detaches the session. The session and its ring are kept.
3. The client's reconnect logic fires; it reattaches, replays via `Last-Event-ID`, possibly hits the `resume_gap` path and refetches.

Failing loud beats falling behind silently. A slow consumer can't backpressure the broker into delaying events for other sessions.

### Proxy hardening

Three settings are non-negotiable for SSE through any reverse proxy:

| Setting                                             | Purpose                                                                                        |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `proxy_buffering off;` (nginx) / equivalent         | Otherwise the proxy holds events in its buffer until the buffer fills or the read timeout hits |
| `Cache-Control: no-cache` (response header)         | Otherwise intermediate caches may try to cache the stream                                      |
| Long read timeout (nginx `proxy_read_timeout 24h;`) | Otherwise the proxy idles the connection at default 60s despite heartbeats                     |

The bundled nginx (`docker/s6/s6-rc.d/nginx/default.conf` and `default.dev.conf`) has a `location /api/v1/events` block with `proxy_buffering off`, `proxy_cache off`, and `proxy_read_timeout 24h`. The backend also sends `Cache-Control: no-cache` and `X-Accel-Buffering: no` on the SSE response as belt-and-suspenders — nginx honors the latter without the location-block config, useful for setups where the user hasn't customized nginx.

Cloudflare additionally requires disabling gzip on `text/event-stream` responses; SSE responses do not gzip well and Cloudflare's default compression breaks them. **Unbuilt:** the backend does not set `Content-Encoding: identity`, and there are no deployment notes for it.

### Retry semantics on the client

The client's reconnect strategy (`web/src/lib/sseClient.ts`):

1. On connection loss, wait 500ms before first retry.
2. Exponential back-off, doubling up to 5s max.
3. **Reset the attempt counter on successful connection.**
4. On every attempt, rebuild the URL (so `?session=` carries the latest session id), re-read the bearer token, and send `Last-Event-ID`.

The generated `@hey-api/openapi-ts` SSE client is not used for the stream: it doesn't reset its attempt counter on success, and it can't re-resolve the URL or token per attempt.

## State delivery patterns

A feature emitting realtime events picks one of two patterns. Both are first-class; the choice is per-feature.

### Pattern A — Snapshot + delta (payload carries state)

The client seeds from a REST baseline (or, with topic subscriptions, the subscribe response — unbuilt), then applies per-change deltas that carry full state:

```
T+0  ↑  GET /api/v1/download-jobs     → [{id: 1, progress: 30}, {id: 2, progress: 80}, ...]
T+1  ↓  download_job_updated          → {id: 1, progress: 35, ...}
T+2  ↓  download_job_updated          → {id: 2, progress: 82, ...}
```

Frontend integration with TanStack Query (`web/src/realtime/bindings.ts`):

```ts
on("download_job_updated", (data) => {
  qc.setQueryData(downloadJobsListQueryKey(), (prev) => upsertJob(prev, data));
});
```

`want_updated` works the same way, patching the want into every cached `trackingByTmdb` entry.

**Use when:**

- The data is small (≤1000 items, ≤a few KB per update)
- The broker has the data in memory anyway
- Updates are frequent (per-second)
- Per-user filtering is simple (event is already user-scoped)

### Pattern B — Notify-and-refetch (payload is a kick)

The server emits a "something changed" kick; the frontend reacts by invalidating its TanStack Query cache:

```
T+0  ↓  title_status                                  → {mediaType: "movie", tmdbId: 42}
T+1  ↓  (frontend invalidates titleStatusGet for that title)
T+2  ↑  GET /api/v1/titles/movie/42/status           → fresh, per-viewer data
```

`import_task_updated` and `proposal_updated` are kicks too; their invalidations are coalesced client-side over 300ms.

**Use when:**

- The data is large or expensive to recompute (full library page, complex joins)
- The REST endpoint already does the right auth filtering
- Update frequency is low (per-minute or less)
- Multiple views consume the same data — invalidation updates all of them automatically

Both patterns can coexist on the same SSE connection. The producer picks based on the data shape and frequency.

### Mixing

A feature can mix both. Example: title status uses pattern B for `title_status` and a viewer-independent tick, `title_progress`, written to a small in-memory store (`web/src/realtime/titleProgress.ts`) rather than the query cache. Same SSE connection serves both.

## Relationship to notifications

The [notifications](../notifications/README.md) spec defines a durable, multi-channel event system with an outbox, preferences, templates, and channel adapters. Realtime defines an ephemeral, single-channel (SSE) event system with a broker, replay buffer, and typed producer API.

They are **separate systems** with **one explicit seam**: the notifications system's `in_app` channel adapter emits a single, generic SSE event (`notification.delivered`) after writing the outbox row. **Unbuilt:** `InAppAdapter.Deliver` is a no-op, no `notification.delivered` event exists, and the bell polls its unread count every 60s and on window focus (`web/src/composables/useNotifications.ts`). The design:

```go
// In the notifications in_app channel adapter:
func (a *InAppAdapter) Deliver(ctx context.Context, row model.NotificationOutbox) error {
    realtime.Emit(ctx, a.broker, realtime.NotificationDelivered(realtime.NotificationDeliveredPayload{
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

The producer of a notification doesn't touch the realtime package directly. Enqueueing a `notifications.WantAvailable` writes the outbox row; the `in_app` channel adapter handles the realtime emission as part of "delivery." The realtime system never sees notification-specific event types — only the generic `notification.delivered`.

**The bell-icon UI is fed by the outbox query**, not by listening to the SSE event in isolation:

1. Page load → REST query against `notification_outbox` (filtered to this user, `in_app` channel) → renders the list
2. SSE `notification.delivered` event fires → TanStack Query invalidates the bell-icon query → it refetches and updates

This is pattern B for the bell icon. The outbox row is the durable record; SSE is just the live kick. If the user is offline, they miss the SSE event but the row is still there on next page load.

**`realtime.Emit(...)` is for everything that isn't notification-shaped**: scan progress, download progress, subscription updates that aren't durable events. These never write to any database; they're transient by design.

## Frontend consumer model

**TanStack Query is the default store for API-shaped state.** Lists, items, paginated views — all live as TanStack queries. SSE frames mutate (`setQueryData`) or invalidate (`invalidateQueries`) query keys based on the chosen pattern.

The frontend realtime layer lives in `web/src/realtime/`:

| Module                   | What it holds                                                                                        |
| ------------------------ | ---------------------------------------------------------------------------------------------------- |
| `connection.ts`          | The app-wide connection, a module singleton (not a Pinia store): status (`disconnected`/`connecting`/`connected`/`reconnecting`/`error`), the captured `sessionId`, a frame dispatcher (`on`), resync listeners (`onResync`), and the `subscribe` / `unsubscribe` control-plane calls |
| `bindings.ts`            | `installRealtime(queryClient)`, called once at app start: the single SSE → query-cache contract. Components stay SSE-unaware and just `useQuery` |
| `useRealtimeListener.ts` | Scoped listener for ephemeral, non-cache UI (live scan progress in library settings)                 |
| `titleProgress.ts`       | In-memory store for `title_progress` ticks                                                           |
| `useRealtimeStatus.ts`   | Connection status for the UI                                                                         |

On recovery — a reconnect after a drop, or `resume_gap` — `bindings.ts` invalidates every active query. On logout, `reset()` drops the session identity so the next login never presents the previous user's session id.

Pinia stays the home for non-API global state (auth, layout, dialogs, app flags, the Downloads view's filter and drawer state).

```ts
on('scan_progress', (data) => { ... })           // returns an unsubscribe fn
useRealtimeListener('scan_progress', (data) => { ... })  // auto-unsubscribes on scope dispose
await subscribe(['scan.progress:library_42'])    // POST /events/subscriptions (unused today)
await unsubscribe('scan.progress:library_42')    // DELETE /events/subscriptions/{topic}
```

**Unbuilt:** `useSSEMutation(queryKey, event, applyDelta)` / `useSSEInvalidation(queryKey, event)` sugar composables. Bindings are written directly in `bindings.ts` instead.

## Does NOT own

- **The notification system** — outbox, preferences, templates, push subscriptions, email delivery. [Notifications spec](../notifications/README.md) owns all of that. Realtime is just one transport the in-app channel uses.
- **Auth on the stream** — JWT verification happens at chi middleware before this spec's handler runs. The handler only reads the subject from the claims.
- **State persistence** — realtime is ephemeral by definition. No tables, no migrations.
- **Per-event-type ACL evaluation beyond recipient tags** — finer-grained scoping (e.g., "this scan progress event is for library 42, only users with `library.read:42` should see it") is built into the recipient resolution at constructor level, not in the broker. The broker trusts the constructor.
- **Frontend state architecture beyond the connection layer** — TanStack Query vs Pinia for specific features is per-feature; this spec gives the default guideline.

## Interactions

| Neighbor                                                                        | How realtime interacts                                                                                                                                                     |
| ------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[Notifications](../notifications/README.md)**                                 | Notifications' `in_app` channel adapter calls `realtime.Emit(notification.delivered)` after writing the outbox row (unbuilt; the bell polls). Realtime never sees notification-specific event types. |
| **[Users](../users/README.md)**                                                 | Supplies the authz keyspace capability recipients name, resolved per session at attach. JWT subject identifies the user on the stream.                                     |
| **[Scan](../scan/README.md)**                                                   | Producer of `scan_started` / `scan_progress` / `scan_completed` / `scan_failed`. Payload carries progress data, consumed by an ephemeral listener.                        |
| **[Acquisition](../acquisition/README.md)**                                     | Producer of `download_job_updated`, `want_updated` (full-state deltas, pattern A) and `import_task_updated`, `proposal_updated` (kicks, pattern B).                         |
| **[Title status](../title-status/README.md)**                                   | Producer (via `internal/titlenotify`) of `title_status` (a kick on transition; each client refetches its per-viewer projection) and `title_progress` (viewer-independent, per tick). The worked example of concept-named events. |
| **[Hygiene](../hygiene/README.md)**                                             | Future producer of audit-started / audit-completed and possibly per-finding deltas. Pattern depends on UI needs.                                                          |
| **[Connectivity-health pattern](../../patterns/connectivity-health/README.md)** | Future producer of `<resource>.health` events on transitions. Pattern A — payload carries new status.                                                                      |
| **[Matching](../matching/README.md)**                                           | Future producer of `match.dropped_in` events for live unmatched-files UI. Pattern A or B per feature owner.                                                                |
| **[Audit pattern](../../patterns/audit/README.md)**                             | Audit pattern's activity view would consume `audit.row_created` events for live-updating the feed. Pattern B — refetch the activity view.                                  |
| **[Work-dispatch pattern](../../patterns/work-dispatch/README.md)**             | Sibling, not a producer. Handles server→server worker wake-ups (at-least-once) over Postgres `LISTEN/NOTIFY`; realtime handles server→browser (lossy-OK). A worker woken by work-dispatch may *then* emit a browser event via `realtime.Emit`. The two never share a bus. |

## Decisions locked

These are woven into the model above and listed here so implementers don't reopen them:

- **Own the backend SSE writer** (forked `Register`); UUIDv7 string IDs; event-name-as-field; single registry. Keep huma's OpenAPI generation. ([Transport](#transport))
- **Own the frontend SSE reader**: the generated client can't re-resolve `?session=` or the token per reconnect. ([Retry semantics](#retry-semantics-on-the-client))
- **`Last-Event-ID` + per-session bounded replay** (200 events / 5 min).
- **Dynamic subscription control plane**: `session_id` via the `X-Realtime-Session` header, `GET /events/subscriptions` for resync, snapshot returned in the subscribe REST response (unbuilt).
- **Reconnect reuses `session_id`** via `?session=<id>`; falls back to fresh allocation if evicted.
- **Recipient is a coarse tag; eligibility is computed per-session at connect**, filtered by set-membership with no per-emit DB call. Topic eligibility checked at subscribe time, re-validated on permission change (both unbuilt).
- **Server→server work dispatch is out of scope** — it lives in the [work-dispatch pattern](../../patterns/work-dispatch/README.md).
- **Events are named after concepts, not tables.** Row-shaped events are admissible only where a single surface owns the row. ([Naming](#naming-events-after-concepts-not-tables))
- **Per-viewer payloads are admissible on scoped topics**, subject to three rules: scoped topics only, viewer-independent high-frequency channel, and withhold by omission rather than by client convention. ([Per-viewer payloads](#per-viewer-payloads))
- **Per-user scoping is a prerequisite, not a hardening pass.** It blocks any per-viewer payload.
- **Coalescing belongs on the producer.** Client-side debouncing without a maximum wait is starvable by the bursts it exists to smooth.

## Open questions

1. **`broadcast` events — do we actually need them?** No maintenance-style producer uses them; `want_updated` is broadcast only pending proper scoping. Maintenance-mode toggles, version-update banners are the candidates. Lean: keep the recipient kind in the model but don't ship a v1 producer; first concrete need drives it.
2. **Migration from current event types to typed constructors.** Existing 9 event types are hardcoded; migration converts them to the constructor pattern. Concrete pass — touches `events.go`, frontend consumers, and OpenAPI spec. Sequencing is owned by the phased implementation plan. — *Answered by the code: every event is a typed constructor in `internal/realtime`.*
3. **Backpressure threshold tuning.** 256-event outbound channel is a guess. Real number depends on observed worst-case (large download job storm, scan with thousands of files). Lean: ship 256, instrument the overflow path, tune from real data.
4. **`resume_gap` UX.** When the replay buffer is exhausted and the client gets a `resume_gap` event, what does the UX look like? Brief toast ("Reconnected — refreshing data") or silent refetch? Lean: silent refetch; the toast wastes attention on a routine network event. — *Answered by the code: silent refetch of every active query.*
5. **Heartbeat cadence.** 15s is the current value. Some setups (especially behind aggressive proxies) might want 10s; others might want 30s to save bandwidth. Lean: keep 15s; expose as a setting if the support load demands.
6. **Cross-browser-tab coordination.** Multiple tabs from the same user each open their own session. Should the connection coordinate via `BroadcastChannel` to share a single underlying connection? Saves connections; complicates the model. Lean: defer — one connection per tab is fine at self-hosted scale.

## What we're explicitly not deciding here

- Exact Go signatures of `realtime.Emit` and the typed constructors — directional only
- The exact `internal/realtime` file layout
- Alternative ID schemes
- The exact replay buffer data structure (ring buffer vs deque)
- Per-event-type wire shapes — those are owned by each producing feature's spec
- The full nginx / Cloudflare / Traefik configuration matrix — covered in deployment docs
- Whether to support HTTP/2 server push as a future alternative (premature; the SSE story is the answer)
- Frontend testing strategy for SSE-consuming components — implementation
- Observability beyond log messages (metrics, traces) — out of v1 scope; covered separately

## Doc neighbors

- [Title status](../title-status/README.md) — the acquisition read model; primary consumer of concept-named events and per-viewer payloads
- [Notifications](../notifications/README.md) — sibling system; its `in_app` channel adapter would emit `notification.delivered` via this spec's `realtime.Emit`
- [Users](../users/README.md) — JWT subject identifies the user on the stream; supplies the keyspace capability recipients name
- [Scan](../scan/README.md) — realtime producer for in-flight scan progress
- [Acquisition](../acquisition/README.md) — realtime producer for download-job, want, import-task and proposal events
- [Connectivity-health pattern](../../patterns/connectivity-health/README.md) — emits `<resource>.health` events on transitions, consumed by admin dashboards
- [Audit pattern](../../patterns/audit/README.md) — sibling cross-cutting system; activity view consumes a realtime event to live-update (pattern B)
- [Work-dispatch pattern](../../patterns/work-dispatch/README.md) — sibling server→server bus; `LISTEN/NOTIFY`-hint over the claim-queue for non-poll worker wake-ups. Explicitly *not* this module's transport
- [Errors pattern](../../patterns/errors/README.md) — typed errors flow through both REST and SSE; clients handle them uniformly
- `backend/internal/sse/`, `backend/internal/realtime/`, `backend/internal/titlenotify/`, `backend/internal/http/handlers/events*.go`, `web/src/realtime/`, `web/src/lib/sseClient.ts` — the implementation
