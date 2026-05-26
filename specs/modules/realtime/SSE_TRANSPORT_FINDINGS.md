# SSE Transport Findings

Investigation into perceived SSE flakiness on the Downloads page (the page has an SSE status indicator that has been observed flapping / showing disconnected states even while the rest of the app appears functional).

This document is scoped to the **transport and connection-management layer**. It deliberately does not cover the separate question of where SSE-derived state lives in the frontend (Pinia vs. TanStack Query cache) — that's a state-architecture decision and orthogonal to reliability.

## TL;DR

There are three independent issues, in rough order of likely impact:

1. **Nginx buffers the SSE stream by default.** The repo's nginx config does not disable `proxy_buffering` for the events endpoint, and the backend doesn't set `X-Accel-Buffering: no`. This causes long silences between events at the browser and can cause nginx to declare the upstream dead at the read timeout.
2. **A stale-closure race in `web/src/stores/events.ts` makes the status indicator lie** about the actual connection state during reconnects.
3. **The generated SSE transport's retry-attempt counter never resets on success**, so long sessions with intermittent drops escalate the backoff into 30s territory permanently.

None of these are caused by TanStack Query — TanStack Query has no SSE integration. The transport is `@hey-api/openapi-ts`'s generated SSE client in `web/src/client/core/serverSentEvents.gen.ts`.

---

## Finding 1: Nginx buffers SSE responses

**File:** `docker/s6/s6-rc.d/nginx/default.conf:14-19`

```nginx
location /api/ {
  proxy_pass http://127.0.0.1:8080;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-For $remote_addr;
  proxy_http_version 1.1;
}
```

Missing for SSE-safe behavior:

- `proxy_buffering off;` — nginx defaults to **on**, holding the response in its 4-8KB buffer before flushing to the client.
- `proxy_read_timeout` override — defaults to 60s. If nginx doesn't see anything from upstream within 60s (which can happen if the buffer is holding the heartbeats), it kills the connection.
- `proxy_set_header Connection '';` — clears the inherited `Connection` header so keep-alive to upstream works correctly.
- `chunked_transfer_encoding off;` — defensive; some nginx versions interact oddly with chunked + buffering.

Backend heartbeat is 15s (`backend/internal/http/handlers/events.go:185-186`), so without buffering it should keep the connection alive. With buffering, individual ping events (~30 bytes) sit in the buffer and don't flush until either:

- The buffer fills (could be minutes of pings for a quiet app), or
- An upstream-close happens and the buffer drains

Observable symptoms this would cause:

- SSE indicator shows "connected" but events arrive in bursts after long silences.
- Connection appears to die periodically (nginx 60s read timeout fires while buffer is sitting on heartbeats it hasn't flushed).
- Behavior different in dev vs. production-like reverse-proxied setups.

### Recommended fix

Two options, either works; both is belt-and-suspenders.

**Option A — fix in nginx (preferred):** add a more specific location block for the events stream so the rest of the API still benefits from default buffering.

```nginx
location /api/v1/events {
  proxy_pass http://127.0.0.1:8080;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-For $remote_addr;
  proxy_set_header Connection '';
  proxy_buffering off;
  proxy_cache off;
  proxy_read_timeout 24h;
  chunked_transfer_encoding off;
}
```

Apply to both `default.conf` and `default.dev.conf`.

**Option B — fix in backend:** set `X-Accel-Buffering: no` on the SSE response. Nginx honors this per-response without needing the location block. Set it in `backend/internal/http/handlers/events.go::Stream` before the first `send.Data(...)` call. Worth checking whether `humasse.Sender` exposes a way to set response headers — if not, this needs to happen earlier in the handler chain via the underlying `http.ResponseWriter`.

Option A is simpler and doesn't depend on the SSE library exposing header control.

---

## Finding 2: Status indicator lies due to stale-closure race

**File:** `web/src/stores/events.ts`

The events store has a `status` ref (`'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'error'`) consumed by the Downloads page indicator. Two interacting facts cause it to mis-report.

**Fact 1:** Each `connect()` call creates a local `hasConnectedBefore` and a fresh `onSseError` closure (lines 82, 109-120). The `onSseError` writes to the **shared** `status` ref but reads from its **own** `hasConnectedBefore`.

**Fact 2:** When `disconnect()` is called (e.g. from `reconnect()` at line 160, or from the visibility handler at line 171-182), the old generator inside the transport doesn't stop instantly. It catches the aborted signal in `serverSentEvents.gen.ts:240`, invokes the **old** `onSseError` callback, and only then exits.

### The race

`reconnect()` flow (`web/src/stores/events.ts:160-165`):

```ts
function reconnect() {
  const types = [...wantedTypes.value]
  disconnect()          // (1) abort.abort(), status = 'disconnected'
  wantedTypes.value = []
  connect(types)        // (2) new AbortController, new onSseError closure
}
```

Sequence on the event loop:

1. `disconnect()` calls `abort.abort()` synchronously and sets `status = 'disconnected'`.
2. `connect(types)` runs synchronously: creates a new AbortController, calls `client.sse.get(...)`, fires the network request. Sets `status = 'connecting'`.
3. The **old** generator (still suspended on `reader.read()`) wakes up because its signal was aborted. It throws, gets caught by `serverSentEvents.gen.ts:240`, which calls the **old** `onSseError(err)`.
4. That old callback's local `hasConnectedBefore` is `true`, so it executes `status.value = 'reconnecting'` — **overwriting the new connection's status**.
5. The new connection eventually receives the server's `ready` event and writes `status = 'connected'`.

Net effect: every clean reconnect flashes through `'reconnecting'` for a tick, even though the new connection is healthy. If reconnects happen often (e.g. on visibility changes), the indicator looks like it's constantly flapping.

The same race applies to plain `disconnect()` followed by anything: the next status write loses to the old generator's `onSseError`.

### Recommended fix

The shared `status` ref needs to be writable only by the **current** connection. A few patterns work:

**Option A — generation counter:**

```ts
let generation = 0

async function connect(types?: string[]) {
  // ... existing setup ...
  const myGen = ++generation
  const setStatus = (s: EventsConnectionStatus) => {
    if (myGen !== generation) return   // I'm not the current connection
    status.value = s
  }
  // pass setStatus instead of writing status.value directly
}

function disconnect() {
  generation++
  // ...
}
```

**Option B — null out the listener:**

Have `disconnect()` install a no-op `onSseError` before calling `abort.abort()`. Trickier because `onSseError` is captured by the generator's closure, not stored anywhere mutable.

Option A is cleaner. The same pattern protects against any other late callback (an `onSseEvent` arriving during the abort window can also dispatch a `ready` and falsely flip status to `connected` on a connection that's being torn down).

---

## Finding 3: Retry-attempt counter never resets

**File:** `web/src/client/core/serverSentEvents.gen.ts` (auto-generated by `@hey-api/openapi-ts` — do not edit directly)

The transport's retry loop:

```ts
const createStream = async function* () {
  let retryDelay: number = sseDefaultRetryDelay ?? 3000;
  let attempt = 0;
  const signal = options.signal ?? new AbortController().signal;

  while (true) {
    if (signal.aborted) break;
    attempt++;
    // ... connect, stream events ...
    } catch (error) {
      onSseError?.(error);
      // ...
      const backoff = Math.min(
        retryDelay * 2 ** (attempt - 1),
        sseMaxRetryDelay ?? 30000,
      );
      await sleep(backoff);
    }
  }
};
```

`attempt` is incremented unconditionally per loop iteration and **never reset** when a connection successfully establishes and streams data. So:

| Session history | Next backoff |
| --- | --- |
| 1st attempt (cold start) | 3000 × 2⁰ = 3s |
| Drop after success, retry | 3000 × 2¹ = 6s |
| Another drop hours later | 3000 × 2² = 12s |
| Another drop | 3000 × 2³ = 24s |
| 5th drop and beyond | capped at 30s |

A user who's had the tab open for a few hours, through a couple of network blips, is permanently in 30s reconnect territory. A one-second blip then takes 30s to recover from — which looks exactly like "SSE is broken."

The correct behavior would be to reset `attempt = 0` and `retryDelay = sseDefaultRetryDelay` once a successful read has occurred (e.g. inside the inner `while (true) { reader.read() }` after the first non-empty chunk).

### Recommended fix

The file is regenerated, so direct edits are wiped on `just web-genclient`. Two paths:

**Option A — bound the damage via config:** pass tight `sseDefaultRetryDelay` and `sseMaxRetryDelay` from the events store, e.g.:

```ts
const { stream } = await client.sse.get({
  url,
  signal: abort.signal,
  sseDefaultRetryDelay: 500,
  sseMaxRetryDelay: 5000,
  onSseEvent: (ev) => { /* ... */ },
  onSseError: (err) => { /* ... */ },
})
```

This won't fix the escalation, but it caps the worst case at 5s instead of 30s.

**Option B — upstream fix:** file an issue / PR against `@hey-api/openapi-ts` to reset `attempt` on successful read. Until that lands, Option A is the band-aid.

**Option C — wrap the transport:** write a thin wrapper in `web/src/lib/sse.ts` that uses `client.sse.get` under the hood but exposes its own retry loop with correct reset semantics. Heavier; only worth it if A is insufficient.

---

## Other observations (lower priority)

### Listener leak in download jobs store

**File:** `web/src/stores/downloadJobs.ts:108-132`

`connectLive()` registers SSE listeners via `events.on(...)` but never stores or invokes the returned unsubscribe functions. The store is a singleton and `connectLive` is called once in practice, so this doesn't leak today. But:

- If `connectLive()` is ever called twice (e.g. during dev HMR, or future refactor that re-subscribes on auth change), listeners stack and every event is dispatched multiple times.
- Compare with `web/src/views/settings/LibrarySettings.vue:76-121` which collects unsubscribes and cleans up on unmount — that's the correct pattern.

### Backend SSE path without a broker

**File:** `backend/internal/http/handlers/events.go:175-179`

If `h.broker == nil`, the handler holds the connection open with `<-ctx.Done()` and **never sends heartbeats**. This is a degraded mode — nginx will kill the connection at its read-timeout (60s default). Likely not the prod path, but worth confirming and either logging loudly or sending pings anyway.

### Auth token is captured at connect time

**File:** `web/src/stores/auth.ts:25-40` + `web/src/stores/events.ts:86`

The bearer token is attached via `client.setConfig({ headers: { Authorization: ... } })`. The SSE transport reads headers from the client config when `client.sse.get(...)` is called, but the retry loop's headers (`serverSentEvents.gen.ts:118-125`) are captured at call time. If the token rotates mid-stream:

- Existing retries will keep using the old token until a fresh `connect()` happens.
- A 401 will trip the global response interceptor (`web/src/main.ts:22`) which logs out and redirects, so this resolves itself in practice.

Low priority but worth noting in case token-rotation behavior changes in the future.

---

## Recommended order of operations

1. **Nginx config (Finding 1).** Smallest change, biggest expected impact. If the symptom is "SSE indicator drops on prod-like setups but not in raw dev," this is almost certainly the cause.
2. **Cap the retry delay (Finding 3, Option A).** One-line change in `events.ts`; bounds the worst case.
3. **Status race (Finding 2).** Bigger change to `events.ts`; do this after the above to see if the indicator is actually still flapping once the underlying transport is healthy.
4. Re-evaluate the lower-priority items based on what's still observable after the above.

## Reference: relevant files

| Purpose | Path |
| --- | --- |
| Nginx config (prod) | `docker/s6/s6-rc.d/nginx/default.conf` |
| Nginx config (dev) | `docker/s6/s6-rc.d/nginx/default.dev.conf` |
| Backend SSE handler | `backend/internal/http/handlers/events.go` |
| Generated SSE transport | `web/src/client/core/serverSentEvents.gen.ts` (do not edit) |
| Frontend SSE connection manager | `web/src/stores/events.ts` |
| Auth token wiring | `web/src/stores/auth.ts` |
| Downloads page consumer | `web/src/stores/downloadJobs.ts` |
| Library scan consumer (good cleanup example) | `web/src/views/settings/LibrarySettings.vue` |
