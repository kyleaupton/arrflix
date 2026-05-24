# Media-server — surfacing the library in Plex / Jellyfin

**Status:** Draft, iteration 1

A media server (Plex today; Jellyfin modeled, not yet built) is a **downstream consumer** of Arrflix's library: the player where users actually watch. This spec defines the machinery that gets a verified-on-disk file *seen* by that server, tracks whether it landed, ingests watch-state back, and treats each server as a [connectivity-health](../../patterns/connectivity-health/README.md) resource.

The hard architectural decision is already made elsewhere and is **not** relitigated here: the [acquisition pipeline](../acquisition/README.md#media-server-propagation-decoupled-from-available) decoupled "the file is in my library" (Arrflix's truth, gates the want) from "Plex has indexed it" (the server's truth, never gates anything). This spec owns the *second* axis end to end. It replaces the old `PlexIntegrationService`.

## TL;DR

- **Arrflix owns truth; the media server is a consumer, not the authority.** Everything here is enrichment or a *legitimately-coupled* read (watch-state). Nothing here can stall a want.
- A **`media_server`** row is a named connection `(name, type, url, credentials, sections-mapping, …)` plus standard `enabled`/`default` flags and the three [connectivity-health](../../patterns/connectivity-health/README.md) status columns. `type` discriminates `plex` / `jellyfin`. The model is **multi-server** — N servers of any type — even though v1 ships the Plex driver only.
- **Propagation** is tracked per-`(media_file, media_server)` in a dedicated record (`state ∈ pending | visible | unknown`, `external_ref` = rating key / item id, `synced_at`). The rating key lives **here**, never on `media_file`.
- Propagation is populated by **(a)** the server webhook (fast path) or **(b)** a reconciliation poll (backfill). Both are **idempotent upserts** — order and duplication don't matter. Poll runs **only while the server is healthy**.
- **Outbound nudge** (Plex partial-refresh / Jellyfin scan) is **debounced per library section** — Story 02's 23-file import fires *one* refresh, not 23.
- **Watch-state ingestion** is the *one* legitimate coupling: `cleanup_after_watch` retention needs "has the requester watched it," which only the player knows. Webhook when available, **poll always as the floor** (Plex server-side play webhooks are Plex Pass-only). Degrades to "never fires" with no server.
- **`MediaServerService`** (outbound nudge + inbound webhook/reconciliation/watch-state) with a **`MediaServerSync`** worker. Emits **`media_file.propagated`** (consumed by [notifications](../notifications/README.md) for deep-link upgrade + SSE).
- **Identity stays in [users](../users/README.md).** This spec owns the *server connection* and *library/playback integration*; the Plex-OAuth-as-login bits stay with users. The bridge is the per-user watch-state mapping.

## Why this is its own spec

The propagation contract is small, but it touches five subsystems (acquisition, notifications, requests retention, hygiene cleanup, connectivity-health) and it's the natural home for a moving target — Plex first, Jellyfin next, possibly two servers at once. Folding it into acquisition would re-couple the want lifecycle to a downstream consumer, which is exactly what the structural pass tore apart. Folding it into notifications would conflate "is it visible in Plex" (a fact about the world) with "tell the user it's ready" (a delivery concern).

It earns its own spec for the same reason downloaders does: it's a **provider abstraction with one concrete implementation today and a clear second**, plus a runtime-health surface, plus inbound/outbound integration mechanics that no single neighbor should own.

## The model

### What a `media_server` owns

A `media_server` row encodes:

- **Name** — display name, unique (case-insensitive).
- **Type** — `plex` (implemented) or `jellyfin` (modeled, no driver in v1). Constrains the driver and the credential/section shape.
- **URL** — the server's base URL (the admin-reachable address, e.g. `http://plex:32400`).
- **Credentials** — Plex: an admin/server token (X-Plex-Token). Jellyfin: API key + user id. Redacted on the wire and treated like downloader credentials (see [Credentials & the SSO boundary](#credentials--the-sso-boundary)).
- **Section mapping** — which server library sections Arrflix maps to which Arrflix [libraries](../libraries/README.md), plus an optional [path-mapping override](#correlation-mapping-a-server-item-back-to-our-media_file) per section for container-mount differences.
- **Enabled flag** — when false, excluded from outbound nudges, reconciliation, and watch-state polling. Inbound webhooks for a disabled server are dropped.
- **Default flag** — at most one default; used when no server is otherwise specified. (Multi-server installs that want *all* servers nudged don't rely on this — nudges fan out to every enabled server.)
- **Connectivity-health columns** — `status` / `status_checked_at` / `status_last_transitioned_at`, per the [pattern](../../patterns/connectivity-health/README.md#persistence).
- **Created / updated timestamps.**

### The propagation record

The core artifact. One row per `(media_file, media_server)` pair:

| Field          | Meaning                                                                                  |
| -------------- | ---------------------------------------------------------------------------------------- |
| `media_file`   | FK to the Arrflix-owned file. Cascade-deletes with it.                                   |
| `media_server` | FK to the configured server. Cascade-deletes with it.                                    |
| `state`        | `pending` (nudged, not yet confirmed) · `visible` (server has indexed it) · `unknown` (server unreachable / never probed) |
| `external_ref` | The server's identifier once known — Plex `rating_key`, Jellyfin item id. This is the **stored result of correlation, not the join key.** |
| `synced_at`    | When the record last advanced (last confirmation or backfill).                           |

*(Column types deferred to iteration 2, per house style.)*

Why server-agnostic and off `media_file`: a file may be visible in two servers (Plex + Jellyfin) at once, each with its own rating key and its own propagation timeline. Hanging a single `rating_key` on `media_file` couldn't model that, and it would falsely imply `media_file` depends on a server — it does not.

**State semantics:**

- A file freshly `available` with a healthy server configured → propagation `pending` (nudge sent).
- Webhook or poll confirms → `visible`, `external_ref` filled, `media_file.propagated` emitted.
- Server unreachable → stays `pending` (no false `unknown`); `unknown` is for "we asked and genuinely can't tell" or pre-probe initial state.
- No server configured at all → no propagation rows exist. That's correct: there's nothing to propagate to. The want is still `available`.

### Correlation: mapping a server item back to our `media_file`

When a server reports "new item X" (via webhook or poll), we must map X to the right `media_file`. **Path-primary** strategy:

1. **Primary — path.** Arrflix wrote the file at a deterministic path via the [name template](../name-templates/README.md), so the path is our strongest key. Match the server-reported path against `media_file` paths.
2. **Path-mapping override.** In a containerized setup the server sees the file at *its* mount (e.g. `/data/movies/…`) while Arrflix knows it as `/media/movies/…` — the classic *arr "remote path mapping" problem. An optional per-`(library, media_server)` mapping translates the server's view into ours before matching. Optional because single-host installs with shared mounts don't need it.
3. **Fallback — basename.** If path translation fails or the server reports no usable path, fall back to matching on filename basename + parent dir. Name templates make these distinctive enough to be reliable in practice; ambiguous matches are left `pending` for the next reconciliation pass rather than guessed.

The **rating key is never the join key** — it's what we *store* once correlation succeeds, so subsequent operations (deep links, watch-state) can address the item directly without re-correlating.

### Outbound nudge — debounced per section

After a file reaches `available`, Arrflix nudges every enabled server to index it:

- **Plex** — partial-refresh of the affected section (scoped to the path, not a full library scan).
- **Jellyfin** — library scan / `RefreshLibrary` (modeled).

Nudges are **debounced per `(media_server, section)`** over a short window. Story 02 imports 23 episodes into one section → the worker coalesces them into a single refresh. Debouncing is a worker concern, not a config knob.

A nudge is a best-effort hint, not a guarantee: if it fails (server down) the file is already `available` and reconciliation will backfill propagation when the server returns. Nudges never retry aggressively — the poll is the safety net.

### Reconciliation poll — the backfill

The poll exists so a missed webhook never leaves propagation permanently `pending`:

- **Gated on health** — runs only while the server is `healthy`. A `blocked` server's poll is held, not failed.
- **Precedence** — the webhook is the fast path; the poll is backfill. Both write the *same* record via idempotent upsert, so a webhook that arrives during a poll (or vice versa) is harmless.
- **Cadence** — two triggers: (a) a short delay after an outbound nudge with no confirming webhook → targeted poll of that section; (b) a slow periodic sweep (~1–5 min per the [pattern's guidance](../../patterns/connectivity-health/README.md#cadence-guidance) for Plex) to catch anything the targeted poll missed. The sweep queries the server's recently-added API and correlates each item.

### Connectivity-health — the server as a probed resource

A media server is a [connectivity-health](../../patterns/connectivity-health/README.md) producer. The pattern owns worker shape, status persistence, transition emission, hysteresis, and cadence. This section declares only the server-specific contract.

**Probe.** Authenticated HTTP to a cheap liveness endpoint (Plex: `GET /identity` with the token; Jellyfin: `GET /System/Info`). Success = reachable + authenticated + a parseable version.

**Extended status** beyond the base (`healthy` / `unreachable` / `unknown`):

| Value              | Meaning                                                                                                   |
| ------------------ | --------------------------------------------------------------------------------------------------------- |
| `version_mismatch` | Reachable and authenticated, but the server version is outside the supported range (API shape may differ). |

**Consumer-behavior mapping** (per the [pattern's vocabulary](../../patterns/connectivity-health/README.md#consumer-gating)):

| Status             | Want lifecycle / [acquisition](../acquisition/README.md) | Propagation worker (nudge + poll) | Watch-state worker        |
| ------------------ | -------------------------------------------------------- | --------------------------------- | ------------------------- |
| `healthy`          | `proceed` (unaffected — always)                          | `proceed`                         | `proceed`                 |
| `unreachable`      | `proceed` (**availability is never gated**)              | `blocked` (resumes on recovery)   | `blocked` (poll holds)    |
| `version_mismatch` | `proceed`                                                | `degraded` (attempt, log oddities)| `degraded`                |
| `unknown`          | `proceed`                                                | `degraded`                        | `degraded`                |
| persistent unreach | `proceed`                                                | `failed` (loud admin alert)       | `failed`                  |

The load-bearing row is the first column: **a media server being down has zero effect on whether a want reaches `available`.** Only propagation and watch-state stall, and both resume naturally on recovery. Escalation to `failed` (loud [notification](../notifications/README.md)) happens when the server is persistently unreachable past a TTL — left as an implementer threshold per the [pattern](../../patterns/connectivity-health/README.md#open-questions).

### Watch-state ingestion — the legitimate coupling

`cleanup_after_watch` retention ([requests](../requests/README.md#retention)) genuinely needs "has the requester watched it," which only the player knows. This is the one place a coupling to the server is correct — and it's a *read*, not a gate.

- **Per-user mapping.** A play event carries the *server's* account identity (Plex account, Jellyfin user). Cleanup is per-request, so "watched" means the **requester** watched it → we need to map server-account → Arrflix user. That mapping field lives on the Arrflix user and is owned by [users](../users/README.md) (it's the same Plex account already used for [Plex SSO](#credentials--the-sso-boundary)); this spec *consumes* it.
- **Webhook when available, poll as the floor.** Plex's server-side play/scrobble webhooks are a **Plex Pass feature** — non-Plex-Pass installs get none. So watch-state cannot be webhook-only: the worker polls the sessions/history API as the baseline and treats webhooks as a latency optimization when present. Jellyfin exposes playback via its webhook plugin + sessions API (modeled).
- **Degrades to nothing.** No server, or watch-state unobtainable → `cleanup_after_watch` simply never fires, behaving like `keep_forever` (optionally paired with `keep_for_days(N)`), exactly as [requests](../requests/README.md#retention) specifies. No want, no file, no retention decision is ever *stuck* waiting on watch-state — it just doesn't trigger the optional cleanup.

This is kept **deliberately distinct** from availability coupling (which we removed). Watch-state feeds *retention*, never the want lifecycle.

### Notifications & lazy deep-link resolution

[Notifications](../notifications/README.md) fire on `available` — Arrflix's own truth, never blocked on a server. The "Open in Plex" link resolves lazily:

- Propagation already `visible` when the push fires → embed the server deep link directly.
- Not yet → link to the Arrflix media detail page ("syncing to your server…"), which flips to "Open in Plex" the instant `media_file.propagated` arrives over SSE.

Never a dead link. An **optional grace window** — hold the push up to ~60–120s when a *healthy* server is configured, so the link is usually live on first tap — is modeled but **defaults off**. The no-grace path is already correct; the window is a UX nicety, not a correctness requirement, and is the kind of thing to ship in iteration 2 once propagation latency is measured in practice.

### Multi-server & the Jellyfin path

The record and connection model are server-agnostic from day one: N `media_server` rows, each with a `type`, each accumulating its own propagation rows and its own health. The driver behind the connection is a provider abstraction (mirroring [downloaders'](../downloaders/README.md) `Client` interface shape — this spec does not pin the Go interface). Each mechanic names its Jellyfin equivalent so the abstraction is demonstrably real:

| Mechanic        | Plex (v1)                          | Jellyfin (modeled)                          |
| --------------- | ---------------------------------- | ------------------------------------------- |
| Probe           | `GET /identity` + token            | `GET /System/Info` + API key                |
| Outbound nudge  | partial-refresh of section by path | `RefreshLibrary` / scan                      |
| Inbound new-item| `library.new` webhook              | webhook-plugin `ItemAdded` event            |
| Reconciliation  | recently-added API                 | `Items` query (recently added)              |
| Watch-state     | scrobble webhook (Plex Pass) + sessions/history poll | webhook-plugin playback events + sessions API |
| `external_ref`  | `rating_key`                       | item id                                     |

v1 implements the Plex column. Jellyfin is a driver + credential-shape addition later — no schema or contract change.

## What it does NOT own

- **The want lifecycle / `available`.** Owned by [acquisition](../acquisition/README.md). This spec is strictly downstream of `available`.
- **Plex-as-login (SSO/OAuth).** Owned by [users](../users/README.md). This spec owns the *server connection*, not user identity. See below.
- **Retention policy.** Owned by [requests](../requests/README.md). This spec only *supplies* watch-state; requests/hygiene decide what to do with it.
- **The cleanup worker.** Owned by [hygiene](../hygiene/README.md). It reads watch-state + retention; it does not live here.
- **The connectivity-health pattern itself** (worker shape, hysteresis, audit hook) — this spec is a *producer* conforming to it, not its definition.
- **Notification delivery / routing.** Owned by [notifications](../notifications/README.md). This spec emits `media_file.propagated`; notifications decides who hears about it.
- **The filesystem `media_file` truth.** Owned by [libraries](../libraries/README.md) / [scan](../scan/README.md).

### Credentials & the SSO boundary

The boundary with [users](../users/README.md)' Plex SSO, made explicit so nothing falls between the two specs:

- **Owned here:** the *server connection* — server URL, the admin/server token used to call the server API (refresh, recently-added, sessions), section mapping. Credentials are redacted on the wire and preserved-on-empty-update, exactly like [downloaders](../downloaders/README.md#credentials).
- **Owned by [users](../users/README.md):** the *Plex-account-as-identity* — the OAuth login flow, and the per-user Plex account id stored on the Arrflix user.
- **The bridge:** watch-state ingestion needs to map a play event's Plex account → Arrflix user. It *reads* the per-user Plex account id that users owns. This spec does not duplicate or own that field; it consumes it.

Clean rule of thumb: **identity in, library/playback out.** Anything about *who a user is* lives in users; anything about *talking to the server box* lives here.

## Interactions

| Neighbor                                                       | How it interacts                                                                                                                            |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| [Acquisition](../acquisition/README.md)                        | Upstream. Emits `want.available` / writes `media_file`; this spec reacts *after* `available`. Never gates it. The contract lives in acquisition's [propagation section](../acquisition/README.md#media-server-propagation-decoupled-from-available). |
| [Connectivity-health](../../patterns/connectivity-health/README.md) | Pattern this spec produces. Probe, `version_mismatch` extension, consumer mapping declared here; worker shape + audit hook owned by the pattern. |
| [Notifications](../notifications/README.md)                    | Consumes `media_file.propagated` (deep-link upgrade + SSE) and subscribes to `media_server.health` for `failed`-tier admin alerts.          |
| [Requests](../requests/README.md)                              | `cleanup_after_watch` retention consumes the watch-state this spec ingests. The legitimate coupling.                                        |
| [Hygiene](../hygiene/README.md)                                | The cleanup worker reads watch-state + retention to decide what's safe to remove. This spec supplies the watch-state signal only.           |
| [Users](../users/README.md)                                    | Owns Plex SSO identity + per-user Plex account id; this spec reads that id for watch-state per-user mapping. Boundary above.                |
| [Libraries](../libraries/README.md)                            | Section mapping ties a server section to an Arrflix library; path-mapping override resolves container-mount differences.                    |
| [Scan](../scan/README.md)                                      | Shares the verify authority (file-on-disk truth). Correlation reuses the path knowledge scan/import establish.                              |
| [Name-templates](../name-templates/README.md)                  | Deterministic output paths are what make path-primary correlation reliable.                                                                 |
| [Realtime](../realtime/README.md)                              | `media_server.health` transition channel; `media_file.propagated` SSE fan-out for the lazy deep-link flip.                                  |
| [Audit](../../patterns/audit/README.md) / [Errors](../../patterns/errors/README.md) | Health transitions → admin-action audit; probe/API failures → typed errors (`BadGateway` upstream).                          |

## Tables

**Owned by this spec** (shapes only; column types deferred to iteration 2):

- **`media_server`** — `{ id, name, type, url, credentials, section_mapping, enabled, default, status, status_checked_at, status_last_transitioned_at, created_at, updated_at }`. Replaces the old single-Plex config.
- **`media_server_propagation`** — `{ media_file, media_server, state, external_ref, synced_at }`. The per-pair record. (Name indicative.)

**Referenced, owned elsewhere:**

- **`media_file`** — [libraries](../libraries/README.md) / [scan](../scan/README.md). Server-agnostic; carries no rating key.
- **user → Plex account id** — [users](../users/README.md). Read for watch-state per-user mapping.
- **request retention flags + watch state** — [requests](../requests/README.md). This spec supplies the watch signal; requests/hygiene own the decision.

## Open questions

1. **Section-mapping config shape.** How much does the operator configure vs. auto-discover? Plex exposes its sections via API — we could auto-list and let the operator bind each to an Arrflix library, vs. requiring manual entry. Lean: auto-discover sections, operator binds. Iteration-2 detail.
2. **Path-mapping override granularity.** Per-`(library, media_server)` is proposed. Is per-section ever needed (one library spanning multiple server sections)? Lean: start per-`(library, server)`; revisit if a real case needs finer.
3. **Targeted-poll delay after nudge.** How long to wait for a confirming webhook before firing a targeted reconciliation poll? Too short = redundant polls; too long = laggy deep links. Lean: ~30–60s, tunable; the slow sweep covers the tail regardless.
4. **Grace-window default.** Modeled, defaults off. Worth shipping the *toggle* in v1, or defer the whole thing to iteration 2? Lean: defer the implementation; keep the design note so the notification flow already anticipates it.
5. **Watch-state poll cost.** Polling sessions/history on every server on a cadence has a cost proportional to library size for some endpoints. Need to confirm Plex/Jellyfin expose an *incremental* "watched since T" query rather than a full history walk. Lean: incremental where the API allows; flag if it doesn't.
6. **"Watched" semantics for shared accounts.** If multiple Arrflix users share one Plex account, "the requester watched it" is ambiguous. Lean: treat any play on the mapped account as the requester watching; document the limitation. Multi-user-per-account is an edge case for v1.
7. **Persistent-unreachable → `failed` TTL.** When does a down server escalate from quiet `blocked` to a loud admin alert? Per the connectivity-health pattern this is an implementer threshold. Lean: longer than downloaders (a server being down doesn't break acquisition) — order of 10–15 min.
8. **Jellyfin webhook-plugin dependency.** Jellyfin's inbound events require the operator to install the webhook plugin. When the Jellyfin driver lands, does Arrflix hard-require it, or fall back to poll-only? Lean: poll-only floor (same as Plex non-Pass), webhook optional. Out of v1 scope regardless.

## What we're explicitly not deciding here

- Exact column types, the `section_mapping` / `credentials` JSON shapes, and the propagation-record table name (iteration 2).
- The Go provider interface for the server driver (deferred until the Jellyfin driver makes the seam concrete — same posture as [downloaders](../downloaders/README.md)).
- API route shapes and OperationIDs for server CRUD / test / webhook receiver (iteration 2).
- UI: the server settings screen, the propagation badge on a media page, the "syncing…" → "Open in Plex" affordance (iteration 2).
- Connectivity-health internals (worker, hysteresis, audit row shape) — owned by the [pattern](../../patterns/connectivity-health/README.md).
- The Plex OAuth login flow and per-user account-id storage — owned by [users](../users/README.md).
- Retention evaluation and the cleanup worker — owned by [requests](../requests/README.md) / [hygiene](../hygiene/README.md).

## Doc neighbors

- [Acquisition](../acquisition/README.md) — upstream; owns the want lifecycle and the propagation contract this spec implements.
- [Connectivity-health](../../patterns/connectivity-health/README.md) — the pattern this spec produces.
- [Notifications](../notifications/README.md) — consumes `media_file.propagated`; lazy deep-link resolution.
- [Requests](../requests/README.md) — `cleanup_after_watch` retention; the legitimate watch-state coupling.
- [Hygiene](../hygiene/README.md) — cleanup worker that reads watch-state + retention.
- [Users](../users/README.md) — Plex SSO identity boundary; per-user account mapping.
- [Libraries](../libraries/README.md) / [Scan](../scan/README.md) — `media_file` truth, section/path mapping, verify authority.
- [Name-templates](../name-templates/README.md) — deterministic paths underpin path-primary correlation.
- [Downloaders](../downloaders/README.md) — sibling provider-abstraction + connectivity-health producer; structural template for this spec.
