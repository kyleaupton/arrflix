# Connectivity health — the runtime-liveness pattern

**Status:** Draft, iteration 1

This doc defines a **cross-cutting pattern**: every peripheral system Arrflix talks to — a [library](../../modules/libraries/README.md) on disk, a [downloader](../../modules/downloaders/README.md) HTTP endpoint, a future Prowlarr or Plex connection — needs the same shape of runtime liveness probing. Each module probes differently (filesystem `stat` vs authenticated HTTP vs search ping), but the contract around persisting the result, signalling transitions, and reacting downstream is identical.

This pattern captures **that contract**, the same way [audit](../audit/README.md) captures the principle around decision artifacts: producers share a shape, not an implementation. It is deliberately **descriptive**, not prescriptive — there is no shared worker, no shared Go interface in v1. Each module's spec owns its own probe and its own worker; this pattern is the agreement they all conform to.

## TL;DR

- **Distinct from hygiene.** Hygiene asks "what's wrong inside my library?" Connectivity health asks "is the system I depend on actually reachable?" Different cadence, different output, different consumers.
- **Common shape**: every probed resource carries a `status` + `status_checked_at` + `status_last_transitioned_at` triple on its row. Same three columns, same three names, every resource type.
- **Base status enum**: `healthy` / `unreachable` / `unknown` is mandatory across all resources. Each resource type extends with values appropriate to its domain (`auth_failed`, `read_only`, `low_space`, `rate_limited`, …).
- **Transitions emit [realtime](../../modules/realtime/README.md) events** on a standardized channel shape (`<resource_type>.health`). Persistent state changes only — flap suppression via hysteresis is mandatory.
- **Cadence guidance**: ~60s default for hot infrastructure (libraries, downloaders), 1-5min for indexers, longer for upstream metadata APIs. Configurable; the pattern recommends ranges, not hard values.
- **Consumer gating vocabulary**: a small set of recommended behaviors (`proceed`, `degraded`, `blocked`, `failed`) that consumer specs map their reactions onto. Standardizing the vocabulary keeps cross-spec behavior coherent.
- **Audit hook**: health transitions are admin-action-audit events ([users spec](../../modules/users/README.md#admin-action-audit)), not decision-artifact events.

## Why this is a cross-cutting pattern, not a module

Three subsystems today (libraries, downloaders, and the soon-to-spec indexers) all need to answer the same operational questions:

- Is this resource reachable right now?
- If it just stopped being reachable, how do downstream consumers find out?
- If it's been unreachable for a while, what should the admin see and where?

If we put that logic in one of the modules, the others would either duplicate it or awkwardly depend on a peer. If we built a single shared health worker, we'd over-design for code reuse that has minimal payoff (the probes themselves are too different — one is a `syscall.Stat`, another is an authenticated HTTP call, a third is a Prowlarr search ping).

The right shape is: **producers share a contract and the operational surfaces; they don't share an implementation.** Same shape as [audit](../audit/README.md) and [errors](../errors/README.md). Each module spec declares "implements this pattern with this probe and these extended statuses"; the pattern says what the shape and downstream contract look like.

## The shape

### Persistence

Every probed resource carries three columns on its row:

| Column                          | Type        | Meaning                                                              |
| ------------------------------- | ----------- | -------------------------------------------------------------------- |
| `status`                        | enum / text | The current health value (see [Base status enum](#base-status-enum)) |
| `status_checked_at`             | timestamptz | When the most recent probe ran (regardless of outcome)               |
| `status_last_transitioned_at`   | timestamptz | When the status last changed value                                   |

Status lives **on the resource row**, not in a centralized table. A library's health is a column on `library`; a downloader's is a column on `downloader`. This means:

- Reads are joinless — listing libraries with status is one query.
- Deleting a resource takes its health with it; no orphan cleanup.
- Multi-resource queries ("show me all unhealthy infrastructure") are unions across small tables, which is fine for the cardinalities involved (tens of rows per type, not millions).

The pattern deliberately does **not** add a separate `health_history` table. Transition history lives in the [admin-action audit stream](#audit-hook), where it belongs.

### Base status enum

Every implementer's status field uses values from the **base set** plus an optional set of resource-specific **extensions**.

**Base (mandatory for every implementer):**

| Value         | Meaning                                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------------------- |
| `healthy`     | The most recent probe succeeded. Resource is operational.                                                     |
| `unreachable` | Probe failed at the connection layer (network, DNS, refused, timeout). Transient or persistent — both fit here. |
| `unknown`     | Initial state, before any probe has completed. Also the state during a probe-worker outage.                   |

**Resource-specific extensions** are declared by each implementer's spec. Examples:

| Resource type    | Extended values                                | Spec where declared                                                |
| ---------------- | ---------------------------------------------- | ------------------------------------------------------------------ |
| Library          | `read_only`, `low_space`                       | [Libraries](../../modules/libraries/README.md#runtime-health-check) |
| Downloader       | `auth_failed`                                  | [Downloaders](../../modules/downloaders/README.md#runtime-health)   |
| Indexer (future) | `rate_limited`, `degraded`                     | Indexers spec (pending)                                            |
| Plex (future)    | `version_mismatch`                             | Plex spec (pending)                                                |

Implementers should **prefer the base set** when a more specific value isn't meaningfully different. Adding extensions is cheap; consumers default to safe behavior for values they don't recognize (see [Consumer gating](#consumer-gating)).

### Probe contract

Each implementer defines its own probe function. The pattern says nothing about implementation — just the contract:

- A probe is **a function the worker calls** that returns a status value plus an optional structured error.
- A probe is **cheap** — milliseconds, not seconds. If a meaningful liveness check takes 30s, the resource's worker should run it less often, not block the polling loop.
- A probe is **side-effect-free** beyond network I/O. It does not write files, mutate database state, or create work.
- A probe is **deterministic at the status level** — same conditions yield the same status. A succeeded probe always returns `healthy`; a connection-refused failure always returns `unreachable`.

The probe is owned by the module's spec. The pattern doesn't prescribe what "healthy" means for a library vs a downloader — only that the status enum and persistence shape are uniform.

### Transition emission

Status changes are signalled via [realtime](../../modules/realtime/README.md) on a per-resource-type channel: `<resource_type>.health` (e.g., `library.health`, `downloader.health`). The event payload includes:

| Field             | Meaning                                                |
| ----------------- | ------------------------------------------------------ |
| `resource_id`     | UUID of the affected resource                          |
| `resource_type`   | `library` / `downloader` / etc.                        |
| `from_status`     | Previous status value                                  |
| `to_status`       | New status value                                       |
| `transitioned_at` | When the change occurred                               |
| `reason`          | Short structured string (optional — e.g., "probe timeout") |

**Only transitions emit events.** A library that's been `unreachable` for an hour does not emit hourly events. The frontend can subscribe once and trust that the absence of events means the status hasn't changed.

### Hysteresis / debouncing

Implementers **must** apply hysteresis to avoid flapping:

- A status flip from `healthy` → `unreachable` requires N consecutive failed probes (default: 2). Default cadence + threshold means flaps shorter than ~2 minutes don't surface to operators.
- A status flip from `unreachable` → `healthy` is **immediate** on the first success. Recovery should propagate fast; degradation should require evidence.
- Asymmetric hysteresis is the standard. The pattern recommends 2-failures-to-degrade, 1-success-to-recover.

Implementers may tune the thresholds per resource type if the probe is noisier than usual; the cadence + hysteresis combination should produce no false positives in steady-state operation.

### Cadence guidance

The pattern recommends ranges, not hard values. Each resource type's spec picks its own.

| Resource type       | Recommended cadence | Rationale                                                  |
| ------------------- | ------------------- | ---------------------------------------------------------- |
| Libraries           | 30-90s              | Hot infrastructure; fast detection matters for write-blocking |
| Downloaders         | 30-90s              | Same; client may need re-auth on cold connections           |
| Indexers (future)   | 1-5min              | Rate-limit-sensitive; probe IS a query against the upstream |
| Plex (future)       | 1-5min              | Restart / version flips matter, but webhook is primary signal |
| Upstream APIs (TMDB) | 5-15min (or none)   | Caller-side error handling probably suffices               |

A **single global cadence per resource type** is the recommendation. Per-row cadence is overkill — every library on the same install has the same disk-poll urgency.

## Consumer gating

The hardest part of this pattern, and the most useful thing to standardize: what consumers do when a resource is unhealthy.

The pattern defines four recommended **behaviors** that consumer specs map their reactions onto:

| Behavior     | What it means                                                                       | Typical for                                                              |
| ------------ | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `proceed`    | Normal operation — no special handling                                              | `healthy`                                                                |
| `degraded`   | Proceed, but be aware. UI badges, tiebreakers in routing, advisory notifications     | `low_space`, `read_only` (for read-only consumers), `degraded` indexer   |
| `blocked`    | Hold pending work; do not enqueue new work; in-flight retries with backoff           | `unreachable`, `auth_failed`, `rate_limited`                             |
| `failed`     | Same as `blocked`, plus loud admin notification — likely a config / persistent issue | `auth_failed`, persistent `unreachable` beyond a TTL                     |

Each resource type's spec includes a **status → behavior mapping** as part of its runtime-health section. Consumers branch on the behavior, not on the raw status, so:

- A library's `read_only` status maps to `blocked` for the import worker, `degraded` for routing tiebreakers, `proceed` for the scanner reading inventory.
- A downloader's `auth_failed` maps to `failed` (loud) rather than `blocked` (quiet), because re-trying without intervention won't help.

Mapping is **per-consumer**, not per-resource — the same status can lead to different behaviors in different contexts, and that's fine. The pattern's job is to give the four behaviors a shared name so cross-spec coordination stays sane.

Consumers encountering an **unknown extended status** should default to `degraded` — surface it in the UI, don't proceed silently, but don't aggressively block either.

## Producer responsibilities

Each module implementing this pattern must:

1. **Declare its probe** in the module spec — what it checks, what success and failure look like.
2. **Add the three status columns** to the resource row (`status`, `status_checked_at`, `status_last_transitioned_at`).
3. **Declare its extended status values** (if any) in the module spec, including the consumer-behavior mapping for each.
4. **Run a health worker** that polls at the recommended cadence, applies hysteresis, persists status, and emits transition events.
5. **Write transition events to the [admin-action audit stream](#audit-hook)**.
6. **Document the consumer-impact mapping** so downstream specs can branch on behavior, not on raw status.

That is the full contract. No shared Go interface, no shared worker base class — just the contract and the consumers.

## Audit hook

Health transitions are admin-relevant operational events. They live in the **admin-action audit stream**, not the [decision-artifact stream](../audit/README.md), because they don't represent decisions Arrflix made — they represent state changes in things Arrflix depends on.

This stream is owned by [users](../../modules/users/README.md#admin-action-audit). Connectivity-health producers contribute event types like:

- `library.health_transitioned` — with `from`, `to`, `reason`
- `downloader.health_transitioned` — same shape
- `indexer.health_transitioned` (future) — same shape

The audit row gives operators "when did this start going wrong?" queryability without the producer needing to maintain its own history table.

## What this pattern does NOT do

- **Probe code sharing.** Each module writes its own probe. The pattern does not prescribe a Go interface, a base struct, or a shared scheduler in v1.
- **Centralized health worker.** No central probing service; each module's worker runs in-process alongside the resource it owns.
- **Multi-host or distributed health.** Single-process assumption holds. Multi-instance Arrflix is out of scope today.
- **SLA tracking.** No uptime calculations, no percentile latency, no per-resource SLA dashboards. If those become useful later, they're additive layers reading the audit stream.
- **Replace per-module dashboards.** Libraries' health badge stays in `LibrarySettings`; downloaders' in `DownloaderSettings`. The pattern unifies the data shape, not the presentation.
- **Compliance-grade audit.** Transitions are observable, not signed. Same call-out as the audit pattern.

## Interactions

| Neighbor                                                | How it interacts                                                                                                       |
| ------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| [Libraries](../../modules/libraries/README.md)          | Producer; filesystem-based probe. Extended statuses: `read_only`, `low_space`.                                          |
| [Downloaders](../../modules/downloaders/README.md)      | Producer; `Client.Test()`-based probe. Extended status: `auth_failed`.                                                  |
| Indexers (pending spec)                                 | Producer; ping-based probe. Extended statuses: `rate_limited`, `degraded`.                                              |
| [Routing](../../modules/routing/README.md)              | Consumer; gates dispatch on target resource's behavior (`blocked` resources hold pending work).                         |
| [Acquisition](../../modules/acquisition/README.md)      | Consumer; defers searches and grabs against `blocked` infrastructure. Resumes naturally on `healthy` transition.        |
| [Scan](../../modules/scan/README.md)                    | Consumer; skips auto-scans of `blocked` libraries; manual scans still allowed (operator may be diagnosing).             |
| Import (existing)                                       | Consumer; holds tasks against `blocked` libraries, retries on `healthy` transition.                                     |
| [Hygiene](../../modules/hygiene/README.md)              | Visual neighbor only — the hygiene dashboard may display connectivity status alongside content findings, but the data lives on the resource rows, not in `hygiene_finding`. |
| [Audit](../audit/README.md)                             | Sibling cross-cutting pattern. Audit covers decisions Arrflix makes; connectivity-health covers state of things Arrflix depends on. Complementary, non-overlapping. |
| [Errors](../errors/README.md)                           | Sibling pattern. A probe failure produces a typed error ([`BadGateway`](../errors/README.md#kind-axis) for upstream, `Internal` for invariants); the worker maps it to a status. |
| [Users](../../modules/users/README.md#admin-action-audit) | Owner of the admin-action audit stream where health transitions land.                                                 |
| [Notifications](../../modules/notifications/README.md)  | Subscribes to `*.health` [realtime](../../modules/realtime/README.md) channels for operator alerts on `failed`-tier transitions.                                 |

## Open questions

1. **Pattern vs scaffolding.** V1 is purely descriptive — every module writes its own worker. Once we have 3+ implementers, the duplication may justify a small shared library (`health.Worker` interface + a per-resource probe function + scheduled-tick boilerplate). Lean: revisit after the third implementer ships. Not a v1 decision.
2. **Naming.** "Connectivity health" disambiguates from the [hygiene](../../modules/hygiene/README.md) health score, but it's a mouthful. Alternatives: `liveness`, `runtime-health`, `infra-health`. Lean: keep `connectivity-health` for now — explicit beats clever.
3. **Status-on-row vs separate health table.** Three columns on every probed resource type vs a unified `resource_health(resource_type, resource_id, status, …)` table. The on-row approach is simpler today (joinless reads, natural cascade-on-delete). The unified-table approach scales better if we add many resource types. Lean: on-row for v1; revisit if the resource-type count grows beyond a handful.
4. **TTL for "persistent unreachable" → `failed`.** When does `unreachable` escalate from `blocked` to `failed`? Probably an implementer-specific decision (libraries: maybe 5 minutes; downloaders with `auth_failed`: immediate). Worth a pattern-level recommendation, or leave to each spec? Lean: leave to each spec; document the typical thresholds in the module spec.
5. **Manual override.** Should an operator be able to mark a resource `healthy` manually to unblock pending work even if the probe disagrees? Useful for "I just fixed the SMB mount, don't wait for the next poll." Lean: yes, opt-in per resource type, audited as an admin-action.
6. **Probe-on-demand endpoint.** Each resource type already has its own variant (`POST /downloaders/{id}/test`, libraries-todo). Should the pattern require a uniform endpoint shape (`POST /<resource>/{id}/probe`)? Lean: yes, recommend it; doesn't have to be enforced.
7. **Status during process startup.** Boot order: the worker starts but hasn't completed its first probe yet. Status is `unknown`. Consumers should treat `unknown` as `degraded` (proceed cautiously) or `blocked` (hold)? Lean: `degraded` to avoid a startup pause; users running a fresh install want the system to respond immediately. Document explicitly.
8. **TMDB and other upstream-API health.** These are different in kind: not "infrastructure we depend on continuously," more "APIs we call occasionally." Probably out of scope for this pattern; the caller's typed-error handling ([errors](../errors/README.md)) is sufficient. Flag and revisit if a real case demands.
9. **Multi-instance future.** If Arrflix ever runs as multiple coordinating processes, the on-row status approach requires a coordinated probe owner (one process polls, all read). Out of scope for now; flag that this assumption exists.
10. **Status persistence across restarts.** On boot, do we trust the last-persisted status until the first probe, or reset to `unknown`? Lean: reset to `unknown`. Stale `healthy` claims after a restart are misleading.

## What we're explicitly not deciding here

- The probe implementation for any specific resource (lives with each module spec)
- Exact threshold values (failure-count for hysteresis, low-space floor, etc.) — module-level
- The [realtime](../../modules/realtime/README.md) channel registration mechanism in code
- Notification routing rules for `failed`-tier transitions (lives in [notifications](../../modules/notifications/README.md))
- A shared Go `Prober` interface (deferred until 3+ implementers exist)
- The exact admin-action audit row shape for health transitions (lives with users spec)
- UI presentation of status (per-module setting screens own their own badges)
- Whether the hygiene dashboard aggregates connectivity status into its overall score

## Doc neighbors

- [Audit](../audit/README.md) — sibling cross-cutting pattern; covers decision artifacts (this pattern covers infrastructure state)
- [Errors](../errors/README.md) — sibling cross-cutting pattern; probe failures produce typed errors
- [Libraries](../../modules/libraries/README.md) — producer
- [Downloaders](../../modules/downloaders/README.md) — producer
- [Routing](../../modules/routing/README.md) — consumer (gates dispatch on target health)
- [Acquisition](../../modules/acquisition/README.md) — consumer (holds against blocked infrastructure)
- [Scan](../../modules/scan/README.md) — consumer (skips blocked libraries)
- [Hygiene](../../modules/hygiene/README.md) — visual neighbor; presentation overlap, no data overlap
- [Users](../../modules/users/README.md#admin-action-audit) — owns the audit stream this pattern writes into
- [Notifications](../../modules/notifications/README.md) — subscribes to transition events
