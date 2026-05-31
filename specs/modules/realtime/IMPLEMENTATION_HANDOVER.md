# Realtime engine — implementation handover

**Purpose:** hand off the in-progress realtime-engine rebuild to another agent/session. This captures the locked design decisions, what's already built, and the precise remaining work with pinned designs so you can execute without re-deriving anything.

**Authoritative spec:** `specs/modules/realtime/README.md` (iteration 2 — already reflects all decisions below). Sibling: `specs/patterns/work-dispatch/README.md` (Phase 5, a SEPARATE PR — do not build it here).

**Branch:** `feature/realtime`. Phases 0–2 are **committed**. Phase 3a (ring buffer) is **written but uncommitted** (working tree). Verify with `git status` and `git log --oneline`.

---

## The goal in one paragraph

Evolve the existing SSE pipeline (in-process broker + huma handler + Pinia events store) into: an **owned wire writer** (string IDs, the huma `humasse` adapter can't carry them), a **typed producer API** (`realtime.Emit` + constructors), **per-user scoping**, **`Last-Event-ID` replay** over a per-session ring buffer, a **dynamic subscription control plane**, and a **TanStack-Query-centric frontend consumer model**. Server→server worker wake-ups (LISTEN/NOTIFY) are explicitly a SEPARATE system (Phase 5, separate PR).

## Locked design decisions (do not relitigate)

- **Own the backend SSE writer** (fork of `humasse.Register`); keep huma's OpenAPI `oneOf` generation. DONE in Phase 1 (`backend/internal/http/handlers/events_stream.go`).
- **Keep the generated frontend SSE client** (`web/src/client/core/serverSentEvents.gen.ts`) — it already tracks `id:` and resends `Last-Event-ID`. Bound its retry via config (DONE Phase 0). No FE transport rewrite.
- **Event IDs are UUIDv7** (`uuid.NewV7()`), not ULID — time-ordered, lexicographically sortable, no new dependency (`github.com/google/uuid` v1.6.0 already present). String compare on IDs == chronological order; the replay/gap logic relies on this.
- **Event names stay snake_case** for this whole PR (`scan_progress`, `download_job_updated`, `resume_gap`, …). The dotted-topic rename (`scan.progress`) in the spec is a SEPARATE future concern — do NOT introduce a half-renamed state.
- **Single eligibility tier for now**: all authenticated sessions are eligible for `Admins` and `Broadcast` events; only `User(id)` events are restricted. Real admin resolution is deferred (seam + comment left in `recipientMatches`). No current producer emits `User(id)`, so scoping is machinery-only today.
- **Recipient lives in `internal/sse`** (not `realtime`) to avoid an import cycle; `realtime` re-exports it via type/var aliases so producer call sites read `realtime.Broadcast` etc.
- **Reattach requires `?session=<id>`**; `Last-Event-ID` says where to resume within that session's ring. Without `?session=`, a fresh session is allocated. The frontend doesn't send `?session=` until Phase 4, so replay is dormant in the running app until then — that's expected; unit-test it.
- **Overflow → lossless teardown** (Phase 3b): a full outbound channel signals the handler to disconnect; the overflowed events are in the ring, so the client reconnects and replays them. Never close a channel from inside `Publish` (send-on-closed panic under concurrent publishers).
- **Phase 5 (work dispatch) is a separate PR.** Don't build it in this branch.

## Phase status

| Phase | Scope | Status |
|---|---|---|
| 0 | Transport debt: nginx SSE blocks, events-store generation-counter race fix, retry-delay bound | ✅ committed |
| 1 | Owned writer + typed producer registry; deleted 9 `rawEventBytes` aliases; migrated 4 publish sites | ✅ committed |
| 2 | Session-aware broker + per-user scoping (recipient × topic filter); `ready` carries `sessionId` | ✅ committed |
| 3a | Ring buffer (`internal/sse/ring.go`) — pure, unit-tested | ✅ written, **uncommitted** |
| 3b | Session lifecycle (Attach/Detach/sweeper) + replay/resume + `resume_gap` + overflow→teardown | ⬜ NOT STARTED |
| 3c | Control-plane endpoints (`/events/subscriptions`) + snapshot-in-response | ⬜ NOT STARTED |
| 4 | Frontend consumer model + downloadJobs Pinia→TanStack migration | ⬜ NOT STARTED |
| 5 | Work dispatch (LISTEN/NOTIFY) — **SEPARATE PR** | ⬜ out of scope here |

## Current code state (key files + shapes)

> NOTE: the Read-tool renderer was intermittently glitching during this session (showed imports at the wrong place, doubled line numbers). The files on disk are valid — verify with `cd backend && go build ./...`. If a Read looks corrupted, cross-check with `cat -n` via Bash.

**`backend/internal/sse/broker.go`** (Phase 2 — to be heavily extended in 3b):
- `type Event struct { Type string; Data json.RawMessage; ID string; At time.Time; Recipient Recipient }`
- `type Session struct { ID, UserID uuid.UUID; topics map[string]bool; out chan Event; connectedAt time.Time }` + `Events() <-chan Event`
- `type Broker struct { mu sync.RWMutex; sessions map[uuid.UUID]*Session }`, `NewBroker()`, `Subscribe(SubscribeParams{UserID, Topics}) (*Session, cancel func())`, `Publish(ev Event)`, `recipientMatches(r, s)`, `(*Session) topicAllowed(t)`.
- `Publish`: RLock map, for each session `recipientMatches && topicAllowed` → non-blocking send; on full channel logs to stderr and drops (this becomes teardown in 3b). `outboundDepth = 256`.

**`backend/internal/sse/recipient.go`**: `Recipient{Kind RecipientKind; UserID uuid.UUID}`, `RecipientKind` enum (`RecipientBroadcast`, `RecipientAdmins`, `RecipientUser`), constructors `Broadcast`, `Admins`, `User(id)`.

**`backend/internal/sse/ring.go`** (Phase 3a — DONE; no mutex, caller locks):
- `newRing(maxLen int, maxAge time.Duration, now func() time.Time) *ring` (nil now → time.Now)
- `(*ring) append(ev Event)` — evicts by age (front-to-back, zero `At` treated as "now"/never stale) then trims to maxLen.
- `(*ring) since(lastID string) (events []Event, gapped bool)` — semantics: `""`→(nil,false); empty buffer→(nil,false); `lastID < events[0].ID`→(nil,true) gap; else events with `ID > lastID` in order.
- `(*ring) len()`, `(*ring) snapshot()` (test helper).
- `backend/internal/sse/ring_test.go`: 10 tests, pass under `-race`. Uses synthetic sortable ids (`"id-001"`…).

**`backend/internal/realtime/`** — producer API:
- `realtime.go`: `Event{Name string; Recipient Recipient; Data json.RawMessage; ID string}`, `Emit(ctx, broker, e)` (stamps UUIDv7 if ID empty, publishes `sse.Event`), `mustMarshal`, Recipient aliases.
- `events_system.go`: `NameReady="ready"`, `NamePing="ping"`; `ReadyPayload{OK bool; SessionID string}`, `PingPayload{TS int64}`; `Ready(sessionID)`, `Ping()`.
- `events_scan.go`: Scan{Started,Progress,Completed,Failed} constructors + payload structs.
- `events_downloads.go`: `DownloadJobsSnapshot([]model.DownloadJobWithSummary)`, `DownloadJobUpdated(model.DownloadJobWithSummary)`.
- `events_imports.go`: `ImportTaskUpdated(taskID uuid.UUID)` + `ImportTaskUpdatedPayload{TaskID}`.
- `registry.go`: `Registry []EventSchema{Name, PayloadSample}` — the single source for spec-gen `oneOf`. Add new events here.

**`backend/internal/http/handlers/events.go`** (Phase 2 handler): `Stream(ctx, *EventsStreamInput, send streamSender)`. `EventsStreamInput{ Types []string \`query:"type,explode"\` }`. Resolves user via `userIDFromContext(ctx, op)` (`"sub"` claim → uuid). `broker.Subscribe`, emits `ready` (with session.ID), connect-time `download_jobs_snapshot`, 15s heartbeat, loops `session.Events()`. Has a no-broker fallback. `typeAllowed` closure gates direct ready/ping/snapshot emits. `RegisterHumachi` calls `registerEventStream`.

**`backend/internal/http/handlers/events_stream.go`** (Phase 1 forked writer): `registerEventStream(api, op, handler)` builds the `oneOf` from `realtime.Registry` (`id` typed string), `huma.StreamResponse` body sets `Content-Type`/`Cache-Control: no-cache`/`X-Accel-Buffering: no`, writes `id:`/`event:`/`data:` frames, flusher+deadline via `unwrapTo[T]`. `streamFrame{ID, Event string; Data []byte}`, `streamSender func(streamFrame) error`.

**`backend/cmd/api/main.go`** ~line 48: `broker := sse.NewBroker()` (single construction site; 3b adds a ctx param here).

**Frontend (Phase 0):** `web/src/stores/events.ts` has the generation-counter race fix + `sseDefaultRetryDelay:500`/`sseMaxRetryDelay:5000`. Consumers: `web/src/stores/downloadJobs.ts`, `web/src/views/Downloads.vue`, `web/src/views/settings/LibrarySettings.vue`. They listen via `events.on('<snake_case_name>', cb)`.

**Infra (Phase 0):** `docker/s6/s6-rc.d/nginx/default.conf` + `default.dev.conf` have a `location /api/v1/events` block (buffering off, 24h read timeout).

---

## Remaining work — pinned designs

### Phase 3b — session lifecycle + replay/resume (DO THIS NEXT)

Goal: sessions survive disconnects so reconnects replay; wire `?session=` + `Last-Event-ID`; flip overflow to teardown.

**Locking model (critical):** `Publish` now writes per-session state (ring append), and Publishes run concurrently. So: `broker.mu` (RWMutex) guards ONLY the `sessions` map; each `Session` gets its OWN `sync.Mutex` guarding `ring`, the outbound channel + `detachedAt`, and `topics`. Lock order: broker before session, never reverse.

**Session struct gains:** `replay *ring` (built `newRing(200, 5*time.Minute, broker.now)`), `detachedAt *time.Time` (nil = attached), buffered `kick chan struct{}` (size 1). Outbound channel becomes per-attachment (nil while detached). Add injectable `now func() time.Time` on the Broker (default time.Now), thread to rings + sweeper.

**Replace `Subscribe` with `Attach`/`Detach`:**
- `Attach(AttachParams{SessionID uuid.UUID /*zero=none*/, UserID uuid.UUID, LastEventID string, Topics []string}) (s *Session, replay []Event, gapped bool, cancel func())`:
  - **Reattach** when SessionID non-zero, found, `UserID == s.UserID` (mismatch ⇒ treat as not found; NEVER reattach across users): clear `detachedAt`, fresh outbound channel + fresh `kick`, `replay, gapped = s.replay.since(LastEventID)`.
  - **Fresh** otherwise: new `uuid.New()`, `newRing(...)`, topics from Topics, outbound channel, kick. `replay=nil, gapped=false`.
  - `cancel` = what the handler defers; calls `Detach(s.ID)`.
- `Detach(sessionID)`: under locks, close outbound channel once (guard double-detach), set it nil, set `detachedAt = now()`. KEEP session in registry.
- **Sweeper goroutine**: every ~1 min, evict sessions with `detachedAt` older than 5-min TTL. Add `ctx context.Context` to `NewBroker` (update the `main.go` call site to pass the app/root context), stop on `ctx.Done()`.

**`Publish`:** for each session matching recipient × topic — take session lock, `s.replay.append(ev)` ALWAYS (even detached, so a briefly-gone client accumulates replayable events); then if attached (outbound non-nil) non-blocking send; on full channel ⇒ non-blocking `select { case s.kick <- struct{}{}: default: }` + brief log (NO drop, NO channel close — events are in the ring). Append must respect recipient × topic (only buffer what the session would receive).

**Handler (`Stream`):**
- Input gains `Session string \`query:"session"\`` and `LastEventID string \`header:"Last-Event-ID"\``. Parse Session→uuid (zero/invalid ⇒ none).
- Resolve user id (as today), call `Attach`.
- Emit `ready` with `session.ID`.
- **Reattach + gapped** ⇒ emit `resume_gap`, then go live (skip replay AND connect-time snapshot — client refetches).
- **Reattach + not gapped** ⇒ send each replay event in order as `streamFrame{ID:ev.ID, Event:ev.Type, Data:ev.Data}`, then go live; SKIP the connect-time snapshot (replay covers it).
- **Fresh** ⇒ emit connect-time `download_jobs_snapshot` (parity), then go live.
- Live loop selects: `ctx.Done()`→return; 15s heartbeat→ping; `<-session.kick`→return (deferred Detach preserves ring); `<-session.Events()`. Keep `typeAllowed` for direct emits. Keep no-broker fallback.

**New event `resume_gap`** in `realtime` (add to `events_system.go` or a new `events_resume.go`, plus a `Registry` entry): `Broadcast`, minimal kick payload (`{"reason":"buffer_exhausted"}` or `{}`). snake_case name `resume_gap`.

**Tests** (`backend/internal/sse/broker_test.go`, `package sse`, `t.Parallel()` first line, drive the injected clock): reattach replays only `id > LastEventID` in order; mismatched UserID ⇒ fresh (no cross-user reattach); Publish while detached lands in ring + replays on reattach; gap (LastEventID older than oldest) ⇒ gapped true, empty replay; overflow ⇒ kick fired, no panic, event still in ring; sweeper evicts after TTL (advance clock); recipient × topic filtering holds for both send and ring append.

**Verify:** `cd backend && go test -race ./internal/sse/...`; `just gen` (stream `oneOf` gains `resume_gap`, op gains `?session=` + `Last-Event-ID`); `just check`.

### Phase 3c — control-plane endpoints

Add to the `Events` handler (it already holds the broker). Follow `backend/internal/http/handlers/CLAUDE.md` (Input/Output structs, OperationID `events-subscriptions-list|add|remove`, header input, `Errors` enumeration).

- `GET /api/v1/events/subscriptions` → returns `{topics: [...]}` for the session.
- `POST /api/v1/events/subscriptions` → body `{topics: [...]}`; adds them; **snapshot-in-response**: if a subscribed topic has a snapshot (only download-jobs today), return it in the 200 body; else 204. Keep this a small `topicSnapshot(ctx, topic)` switch with an extension comment (don't over-engineer a registry).
- `DELETE /api/v1/events/subscriptions/{topic}` → removes a topic.
- All take `SessionID uuid.UUID` via `X-Realtime-Session` header (`header:"X-Realtime-Session" required:"true"`). Validate session exists AND `session.UserID == claims sub`; unknown/foreign session ⇒ 404 (don't leak existence).
- **Subscribe-time eligibility seam**: a `canSubscribe(userID, topic) bool` returning true now, with a comment that per-resource scoping (`scan.progress:library_42` → `library.read:42`) lands later.
- **Broker topic-mutation API** (concurrency-safe, under the session mutex established in 3b): `AddTopics(sessionID, topics)`, `RemoveTopic(sessionID, topic)`, `Topics(sessionID) ([]string, bool)`. (3b should expose the exact safe surface; 3c consumes it.)
- `just gen` (new endpoints → TS client `events-subscriptions-*`), `just check`.

### Phase 4 — frontend consumer model

- `web/src/stores/events.ts`: capture `sessionId` from the `ready` event; send `?session=<id>` on reconnect (so the generated client's auto `Last-Event-ID` actually resumes); add `subscribe`/`unsubscribe` calling the generated `events-subscriptions-*` SDK fns with the `X-Realtime-Session` header; on reconnect, resync via `GET /events/subscriptions`; handle `resume_gap` → trigger refetch of affected queries; keep the generation-counter + retry bounds from Phase 0.
- Provide composables: `useSSEMutation(queryKey, eventName, applyDelta)` (→ `setQueryData`) and `useSSEInvalidation(queryKey, eventName)` (→ `invalidateQueries`), per the spec's "Frontend consumer model".
- **Migrate `web/src/stores/downloadJobs.ts` from Pinia → TanStack Query** (the user chose this): downloadJobs becomes TanStack queries mutated/invalidated by SSE handlers. Update `Downloads.vue` accordingly. `LibrarySettings.vue` scan consumers move to the composables.
- Snapshot-on-subscribe: the subscribe POST returns the snapshot in its body — seed the query cache from it.
- Follow `web/src/CLAUDE.md` (generated SDK only, `throwOnError`, `problemMessage`, invalidate via generated `*QueryKey()`).
- `just check` (web lint + type-check).

### Phase 5 — work dispatch (SEPARATE PR, not this branch)

Per `specs/patterns/work-dispatch/README.md`: Postgres `LISTEN/NOTIFY` wake-up hint over the existing `ClaimRunnable*` claim-queue, ticker demoted to fallback. Touches `backend/internal/jobs/{download,import}/worker.go`, the claim queries (`pg_notify` migration), and a new shared `pgx` listener. Depends only on Phase 1 (`realtime.Emit`).

---

## Verification & gotchas

- **`just check` runs in the dev container** (now has gcc, so `go test -race` works there). It mirrors CI: backend vet/lint + `go test -race ./...`, frontend lint/prettier/type-check. Use `just gen` after any backend API change (sqlc → genspec → openapi-ts, in order). `just preflight` = fix + check.
- **Agents do NOT commit** — the user commits selectively.
- **Don't bundle a `Read` of a just-written file onto the same message as a subagent dispatch** — it races the write and errors (a recurring mistake this session). Dispatch alone; verify after it returns.
- **Subagent overhead was high** for these tightly-coupled backend changes — direct inline implementation (Edit/Write/Bash) was faster. Reserve subagents for the sprawling/parallel Phase 4 frontend work if at all.
- **Render glitches**: if a `Read` looks malformed, cross-check with `cat -n`. Files on disk are valid (they compile).
- **No integration-test impact expected** until endpoints land; integration tests are behind `//go:build integration` and need the postgres testcontainer (`just check-all`).

## Spec pointers

- `specs/modules/realtime/README.md` — the contract (iteration 2; "Decisions locked" section near the open questions).
- `specs/modules/realtime/SSE_TRANSPORT_FINDINGS.md` — the three Phase 0 fixes (now resolved).
- `specs/patterns/work-dispatch/README.md` — Phase 5 (separate PR).
- `specs/modules/acquisition/README.md` — "Event bus / messaging" section; consumes both realtime and work-dispatch.
