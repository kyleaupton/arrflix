# Importer — making a completed download real in the library

**Status:** Draft, iteration 1

The importer is the **back half** of the [acquisition pipeline](../acquisition/README.md#the-pipeline-today-vs-new): it takes a download the downloader reports as `completed` and turns it into placed, indexed `file` rows in a [library](../libraries/README.md). It decides nothing about _what_ to grab or _where_ it goes — [quality profiles](../quality-profiles/README.md) picked the release and [routing](../routing/README.md) already stamped the destination (downloader, library, name template) onto the `download_job`. The importer is the **executor** of those decisions: resolve the files on disk, assign them to the wants they fulfil, hardlink them into place under the rendered [name template](../name-templates/README.md), and write the rows.

This spec is **primarily descriptive** — the import worker exists and runs in v0 — and locks in a focused set of v1 additions: [path-mapping](../path-mapping/README.md) integration (the boundary the v0 code stubs out), want linkage so an import advances the [want lifecycle](../acquisition/README.md#want-status-the-two-axes), and the season-pack assignment the M:N model needs. The filesystem mechanics (hardlink-first, copy-fallback) stay as-is.

## TL;DR

- The importer runs **after** a `download_job` reaches `completed`. It spawns one **`import_task` per file**, then a worker drains tasks: resolve source → assign to a want → **ffprobe + re-gate on asserted attributes** → render destination → hardlink/copy → write rows → advance the want to `imported`. A re-gate **hard-fail** short-circuits to reject + blocklist instead.
- An `import_task` is the unit of work and the audit trail for one file: `{ source, want, library, name_template, dest, method, file }` plus a retry/lifecycle envelope. It carries the routing decision copied off the `download_job`.
- **The importer executes; it does not decide.** Library, name template, and downloader are routing's output. The importer reads them off the job.
- **The re-gate is a decision the importer _consumes_, not makes.** After `ffprobe`, before the hardlink, it asks [quality profiles](../quality-profiles/README.md#import-time-re-gate) "does the asserted file still pass the profile it was grabbed under?" — the same boundary it keeps with routing and path-mapping. Pass/soft-fail → place (soft-fail logs a `quality/advertised-mismatch` finding); hard-fail → fail the task `quality_rejected`, and [acquisition](../acquisition/README.md#import-time-re-gate-asserted-verification) blocklists + re-searches.
- **File assignment is closed-world.** The job already names the wants it fulfils; the importer only maps the torrent's files _onto_ those known targets (which file is S03E05). This is distinct from [matching](../matching/README.md)'s open-world identity resolution — they share the filename parser, nothing else.
- **Path resolution is a [path-mapping](../path-mapping/README.md#the-four-boundaries) boundary-1 call** (`translate(downloader, arrflix, content_path)`), and the hardlink-vs-copy choice reads path-mapping's [device verdict](../path-mapping/README.md#hardlink-feasibility-the-first-class-output). v0 stubs both; v1 wires them.
- **Hardlink-first, copy-fallback.** `os.Link` when the source and library share a device; a byte copy (temp file + atomic rename) otherwise. The method is recorded per file.
- The importer owns the want transition `downloading → imported` and emits `want.imported`. It does **not** own `available` — a separate [verify-on-disk step](../acquisition/README.md#available-means-verified-on-disk-not-seen-by-plex) carries `imported → available`, and the [media-server](../media-server/README.md) nudge is downstream of that. The importer's job ends when the file is placed and the row is written.
- Failures are [typed](../../patterns/errors/README.md): retryable (transient FS / downloader) back off and retry; non-retryable (missing source, unresolvable path, render failure) fail the task loudly. A frozen source path self-heals by re-deriving from the downloader.

## Where it sits

```
download_job: created → enqueued → downloading → completed     ← downloader's job (unchanged)
                                                     │
                                                     ▼  spawn import_task per file
import_task:  pending → in_progress → completed                ← this spec
                                          │
                                          ├─ write file (+ state, + import record)
                                          └─ want: downloading → imported, emit want.imported
                                                     │
                                                     ▼
                              VerifyStep: imported → available  ← acquisition (boundary, not here)
                                                     ▼
                              MediaServerSync: nudge + propagate ← media-server (downstream, not here)
```

The importer is bounded above by routing's decision (already on the job) and below by the `imported` transition. Everything past `imported` — verify, propagation, notification — is someone else's.

## The trigger — spawning tasks

The [download job worker](../acquisition/README.md#what-stays-the-same) polls the downloader; when a job transitions to `completed`, it spawns the import tasks for that job. Spawning is where the file set is read and assigned:

1. **List the job's files** via the downloader's `ListFiles` ([downloaders](../downloaders/README.md)). Skip samples and non-video files.
2. **Assign files to wants** (below). A movie job has one want and picks one file; a series job has N wants (it may be a season pack) and maps each in-scope file to its want.
3. **Create one `import_task` per assigned file**, copying the routing decision (`library_id`, `name_template_id`) and the resolved `want_id` off the job, and recording the source path.

A job that yields no assignable file (matched nothing, all samples) fails as non-retryable rather than spawning empty work.

## File assignment — closed-world

The `download_job` already carries the identity: `media_item_id`, and for series the season/episode of each linked want. The importer is **not** discovering what a file is — it is distributing a known set of files across a known set of targets. Two shapes:

- **Movie** — pick the main file: the largest video file, samples excluded. One file, one want.
- **Series** — parse each filename's episode marker (`S03E05`, `3x05`, multi-episode ranges like `S03E05-E06`) and assign it to the matching want. The job's linked wants are the target set; a file whose marker matches no open want is overflow.

The parsing primitive (filename → season/episode markers) is the _same_ primitive [matching](../matching/README.md) needs for its open-world identity work. It is a pure function — it touches no DB, no metadata provider — which makes it the cheapest and most valuable unit-test surface in the flow, and the natural thing to share. The importer owns the **assignment** (closed-world, deterministic, target set known); matching owns **identity** (open-world, TMDB-validated, confidence-banded) on the [scan](../scan/README.md)/drop-in path. They share the parser — the unified [parsing](../parsing/README.md) engine (path flavor) — and diverge immediately after. v0 keeps its own copy in the importer package; consolidating onto `internal/parsing` is intended, not blocking — see [open questions](#open-questions).

### Season packs and overflow

A single `download_job` fulfils **many wants** (M:N — see [acquisition § Season packs](../acquisition/README.md#season-packs)); each `import_task`, and so each imported file, fulfils **exactly one** want (`import_task.want_id` is a single FK). Assignment is the join between the two:

- **Covered** — file marker matches an open linked want → one task, want set.
- **Overflow** — file matches no open want (pack carries E11; we only wanted E01–E10) → left as an unidentified `file` (`media_item_id` NULL) for [scan](../scan/README.md)/matching to reconcile, or surfaced as extras (final disposition is acquisition's [overflow question](../acquisition/README.md#overflow--under-coverage), not the importer's).
- **Under-coverage** — a want with no covering file in the pack stays `searching`; the importer simply produces no task for it. The scheduler keeps looking.

v0's assignment helper takes a single target episode; v1 widens it to a **target set** so one season-pack job spawns the right per-episode tasks in one pass.

## Path resolution — the boundary-1 call

The downloader reports a path in _its_ frame (`/downloads/torrents/X/X.mkv`). Before the importer can touch the file it resolves that to Arrflix's canonical frame via [path-mapping boundary 1](../path-mapping/README.md#the-four-boundaries):

```
translate(downloader, arrflix, content_path) → /data/torrents/X/X.mkv
```

v0 has a `pathMapper.Apply()` **stub that returns the path unchanged** — correct only when the downloader and Arrflix share a mount (the single-volume common case). v1 routes through the real resolver, so split-mount setups work and an **unresolvable path** becomes a typed [hold-fix-retry error](../path-mapping/README.md#edge-cases) instead of a confusing "file not found." The audit record notes the translation applied (`qbit:/downloads/X → /data/torrents/X via volume "media"`), turning the hardest-to-debug *arr problem into a glance.

**The hardlink verdict** comes from the same layer: `device(source) == device(library_root)` ([path-mapping](../path-mapping/README.md#hardlink-feasibility-the-first-class-output)). Equal device → hardlink; unequal → the import still succeeds via copy, just slower and at 2× space. The importer reads this verdict; it does not compute the device check itself.

### Self-healing the source path

An `import_task` records its source path at spawn time. If that path is stale by the time the worker runs it (a container remount, a path change), the worker **re-derives** it by re-querying the downloader for the job's files rather than failing outright. The frozen-string source path is a v0 quirk — the twin of [libraries OQ#4](../libraries/README.md#open-questions) (`import_task.library_root_path` as a string). v1 resolves both through the volume layer so a task references stable identity, not a captured string; the migration is coordinated with path-mapping ([OQ#9](../path-mapping/README.md#open-questions)).

## The asserted re-gate

Before rendering and placement, the importer extracts the file's asserted attributes with `ffprobe` and asks [quality profiles](../quality-profiles/README.md#import-time-re-gate) to re-run the profile the release was grabbed under — against the real file this time. **The importer consumes the verdict; it does not compute it** — the same boundary it keeps with [routing](../routing/README.md) (destination) and [path-mapping](../path-mapping/README.md) (device verdict).

- **Pass / soft-fail** → proceed to render + placement. On a soft-fail (over-advertised but acceptable), the importer records the score penalty and writes a [`quality/advertised-mismatch`](../hygiene/README.md) finding — the importer is the one place that holds **both** the advertised parse (off the job) and the fresh `ffprobe`, so it is where the delta is computed.
- **Hard-fail** → the task fails with a typed `quality_rejected` ([errors](../../patterns/errors/README.md)), non-retryable; no file is placed. [Acquisition](../acquisition/README.md#import-time-re-gate-asserted-verification) reacts by blocklisting the release and returning the want to `searching`. Rejecting **before** the hardlink means there's no placed file to clean up.

`ffprobe` runs once here regardless — the rendered name template already needs `MediaInfo` — so the re-gate is essentially free on top.

## Rendering the destination

The destination path is the chosen [name template](../name-templates/README.md) rendered against an evaluation context the importer assembles:

- **Media** — type, title, year, TMDB id, and for series the season/episode of the assigned want.
- **Release** — title/group/edition parsed from the job's release name.
- **MediaInfo** — codec, resolution, HDR, audio, container, extracted by `ffprobe` on the source file. Import is the **one place** this exists: it's post-download by definition, so routing (pre-download) never sees it, and a name template that references `{mediainfo.*}` resolves only here.
- **Quality** — the advertised parse (`Full`, `Resolution`, `Source`, …), **not** reconciled with `ffprobe`. For a grabbed file the [re-gate](#the-asserted-re-gate) already hard-failed any resolution mismatch, so the advertised bin matches reality; `Source` is advertised-only. Rendered from the file's [persisted parse](../parsing/README.md#persisted-parse).

For series the importer renders show-folder → season-folder → file and joins them; for movies, optional movie-folder → file. The full destination is `library.root_path` joined with the rendered relative path. A render failure (typo, missing required field) fails the task — currently non-retryable, with no fallback template; whether v1 should ship a hardcoded fallback is an [open question](#open-questions).

## The filesystem operation

`HardlinkOrCopy`: attempt `os.Link(source, dest)` first; on failure (cross-device, or the verdict already says different filesystems) fall back to a byte-for-byte copy into a temp file followed by an atomic rename. The chosen method (`hardlink` | `copy`) is recorded on the task and the import record. That recorded method is half the [hardlinks](../hardlinks/README.md) broken-link predicate — a `hardlink`-method file whose live `nlink` later drops to `1` is _provably_ broken — and a cheap post-link `nlink ≥ 2` assertion here guards against an FS reporting a link it didn't actually make.

Collision handling: if the destination already exists and this is a **reimport** (the task chains off a `previous_task_id`), the old file is removed first; otherwise the collision is a [`Conflict`](../../patterns/errors/README.md) and the task fails rather than clobbering.

## Persistence — one transaction

On a successful placement the importer writes, in a single transaction:

- **`file`** — `{ library_id, media_item_id, episode_id?, path }`. The relative `path` within the library. The entity is owned by [files](../files/README.md); the importer is its writer on the grab path (identity already known — closed-world). It carries **no** media-server rating key — propagation lives [elsewhere](../media-server/README.md#the-propagation-record).
- **`file_state`** — `exists = true`, `size_bytes`. The presence/size facts [scan](../scan/README.md)'s verify mode reconciles against.
- **`file_import`** — the per-attempt record: method, source, dest, success. This is the importer's slice of the [audit](../../patterns/audit/README.md) story — the durable "how did this file get here" trail, including the path translation applied.
- **The [persisted parse](../parsing/README.md#persisted-parse)** — the raw release title + parsed `Quality`/`Release` + `parser_version` + `origin: grabbed`, stored in the `file_parse` companion so a template can be re-rendered (mass-rename) after the `download_job` is purged.
- **`import_task`** completion — final status, `dest`, `method`, `file_id`.

Then, outside-but-paired-with the placement: the linked **want transitions `downloading → imported`** and the importer emits `want.imported`. This want coupling is a **v1 addition** — v0 writes the `file` row but never touches want state (the want lifecycle didn't exist yet). A transaction that fails _after_ a successful hardlink leaves an orphaned link on disk; cleanup of that orphan is an [open question](#open-questions) (today it persists until a scan reconciles it).

## The `import_task` lifecycle

```
pending ──► in_progress ──► completed
                │
                ├──► failed      (retries exhausted, or non-retryable)
                └──► cancelled   (parent job cancelled)
```

The worker claims a bounded batch of runnable `pending` tasks per tick, marks them `in_progress`, and processes each independently — failure or retry is **per task**, not per job, so one bad file in a season pack doesn't sink the other nine.

### Failure & retry

Retryability derives from the [error kind](../../patterns/errors/README.md):

- **Retryable** — transient filesystem errors, a downloader briefly unreachable during source re-derivation (`BadGateway`, `Internal`). The task backs off (exponential, capped attempts) via `next_run_at` and is reclaimed.
- **Non-retryable** — source missing or is a directory, path unresolvable, destination collision (non-reimport), name-template render failure. These fail fast; retrying can't help.

`last_error` + `error_kind` persist on the task for the activity surface.

### Reimport

A reimport creates a **new** task pointing at the old via `previous_task_id`, re-runs the pipeline, and (because it's flagged a reimport) overwrites the existing destination. The chain is walkable for history. This is the mechanism behind an [upgrade](../tracking/README.md) replacing a file in place.

## Manual & orphan imports

`import_task.download_job_id` is nullable: an import need not originate from an Arrflix-managed download (admin-added torrent, manual file drop). With no routing decision to copy, the importer falls back to the [per-type default library](../libraries/README.md#per-type-default) and the default name template. Note the boundary: a file of **unknown identity** dropped into a library is [scan](../scan/README.md) + [matching](../matching/README.md)'s problem (open-world), not the importer's — the importer handles imports where the target is already known.

## Events

| Event             | When                                          | Consumers                                                     |
| ----------------- | --------------------------------------------- | ------------------------------------------------------------- |
| `import_task_updated` | Any task status change                    | [Realtime](../realtime/README.md) SSE (progress UI)           |
| `download_job_updated` | Task change rolls up to the parent job's summary | [Realtime](../realtime/README.md) SSE                  |
| `want.imported`   | A file is placed and its want advances        | VerifyStep (presence-verify → `available`) per [acquisition](../acquisition/README.md#event-bus--messaging) |

The importer does **not** emit `want.available` (the verify step does) or `media_file.propagated` ([media-server](../media-server/README.md) does).

## Integration points

| Neighbor | How it interacts |
| -------- | ---------------- |
| [Acquisition](../acquisition/README.md) | Owns the want lifecycle and the M:N `download_job ↔ want` linkage; the importer is its back half, advancing wants to `imported`. VerifyStep (acquisition) carries `imported → available`. |
| [Routing](../routing/README.md) | Decides downloader + library + name template; the importer reads those off the `download_job` and executes them. Never re-decides. |
| [Path-mapping](../path-mapping/README.md) | Boundary-1 `translate` resolves the downloader path to canonical; the device verdict drives hardlink-vs-copy. The importer is a consumer; the mechanics are the pattern's. |
| [Name-templates](../name-templates/README.md) | Rendered here against the media + release + mediainfo context to compute the destination path. |
| [Quality profiles](../quality-profiles/README.md) | The importer runs the import-time re-gate before placement — supplies asserted `ffprobe` attributes, acts on pass / penalize / hard-fail, doesn't own the logic. |
| [Parsing](../parsing/README.md) | The closed-world filename-marker parse is the path flavor of the unified parser; the importer calls `internal/parsing` instead of keeping a private copy. |
| [Files](../files/README.md) | The importer writes `file` / `file_state` / `file_import` rows on the grab path (identity known); the entity model is owned by files. |
| [Libraries](../libraries/README.md) | Destination root; per-type default for orphan imports. The importer writes `file.library_id`. |
| [Matching](../matching/README.md) | Owns open-world identity (scan/drop-in); the importer owns closed-world assignment (grab path). Both consume the same [parser](../parsing/README.md). Overflow files hand off to matching as unidentified `file` rows. |
| [Scan](../scan/README.md) | Reconciles what the importer writes (`file_state` presence/size); cleans up orphaned links. Shares the file-on-disk truth. |
| [Downloaders](../downloaders/README.md) | `ListFiles` at spawn; re-queried during source self-heal. |
| [Realtime](../realtime/README.md) | `import_task_updated` / `download_job_updated` SSE for progress. |
| [Errors](../../patterns/errors/README.md) | Typed kinds drive retryable-vs-not; unresolvable path is a hold-fix-retry case. |
| [Audit](../../patterns/audit/README.md) | `file_import` is the per-file import record, including the path translation applied. |

## What the importer does NOT own

- **The decision** of where a file goes or what template renders it — [routing](../routing/README.md).
- **The want lifecycle** as a whole — [acquisition](../acquisition/README.md). The importer performs exactly one transition (`downloading → imported`) and emits one event.
- **`available`** — the [verify-on-disk step](../acquisition/README.md#available-means-verified-on-disk-not-seen-by-plex). The importer stops at `imported`.
- **Media-server propagation** — the nudge, correlation, rating key, deep links — [media-server](../media-server/README.md).
- **Path-translation and device mechanics** — [path-mapping](../path-mapping/README.md). The importer consumes the resolved path and the verdict.
- **Open-world identity** — what an unknown file is — [matching](../matching/README.md).
- **Drift reconciliation / orphan sweeps** — [scan](../scan/README.md). The importer writes the truth at placement; scan keeps it honest over time.

## Open questions

1. **Orphaned hardlink on transaction failure.** If the link succeeds but the DB write rolls back, a link is left on disk. Options: a compensating unlink in the failure path, or leave it for [scan](../scan/README.md) to reconcile (today's de-facto behavior). Lean: compensating unlink for the common case + scan as the backstop; pin in iteration 2.
2. **Frozen source path → volume-resolved.** v1 resolves the v0 string-path quirk through the volume layer, coordinated with [libraries OQ#4](../libraries/README.md#open-questions) and [path-mapping OQ#9](../path-mapping/README.md#open-questions). Confirm they land together rather than as two migrations.
3. **Name-template render failure.** Non-retryable and fatal today. Should v1 ship a hardcoded fallback template (so a typo degrades to a sane path instead of a stuck file), or keep it loud? Lean: loud + a clear activity error — a silent fallback hides a misconfiguration.
4. **Overflow disposition.** Files a season pack carries beyond the wanted set: left unidentified (reconcile via matching) vs first-class "extras." Owned by [acquisition](../acquisition/README.md#overflow--under-coverage); the importer just needs the disposition pinned to know where to route them.
5. **Shared parser extraction.** The filename-marker parse is the path flavor of the unified [parser](../parsing/README.md); the importer calls `internal/parsing` rather than keeping its v0 copy. Lands when parsing is built; until then the importer's copy is the reference. Not a blocker.
6. **Verify step ownership.** The `imported → available` presence-verify is listed in [acquisition](../acquisition/README.md#event-bus--messaging) as a distinct step. Confirm it lives at the acquisition boundary (a thin worker reacting to `want.imported`) rather than being folded into the import worker — keeps the importer's responsibility bounded at `imported`.
7. **Concurrency.** The worker claims a bounded batch per tick. Right knob for v1 (fixed batch size vs a worker pool sized to FS throughput)? Lean: keep the simple bounded batch; revisit only if large libraries show import as a bottleneck.
8. **Re-gate hard-fail scope in a season pack.** One file hard-failing the re-gate fails its own task (per-task, as today) while siblings proceed — but is the **blocklist** scoped to the single file or the whole release? Lean: the release (the pack is the grabbable unit, so re-grabbing should avoid the whole pack), with the surviving siblings still imported. Pin with [acquisition](../acquisition/README.md#import-time-re-gate-asserted-verification).

## What we're explicitly not deciding here

- Exact `import_task` / `file_import` column types and indexes.
- The volume-resolution migration plan for the source-path quirk (coordinated, tracked separately).
- The filename-parser package boundary and signature (lands with matching).
- VerifyStep internals and the `available` transition mechanics ([acquisition](../acquisition/README.md)).
- API/UI surfaces for the import queue, the per-file activity view, and manual reimport (iteration 2).
- Hardlink/copy implementation details beyond "link-first, atomic-rename copy fallback."
- Worker batch size / back-off curve tuning.

## Doc neighbors

- [Acquisition](../acquisition/README.md) — owns the pipeline and the want lifecycle; the importer is its back half.
- [Routing](../routing/README.md) — decides the destination the importer executes.
- [Path-mapping](../path-mapping/README.md) — boundary-1 resolution + the hardlink verdict.
- [Hardlinks](../hardlinks/README.md) — the recorded import method feeds its broken-link predicate; optional post-link `nlink` assertion.
- [Name-templates](../name-templates/README.md) — rendered here to compute the destination path.
- [Files](../files/README.md) — the `file` / `file_state` / `file_import` rows the importer writes on the grab path.
- [Libraries](../libraries/README.md) — destination roots, per-type default, the `file` index.
- [Matching](../matching/README.md) — open-world identity; shares the filename parser, owns the drop-in path.
- [Scan](../scan/README.md) — reconciles `file` truth and orphan sweeps.
- [Media-server](../media-server/README.md) — downstream of `available`; propagation, never the importer's concern.
- [Errors](../../patterns/errors/README.md) — typed kinds drive retry behavior.
- [Audit](../../patterns/audit/README.md) — `file_import` as the per-file record.
- [Downloaders](../downloaders/README.md) — file listing at spawn and source self-heal.
