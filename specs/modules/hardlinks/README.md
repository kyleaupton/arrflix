# Hardlinks — the reference graph behind verifiable storage

**Status:** Draft, iteration 1

Arrflix is hardlink-first: a completed download is `os.Link`'d into the library so the same bytes live under two paths (the torrent's and the library's) on one device, costing one copy of disk instead of two. That strategy is only as good as Arrflix's ability to _see_ the links — which file shares an inode with which, how many references an inode has, and which of those references is a live downloader torrent holding the file alive. This spec defines that **reference graph**: the inode facts Arrflix captures, the small query surface over them, and the downloader correlation that turns "we linked and hoped" into "we can prove what's linked to what, right now."

It is the explicit home for a concept four other specs already lean on without owning: [importer](../importer/README.md) does the linking, [path-mapping](../path-mapping/README.md) answers the _single-file, predictive_ question ("will this link?"), [scan](../scan/README.md) notices breakage, and [hygiene](../hygiene/README.md) wants to _explain_ and _act on_ it. None of them owns the **retrospective, multi-path graph** itself — and two differentiators (hygiene's hardlink-intelligence UX and the future storage-intelligence wedge) rest entirely on it.

## TL;DR

- The "graph" is **emergent, not a maintained structure**. Three facts per file — `st_dev`, `st_ino`, `st_nlink` — are all it takes: two paths with the same `(st_dev, st_ino)` _are_ the same inode (hardlinked), and `st_nlink` is that inode's reference count. The graph is a `GROUP BY (st_dev, st_ino)` over captured facts.
- **Capture rides existing `stat()`s — no new walk.** [Scan](../scan/README.md) (full / diff / verify), the [importer](../importer/README.md) (post-link), and the verify step already `stat()` files; the three inode fields come back in the _same_ `syscall.Stat_t`. Capture is three field assignments, zero extra I/O. The facts live on `media_file_state` beside `file_size` / `last_verified_at`.
- **This upgrades broken-hardlink detection from heuristic to exact.** Scan today infers breakage from "size/mtime drift," which _misses the canonical case_ (a torrent is removed, `nlink` drops 2→1, size/mtime unchanged). A file imported via **hardlink** whose live `nlink == 1` is _provably_ broken — no heuristic.
- **A small `internal/hardlink` package owns the queries**, not a worker: `RefCount`, `PathsSharing`, `ReclaimableBytes`, `IsBrokenHardlink`. Pure reads over the captured facts. Weight is closer to [`internal/parsing`](../parsing/README.md) than to a module with its own lifecycle.
- **The one genuinely new computation is the downloader correlation** — "this inode is held alive by torrent X in qBittorrent." It stats each downloader's per-torrent files ([`ListFiles`](../downloaders/README.md#the-provider-model)), groups by inode, joins against library inodes. It runs **on-demand / slow-cadence**, never per-scan, because statting all torrents is the expensive part.
- **Detection ≠ explanation.** Detecting a broken link needs only `nlink` + the recorded import method. _Explaining_ it ("the torrent that held it was removed") and computing reclamation need the correlation. The two are cleanly separated so the cheap signal doesn't wait on the expensive join.
- **Captured facts are a snapshot; destructive acts re-stat live.** Inode numbers are reused after deletion, so the columns are for fast reads + forecasting; any cleanup/delete preflight re-stats before acting. Same posture as [path-mapping](../path-mapping/README.md#open-questions)'s "no persisted-staleness traps."
- **Composes with path-mapping, doesn't overlap it.** Path-mapping owns the _predictive, single-file_ primitive (`device()`, `sameFile()`). This spec owns the _retrospective, multi-path_ graph and uses those primitives for its live checks.

## Why this is its own spec

The same "four consumers, no owner" shape that earned [parsing](../parsing/README.md) its module. Hardlinks are referenced by:

- [importer](../importer/README.md) — performs `HardlinkOrCopy`, records the method per file, but doesn't track the resulting link over time.
- [path-mapping](../path-mapping/README.md#hardlink-feasibility-the-first-class-output) — owns `device()` (hardlink-safe?) and `sameFile()` (is this translation correct?), explicitly scoped to a _single file, before the fact_.
- [scan](../scan/README.md) — detects breakage, but via a size/mtime _heuristic_, and captures no inode identity.
- [hygiene](../hygiene/README.md#killer-ux-moves) — _describes_ the flagship "held by 3 torrents; deleting frees nothing" UX and the hardlink-aware [cleanup preflight](../hygiene/README.md#retention--cleanup-the-home-for-request-retention), but disclaims data shapes.

Each touches the link graph; none defines it. And it is **load-bearing for two differentiators**: hygiene's hardlink intelligence has no engine without it, and the storage-intelligence wedge (*"reclaim 200 GB by downgrading these never-watched remuxes"*) is _entirely_ a query over this graph. Naming it once stops hygiene and storage-intelligence from each half-inventing it. It's also genuinely **cross-actor** — the graph is built from the filesystem, the downloader's torrent file lists, and the library DB — so it can't live cleanly on any single connection spec.

The dividing line with path-mapping is the crux:

| | [Path-mapping](../path-mapping/README.md) | Hardlinks (this spec) |
| --- | --- | --- |
| Question | "Will these two paths hardlink?" / "Is this translation the same file?" | "What is currently linked to what, and who holds it alive?" |
| Tense | **Predictive** — before the import | **Retrospective** — after, and over time |
| Scope | A single file / a path pair | The whole inode reference graph |
| State | Stateless (live `stat`) | Captured facts + live re-stat for destructive acts |
| Primitive | `device()`, `sameFile()` | _Consumes_ those; adds `nlink`, ref-grouping, downloader correlation |

## The model

### The three facts

Every probed `media_file` carries, alongside the presence/size facts [scan](../scan/README.md#what-scan-writes-data-shapes-sketched) already reconciles:

| Fact | From | Meaning |
| --- | --- | --- |
| `st_dev` | `syscall.Stat_t.Dev` | The device. Hardlinks exist **only within one device**, so this scopes every inode comparison. |
| `st_ino` | `syscall.Stat_t.Ino` | The inode number. `(st_dev, st_ino)` is the identity of the underlying bytes — the join key for "same file." |
| `st_nlink` | `syscall.Stat_t.Nlink` | The reference count: how many directory entries point at this inode. |

Lean: these are **columns on `media_file_state`** (the existing per-file FS-facts row), not a new table — they're the same shape and refresh on the same `stat()`. A companion table is the fallback if the row gets unwieldy ([open questions](#open-questions)).

### The graph is emergent

There is no graph object to build or keep coherent. "All paths sharing this inode" is `GROUP BY (st_dev, st_ino)` over `media_file_state`. The package surfaces that as named queries; the storage engine does the work.

### Broken-hardlink detection — exact, via `nlink` + import method

The canonical failure: the torrent client is wiped or a torrent removed, the library file's sibling link vanishes, and the file becomes a lonely full-size copy — silently. Scan's current size/mtime heuristic misses it (neither changes).

The exact signal combines two facts Arrflix already has the pieces for:

- The [importer](../importer/README.md#persistence--one-transaction) records the placement **method** (`hardlink` | `copy`) on `media_file_import`.
- This spec captures live `st_nlink`.

> A `media_file` whose recorded import method was **`hardlink`** but whose live **`st_nlink == 1`** is a **broken hardlink** — proven, not inferred. (`nlink == 1` on a `copy`-imported file is normal and is _not_ a finding.)

That predicate is all **detection** needs. It does not require knowing _which_ torrent vanished — that's explanation, below.

### The downloader correlation — the one new computation

To turn detection into the hygiene-grade story (*"held alive by 3 torrents; deleting frees nothing until you also remove torrent X"*) and into reclamation math, Arrflix correlates inodes to live downloader content:

1. For each enabled [downloader](../downloaders/README.md), list its torrents' files (`ListFiles`).
2. `stat()` each, capturing `(st_dev, st_ino)`.
3. Group by inode; join against library `media_file` inodes.

The result is, per library inode: the set of downloader torrents currently holding it alive. From that fall out the two consumer payloads:

- **Reclamation** — deleting a library file frees space only if no _other_ reference survives. `ReclaimableBytes` sums sizes where the post-delete reference count would reach zero (`nlink == 1` and no surviving sibling). This is the substrate for the storage-intelligence wedge.
- **Explanation** — the per-finding "held by torrent X" narrative hygiene renders.

**Cadence:** this is the expensive part (statting every torrent's files), so it runs **on demand** (hygiene cleanup preflight, a storage forecast) or on a **slow sweep**, never inside a scan. Whether the correlation result is cached or recomputed live each time is an [open question](#open-questions); the detection signal above never depends on it.

### Freshness — snapshot for reads, live for destructive acts

Captured `(st_dev, st_ino, st_nlink)` are a point-in-time snapshot, and **inode numbers are reused** after a file is deleted. So:

- Fast reads, dashboards, and forecasts use the captured columns.
- Any **destructive** operation (a [hygiene](../hygiene/README.md#destructive-preflight) cleanup/delete preflight) **re-stats live** before acting, and treats the captured value as advisory.

This mirrors path-mapping's stance ([OQ#11](../path-mapping/README.md#open-questions): import-time stat is authoritative; no persisted-staleness traps) rather than inventing a new freshness rule.

## The package — `internal/hardlink`

A thin query + correlation package, not a worker. Conceptual surface (exact signatures deferred):

- `RefCount(ctx, mediaFileID) → int` — live `nlink` (or captured, per caller's freshness need).
- `PathsSharing(ctx, dev, ino) → []FileRef` — every known library path on the inode (the within-Arrflix view; cross-library hardlinks fall out for free here — see [enables](#what-this-enables)).
- `IsBrokenHardlink(ctx, mediaFileID) → bool` — the `method == hardlink && nlink == 1` predicate.
- `Holders(ctx, dev, ino) → []DownloaderRef` — the correlation: which torrents hold this inode alive.
- `ReclaimableBytes(ctx, targets) → int64` — what a proposed deletion set would actually free.

It depends on [path-mapping](../path-mapping/README.md)'s `device()` / `sameFile()` for the live checks and on the [downloader manager](../downloaders/README.md) for `ListFiles`. It owns no lifecycle, emits no events, runs no background loop of its own (the slow correlation sweep, if added, is a thin scheduled call into this package — see [open questions](#open-questions)).

## What this enables

- **Hygiene `integrity/broken-hardlink`, now exact** — the [catalog](../hygiene/README.md#the-catalog) rule's detection becomes the `nlink + method` predicate instead of drift-guessing, closing the silent-lonely-copy hole.
- **Hygiene hardlink-intelligence UX** — [killer UX move #3](../hygiene/README.md#killer-ux-moves) ("held by 3 torrents; deleting won't free space until…") gets its engine: the correlation.
- **Hardlink-aware cleanup preflight** — the [lifecycle/cleanup](../hygiene/README.md#retention--cleanup-the-home-for-request-retention) "removing this frees nothing" guard is a `ReclaimableBytes` call.
- **Storage-intelligence wedge (future)** — reclamation forecasting (*"downgrade these → free N GB"*) is built on `ReclaimableBytes` over the graph. This spec is its substrate.
- **Importer post-link assertion (optional)** — after `os.Link`, confirm `nlink` actually incremented, catching a "link reported success but the FS lied" case before the want is marked done.
- **Copy-that-could-have-been-a-link finding** — path-mapping already wants to surface files that were copied when the device check says they _could_ have linked ([path-mapping](../path-mapping/README.md#hardlink-feasibility-the-first-class-output)); this graph is where that's detectable (`method == copy` && download dir shares device with the library root).
- **Cross-library hardlink awareness (deferred)** — [scan OQ](../scan/README.md#open-questions) parks "same inode, two library paths." Capturing `(st_dev, st_ino)` means the data's already there when that feature is wanted; no migration.

## What hardlinks does NOT own

- **The mechanic** — `os.Link` / copy-fallback / atomic rename, and recording the method ([importer](../importer/README.md#the-filesystem-operation)). This spec _reads_ the recorded method; it doesn't link.
- **The predictive, single-file verdict** — `device()` / `sameFile()` ([path-mapping](../path-mapping/README.md#hardlink-feasibility-the-first-class-output)). Consumed here, not owned.
- **The filesystem walk** — discovery and reconciliation ([scan](../scan/README.md)). This spec defines _what extra fields scan captures_, not how scan walks.
- **Findings, severities, remediation, the dashboard** — ([hygiene](../hygiene/README.md)). This spec provides the predicate + reclamation math; hygiene presents and acts.
- **Storage forecasting policy** — the future storage-intelligence wedge owns the _policy_ ("warn at 6 weeks to full"); this spec owns the _graph queries_ it runs on.
- **Path translation across reference frames** — ([path-mapping](../path-mapping/README.md)). Correlation _uses_ `translate` to resolve a downloader's reported path to canonical before statting.

## Interactions

| Neighbor | How it interacts |
| --- | --- |
| [Scan](../scan/README.md) | The capture point: stamps `st_dev` / `st_ino` / `st_nlink` on `media_file_state` during the stats it already does (full / diff / verify). Its broken-hardlink detection upgrades from drift-heuristic to the exact `nlink` predicate. |
| [Importer](../importer/README.md) | Records the placement method this spec's detection keys on; can call a post-link `nlink` assertion. Captures inode facts on the file it just placed. |
| [Path-mapping](../path-mapping/README.md) | Supplies `device()` / `sameFile()` for live checks and `translate` to bring a downloader path into the canonical frame before correlation. Strict primitive/consumer split — no overlap. |
| [Hygiene](../hygiene/README.md) | Primary consumer: `integrity/broken-hardlink` detection, the hardlink-intelligence narrative, and the hardlink-aware cleanup preflight all read this package. |
| [Downloaders](../downloaders/README.md) | `ListFiles` feeds the correlation (which torrent holds which inode alive). |
| [Libraries](../libraries/README.md) | `media_file` / `media_file_state` are the library-side rows the graph is grouped over. |
| Storage-intelligence (future wedge) | Built on `ReclaimableBytes` + the correlation; this spec is its substrate. |
| [Errors](../../patterns/errors/README.md) | A failed `stat` or downloader `ListFiles` during correlation produces a typed error (`BadGateway` upstream); correlation degrades to "unknown holders," never blocks. |

## Tables

**Owned by this spec** (shapes indicative; column types deferred to iteration 2):

- **`media_file_state` extension** — adds `st_dev`, `st_ino`, `st_nlink` (and the `stat` timestamp already present). Lean: columns on the existing FS-facts row rather than a new table. Captured by scan / importer / verify.

**Referenced, owned elsewhere:**

- **`media_file` / `media_file_import`** — [libraries](../libraries/README.md) / [scan](../scan/README.md) / [importer](../importer/README.md). The import **method** (`hardlink` | `copy`) on `media_file_import` is half the broken-link predicate.
- **`download_job` / downloader torrent file lists** — [downloaders](../downloaders/README.md). The correlation source.

No table stores "the graph" — it's queried, not materialized. A cached correlation table is a possible optimization, [open](#open-questions).

## Open questions

1. **Columns vs companion table.** `st_dev` / `st_ino` / `st_nlink` on `media_file_state` (lean — same shape, same refresh) vs a `media_file_inode` companion. Revisit only if the state row grows unwieldy.
2. **Correlation: cached or live.** Recompute the inode→torrent correlation on each consumer call (always fresh, expensive) vs cache it with a TTL refreshed by a slow sweep (fast reads, possible staleness). Lean: cache with a slow-sweep refresh for dashboards; destructive preflights re-stat live regardless.
3. **Correlation cadence + ownership of the sweep.** If a periodic correlation sweep exists, is it its own scheduled job or folded into scan's verify pass / hygiene's nightly audit? Lean: ride [hygiene's nightly audit](../hygiene/README.md#computation-model) (it's already the retrospective, FS-touching cadence) rather than a new worker.
4. **`nlink` baseline for non-import-tracked files.** The broken-link predicate leans on the recorded import method. For files Arrflix didn't grab (scanned-in, drop-ins) there may be no `hardlink` method recorded — so `nlink == 1` is ambiguous (could be a legit single copy). Lean: the finding only fires for `method == hardlink`; scanned files get a weaker, opt-in "single-linked file in a hardlink-first library" advisory at most.
5. **Cross-filesystem reality check.** `st_ino` is only unique _within_ `st_dev`; never compare inodes across devices. Enforce `(st_dev, st_ino)` as the composite key everywhere — a pure correctness rule, but worth stating so no query keys on `ino` alone.
6. **Inode reuse window.** A deleted file's `(dev, ino)` can be reassigned to a new file. Captured facts are advisory; the live re-stat on destructive acts is the guard. Is there any read path where a stale inode match could mislead a _non_-destructive surface badly enough to matter? Lean: no — worst case is a transiently wrong ref-count in a dashboard, corrected on next capture.
7. **Copy-that-could-have-linked finding ownership.** The "you copied when you could have linked" finding needs this graph (`method == copy` + same-device download dir) but is presented by hygiene and predicted by path-mapping. Confirm it's a hygiene finding fed by this package, not a fourth owner.
8. **Post-link `nlink` assertion in the importer.** Worth adding a cheap "did `nlink` actually become ≥2?" check right after `os.Link`, as a guard against silent FS lies? Lean: yes, cheap insurance; coordinate with the importer's [orphaned-link OQ](../importer/README.md#open-questions).

## What we're explicitly not deciding here

- Exact column types / index shape for the `media_file_state` inode fields.
- The Go signatures of the `internal/hardlink` query surface.
- Whether the downloader correlation is materialized in a table and, if so, its schema.
- The slow-sweep scheduling mechanics (cadence, dedup) — coordinated with scan / hygiene.
- The storage-intelligence wedge's forecasting policy and UI (future, separate).
- Remediation handlers for broken-hardlink / reclaim findings (owned by [hygiene](../hygiene/README.md)).
- Non-POSIX (`nlink`/inode-less) filesystem behavior — out of scope; the hardlink-first strategy already assumes a POSIX device.

## Doc neighbors

- [Path-mapping](../path-mapping/README.md) — the predictive, single-file primitive (`device()` / `sameFile()`) this graph consumes; strict primitive/consumer split.
- [Importer](../importer/README.md) — performs the link, records the method the detection predicate keys on.
- [Scan](../scan/README.md) — the capture point; its broken-hardlink detection upgrades to the exact `nlink` predicate.
- [Hygiene](../hygiene/README.md) — primary consumer: broken-hardlink finding, hardlink-intelligence UX, hardlink-aware cleanup preflight.
- [Downloaders](../downloaders/README.md) — `ListFiles` feeds the inode→torrent correlation.
- [Libraries](../libraries/README.md) — owns `media_file` / `media_file_state` the graph groups over.
- [Errors](../../patterns/errors/README.md) — typed errors for failed stats / downloader calls during correlation.
