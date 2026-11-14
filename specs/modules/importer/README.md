---
status: v1
---

# Importer — making a completed download real in the library

> **Beyond what's built:** §The asserted re-gate is unbuilt (`ffprobe` runs today only to feed the name template), the path-mapping resolver and device verdict in §Path resolution are stubbed, and §Manual & orphan imports has no code path. V1's path slice is defined in [V1.md](../../V1.md#path-mapping); V1 also adds inode capture on import ([V1.md](../../V1.md#hardlink-inode-capture--hardlinks)).

The importer is the **back half** of the [acquisition pipeline](../acquisition/README.md#the-pipeline): it takes a download the downloader reports as `completed` and turns it into placed, indexed `file` rows in a [library](../libraries/README.md). It decides nothing about _what_ to grab or _where_ it goes — [quality profiles](../quality-profiles/README.md) picked the release and [routing](../routing/README.md) already stamped the destination (downloader, library, name template) onto the `download_job`. The importer is the **executor** of those decisions: resolve the files on disk, assign them to the wants they fulfil, hardlink them into place under the rendered [name template](../name-templates/README.md), and write the rows.

This spec is **primarily descriptive** — the import worker exists and runs. Want linkage (an import advances the [want lifecycle](../acquisition/README.md#want-status-the-two-axes)) and season-pack fan-out over the M:N model are built; [path-mapping](../path-mapping/README.md) integration is the main piece still stubbed. The filesystem mechanics are hardlink-first, copy-fallback.

## TL;DR

- The importer runs **after** a `download_job` reaches `completed`. The download worker spawns one **`import_task` per file** (assigning each to a want), then the import worker drains tasks: re-derive source → `ffprobe` → render destination → hardlink/copy → verify on disk → write rows → advance the want `imported` → `available`. **Unbuilt:** a re-gate on the asserted `ffprobe` attributes between probe and render, whose hard-fail would reject + blocklist instead.
- An `import_task` is the unit of work and the audit trail for one file: `{ source, want, library, name_template, dest, method, file }` plus a retry/lifecycle envelope. It carries the routing decision copied off the `download_job`.
- **The importer executes; it does not decide.** Library, name template, and downloader are routing's output. The importer reads them off the job.
- **The re-gate (unbuilt) is a decision the importer _consumes_, not makes.** After `ffprobe`, before the hardlink, it asks [quality profiles](../quality-profiles/README.md#import-time-re-gate) "does the asserted file still pass the profile it was grabbed under?" — the same boundary it keeps with routing and path-mapping. Pass/soft-fail → place (soft-fail logs a `quality/advertised-mismatch` finding); hard-fail → fail the task `quality_rejected`, and [acquisition](../acquisition/README.md#import-time-re-gate-asserted-verification) blocklists + re-searches.
- **File assignment is closed-world.** The job already names the wants it fulfils; the importer only maps the torrent's files _onto_ those known targets (which file is S03E05). This is distinct from [matching](../matching/README.md)'s open-world identity resolution — they share the filename parser, nothing else.
- **Path resolution is a [path-mapping](../path-mapping/README.md#the-four-boundaries) boundary-1 call** (`translate(downloader, arrflix, content_path)`), and the hardlink-vs-copy choice reads path-mapping's [device verdict](../path-mapping/README.md#hardlink-feasibility-the-first-class-output). Both are stubbed today (`pathmapping.Mapper.Apply` returns the path unchanged; the link is simply attempted).
- **Hardlink-first, copy-fallback.** `os.Link` is attempted; on any failure, a byte copy (temp file + atomic rename). The method is recorded per file.
- The importer owns the want transitions `downloading → imported` (in the placement transaction) and `imported → available` (right after it). The [verify-on-disk step](../acquisition/README.md#available-means-verified-on-disk-not-seen-by-plex) runs inside the import worker as a precondition of the commit, and the worker enqueues the `want.available` notification. The [media-server](../media-server/README.md) nudge is downstream and unbuilt.
- Failures are [typed](../../patterns/errors/README.md): retryable (transient FS / downloader) back off and retry; non-retryable (missing source, no matching file, render failure, destination collision) fail the task loudly. A failed task terminally fails its want. The source path self-heals by re-deriving from the downloader.

## Where it sits

```
download_job: created → enqueued → downloading → completed     ← downloader's job
                                                     │
                                                     ▼  spawn import_task per file
import_task:  pending → in_progress → completed                ← this spec
                                          │
                                          ├─ verify placed file (present, non-empty)
                                          ├─ write file (+ state, + import record, + origin)
                                          ├─ want: downloading → imported   (same tx)
                                          └─ want: imported → available, enqueue want.available notification
                                                     ▼
                              MediaServerSync: nudge + propagate ← media-server (downstream, unbuilt)
```

The importer is bounded above by routing's decision (already on the job) and below by the `available` transition. Propagation is someone else's; notification delivery is [notifications](../notifications/README.md)'.

## The trigger — spawning tasks

The [download job worker](../acquisition/README.md#what-stays-the-same) polls the downloader; when a job transitions to `completed`, it spawns the import tasks for that job. Spawning is where the file set is read and assigned:

1. **List the job's files** via the downloader's `ListFiles` ([downloaders](../downloaders/README.md)). Skip samples and non-video files. (A movie job whose downloader can't list files falls back to the item's content path.)
2. **Assign files to wants** (below). A movie job has one want and picks one file; a series job has N wants (it may be a season pack) and maps each in-scope file to its want.
3. **Create one `import_task` per assigned file**, copying the routing decision (`library_id`, `name_template_id`) and the resolved `want_id` off the job, and recording the source path.

A job that yields no assignable file (matched nothing, all samples) fails as non-retryable rather than spawning empty work. Spawning lives in the download worker (`jobs/download`); the import worker (`jobs/import`) only drains tasks.

## File assignment — closed-world

The `download_job` already carries the identity: `media_item_id`, and for series the season/episode of each linked want. The importer is **not** discovering what a file is — it is distributing a known set of files across a known set of targets. Two shapes:

- **Movie** — pick the main file: the largest video file, samples excluded. One file, one want.
- **Series** — parse each filename's episode marker (`S03E05`, `3x05`, multi-episode ranges like `S03E05-E06`) and assign it to the matching want. The job's linked wants are the target set; a file whose marker matches no open want is overflow.

The parsing primitive (filename → season/episode markers) is the _same_ primitive [matching](../matching/README.md) needs for its open-world identity work. It is a pure function — it touches no DB, no metadata provider — which makes it the cheapest and most valuable unit-test surface in the flow, and the natural thing to share. The importer owns the **assignment** (closed-world, deterministic, target set known); matching owns **identity** (open-world, TMDB-validated, confidence-banded) on the [scan](../scan/README.md)/drop-in path. They are meant to share the parser — the unified [parsing](../parsing/README.md) engine (path flavor) — and diverge immediately after. Today the importer keeps its own regex (`importer.ParseSeriesInfo`); consolidating onto `internal/parsing` is intended, not blocking — see [open questions](#open-questions).

### Season packs and overflow

A single `download_job` fulfils **many wants** (M:N — see [acquisition § Season packs](../acquisition/README.md#season-packs)); each `import_task`, and so each imported file, fulfils **exactly one** want (`import_task.want_id` is a single FK). Assignment is the join between the two:

- **Covered** — file marker matches an open linked want → one task, want set.
- **Overflow** — file matches no open want (pack carries E11; we only wanted E01–E10) → no task is spawned; the file stays in the download directory, never filed onto an unrelated want. Whether it should instead land as an unidentified `file` for [scan](../scan/README.md)/matching or as extras is acquisition's [overflow question](../acquisition/README.md#overflow--under-coverage), not the importer's.
- **Under-coverage** — a linked want with no covering file in the pack is released back to `pending` and unlinked from the job, so it re-searches.

The assignment helper (`importer.MatchFilesToEpisodes`) takes an optional target season and episode: a single-episode job passes both, a season-pack job passes only the season, and the fan-out maps every matched episode onto its linked want in one pass. A job with no linked wants (a manual grab of an untracked episode) files every match onto a NULL `want_id`.

## Path resolution — the boundary-1 call

The downloader reports a path in _its_ frame (`/downloads/torrents/X/X.mkv`). Before the importer can touch the file it resolves that to Arrflix's canonical frame via [path-mapping boundary 1](../path-mapping/README.md#the-four-boundaries):

```
translate(downloader, arrflix, content_path) → /data/torrents/X/X.mkv
```

Today `pathMapper.Apply()` is a **stub that returns the path unchanged** — correct only when the downloader and Arrflix share a mount (the single-volume common case). **Unbuilt:** routing through the real resolver, so split-mount setups work and an **unresolvable path** becomes a typed [hold-fix-retry error](../path-mapping/README.md#edge-cases) instead of a confusing "file not found." The audit record notes the translation applied (`qbit:/downloads/X → /data/torrents/X via volume "media"`), turning the hardest-to-debug *arr problem into a glance.

**The hardlink verdict** is meant to come from the same layer: `device(source) == device(library_root)` ([path-mapping](../path-mapping/README.md#hardlink-feasibility-the-first-class-output)). Equal device → hardlink; unequal → the import still succeeds via copy, just slower and at 2× space. The importer reads this verdict; it does not compute the device check itself. **Unbuilt:** today the importer just attempts `os.Link` and falls back to copy on any error.

### Self-healing the source path

An `import_task` records its source path at spawn time. Every time the worker runs a task that has a download job, it **re-derives** the path by re-querying the downloader for the job's files (and updates the task if it changed), so a stale path (a container remount, a path change) heals rather than failing outright; if the downloader can't answer, it falls back to the stored path. The frozen-string source path is a known quirk — the twin of [libraries OQ#4](../libraries/README.md#open-questions) (`import_task.library_root_path` as a string). The design resolves both through the volume layer so a task references stable identity, not a captured string; the migration is coordinated with path-mapping ([OQ#9](../path-mapping/README.md#open-questions)).

## The asserted re-gate

**Unbuilt.** Before rendering and placement, the importer extracts the file's asserted attributes with `ffprobe` and asks [quality profiles](../quality-profiles/README.md#import-time-re-gate) to re-run the profile the release was grabbed under — against the real file this time. **The importer consumes the verdict; it does not compute it** — the same boundary it keeps with [routing](../routing/README.md) (destination) and [path-mapping](../path-mapping/README.md) (device verdict).

- **Pass / soft-fail** → proceed to render + placement. On a soft-fail (over-advertised but acceptable), the importer records the score penalty and writes a [`quality/advertised-mismatch`](../hygiene/README.md) finding — the importer is the one place that holds **both** the advertised parse (off the job) and the fresh `ffprobe`, so it is where the delta is computed.
- **Hard-fail** → the task fails with a typed `quality_rejected` ([errors](../../patterns/errors/README.md)), non-retryable; no file is placed. [Acquisition](../acquisition/README.md#import-time-re-gate-asserted-verification) reacts by blocklisting the release and returning the want to `searching`. Rejecting **before** the hardlink means there's no placed file to clean up.

`ffprobe` already runs once here — the rendered name template needs `MediaInfo` — so the re-gate is essentially free on top.

## Rendering the destination

The destination path is the chosen [name template](../name-templates/README.md) rendered against an evaluation context the importer assembles:

- **Media** — type, title, year, TMDB id, and for series the season/episode of the assigned want.
- **Release** — title/group/edition parsed from the job's release name.
- **MediaInfo** — codec, resolution, HDR, audio, container, extracted by `ffprobe` on the source file. Import is the **one place** this exists: it's post-download by definition, so routing (pre-download) never sees it, and a name template that references `{mediainfo.*}` resolves only here.
- **Quality** — the advertised parse (`Full`, `Resolution`, `Source`, …) of the job's `candidate_title`, **not** reconciled with `ffprobe`. Once the [re-gate](#the-asserted-re-gate) exists it hard-fails any resolution mismatch, so the advertised bin matches reality; `Source` is advertised-only. The parse is re-derived from the title at import (`parsing.Parse` is pure, so it matches the pick by construction) and then persisted — see below.

For series the importer renders show-folder → season-folder → file and joins them; for movies, optional movie-folder → file. The full destination is `library.root_path` joined with the rendered relative path. A render failure (typo, missing required field) fails the task — non-retryable, with no fallback template; whether to ship a hardcoded fallback is an [open question](#open-questions).

## The filesystem operation

`HardlinkOrCopy`: attempt `os.Link(source, dest)` first; on any failure (typically cross-device) fall back to a byte-for-byte copy into `dest.tmp` followed by an atomic rename. The chosen method (`hardlink` | `copy`) is recorded on the task and the import record. That recorded method is half the [hardlinks](../hardlinks/README.md) broken-link predicate — a `hardlink`-method file whose live `nlink` later drops to `1` is _provably_ broken.

After placement the worker `stat`s the destination and fails the task (retryably) if it is missing or zero-length — the verify step, run before any row is written so DB and disk can't diverge. **Unbuilt:** capturing `st_dev` / `st_ino` / `st_nlink` from that `stat` ([hardlinks](../hardlinks/README.md), V1), and a post-link `nlink ≥ 2` assertion guarding against an FS reporting a link it didn't actually make.

Collision handling: if the destination already exists and this is a **reimport** (the task chains off a `previous_task_id`), the old file is removed first; otherwise the collision is a [`Conflict`](../../patterns/errors/README.md) and the task fails rather than clobbering.

## Persistence — one transaction

On a successful placement the importer writes, in a single transaction:

- **`file`** — `{ library_id, media_item_id, episode_id?, path }`. The relative `path` within the library. The entity is owned by [files](../files/README.md); the importer is its writer on the grab path (identity already known — closed-world). It carries **no** media-server rating key — propagation lives [elsewhere](../media-server/README.md#the-propagation-record).
- **`file_state`** — `exists = true`, `size_bytes`, and a best-effort `osdb_hash`. The presence/size facts [scan](../scan/README.md)'s verify mode reconciles against.
- **`file_import`** — the per-attempt record: method, source, dest, success. This is the importer's slice of the [audit](../../patterns/audit/README.md) story — the durable "how did this file get here" trail. (Recording the path translation applied is unbuilt with path-mapping.)
- **`file_origin`** — the [persisted parse](../parsing/README.md#persisted-parse): `origin = 'grab'`, the release title (`source_title`), the projected bin, release group, edition, the full parse as JSONB, and the grab's `indexer_id` / `guid` / `download_job_id` — so a template can be re-rendered (mass-rename) after the `download_job` is purged. Insert-once.
- **`import_task`** completion — final status, `dest`, `method`, `file_id`.
- **The linked want transitions `downloading → imported`**, via the terminal-sticky mirror (a want canceled mid-flight stays `canceled`; the file still commits).

After the commit, the want advances **`imported → available`** and, on that real transition, the worker enqueues the `want.available` notification (best-effort). A transaction that fails _after_ a successful hardlink leaves an orphaned link on disk; cleanup of that orphan is an [open question](#open-questions) (today it persists until a scan reconciles it).

## The `import_task` lifecycle

```
pending ──► in_progress ──► completed
                │
                ├──► failed      (retries exhausted, or non-retryable)
                └──► cancelled   (operator cancels a pending task)
```

The worker claims a bounded batch (10, every 2s) of runnable `pending` tasks per tick, marks them `in_progress`, and processes each independently — failure or retry is **per task**, not per job, so one bad file in a season pack doesn't sink the other nine.

### Failure & retry

Retryability derives from the [error kind](../../patterns/errors/README.md):

- **Retryable** — transient filesystem errors, a placed file that fails the verify check (`BadGateway`, `Internal`). The task backs off (exponential, 5 attempts by default) via `next_run_at` and is reclaimed.
- **Non-retryable** — source missing or is a directory, no downloader file matching the target, destination collision (non-reimport), name-template render failure. These fail fast; retrying can't help.

`last_error` + `error_kind` persist on the task for the activity surface, and each attempt writes an `import_task_event`. A task that fails terminally also fails its want — deliberately unlike a failed download (which excludes the release and re-searches), because re-downloading a different release into the same broken import doesn't help.

### Reimport

A reimport (per task, or in bulk per download job) creates a **new** task pointing at the old via `previous_task_id`, re-runs the pipeline, and (because it's flagged a reimport) overwrites the existing destination. The chain is walkable for history. This is the mechanism behind an [upgrade](../tracking/README.md) replacing a file in place.

## Manual & orphan imports

**Unbuilt** — no code path creates an import task without a download job (reimports copy the original's). `import_task.download_job_id` is nullable: an import need not originate from an Arrflix-managed download (admin-added torrent, manual file drop). With no routing decision to copy, the importer falls back to the [per-type default library](../libraries/README.md#per-type-default) and the default name template. Note the boundary: a file of **unknown identity** dropped into a library is [scan](../scan/README.md) + [matching](../matching/README.md)'s problem (open-world), not the importer's — the importer handles imports where the target is already known.

## Events

| Event             | When                                          | Consumers                                                     |
| ----------------- | --------------------------------------------- | ------------------------------------------------------------- |
| `import_task_updated` | Any task status change                    | [Realtime](../realtime/README.md) SSE (progress UI)           |
| `download_job_updated` | Task change rolls up to the parent job's summary | [Realtime](../realtime/README.md) SSE                  |
| `want_updated`    | The want advances to `imported`, then `available` (or `failed`) | [Realtime](../realtime/README.md) SSE; the title-status projection is re-announced alongside |

On the `available` transition the worker also enqueues a `want.available` [notification](../notifications/README.md). It does **not** emit `file.propagated` ([media-server](../media-server/README.md) would).

## Integration points

| Neighbor | How it interacts |
| -------- | ---------------- |
| [Acquisition](../acquisition/README.md) | Owns the want lifecycle and the M:N `download_job ↔ want` linkage; the importer is its back half, advancing wants through `imported` to `available` (the verify step runs inside the import worker). |
| [Routing](../routing/README.md) | Decides downloader + library + name template; the importer reads those off the `download_job` and executes them. Never re-decides. |
| [Path-mapping](../path-mapping/README.md) | Boundary-1 `translate` resolves the downloader path to canonical; the device verdict drives hardlink-vs-copy. The importer is a consumer; the mechanics are the pattern's. |
| [Name-templates](../name-templates/README.md) | Rendered here against the media + release + mediainfo context to compute the destination path. |
| [Quality profiles](../quality-profiles/README.md) | **Unbuilt:** the importer runs the import-time re-gate before placement — supplies asserted `ffprobe` attributes, acts on pass / penalize / hard-fail, doesn't own the logic. |
| [Parsing](../parsing/README.md) | `parsing.Parse` re-derives the release's quality/release attributes for rendering and `file_origin`. The closed-world filename-marker parse is meant to be the path flavor of the unified parser; today the importer keeps a private copy. |
| [Files](../files/README.md) | The importer writes `file` / `file_state` / `file_import` rows on the grab path (identity known); the entity model is owned by files. |
| [Libraries](../libraries/README.md) | Destination root; per-type default for orphan imports. The importer writes `file.library_id`. |
| [Matching](../matching/README.md) | Owns open-world identity (scan/drop-in); the importer owns closed-world assignment (grab path). Both consume the same [parser](../parsing/README.md). Overflow files hand off to matching as unidentified `file` rows. |
| [Scan](../scan/README.md) | Reconciles what the importer writes (`file_state` presence/size); cleans up orphaned links. Shares the file-on-disk truth. |
| [Downloaders](../downloaders/README.md) | `ListFiles` at spawn; re-queried during source self-heal. |
| [Notifications](../notifications/README.md) | The importer enqueues `want.available` when a want reaches `available`. |
| [Realtime](../realtime/README.md) | `import_task_updated` / `download_job_updated` / `want_updated` SSE for progress. |
| [Errors](../../patterns/errors/README.md) | Typed kinds drive retryable-vs-not; unresolvable path is a hold-fix-retry case. |
| [Audit](../../patterns/audit/README.md) | `file_import` is the per-file import record, including the path translation applied. |

## What the importer does NOT own

- **The decision** of where a file goes or what template renders it — [routing](../routing/README.md).
- **The want lifecycle** as a whole — [acquisition](../acquisition/README.md). The importer performs the back-half transitions (`downloading → imported → available`, or `failed`).
- **Media-server propagation** — the nudge, correlation, rating key, deep links — [media-server](../media-server/README.md).
- **Path-translation and device mechanics** — [path-mapping](../path-mapping/README.md). The importer consumes the resolved path and the verdict.
- **Open-world identity** — what an unknown file is — [matching](../matching/README.md).
- **Drift reconciliation / orphan sweeps** — [scan](../scan/README.md). The importer writes the truth at placement; scan keeps it honest over time.

## Open questions

1. **Orphaned hardlink on transaction failure.** If the link succeeds but the DB write rolls back, a link is left on disk. Options: a compensating unlink in the failure path, or leave it for [scan](../scan/README.md) to reconcile (today's de-facto behavior). **Decided:** compensating unlink for the common case + scan as the backstop; pin in iteration 2.
2. **Frozen source path → volume-resolved.** v1 resolves the v0 string-path quirk through the volume layer, coordinated with [libraries OQ#4](../libraries/README.md#open-questions) and [path-mapping OQ#9](../path-mapping/README.md#open-questions). Confirm they land together rather than as two migrations. **Answered by the code:** Moot; there is no `library_root_path` column and the root is joined at read (`internal/db/queries/import_tasks.sql:164`).
3. **Name-template render failure.** Non-retryable and fatal today. Should v1 ship a hardcoded fallback template (so a typo degrades to a sane path instead of a stuck file), or keep it loud? Lean: loud + a clear activity error — a silent fallback hides a misconfiguration. **Answered by the code:** Loud; render errors are `Internalf(...).NotRetryable()` (`internal/jobs/import/worker.go:439-473`).
4. **Overflow disposition.** Files a season pack carries beyond the wanted set: left unidentified (reconcile via matching) vs first-class "extras." Owned by [acquisition](../acquisition/README.md#overflow--under-coverage); the importer just needs the disposition pinned to know where to route them. *Frozen — beyond V1.*
5. **Shared parser extraction.** The filename-marker parse is the path flavor of the unified [parser](../parsing/README.md); the importer is meant to call `internal/parsing` rather than keep its own copy. Until then the importer's copy is the reference. Not a blocker. *Frozen — beyond V1.*
6. **Verify step ownership.** The `imported → available` presence-verify is listed in [acquisition](../acquisition/README.md#event-bus--messaging) as a distinct step. Confirm it lives at the acquisition boundary (a thin worker reacting to `want.imported`) rather than being folded into the import worker — keeps the importer's responsibility bounded at `imported`. — *Answered by the code: the verify is folded into the import worker, as a precondition of the commit, and the worker carries `imported → available`.*
7. **Concurrency.** The worker claims a bounded batch per tick. Right knob for v1 (fixed batch size vs a worker pool sized to FS throughput)? Lean: keep the simple bounded batch; revisit only if large libraries show import as a bottleneck. **Answered by the code:** A bounded batch, `ClaimLimit: 10` (`internal/jobs/import/worker.go:66,110`).
8. **Re-gate hard-fail scope in a season pack.** One file hard-failing the re-gate fails its own task (per-task, as today) while siblings proceed — but is the **blocklist** scoped to the single file or the whole release? Lean: the release (the pack is the grabbable unit, so re-grabbing should avoid the whole pack), with the surviving siblings still imported. Pin with [acquisition](../acquisition/README.md#import-time-re-gate-asserted-verification). *Frozen — beyond V1.*

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
