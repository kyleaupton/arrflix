---
status: as-built
---

# Downloaders — the clients that pull bits down

> **Beyond what's built:** §Runtime health (the background probe, `auth_failed`, consumer gating), password redaction on the wire in §Credentials, and the category catalog in §Categories and tags are unbuilt design. Connectivity health is deferred ([V1.md](../../V1.md#out--later)).

A downloader is a configured connection to a third-party client (qBittorrent today; SABnzbd / NZBGet planned) that Arrflix hands releases off to for actual transfer. It is one of the three things [routing](../routing/README.md) dispatches to (alongside libraries and name templates) and the runtime engine the [download-job worker](../acquisition/README.md) polls.

The model has a well-designed **provider interface** with qBittorrent as the sole concrete implementation today. This spec captures what exists, describes the planned additions (background health, credential redaction on the wire), and surfaces the still-open questions about categories, encryption, and failover.

## TL;DR

- A downloader is a named connection: `(name, type, protocol, url, credentials, config_json)` plus standard `enabled` / `default` flags.
- **Two-axis classification**: `type` is the provider (`qbittorrent`, future: `sabnzbd`, `nzbget`); `protocol` is the wire (`torrent` or `usenet`). Each downloader picks one type and one protocol.
- One default per **protocol** (not per type) — a torrent default and a usenet default, picked when routing doesn't specify.
- All providers conform to a single **`Client` interface**: `Test`, `Add`, `Get`, `List`, `ListFiles`, `Pause`, `Resume`, `Remove` (plus `Type` / `InstanceID`). Providers register via a builder registry; the `Manager` instantiates and caches one client per enabled downloader row.
- **Provider extensibility is built in** but only qBittorrent ships. Adding a provider is: implement the interface, register a builder, declare type-specific `config_json` shape, and widen the `type` CHECK.
- Planned: runtime health via the [connectivity-health pattern](../../patterns/connectivity-health/README.md), **credential redaction on the wire**, and a path toward **encrypted-at-rest credentials** (open question).
- The download-job state machine lives **with the acquisition pipeline**, not here. This spec covers downloader configuration, provider abstraction, and runtime health — not job orchestration.

## What a downloader owns

A downloader row encodes:

- **Name** — display name, unique (case-insensitive).
- **Type** — provider tag. The schema CHECK and service validation admit only `qbittorrent`; `sabnzbd` and `nzbget` exist only as Go constants (`downloader.TypeSABnzbd` / `TypeNZBGet`) with no implementation.
- **Protocol** — `torrent` or `usenet`. Constrains which releases this downloader can accept; routing uses this for natural eligibility.
- **URL** — the client's base URL.
- **Username / password** — plaintext on the wire and at rest. See [Credentials](#credentials) and [open questions](#open-questions).
- **`config_json`** — nullable JSONB free-form payload for provider-specific config. Nothing reads it yet; reserved for things like default category, default save-path mapping, port-mapping hints, etc.
- **Enabled flag** — when false, the downloader is excluded from the manager's init pass, and saving it disabled drops its cached client. Existing jobs targeting a disabled downloader therefore can't reach it: the download worker's client lookup fails non-retryably and the job fails.
- **Default flag** — at most one default per protocol. Used when routing's action set has no explicit downloader.
- **Created / updated timestamps**.

The runtime view adds:

- **`Initialized`** — whether the manager successfully connected (and `Test` passed) at startup or after a save. Not persisted; derived from the in-memory client cache.

## The provider model

Providers conform to a single interface (`internal/downloader/downloader.go`):

```go
type Client interface {
    Type() Type
    InstanceID() InstanceID
    Test(ctx) (TestResult, error)
    Add(ctx, AddRequest) (AddResult, error)
    Get(ctx, externalID) (Item, error)
    List(ctx) ([]Item, error)
    ListFiles(ctx, externalID) ([]File, error)
    Pause(ctx, externalID) error
    Resume(ctx, externalID) error
    Remove(ctx, externalID, deleteData bool) error
}
```

`List`, `Pause`, `Resume`, `Remove`, and `ListFiles` may return `ErrUnsupported`; the movie import spawn falls back to the item's content path when `ListFiles` is unsupported.

Two registries make this extensible:

- **Type registry** — maps a type string (`qbittorrent`) to a builder function `func(ConfigRecord) (Client, error)`. Adding a provider means writing the implementation and registering a builder; no central switch statements.
- **Manager** — loads enabled downloaders on startup, dispatches to the builder registry per row, runs `Test`, and caches the resulting `Client` keyed by downloader ID only if the test passes. A save re-initializes that one row; a delete drops it. Jobs at execution time fetch their client via `manager.GetClientByID(ctx, id)`, which only reads the cache — a downloader that was unreachable at startup stays uncached until it is re-saved.

This is the right shape. The work to add SABnzbd or NZBGet is bounded: write a wrapper, register it, declare the type-specific `config_json` keys it reads.

### `AddRequest` shape

The interface carries enough fields to express the union of provider expectations. The download worker sets only the link today (`MagnetURL` for torrent, `NZBURL` for usenet); the rest are unused:

- `MagnetURL` / `NZBURL` — protocol-appropriate payload
- `Category` — provider-side organizational tag (qBit category, SAB category)
- `Tags` — provider-side tags (currently torrent-only via qBit)
- `Paused` — start paused vs immediately downloading
- `SavePath` — override the provider's default save-path (used to land files in a location the import worker knows to scan)

`Add` returns `AddResult{ExternalID, ...}` — the provider's identifier (torrent infohash, NZB queue ID), which the download-job persists in `downloader_external_id` for subsequent polling.

### `Item` shape (returned by `Get` / `List`)

- `Status` — a normalized `JobStatus` (`queued` / `downloading` / `seeding` / `completed` / `paused` / `errored` / `unknown`). The provider maps its native states into it (qBit `stalledDL` → `downloading`, `stalledUP` → `seeding`, `missingFiles` → `errored`, …). Stored in `download_job.downloader_status` for UI display.
- `Progress` — 0.0 → 1.0
- `SavePath` / `ContentPath` — where the provider claims the bits live; persisted onto `download_job.save_path` / `content_path`
- `DownloadSpeed`, `ETA` (seconds), `TotalSize` — for the live stats column

The download-job worker maps `JobStatus` into the job's own state machine (created → enqueued → downloading → completed / failed / cancelled): `completed` / `seeding` → `completed`, `errored` → `failed`, everything else → `downloading`. That mapping is the worker's concern, not the provider's.

## Credentials

Today:

- Username and password are **stored plaintext** in the `downloader` table.
- Password is **returned plaintext on the wire** in `GET /downloaders` and `GET /downloaders/{id}`, because the frontend re-displays the value in the edit form.
- `PUT /downloaders/{id}` already treats an empty or absent password as "preserve existing".
- The qBit client logs in once per manager session, caches the session cookie (SID), and uses the cookie for subsequent calls. Credentials are not re-sent per request, but they sit in memory on the `Client` struct for re-login.

This is a known limitation, called out in `model/downloader.go`. The wire side is the cheap fix; at-rest encryption is a bigger conversation (see [open questions](#open-questions)).

**Wire-side fix** (unbuilt; concrete, low-risk):

- `GET /downloaders` and `GET /downloaders/{id}` redact `password` to an empty string or a sentinel.
- `PUT /downloaders/{id}` keeps treating an empty / sentinel password as "preserve existing" (the handler + `service/downloaders.go` already do); only an explicit non-empty value overwrites. This only requires the FE to surrender the "show the existing password in the form" pattern.
- A separate `POST /downloaders/{id}/password` endpoint for explicit rotation is cleaner if the FE wants a "change password" affordance instead of mixing into the general edit form. Lean: skip the separate endpoint for v1, just teach the form to treat empty as preserve.

## Lifecycle

```
        ┌───────────┐                  ┌───────────┐
  ──►   │  created  │ ◄───── update ──►│  enabled  │ ◄──► (test on demand)
        └─────┬─────┘                  └─────┬─────┘
              │                              │
              │ initial test                 │ disable
              ▼                              ▼
       ┌─────────────┐                ┌───────────┐
       │ initialized │                │ disabled  │
       │  (cached)   │                └─────┬─────┘
       └─────────────┘                      │
                                            │ delete
                                            ▼
                                      ┌───────────┐
                                      │  deleted  │
                                      └───────────┘
```

**State semantics:**

- **created** — row exists; the handler best-effort calls `InitializeDownloader` after a successful save. Failure is swallowed and does not fail the API response.
- **initialized** — the manager has a live, authenticated `Client` cached for this row. The worker can hand it jobs.
- **enabled / disabled** — controls inclusion in the manager's init pass. Disabling drops the cached client, so in-flight jobs on it fail at their next poll (see the enabled flag above).
- **deleted** — row removed and client dropped. `download_job.downloader_id` is `ON DELETE RESTRICT`, so a downloader with any job history can't be deleted; routing rules referencing it are `ON DELETE SET NULL`.

## Operations

CRUD plus two test verbs. All routes are JWT-gated; permission scoping lives in [users](../users/README.md).

| OperationID                    | Method | Path                                          | Notes                                                                            |
| ------------------------------ | ------ | --------------------------------------------- | -------------------------------------------------------------------------------- |
| `downloaders-list`             | GET    | `/api/v1/downloaders`                         | All downloaders, with `initialized` runtime flag.                                |
| `downloaders-get`              | GET    | `/api/v1/downloaders/{id}`                    | Password returned plaintext; redaction unbuilt (see [Credentials](#credentials)). |
| `downloaders-get-default`      | GET    | `/api/v1/downloaders/default/{protocol}`      | Per-protocol default lookup.                                                     |
| `downloaders-create`           | POST   | `/api/v1/downloaders`                         | Validates + initializes (best-effort).                                           |
| `downloaders-update`           | PUT    | `/api/v1/downloaders/{id}`                    | Empty password = preserve existing. Re-initializes the client (best-effort).     |
| `downloaders-delete`           | DELETE | `/api/v1/downloaders/{id}`                    | Refused while jobs reference it (`RESTRICT`); see [open questions](#open-questions). |
| `downloaders-test`             | POST   | `/api/v1/downloaders/{id}/test`               | Tests an existing downloader's connection. Calls `Client.Test()`.                |
| `downloaders-test-config`      | POST   | `/api/v1/downloaders/test`                    | Tests a candidate config pre-save. Same probe, no persistence.                   |

Permissions (defined in [users](../users/README.md)): every downloader endpoint is gated on the single `admin.downloaders.manage` key. A read / write / test split (so connection-testing could be delegated without CRUD) is not in the frozen catalog.

## Validation

At create / update time the service validates:

1. **Name** non-empty; case-insensitive uniqueness is a unique index (a duplicate surfaces as `Conflict`).
2. **Type** is `qbittorrent`.
3. **Protocol** in (`torrent`, `usenet`).
4. **URL** non-empty and parseable.
5. **Default flag** — clears `default` on any other downloader of the same protocol (best-effort, before the write); a partial unique index enforces one default per protocol.

Validation does **not** include a live connection check at save time — that's the explicit `test` endpoint. Save-then-test is intentional: a user may legitimately add a downloader whose target is currently offline.

## Runtime health

Downloaders are a producer of the [connectivity-health pattern](../../patterns/connectivity-health/README.md). The pattern owns the worker shape, status persistence, transition emission, hysteresis, and cadence. This section covers only the downloader-specific contract.

**Today**, health is checked at three points only:

1. **Manager init** at process startup.
2. **Best-effort init** after a successful create or update.
3. **Explicit `POST /test`** when the operator clicks the button.

Between those, the system has no idea whether a downloader is reachable. A qBit instance whose container is down silently breaks every dispatched job until someone notices the pile-up. The runtime-health worker (unbuilt) closes that gap.

**Probe.** Per enabled, initialized downloader: `Client.Test()` succeeds (authenticates + fetches version). On `auth_failed`, the cached session is invalidated so the next probe re-attempts login.

**Extended status** beyond the pattern's base (`healthy` / `unreachable` / `unknown`):

| Value         | Meaning                                                                                                          |
| ------------- | ---------------------------------------------------------------------------------------------------------------- |
| `auth_failed` | Connection succeeds but auth is rejected. Distinguished from `unreachable` because re-trying alone won't help.  |

**Consumer-behavior mapping** (per the [pattern's vocabulary](../../patterns/connectivity-health/README.md#consumer-gating)):

| Status        | [Routing](../routing/README.md) | [Acquisition](../acquisition/README.md) (planned grabs) | Acquisition (in-flight jobs)                            |
| ------------- | ------------------------------- | -------------------------------------------------------- | ------------------------------------------------------- |
| `healthy`     | `proceed`                       | `proceed`                                                | `proceed`                                               |
| `unreachable` | `blocked`                       | `blocked`                                                | retry loop catches naturally (`BadGateway`, retryable)  |
| `auth_failed` | `failed`                        | `failed`                                                 | `failed` — admin must fix credentials                   |
| `unknown`     | `degraded`                      | `degraded`                                               | `proceed`                                               |

In-flight jobs are **not aborted** on a health transition. A downloader that flips to `unreachable` mid-job lets the existing worker retry-loop catch the failure naturally. `auth_failed` is the loud case because retrying alone can't recover — the admin-action audit row + [notification](../notifications/README.md) are the operator's signal to intervene.

## Categories and tags

qBit (and SAB) support **categories** (a primary organizational tag) and qBit additionally supports **tags** (free-form labels). Today:

- `AddRequest.Category` and `AddRequest.Tags` exist on the interface (qBit's wrapper honors them), but the download worker never sets them.
- There is **no validation** that the category exists on the downloader; the request is accepted, qBit may or may not surface the unknown category.
- There is **no configured catalog** of categories per downloader. `config_json` is empty by default; no schema reads from it.

This is a known gap. The likely shape (deferred):

- `config_json` grows a `categories` array (typed per provider) — informational only, not synced with the upstream.
- Routing actions can carry an optional category override. Routing actions carry no category today.

Open question on whether we should ever *sync* the downloader's actual categories back into `config_json` — pulled into [open questions](#open-questions).

## Multi-downloader semantics

The system is already multi-downloader-aware. Documented for completeness:

- **Manager caches one client per row** — independent sessions, independent credentials.
- **Per-protocol default** — torrent and usenet defaults are separate; routing's fallback picks the appropriate one for the release's protocol.
- **No global default** — there's no "the downloader" assumption anywhere in the code.
- **No automatic failover** — if a routing rule picks downloader A and A is unreachable, the job retries (or, if A has no cached client, fails). It is never re-routed to downloader B. This is an [open question](#open-questions).
- **No per-downloader concurrency cap** — the download-job worker claims 20 jobs per tick *globally*, not per-downloader. If one downloader is saturated, the worker doesn't know to back off there specifically.

## Integration points

| Consumer                                          | How it uses downloaders                                                                                                              |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| **[Routing](../routing/README.md)**               | Rules reference downloaders by ID. The action set on a fired rule (or the protocol default) fills `download_job.downloader_id`. Health gating the dispatch is unbuilt. |
| **[Acquisition](../acquisition/README.md)**       | The download-job worker fetches the right client per job and drives the state machine via `Add` / `Get`.                             |
| **[Importer](../importer/README.md)**             | `ListFiles` (joined onto `Item.SavePath` / `ContentPath`) at import-task spawn, and again when the import worker re-derives a source path. |
| **[Users](../users/README.md)**                   | `admin.downloaders.manage` gates API access.                                                                                          |
| **[Audit pattern](../../patterns/audit/README.md)** | Health transitions, manual test invocations, and credential changes are audit-worthy admin actions.                                |
| **Frontend (`views/settings/downloader/DownloaderSettings.vue`)** | CRUD + test UI. Unbuilt: live health badge.                                                                          |

## What downloaders does NOT own

- The **download-job state machine** ([acquisition](../acquisition/README.md))
- The routing rules that pick a downloader ([routing](../routing/README.md))
- Indexer / Prowlarr connections — those are separate ([indexers](../indexers/README.md))
- Import / hardlink mechanics ([importer](../importer/README.md))
- The "what to grab" decision ([quality profiles](../quality-profiles/README.md))
- Notification delivery (lives in [notifications](../notifications/README.md))
- The job-worker's concurrency model and tick cadence (acquisition's concern)

## Open questions

1. **Credentials at rest.** Plaintext in `downloader.password` today. Options: app-level encryption with a key in env / secrets file, OS-level via filesystem permissions, or a real secrets backend. Lean: app-level symmetric encryption with the key in env, mirroring how some other arrflix secrets are handled (cross-check the broader secrets-handling story before committing). Concrete decision in iteration 2.
2. **Password rotation flow.** Once wire-redacted (v1), is "leave field blank to preserve" enough, or do we want an explicit `POST /downloaders/{id}/password` for rotation auditability? Lean: blank-preserves for v1; add explicit endpoint if rotation becomes a real workflow.
3. **Category catalog + validation.** Should we model per-downloader categories in `config_json`, validate `AddRequest.Category` against it, and surface the catalog in routing-rule UI for pickability? Or stay loose and let the operator type whatever? Lean: validate against a typed list in `config_json`; sync-from-upstream is a separate "nice to have."
4. **Sync categories from upstream.** Some operators manage categories in qBit directly and would want arrflix to pull the current list. Worth a "sync categories now" button + periodic refresh? Defer until #3 is settled.
5. **Job-vs-downloader delete semantics.** Today `download_job.downloader_id` does NOT use `RESTRICT`, so a downloader can be deleted while it has active jobs. Those jobs will then fail next time the worker tries `GetClientByID`. Options: (a) `RESTRICT`, mirroring libraries; (b) cascade-cancel jobs on delete; (c) keep current behavior with a louder error. Lean: `RESTRICT`, with a server-side flag the admin sets to acknowledge cancellation if they really want to force-delete. — *Answered by the code: `download_job.downloader_id` is already `ON DELETE RESTRICT` (0008); only the force-delete flag is open.*
6. **Automatic failover.** When routing picks an unhealthy downloader, should the system fall back to another downloader of the same protocol (e.g., the default) instead of holding the job? Trade-off: predictability vs. resilience. Lean: opt-in via a `routing_rule.failover_to` field rather than an implicit system behavior. Decision lives with [routing](../routing/README.md); flagging here.
7. **Per-downloader concurrency.** Today the worker claims 20 jobs globally per tick. A "max in-flight per downloader" would help avoid saturating one client while another sits idle. Probably a downloader-level setting (`max_concurrent_jobs`) with the worker respecting it. Iteration 2.
8. **Type extensibility / `type` enum.** Same shape as the libraries type-enum issue: hardcoded enum, schema-level constraint. Adding a provider needs migration + code in lockstep. Worth graduating to a `downloader_type` registry table once we add a real second provider, or until then just rip the CHECK constraint and trust the registry.
9. **Removing `placeholder` types from the schema.** `sabnzbd` / `nzbget` are in the type CHECK constraint but have no provider. Either keep as "scaffolded, planned" or remove and re-add when the implementation lands. Lean: keep — the placeholder cost is nothing and removing-then-adding has a migration churn cost. — *Answered by the code: the CHECK admits only `qbittorrent`; the placeholders exist only as Go constants.*
10. **Tags as a first-class field.** Today `AddRequest.Tags` is passed through but the schema doesn't surface tag config anywhere. Worth modeling per-downloader tag policy (auto-tag with `arrflix`, `quality:4k`, etc.)? Lean: small win, low priority.
11. **`config_json` schema versioning.** Once `config_json` carries real keys (categories, etc.), we'll want a versioning story for migrations. Probably a `schema_version` field inside the JSON, validated by the provider builder. Iteration 2.

## What we're explicitly not deciding here

- The encryption scheme for credentials at rest
- Concrete schema for `config_json` per provider (lives with each provider implementation)
- The acquisition worker's concurrency, tick cadence, claim semantics
- Routing's failover-rule grammar (lives with [routing](../routing/README.md))
- A force-delete path past `download_job.downloader_id`'s `RESTRICT`
- UI redesign for the health badge and password-redaction edit flow
- The SABnzbd / NZBGet implementation details

## Doc neighbors

- [Routing](../routing/README.md) — picks the downloader a release is dispatched to
- [Acquisition](../acquisition/README.md) — owns the download-job state machine and the worker that drives downloaders
- [Libraries](../libraries/README.md) — sibling routing-action; same shape (CRUD + connectivity-health + default)
- [Connectivity-health pattern](../../patterns/connectivity-health/README.md) — owns the runtime-health shape downloaders implement
- [Users](../users/README.md) — `downloaders.*` permissions
- [Audit pattern](../../patterns/audit/README.md) — sibling pattern; test invocations and credential changes are admin-action audit events
- [Name templates](../name-templates/README.md) — sibling routing-action; picked alongside the downloader
- [Indexers](../indexers/README.md) — different role (release discovery), but similar shape (typed external connections, also a connectivity-health producer)
