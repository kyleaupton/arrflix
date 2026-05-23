# Libraries — the on-disk roots where media lives

**Status:** Draft, iteration 1

A library is a named, typed root path on disk where Arrflix places media. It is one of the three things [routing](../routing/README.md) dispatches to (alongside downloaders and name templates), and the unit the [scanner](../scan/README.md) walks.

The library model is one of the simplest and most stable in the system. This spec is **primarily descriptive** — it captures what already exists in v0 and locks in a small set of additions for v1 (runtime health checks, a few audit-worthy edges). Most of the surface area is left as-is.

## TL;DR

- A library is `(name, type, root_path)` plus a few flags (`enabled`, `default`). One row per library, one root path per row.
- Two media types today: `movie` and `series`. Each can have one library marked `default`, used when nothing else picks a destination.
- CRUD via the existing REST surface; scan is a per-library verb.
- v1 adds **runtime health checks**: a background poll confirms the root path is reachable and writable, surfaces a status field, and gates routing / scan / import on it.
- Multi-root paths and library grouping are deliberately deferred — see [open questions](#open-questions).

## What a library owns

A library row encodes:

- **Name** — display name, unique (case-insensitive).
- **Type** — `movie` or `series`. Drives default-library lookup and constrains what routing rules can target.
- **Root path** — absolute filesystem path. Single value, single root.
- **Enabled flag** — when false, library accepts no new imports and is hidden from routing-action pickers. Existing media files are unaffected.
- **Default flag** — at most one default per type. Used by callers that need a fallback destination ("import this movie somewhere"). Enforced by a partial unique index.
- **Created / updated timestamps** — bookkeeping.

That is the full surface. No tags, no quotas, no scheduling, no description — additions land via [open questions](#open-questions) if real demand emerges.

## Type enum

Media type is a string-typed enum (`'movie' | 'series'`) hardcoded in the schema and validated in the service. The single-source-of-truth lives in the migration; the service mirrors it.

This is **deliberately not extensible** in v1. Adding a new type (e.g., `music`, `book`) requires a migration + service change. The constraint protects routing, scan, import, and matching from polymorphism they're not ready for.

## Per-type default

`GetDefault(ctx, type)` returns the library marked `default` for that type. Used by:

- Routing's catch-all rule when no explicit `library` action is set.
- Import fallback when a download arrives with no associated routing decision (admin-added torrents, manual file drops — rare paths).
- The interactive grab flow when a user grabs without an explicit library pick.

There is no system-wide default — defaults are per-type. A fresh install with one movie library and one series library has both as defaults; adding a second of either type leaves the original as default until an admin moves the flag.

## Lifecycle

```
        ┌───────────┐                  ┌───────────┐
  ──►   │  created  │ ◄───── update ──►│  enabled  │
        └───────────┘   (incl. flags)  └─────┬─────┘
                                             │
                                             │ disable
                                             ▼
                                       ┌───────────┐
                                       │ disabled  │
                                       └─────┬─────┘
                                             │
                                             │ delete (RESTRICT
                                             │  if jobs reference)
                                             ▼
                                       ┌───────────┐
                                       │  deleted  │
                                       └───────────┘
```

**State semantics:**

- **enabled** — accepts imports, surfaces in routing UI, scannable.
- **disabled** — invisible to new imports and routing. Existing media files keep their library reference; scan can still be triggered manually for inventory but routing won't dispatch new work here.
- **deleted** — row removed. The schema uses `RESTRICT` on `download_job.library_id` and `import_task.library_id`, so a library cannot be deleted while in-flight work points to it. `media_file.library_id` and `unmatched_file.library_id` use `CASCADE` — deleting a library wipes its media-file index (the files on disk are untouched).

## Operations

CRUD and one verb. All routes are JWT-gated; permission scoping lives in [users](../users/README.md#permissions).

| OperationID         | Method | Path                              | Notes                                                                                          |
| ------------------- | ------ | --------------------------------- | ---------------------------------------------------------------------------------------------- |
| `libraries-list`    | GET    | `/api/v1/libraries`               | All libraries, regardless of enabled state.                                                    |
| `libraries-get`     | GET    | `/api/v1/libraries/{id}`          |                                                                                                |
| `libraries-create`  | POST   | `/api/v1/libraries`               | Validates type enum + filesystem reachability of `root_path`.                                  |
| `libraries-update`  | PUT    | `/api/v1/libraries/{id}`          | Same validations as create. Changing `root_path` is allowed but flagged in audit (see below).  |
| `libraries-delete`  | DELETE | `/api/v1/libraries/{id}`          | 409 if jobs reference; success cascades to `media_file` + `unmatched_file` rows.               |
| `libraries-scan`    | POST   | `/api/v1/libraries/{id}/scan`     | Kicks an async scan, returns `scanId`. Owned by [scan](../scan/README.md).                     |

Permissions to keep in mind (defined in [users](../users/README.md)):

- `libraries.read` — list / get
- `libraries.write` — create / update / delete
- `libraries.scan` — trigger scans (separable from write — operators may want to grant scan without giving up CRUD)

## Validation

At create / update time the service validates:

1. **Name** non-empty, unique case-insensitive.
2. **Type** in the allowed enum.
3. **Root path** non-empty, absolute, and reachable via `os.Stat` at the time of the call. Errors are logged in full server-side; the user sees a path-safe summary ("path not accessible: permission denied").
4. **Default flag** — setting `default=true` on a library implicitly clears `default` on any other library of the same type (enforced atomically via the partial unique index + an explicit clear in the service).

Validation does **not** cover ongoing health — see next section.

## Runtime health check

**New in v1.** Today, a library's root path is only checked at create / update. If the underlying drive goes offline, the path becomes unwritable, or permissions flip, the system silently breaks — scans fail with cryptic errors, imports stall, routing dispatches into a black hole.

V1 adds a background **library health worker**:

- Polls every enabled library at a configurable interval (default ~60s; not user-tunable per library).
- Per-library checks:
  - Path exists (`os.Stat` succeeds)
  - Path is a directory
  - Path is writable (probe-and-cleanup, not a mount-flag check — mount flags lie)
  - Optional: free-space threshold (warn below, fail below floor)
- Persists a **status field** on the library row: `healthy` / `unreachable` / `read_only` / `low_space` / `unknown` (transient on startup).
- Status is emitted on the SSE bus so the UI reflects it live.

**Health-gated behavior:**

- **Routing** treats `unreachable` and `read_only` libraries as ineligible — rules that match still fire, but the resulting `download_job` lands in a "blocked: library unhealthy" state and emits a notification.
- **Scan** can still be triggered manually against an unhealthy library (operators sometimes need to scan-after-recovery), but the worker won't auto-scan an unhealthy library.
- **Import** holds tasks pointed at unhealthy libraries in a `paused` substate, retrying on next health-poll success rather than failing outright.
- **`low_space`** is advisory-only by default — surfaced in the UI, used by routing as a tiebreaker if rules express `prefer_most_free`, but does not block. Hard floor is opt-in.

Health checks are **stateless** in the sense that a transient blip doesn't poison the library — the next successful poll restores `healthy`. Notifications fire on transitions, not on every failed poll.

The implementation lives in `internal/service/library_health` (or similar — exact placement is iteration-2). Schema cost is one column on `library` (`status`, plus a timestamp of last check); no new tables required for v1.

## Integration points

| Consumer                                        | How it uses libraries                                                                                                       |
| ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| **[Routing](../routing/README.md)**             | Rules reference libraries by ID. The action set on a fired rule fills `library_id` on the resulting `download_job`.          |
| **[Scan](../scan/README.md)**                   | Walks a library's `root_path`, attributing discovered files via `media_file.library_id` (matched) or `unmatched_file.library_id` (orphans). |
| **Import (existing)**                           | Hardlinks completed downloads into the library's root, applying the chosen [name template](#doc-neighbors). Writes `media_file.library_id`. |
| **[Hygiene](../hygiene/README.md)**             | Cleanup eligibility is evaluated within a library; cross-library moves are out of scope.                                    |
| **[Matching](../matching/README.md)**           | Scoped per-library when resolving ambiguous matches.                                                                        |
| **[Acquisition](../acquisition/README.md)**     | Reads health to decide whether to grab — a planned download whose target library is `unreachable` is held, not enqueued.    |
| **Frontend (`LibrarySettings.vue`)**            | CRUD + scan-trigger UI. v1 surface adds the live health badge per library.                                                  |

## What libraries does NOT own

- The routing rules that select a library ([routing](../routing/README.md))
- Scan execution and matching logic ([scan](../scan/README.md), [matching](../matching/README.md))
- Hardlink mechanics and name-template application (import)
- Storage-pressure heuristics for cleanup ([hygiene](../hygiene/README.md))
- Free-space *thresholds* used by routing tiebreakers — those live on routing rules; the library only reports the current free-space number.
- Anything about quality (tier, profile) — orthogonal axis.

## Open questions

1. **Multi-root vs grouping vs single-root.** A library is one path today. Two extensions have come up:
   - **Multi-root** — a single library spans multiple root paths, system picks one at import time (usually free-space-aware). Sonarr/Radarr solve "Movies drive 1 / Movies drive 2" this way.
   - **Grouping** — multiple libraries can be tagged into a logical group, used for queries/UI ("show me all my 4K libraries") but not for routing destination selection.

   The **real use case driving multi-root** is *the same logical library spanning multiple drives*: drive 1 fills up, drive 2 starts catching new files, but they all belong to "Movies" conceptually — Plex / users see one library, the filesystem is split. Grouping does not solve this on its own; you'd need a separate "spillover" rule in routing.

   Decision deferred. Lean is grouping first (cheaper, additive, no FK changes), revisit multi-root if the spanning-drives case shows up in real installs.

2. **Type extensibility.** `'movie' | 'series'` is hardcoded. When (not if) we add `music` or `book` or `audiobook`, the migration + service enum has to change in lockstep, and routing's type-matching conditions need a new value. Worth a single iteration-2 pass to decide whether the enum graduates to a real table (`media_type` registry) or stays inline.

3. **Path mutability after creation.** Changing `root_path` post-create is allowed today, but it silently breaks every running `import_task` that copied the path string at creation time (see #4). Options: forbid path changes once `media_file` rows exist; allow with an explicit cascade-rewrite of in-flight tasks; or migrate the import-task data model to use `library_id` instead of a raw path. Lean: third option, separately tracked.

4. **Import task carries `library_root_path` (string), not `library_id`.** This is a v0 quirk. An import task captures the path at creation time; if the library is renamed/repath'd, in-flight tasks still target the old location. Fixing this is a small data-model change. Track separately.

5. **`MEDIA_LIBRARIES` env var.** Documented in README + CLAUDE.md, referenced nowhere in code. Either wire it up (auto-create libraries from a colon-separated list on first boot) or remove from docs. Lean: remove — libraries are first-class DB rows, the env var is a relic.

6. **Free-space hard floor.** Should every library have a "stop accepting writes below N GB free" setting, or is that a routing-rule concern? Lean: routing-rule, because the right floor depends on what's being placed (don't accept a 4K remux when the floor is 50 GB, but accept a TV episode). Library exposes the current number; routing decides.

7. **Health-check interval.** Fixed default vs per-library override vs admin-configurable global? Lean: single global, configurable; per-library is overkill until a real case appears.

8. **Read-only libraries as a first-class concept.** Some operators want a "this library is archival, scan it but never import to it" library. Today, `enabled=false` covers that crudely (no imports, no scan-auto, but also invisible to routing). Worth a separate `read_only` flag? Lean: defer.

9. **Move-between-libraries.** "Move this movie from Movies-HD to Movies-4K" is a real operator action that today requires manual filesystem work + re-scan. Worth a first-class operation? Probably yes eventually, but it spans hygiene + import + scan and doesn't have a clear home. Defer.

10. **Per-library permissions.** `libraries.read:<id>` — can a user have CRUD access to one library but not another? Lean: not in v1; lift via `resource_id` on permission grants if real demand emerges (the [users](../users/README.md) spec already supports per-resource scoping in the grant table).

## What we're explicitly not deciding here

- Exact health-check probe implementation (test-file create/delete vs `unix.Access` vs something more clever)
- Schema for the `status` / `status_checked_at` columns on `library`
- Notification routing for health transitions (lives in the future notifications spec)
- The grouping data model, if/when we add it
- The multi-root data model, if/when we add it
- Migration plan for the `import_task.library_root_path` → `library_id` rework
- Admin UI redesign for the health badge

## Doc neighbors

- [Routing](../routing/README.md) — picks the library a release is dispatched to
- [Scan](../scan/README.md) — walks the library's root path
- [Acquisition](../acquisition/README.md) — gates planned grabs on library health
- [Matching](../matching/README.md) — operates within library scope
- [Hygiene](../hygiene/README.md) — cleanup eligibility scoped per library
- [Users](../users/README.md) — `libraries.*` permissions
- Name templates (existing, spec pending) — picked by routing, applied during import alongside the library destination
- Downloaders (spec pending) — sibling routing-action; same shape as libraries (named ID, referenced by rules)
