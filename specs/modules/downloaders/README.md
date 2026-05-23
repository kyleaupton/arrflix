# Downloaders — the clients that pull bits down

**Status:** Draft, iteration 1

A downloader is a configured connection to a third-party client (qBittorrent today; SABnzbd / NZBGet planned) that Arrflix hands releases off to for actual transfer. It is one of the three things [routing](../routing/README.md) dispatches to (alongside libraries and name templates) and the runtime engine the [download-job worker](../acquisition/README.md) polls.

The model has a well-designed **provider interface** with qBittorrent as the sole concrete implementation today. This spec captures what already exists, locks in v1 additions (background health, credential redaction on the wire), and surfaces the still-open questions about categories, encryption, and failover.

## TL;DR

- A downloader is a named connection: `(name, type, protocol, url, credentials, config_json)` plus standard `enabled` / `default` flags.
- **Two-axis classification**: `type` is the provider (`qbittorrent`, future: `sabnzbd`, `nzbget`); `protocol` is the wire (`torrent` or `usenet`). Each downloader picks one type and one protocol.
- One default per **protocol** (not per type) — a torrent default and a usenet default, picked when routing doesn't specify.
- All providers conform to a single **`Client` interface**: `Test`, `Add`, `Get`, `List`, `ListFiles`, `Pause`, `Resume`, `Remove`. Providers register via a builder registry; the `Manager` instantiates and caches one client per downloader row.
- v0 has **provider extensibility built-in** but only qBittorrent shipped. Adding a provider is: implement the interface, register a builder, declare type-specific `config_json` shape.
- v1 adds runtime health via the [connectivity-health pattern](../../patterns/connectivity-health/README.md), **credential redaction on the wire**, and a path toward **encrypted-at-rest credentials** (open question).
- The download-job state machine lives **with the acquisition pipeline**, not here. This spec covers downloader configuration, provider abstraction, and runtime health — not job orchestration.

## What a downloader owns

A downloader row encodes:

- **Name** — display name, unique (case-insensitive).
- **Type** — provider tag, constrained to the registered set (`qbittorrent`; placeholders `sabnzbd` and `nzbget` exist in the schema enum but have no implementation today).
- **Protocol** — `torrent` or `usenet`. Constrains which releases this downloader can accept; routing uses this for natural eligibility.
- **URL** — the client's base URL.
- **Username / password** — plaintext on the wire today, plaintext at rest today. See [Credentials](#credentials) and [open questions](#open-questions).
- **`config_json`** — JSONB free-form payload for provider-specific config. Empty `{}` by default; reserved for things like default category, default save-path mapping, port-mapping hints, etc.
- **Enabled flag** — when false, the downloader is excluded from routing dispatch and the manager's init pass. Existing jobs targeting a disabled downloader still complete (and the manager still loads its client to honor those jobs).
- **Default flag** — at most one default per protocol. Used when routing's action set has no explicit downloader.
- **Created / updated timestamps**.

The runtime view adds:

- **`Initialized`** — whether the manager successfully connected at startup or after a save. Not persisted; derived from the in-memory client cache.

## The provider model

Providers conform to a single interface (`internal/downloader/downloader.go`):

```go
type Client interface {
    Test(ctx) error
    Add(ctx, AddRequest) (AddResult, error)
    Get(ctx, externalID) (Item, error)
    List(ctx) ([]Item, error)
    ListFiles(ctx, externalID) ([]File, error)
    Pause(ctx, externalID) error
    Resume(ctx, externalID) error
    Remove(ctx, externalID, deleteFiles bool) error
}
```

Two registries make this extensible:

- **Type registry** — maps a type string (`qbittorrent`) to a builder function `func(Downloader) (Client, error)`. Adding a provider means writing the implementation and registering a builder; no central switch statements.
- **Manager** — loads enabled downloaders on startup, dispatches to the builder registry per row, caches the resulting `Client` keyed by downloader ID. Jobs at execution time fetch their client via `manager.GetClientByID(ctx, id)`.

This is the right shape and stays untouched in v1. The work to add SABnzbd or NZBGet is bounded: write a wrapper, register it, declare the type-specific `config_json` keys it reads.

### `AddRequest` shape

The interface carries enough fields to express the union of provider expectations:

- `MagnetURL` / `NZBURL` — protocol-appropriate payload
- `Category` — provider-side organizational tag (qBit category, SAB category)
- `Tags` — provider-side tags (currently torrent-only via qBit)
- `Paused` — start paused vs immediately downloading
- `SavePath` — override the provider's default save-path (used to land files in a location the import worker knows to scan)

`Add` returns `AddResult{ExternalID, ...}` — the provider's identifier (torrent infohash, NZB queue ID), which the download-job persists in `downloader_external_id` for subsequent polling.

### `Item` shape (returned by `Get` / `List`)

- `Status` — provider-native status string (qBit `downloading`/`stalledDL`/`completed`/etc.). Stored as-is in `download_job.downloader_status` for UI display.
- `Progress` — 0.0 → 1.0
- `SavePath` / `ContentPath` — where the provider claims the bits live
- `DownloadSpeed`, `ETASeconds`, `TotalSize` — for the live stats column

The download-job worker maps provider-native status into the job's own state machine (created → enqueued → downloading → completed / failed / cancelled). That mapping is the worker's concern, not the provider's. Providers are kept dumb on purpose.

## Credentials

Today:

- Username and password are **stored plaintext** in the `downloader` table.
- Password is **returned plaintext on the wire** in `GET /downloaders/{id}`, because the frontend re-displays the value in the edit form.
- The qBit client logs in once per manager session, caches the session cookie (SID), and uses the cookie for subsequent calls. Credentials are not re-sent per request, but they sit in memory on the `Client` struct for re-login.

This is a known limitation, called out in `model/downloader.go`. V1 should fix at least the wire side; at-rest encryption is a bigger conversation (see [open questions](#open-questions)).

**V1 wire-side fix** (concrete, low-risk):

- `GET /downloaders` and `GET /downloaders/{id}` redact `password` to an empty string or a sentinel.
- `PUT /downloaders/{id}` treats an empty / sentinel password as "preserve existing"; only an explicit non-empty value overwrites. This mirrors the existing server-side preservation logic (`service/downloaders.go`) and only requires the FE to surrender the "show the existing password in the form" pattern.
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

- **created** — row exists; the handler best-effort calls `InitializeDownloader` after a successful save. Failure logs but does not fail the API response.
- **initialized** — the manager has a live, authenticated `Client` cached for this row. Routing can dispatch here; the worker can hand it jobs.
- **enabled / disabled** — controls routing eligibility and inclusion in the manager's init pass. Disabling does not abort in-flight jobs; the manager retains the client for ongoing work.
- **deleted** — row removed. Note: `download_job.downloader_id` does **not** use RESTRICT today, so deleting a downloader with active jobs is currently possible (this is a quirk — see [open questions #5](#open-questions)).

## Operations

CRUD plus two test verbs. All routes JWT-gated; permission scoping lives in [users](../users/README.md).

| OperationID                    | Method | Path                                          | Notes                                                                            |
| ------------------------------ | ------ | --------------------------------------------- | -------------------------------------------------------------------------------- |
| `downloaders-list`             | GET    | `/api/v1/downloaders`                         | All downloaders, with `initialized` runtime flag.                                |
| `downloaders-get`              | GET    | `/api/v1/downloaders/{id}`                    | Password redacted in v1 (see [Credentials](#credentials)).                       |
| `downloaders-get-default`      | GET    | `/api/v1/downloaders/default/{protocol}`      | Per-protocol default lookup.                                                     |
| `downloaders-create`           | POST   | `/api/v1/downloaders`                         | Validates + initializes (best-effort).                                           |
| `downloaders-update`           | PUT    | `/api/v1/downloaders/{id}`                    | Empty password = preserve existing (v1).                                         |
| `downloaders-delete`           | DELETE | `/api/v1/downloaders/{id}`                    | See deletion quirk in [open questions](#open-questions).                          |
| `downloaders-test`             | POST   | `/api/v1/downloaders/{id}/test`               | Tests an existing downloader's connection. Calls `Client.Test()`.                |
| `downloaders-test-config`      | POST   | `/api/v1/downloaders/test`                    | Tests a candidate config pre-save. Same probe, no persistence.                   |

Permissions to keep in mind (defined in [users](../users/README.md)):

- `downloaders.read` — list / get / get-default
- `downloaders.write` — create / update / delete
- `downloaders.test` — both test endpoints (separable: operators may want to delegate connection-testing without granting CRUD)

## Validation

At create / update time the service validates:

1. **Name** non-empty, unique case-insensitive.
2. **Type** in the registered provider set (`qbittorrent` today).
3. **Protocol** in (`torrent`, `usenet`).
4. **URL** non-empty and parseable.
5. **Default flag** — atomically clears `default` on any other downloader of the same protocol.

Validation does **not** include a live connection check at save time — that's the explicit `test` endpoint. Save-then-test is intentional: a user may legitimately add a downloader whose target is currently offline.

## Runtime health

Downloaders are a producer of the [connectivity-health pattern](../../patterns/connectivity-health/README.md). The pattern owns the worker shape, status persistence, transition emission, hysteresis, and cadence. This section covers only the downloader-specific contract.

**Today**, health is checked at three points only:

1. **Manager init** at process startup.
2. **Best-effort init** after a successful create.
3. **Explicit `POST /test`** when the operator clicks the button.

Between those, the system has no idea whether a downloader is reachable. A qBit instance whose container is down silently breaks every dispatched job until someone notices the pile-up. The runtime-health worker closes that gap.

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

In-flight jobs are **not aborted** on a health transition. A downloader that flips to `unreachable` mid-job lets the existing worker retry-loop catch the failure naturally. `auth_failed` is the loud case because retrying alone can't recover — the admin-action audit row + notification are the operator's signal to intervene.

## Categories and tags

qBit (and SAB) support **categories** (a primary organizational tag) and qBit additionally supports **tags** (free-form labels). Today:

- `AddRequest.Category` and `AddRequest.Tags` are passed at job-add time.
- There is **no validation** that the category exists on the downloader; the request is accepted, qBit may or may not surface the unknown category.
- There is **no configured catalog** of categories per downloader. `config_json` is empty by default; no schema reads from it.

For v1 this is a known gap. The likely shape (deferred to iteration 2):

- `config_json` grows a `categories` array (typed per provider) — informational only, not synced with the upstream.
- Routing actions can carry an optional category override; today the category typically defaults from the policy/routing rule context.

Open question on whether we should ever *sync* the downloader's actual categories back into `config_json` — pulled into [open questions](#open-questions).

## Multi-downloader semantics

The system is already multi-downloader-aware. Documented for completeness:

- **Manager caches one client per row** — independent sessions, independent credentials.
- **Per-protocol default** — torrent and usenet defaults are separate; routing's fallback picks the appropriate one for the release's protocol.
- **No global default** — there's no "the downloader" assumption anywhere in the code.
- **No automatic failover** — if a routing rule picks downloader A and A is unhealthy, the job waits. We do not currently re-route to downloader B. This is an [open question](#open-questions).
- **No per-downloader concurrency cap** — the download-job worker claims 20 jobs per tick *globally*, not per-downloader. If one downloader is saturated, the worker doesn't know to back off there specifically.

## Integration points

| Consumer                                          | How it uses downloaders                                                                                                              |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| **[Routing](../routing/README.md)**               | Rules reference downloaders by ID. The action set on a fired rule fills `download_job.downloader_id`. v1 health gates the dispatch.  |
| **[Acquisition](../acquisition/README.md)**       | The download-job worker fetches the right client per job and drives the state machine via `Add` / `Get` / `Remove`.                  |
| **Import (existing)**                             | Reads `download_job.content_path` (set from `Item.ContentPath`) to find files for hardlink import.                                   |
| **[Users](../users/README.md)**                   | `downloaders.*` permissions gate API access.                                                                                         |
| **[Audit pattern](../../patterns/audit/README.md)** | Health transitions, manual test invocations, and credential changes are audit-worthy admin actions.                                |
| **Frontend (`DownloaderSettings.vue`)**           | CRUD + test UI; v1 surface adds live health badge.                                                                                   |

## What downloaders does NOT own

- The **download-job state machine** ([acquisition](../acquisition/README.md))
- The routing rules that pick a downloader ([routing](../routing/README.md))
- Indexer / Prowlarr connections — those are separate (indexers spec, pending)
- Import / hardlink mechanics (import, lives near scan)
- The "what to grab" decision ([quality profiles](../quality-profiles/README.md))
- Notification delivery (future notifications spec)
- The job-worker's concurrency model and tick cadence (acquisition's concern)

## Open questions

1. **Credentials at rest.** Plaintext in `downloader.password` today. Options: app-level encryption with a key in env / secrets file, OS-level via filesystem permissions, or a real secrets backend. Lean: app-level symmetric encryption with the key in env, mirroring how some other arrflix secrets are handled (cross-check the broader secrets-handling story before committing). Concrete decision in iteration 2.
2. **Password rotation flow.** Once wire-redacted (v1), is "leave field blank to preserve" enough, or do we want an explicit `POST /downloaders/{id}/password` for rotation auditability? Lean: blank-preserves for v1; add explicit endpoint if rotation becomes a real workflow.
3. **Category catalog + validation.** Should we model per-downloader categories in `config_json`, validate `AddRequest.Category` against it, and surface the catalog in routing-rule UI for pickability? Or stay loose and let the operator type whatever? Lean: validate against a typed list in `config_json`; sync-from-upstream is a separate "nice to have."
4. **Sync categories from upstream.** Some operators manage categories in qBit directly and would want arrflix to pull the current list. Worth a "sync categories now" button + periodic refresh? Defer until #3 is settled.
5. **Job-vs-downloader delete semantics.** Today `download_job.downloader_id` does NOT use `RESTRICT`, so a downloader can be deleted while it has active jobs. Those jobs will then fail next time the worker tries `GetClientByID`. Options: (a) `RESTRICT`, mirroring libraries; (b) cascade-cancel jobs on delete; (c) keep current behavior with a louder error. Lean: `RESTRICT`, with a server-side flag the admin sets to acknowledge cancellation if they really want to force-delete.
6. **Automatic failover.** When routing picks an unhealthy downloader, should the system fall back to another downloader of the same protocol (e.g., the default) instead of holding the job? Trade-off: predictability vs. resilience. Lean: opt-in via a `routing_rule.failover_to` field rather than an implicit system behavior. Decision lives with [routing](../routing/README.md); flagging here.
7. **Per-downloader concurrency.** Today the worker claims 20 jobs globally per tick. A "max in-flight per downloader" would help avoid saturating one client while another sits idle. Probably a downloader-level setting (`max_concurrent_jobs`) with the worker respecting it. Iteration 2.
8. **Type extensibility / `type` enum.** Same shape as the libraries type-enum issue: hardcoded enum, schema-level constraint. Adding a provider needs migration + code in lockstep. Worth graduating to a `downloader_type` registry table once we add a real second provider, or until then just rip the CHECK constraint and trust the registry.
9. **Removing `placeholder` types from the schema.** `sabnzbd` / `nzbget` are in the type CHECK constraint but have no provider. Either keep as "scaffolded, planned" or remove and re-add when the implementation lands. Lean: keep — the placeholder cost is nothing and removing-then-adding has a migration churn cost.
10. **Tags as a first-class field.** Today `AddRequest.Tags` is passed through but the schema doesn't surface tag config anywhere. Worth modeling per-downloader tag policy (auto-tag with `arrflix`, `quality:4k`, etc.)? Lean: small win, low priority.
11. **`config_json` schema versioning.** Once `config_json` carries real keys (categories, etc.), we'll want a versioning story for migrations. Probably a `schema_version` field inside the JSON, validated by the provider builder. Iteration 2.

## What we're explicitly not deciding here

- The encryption scheme for credentials at rest
- Concrete schema for `config_json` per provider (lives with each provider implementation)
- The acquisition worker's concurrency, tick cadence, claim semantics
- Routing's failover-rule grammar (lives with [routing](../routing/README.md))
- Migration plan for adding `RESTRICT` to `download_job.downloader_id`
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
- Indexers (spec pending) — different role (release discovery), but similar shape (typed external connections, also a connectivity-health producer)
