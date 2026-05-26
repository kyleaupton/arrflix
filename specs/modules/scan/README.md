# Scan — keeping disk and database in sync

**Status:** Draft, iteration 1

This doc defines **scan**: the subsystem that reconciles what's on disk with what's in the database. It captures _what scan owns_, _what it doesn't_, _how it reconciles missing and changed files_, _what cheap metadata it extracts on the way_, and _what scan modes and triggers we need_. It does **not** pin down table names, column types, or wire formats — those come in a later iteration.

This doc is a back-fill: scan exists today as the 4-phase `ScannerService` in `backend/internal/service/scan.go`. The greenfield design carves off the identification logic (which moves to [matching](../matching/README.md)) and shifts scan's focus to discovery, reconciliation, and state refresh.

## TL;DR

- Scan's job clarifies once matching becomes its own subsystem: **discovery** (what's on disk?), **reconciliation** (what's gone or changed?), **state refresh** (`last_verified_at`, size/mtime drift), **cheap metadata capture** (OSDb hash, optionally ffprobe), and **event emission** for downstream consumers.
- The single biggest gap today is **reconciliation** — scan barely notices when files disappear. The greenfield model does two passes: an FS pass (what's on disk) and a DB pass (what we think is on disk), and resolves the diff.
- Four **scan modes**: `full` (walk everything), `diff` (walk only directories with changed mtime), `verify` (no walk; `stat()` known files), `targeted` (a specific path or subtree).
- Several **triggers**: manual, scheduled (cron), post-import, post-grab (downloader webhook), inotify (real-time on local FS), drift-detection cascades.
- **Streaming pipeline**, not batched phases: walk emits events; matcher consumes batches; probe worker consumes events; [realtime](../realtime/README.md) emits live progress to clients. Failures in one stage don't stall the others.
- **OSDb hash computed unconditionally** at scan time — cheap, pays back hugely in [matching](../matching/README.md) v2 and [hygiene](../hygiene/README.md) v1.
- **ffprobe metadata captured lazily** by a separate worker — drives quality-related hygiene rules without slowing the walk.
- Scan is mostly plumbing. The differentiators it enables — instant drop-ins, paranoid integrity verification, no orphan rows, fast scheduled re-scans — are what users notice.

## What scan owns (and what it doesn't)

Once matching becomes its own subsystem, scan's responsibilities tighten:

| Responsibility               | Role                                                                                                  |
| ---------------------------- | ----------------------------------------------------------------------------------------------------- |
| **Discovery**                | Walk the filesystem under each library root; produce a list of media files                            |
| **Reconciliation**           | Detect files in DB that no longer exist on disk; detect size/mtime drift on present files            |
| **State refresh**            | Update `media_file_state.file_exists`, `last_verified_at`, file_size as appropriate                  |
| **Cheap metadata capture**   | Compute OSDb hash on first discovery; emit "needs probe" events for ffprobe metadata                  |
| **Event emission**           | Publish file events to the matcher (new files), hygiene (integrity findings), and [realtime](../realtime/README.md) (live progress to clients) |

What scan **doesn't** own:

- Identification — that's [matching](../matching/README.md). Scan emits "found a new file"; matcher decides what it is.
- Match decisions / TMDB lookups — same; matcher.
- Hardlink and rename mechanics — that's import + name templates.
- Long-running metadata refresh — that's [metadata](../metadata/README.md)'s refresh cadence work.
- Quality decisions on releases — that's [quality profiles](../quality-profiles/README.md).

The clean line: **scan is filesystem-aware; matcher is identity-aware; import is mutation-aware**. They cooperate via events.

## The reconciliation gap

The single most important shift from v0 is that scan needs to notice when files **go away**, not just when they show up. Today's scan walks the tree and adds new entries; missing files mostly leak.

Greenfield scan does two passes per run:

```
1. DB pass: list all media_file rows scoped to this library (and unmatched_file rows)
2. FS pass: walk the library rootpath, collect every media file

3. Resolve the diff:
   ┌─────────────────────────────────────────────────────────────────────┐
   │  In DB + on disk   → update last_verified_at, check size/mtime drift │
   │  Not in DB, on disk → emit FileEvent → matcher.MatchBatch            │
   │  In DB, not on disk → mark media_file_state.file_exists = false      │
   │                       emit integrity/orphan-db-row finding           │
   └─────────────────────────────────────────────────────────────────────┘
```

The "in DB, not on disk" branch is what plugs `integrity/orphan-db-row` in [hygiene](../hygiene/README.md). Broken-hardlink detection no longer rides the "size/mtime drift" guess (which misses the canonical case — a torrent removed, link count 2→1, size/mtime unchanged): scan captures `st_nlink` per file, and a hardlink-imported file whose live `nlink == 1` is a _proven_ broken hardlink. That exact predicate and the inode reference graph it feeds are owned by [hardlinks](../hardlinks/README.md).

Scan does not *fix* missing files; it surfaces them as findings. Remediation is the hygiene system's call.

## Scan modes

One scanner, four flavors of run, distinguished by what they cover:

| Mode          | Walks                                  | Identifies new | Reconciles missing | When to use                                |
| ------------- | -------------------------------------- | -------------- | ------------------ | ------------------------------------------ |
| **Full**      | Everything under each root             | Yes            | Yes                | Initial setup, post-restore, manual deep-check |
| **Diff**      | Only directories with `mtime > last_scan_at` | Yes      | Yes (within walked dirs) | Default scheduled scan                     |
| **Verify**    | Walks nothing; `stat()`s known files only | No           | Yes                | Cheap continuous integrity check           |
| **Targeted**  | A specific path / glob                 | Yes            | Yes (within scope) | Post-import, post-grab, inotify event      |

Today's manual button is Full. The big wins are **Diff** (default scheduled run; massive speedup on stable libraries) and **Verify** (cheap continuous integrity check; the engine behind paranoid hygiene).

### How Diff actually works

Most directories never change between scans. If a directory's mtime hasn't moved past `last_scan_at`, its immediate contents haven't been added/removed. (File *contents* may have changed, but file presence has not.) On a 10K-item library, a Diff scan often only needs to walk a handful of directories.

Caveats:

- A backup/restore can reset mtimes to wrong values. Provide a Full mode and a "run a Full when in doubt" hint.
- Some shared filesystems lie about mtimes. Verify mode covers the gap by `stat()`-ing known files regardless.

### How Verify actually works

Verify never walks the FS. It iterates the known `media_file` rows, `stat()`s each one, and:

- File present, size matches → update `last_verified_at`
- File present, size differs → flag as drifted; emit hygiene finding
- File missing → flip `file_exists = false`; emit `integrity/orphan-db-row` finding
- File present, but a hardlink-imported file's `st_nlink` is now `1` → emit `integrity/broken-hardlink` (the exact [hardlinks](../hardlinks/README.md) `nlink` signal, not a size/mtime guess)

Verify is the cheap continuous check. Run it hourly or daily — sample-based for large libraries (don't `stat()` 100K files at once).

## Triggers

Today: only manual via API. Greenfield supports several:

| Trigger          | When                                                            | Mode default        |
| ---------------- | --------------------------------------------------------------- | ------------------- |
| **Manual**       | User clicks "Scan now"                                          | Full or Diff (user picks) |
| **Scheduled**    | Cron — default every 6 hours                                    | Diff                |
| **Post-import**  | Importer just placed a file → scan-confirm it landed            | Targeted (just the new path) |
| **Post-grab**    | Downloader webhook says download finished                       | Targeted            |
| **Inotify**      | OS event says something changed in a library root               | Targeted            |
| **Drift cascade**| Verify run found something missing → run Diff in that subtree   | Diff (scoped)       |
| **On-demand**    | Hygiene audit job wants fresh state                              | Verify or Diff      |

Triggers coalesce: if a scheduled Diff is mid-run and an inotify event fires, the running scan picks up the new directory if it hasn't passed it yet; otherwise the trigger is dedup'd into a pending queue. No back-to-back scans of the same scope.

### Inotify — the real-time scan

The genuine UX win. Sonarr / Radarr poll. We can be event-driven on local filesystems.

```
file dropped in /media/movies/
    ↓ (inotify event, microseconds)
ScannerService picks up the event
    ↓ (targeted scan: just this file)
MatcherService.MatchBatch([file])
    ↓ (microseconds for path-embed; ms for guessit fallback)
file appears in library; notification fires
```

Total latency from drop to library: hundreds of milliseconds. Compared to "wait 15 minutes for the next poll cycle," this is a noticeable UX upgrade and a clear story-1-style win.

Realities to handle:

- **Inotify is fragile across NFS / SMB.** Network mounts often don't propagate events. We need a graceful fallback: try inotify, detect when it's broken (no events ever arrive, or watches fail to set), fall back to scheduled Diff scans for that root. The detection model: each root advertises an "event mode" — `inotify` if working, `polling` otherwise.
- **Per-user watch limit on Linux.** Default `max_user_watches` is 8192, which a big library can blow through. Don't recurse watches — watch only library roots, and re-walk the affected subtree on event. Trade-off: events on deeply-nested files come in less granularly, but the watch count stays bounded.
- **Bursty events.** A `cp -r` of 500 files generates 500 events. Debounce + coalesce — wait for 2 seconds of quiet, then run a single targeted scan over the affected paths.

Worth shipping in v1 with explicit fallback; not worth blocking the rest of scan on it. The base case (scheduled Diff) is fine without inotify.

## Streaming pipeline architecture

Today's scan is batched phases — walk everything, then identify everything, then persist everything. The greenfield shift is to a streaming pipeline:

```
              walk emits FileEvent       matcher consumes batches    probe consumes events   realtime emits to UI
                       │                            │                          │                      │
                       ▼                            ▼                          ▼                      ▼
        ┌──────────────────┐         ┌────────────────────┐       ┌────────────────────┐    ┌──────────────────┐
        │   ScanWalker     │────────►│   MatcherService   │──────►│  MediaProbeWorker  │    │ realtime.Emit    │
        │                  │         │   (batched calls)  │       │  (async, lazy)     │    │                  │
        └──────────────────┘         └────────────────────┘       └────────────────────┘    └──────────────────┘
                 │                            │                              │                         │
                 └────────────────────────────┴──────────────────────────────┴─────────────────────────┘
                                            (each stage publishes events)
```

Benefits:

- **Live progress with file-level granularity** — UI shows *"identifying The Matrix.mkv (1247 of 3402)"* rather than a phase-level percentage
- **Identification starts before the walk finishes** — earlier results, not all-or-nothing
- **Failures in one stage don't block the others** — a guessit timeout doesn't stall the walk
- **Easier to pause / resume** — each stage's state lives in `scan_run` rows

The matcher's `MatchBatch` API stays — it just gets called repeatedly as the walker fills buffers, not once at the end of phase 1.

## Hash and metadata at scan time

Two operations to do during the walk that pay back hugely downstream.

### OSDb hash — always, unconditionally

128 KiB total disk read per file (the OpenSubtitles hash sums file size + first 64 KiB + last 64 KiB). Trivial cost. Store on `media_file.osdb_hash`. Pays back in:

- **Matching v2** — OpenSubtitles resolver consumes stored hashes for near-100%-confidence identification
- **Hygiene v1** — `layout/duplicate-files` becomes a fast `GROUP BY osdb_hash` lookup, not a fuzzy comparison
- **Drop-in re-identification** — files that move within a library get recognized by hash even when the path changes

No reason to gate this behind any toggle. Compute on first scan, never recompute (hash is content-derived; file changes ⇒ size changes ⇒ Verify catches the drift ⇒ we rehash).

### ffprobe metadata — lazy, out-of-band

A `ffprobe -show_format -show_streams` invocation pulls:

- Container, duration, total bitrate
- Video codec, resolution, framerate, color space, HDR
- Audio codecs, languages, channel layouts, bitrates
- Subtitle tracks and languages

Foundation for several quality-related hygiene rules (`quality/bitrate-outlier`, `quality/codec-preference`, `quality/format-inconsistency`, future audio-language-mismatch).

Two challenges: costs hundreds of ms per file (slow over thousands of files), and it requires `ffprobe` in the container (already present for downloader/transcode pipelines).

Solution: run ffprobe **lazily and out-of-band**. Scan emits a "needs probe" event for each new file. A separate `MediaProbeWorker` consumes events at its own pace and writes into a `media_file_probe` table. Scan never blocks on probe.

Probe re-runs only when explicitly requested (codec/bitrate of a static file don't change). On re-match or re-import, the file path may change but the probe metadata travels with the underlying file (keyed on osdb_hash if available, else file path).

## Killer features

Scan is mostly plumbing, but a few well-placed features make it feel like a thing users notice:

1. **Inotify-driven drop-ins.** Sub-second latency from drop to library appearance. Real-time vs. polling.
2. **Continuous integrity verification.** A background `IntegrityVerifyWorker` `stat()`s known files on a rolling cadence. Broken hardlinks surface within hours, not at the next manual scan. Exactly what Strict-mode hygiene users want.
3. **Smart re-scan via directory mtime.** Diff scans skip unchanged subtrees. 10×+ speedup on stable libraries.
4. **Drive-aware fail-soft.** Detect when a library root is on an offline drive (root `stat()` returns ENOENT / EACCES). Pause scan; surface a clear warning (*"Drive holding /movies appears offline — scan paused; not marking files as missing"*). Don't nuke a thousand `file_exists=false` flags because someone unplugged a USB drive.
5. **Resumable scans.** Persistent scan state per `scan_run`. If the API restarts mid-scan, resume from the last committed batch. Big libraries (10TB+) need this.
6. **Scan history.** `scan_run` table records every scan: started, completed, mode, trigger source, files walked / new / missing / errors. Surfaces *"your library is healthy because the last 7 scans were clean."*
7. **Path policy filters.** Per-library exclusion patterns beyond just `extras/` and `sample.*`. *"Don't scan `**/.recycle/**`."* Useful for Synology / unRAID setups with snapshot directories.
8. **Live progress with ETA.** [Realtime](../realtime/README.md)-driven UI: current file, files-per-second, percent complete, ETA. The "scan in progress" UX today is basically zero.
9. **Pause / resume scans.** A long scan can be paused (before a backup window, say) and resumed. Maps cleanly to the resumable-scan state machine.
10. **Scan-trigger audit log.** *"This scan started because the post-grab webhook fired."* Helps explain surprising scan results — the user sees the cause without digging.

## Interactions

| Neighbor                                              | How scan interacts                                                                                                |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| **[Matching](../matching/README.md)**                 | Scan emits `FileEvent`s for new files → `MatcherService.MatchBatch` consumes batches. Scan reads matcher's writes for dedup. |
| **[Hygiene](../hygiene/README.md)**                   | Scan opportunistically populates `hygiene_finding` rows during walks (broken hardlinks, orphan rows, phantom files, drift). |
| **[Hardlinks](../hardlinks/README.md)**               | Scan is the capture point: stamps `st_dev` / `st_ino` / `st_nlink` on `media_file_state` during the stats it already does; the exact `nlink` predicate replaces the old drift heuristic for broken-link detection. |
| **[Metadata](../metadata/README.md)**                 | New matches trigger enrichment downstream — scan itself is metadata-agnostic.                                      |
| **[Tracking / wants](../tracking/README.md)**         | A file appearing on disk (drop-in or scan-discovered) can satisfy an open want via matcher's drop-in flow. Scan doesn't talk to tracking directly. |
| **Import**                                            | Import is the *write* side; scan is the *read* side. Together they keep DB and FS in sync. Post-import targeted scan confirms the file landed. |
| **Downloader (qbittorrent etc.)**                     | Post-grab webhook fires a targeted scan. Future: attach torrent metadata (info-hash) to the resulting `media_file` rows. |
| **Plex / Jellyfin**                                   | Plex `library.new` webhook from Story 1 confirms a file actually appeared in Plex — orthogonal to scan, but they share the "file is now real" moment. |
| **MediaProbeWorker** (new)                            | Scan emits "needs probe" events; probe worker consumes async; populates ffprobe metadata into `media_file_probe`. |
| **IntegrityVerifyWorker** (new)                       | Walks no files; `stat()`s known files; updates `last_verified_at`; fires hygiene findings on stat errors.        |
| **[Realtime](../realtime/README.md)**                 | Live progress events (current file, files-per-second, ETA) emitted continuously during scan runs.                |

## Edge cases

A handful of FS realities that need explicit handling:

- **Hidden files / Apple metadata.** `._foo.mkv`, `.DS_Store`, `Thumbs.db` are pervasive on shared mounts. Filter at scan boundary.
- **Symlinks.** Follow with cycle detection. Skipping them misses legitimate setups (large libraries often use symlinks for organization).
- **Mount points and bind mounts inside library roots.** Don't cross filesystem boundaries by default; configurable per library.
- **In-progress downloads in library roots.** Hardlink staging often lives inside libraries. `.part` files, half-written MKVs. Filter by extension and by size-progressing detection (file growing across stat samples).
- **NFS / SMB latency.** A `stat()` on a network mount can take a second. Scan emits progress events continuously so the UI can show that work is happening even when the FS is slow.
- **Unicode normalization issues.** Filenames with combining characters can appear different between FS encoding and DB storage. Normalize at scan boundary (NFC).
- **Time-travel mtimes.** mtime can be wrong after a backup/restore. Diff scan's mtime heuristic can miss things. Document the "run a Full when in doubt" affordance; Verify covers a different angle.
- **Hardlinks across libraries.** Same inode, two paths in different libraries. Walking either should resolve to the same logical file. Probably out of scope for v1 — document as a known limitation.

## What scan writes (data shapes, sketched)

Iteration 1 doesn't pin column types, but sketching the touch points clarifies the model:

- `media_file` — added (on new discoveries), kept (on present files), unchanged (on missing — see state below)
- `media_file_state` — `file_exists`, `file_size`, `last_verified_at`, possibly `mtime` for future Diff heuristics, and `st_dev` / `st_ino` / `st_nlink` captured on every `stat()` (zero extra I/O — same `Stat_t`) for the [hardlinks](../hardlinks/README.md) reference graph
- `media_file.osdb_hash` — populated on first discovery; never recomputed unless drift detected
- `media_file_probe` (new table) — ffprobe output keyed on `media_file_id`; populated by `MediaProbeWorker`
- `scan_run` (new table) — per-scan audit row (mode, trigger, started/completed, counts, errors)
- `unmatched_file` — written via matcher, but scan's "new file" events feed it
- `hygiene_finding` — opportunistic writes when scan notices an integrity issue mid-walk

Data shapes resolve in iteration 2.

## Open questions

1. **Inotify scope.** Root-only watches with re-walk on event, or recursive watches? Lean root-only — the watch-count limit on Linux makes recursive watches a footgun on large libraries.
2. **Verify cadence.** Default hourly? Daily? Configurable? Trade-off is hygiene-finding latency vs FS load. Lean daily by default with sample-based stat'ing, configurable.
3. **ffprobe in scan or post-scan?** Lean post-scan via `MediaProbeWorker` — keep scan fast; metadata lags by minutes but that's fine for audit-style hygiene rules.
4. **Resumable-scan state granularity.** Per-file, per-directory, or per-batch? Per-batch (commit every N files) hits the sweet spot.
5. **Multi-root libraries vs library grouping.** Two design options:
   - **Multi-root**: a single library has N rootpaths; scan walks them in parallel. Simple from the user's POV ("Movies" is one library, scans all drives it lives on).
   - **Library grouping**: each rootpath is its own library; users can group libraries into a "library group" (or similar). More flexible but more concepts.
   Both work. Both have data-shape implications that touch beyond scan. Probably revisit when we draft the libraries spec; until then, scan v1 ships single-root and the model can absorb either choice later.
6. **Scan-run retention.** History is small (a few KB per run); keep forever with maybe a UI cap on what's displayed.
7. **Trigger priority and dedup.** Coalesce inotify events into a running scan if not yet past the affected directory; otherwise queue. Don't run back-to-back scans of overlapping scope.
8. **Probe worker re-run policy.** Probe data doesn't change for a static file. Re-probe only on explicit request, OR if a file's size/mtime drifts. Lean: on drift.
9. **Drive-offline detection heuristic.** Library root returning ENOENT/EACCES is the clear signal. Ambiguity (a single missing subdirectory) doesn't trigger; that's a normal "file was deleted" event.
10. **Walker concurrency.** Parallel walks across library roots (or grouped libraries) is obvious. Parallel walks *within* a root are harder (FS contention, ordering). Lean serial within a root for simplicity.
11. **Inotify "event mode" persistence.** When scan determines that inotify doesn't work on a root (no events, watch failures), where does that flag live? On the library/root config row, with a periodic re-test? Probably yes.
12. **[Realtime](../realtime/README.md) event vocabulary.** What progress events does scan emit? Probably: `scan_started`, `scan_progress` (with file path + counts), `scan_file_matched`, `scan_file_missing`, `scan_completed`, `scan_failed`. Concrete event shapes resolve in iteration 2.

## What we're explicitly not deciding here

- Exact table names, columns, indexes for `scan_run`, `media_file_probe`, or `media_file_state` extensions
- API endpoint shapes for scan triggers and progress queries
- [Realtime](../realtime/README.md) event payload shapes (vocabulary only, no payloads)
- Library multi-root vs grouping (deferred to libraries spec)
- ffprobe field schema — what we extract and how we store it (deferred to hygiene's data-shape pass)
- Hardlinks-across-libraries semantics
- The exact debounce window for inotify event coalescing
- Per-library inotify on/off configurability (probably a setting; UI deferred)

Each gets its own pass once this model holds up against real workloads.
