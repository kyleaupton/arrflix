# Path-mapping — one folder, many vantage points

**Status:** Draft, iteration 1

Arrflix's backend filesystem is the **canonical reference frame** — a want reaches `available` when Arrflix verifies the file _on its own disk_ ([acquisition](../acquisition/README.md#media-server-propagation-decoupled-from-available)). But every other actor in a deployment — each [downloader](../downloaders/README.md), each [media server](../media-server/README.md) — may see the _same bytes_ at a _different path_, because containers and hosts mount the same storage at different locations. This spec defines the translation layer between each actor's frame and Arrflix's: the **volume** model, the resolver that maps any path between frames, and the detection that makes the common case configure itself.

It is the explicit home for what the rest of the system has so far assumed (downloader → import) or bolted on (the media-server path-mapping override). It also turns the project's hardlink-first storage strategy into something Arrflix can _verify_ rather than hope for.

## TL;DR

- A **volume** is one logical storage location — one filesystem, one hardlink boundary — that multiple actors reach by different paths. It holds a **canonical Arrflix base path** plus an optional **alias** per actor (`{ actor, base, flavor }`), where an actor is a configured [downloader](../downloaders/README.md) or [media server](../media-server/README.md). Arrflix is the anchor, not a peer.
- **Most installs need exactly one volume.** A shared `/data` mount (the TRaSH convention) collapses every alias to identity — the layer is invisible and configures itself.
- The resolver is three composable operations — `toCanonical`, `fromCanonical`, `translate(from, to, path)` — plus a `device()` check. Every cross-frame path question in the system is one `translate` call against this table.
- A path is matched to its volume by **longest-prefix inference** on the relevant actor's base. **Libraries and download dirs are unchanged** — they stay "just a path"; the volume layer sits _underneath_ as a resolver. No FK is added to libraries.
- **Four boundaries** consume it: downloader→Arrflix (import), Arrflix→downloader (`SavePath`), media-server→Arrflix (correlation), Arrflix→media-server (targeted refresh). All four are the same `translate` call.
- **Hardlink feasibility is a first-class output.** `(st_dev, st_ino)` proves "same file"; `st_dev` equality proves "hardlink-safe." Both are cheap `stat` calls — Arrflix can tell you _before_ an import whether it will hardlink or fall back to a slow full copy.
- **Auto-detection is an assist, never a gate.** Three tiers: detect-need (trivial, always), propose (Plex hands you its section paths; downloaders correlate by inode), auto-apply (**only** when inode-proven). It writes to the same table manual authoring does and never clobbers a hand-entered value.
- **Manual authoring is first-class.** Author a volume by hand, with a live translation preview and a `Test` action that _proves_ the mapping by inode rather than assuming it. The operator chooses the posture: fully manual, assisted, or hands-off.
- A volume's alias is a [connectivity-health](../../patterns/connectivity-health/README.md) input: a downloader that is reachable but whose paths don't resolve is _reachable-but-unusable_, the same shape as `auth_failed`.

## Why this is its own spec

Three reasons it can't live on the existing connection specs:

1. **It spans actors.** A volume is shared by Arrflix _and_ a downloader _and_ a media server. "Downloads and library are on different mounts" is not a fact about any single connection, so it can't live on the downloader edit screen the way Sonarr's per-downloader "Remote Path Mappings" do. It needs a layer that sits below all of them.
2. **It's already being solved piecemeal, inconsistently.** Import implicitly assumes the downloader's `content_path` is directly accessible (a shared mount). The [media-server spec](../media-server/README.md#correlation-mapping-a-server-item-back-to-our-file) proposes a per-`(library, media_server)` path-mapping override. Those are the _same problem_ solved two different ways. This spec unifies them: the media-server override becomes a volume lookup and disappears as a bespoke field.
3. **It's the natural home for the hardlink-correctness guard** — the thing that makes the project's "efficient storage" promise real instead of aspirational. That reasoning is cross-actor (download dir vs library) and belongs nowhere else.

## The model

### What a volume owns

A volume row encodes:

- **Name** — display name, unique (case-insensitive).
- **Arrflix base** — the canonical absolute path as Arrflix sees it (e.g. `/data`). The anchor. Validated reachable + directory at create/update, like a [library root](../libraries/README.md#validation).
- **Aliases** — zero or more `{ actor, base, flavor }` entries. `actor` references an existing [downloader](../downloaders/README.md) or [media server](../media-server/README.md) row. `base` is the path _that actor_ sees for this same storage (e.g. qBittorrent's `/downloads`, Plex's `/media`). `flavor` captures path semantics (separator, case-sensitivity, drive-letter) for non-POSIX actors — see [edge cases](#edge-cases). Storage shape (child table vs JSON) deferred to iteration 2.
- **Created / updated timestamps.**

A volume corresponds to **one filesystem**. That is the load-bearing invariant: everything under a volume's Arrflix base is on one device, so anything within it is hardlink-compatible with anything else within it. Arrflix can _verify_ this (`device()` of the base), not just assert it.

There is no media type, no `enabled`/`default` flag, no quota. A volume is a translation + device-identity fact, not a destination — that axis is owned by [libraries](../libraries/README.md).

### The resolver

Three operations, everything composes from them (conceptual; exact signatures deferred to iteration 2):

```
toCanonical(actor, path)          → (volume, relpath)   // strip the actor's base prefix
fromCanonical(volume, rel, actor) → path                // prepend the actor's base prefix
translate(from, to, path)         = fromCanonical(toCanonical(from, path)…, to)

device(arrflixPath)               = stat(path).st_dev   // the hardlink-boundary check
sameFile(arrflixPath, externalReportedFile) = (st_dev, st_ino) match
```

**Inference, not wiring.** `toCanonical` finds the volume by **longest-prefix match** on the given actor's base. The actor is _always known at the call site_ — a qBittorrent `content_path` is qBit-frame by definition; a Plex webhook is Plex-frame — so the actor is never guessed from the string. Longest-prefix correctly resolves nested mounts (`/data` and `/data/torrents` as two volumes → `/data/torrents/x` matches the longer base).

Because resolution is inference, **nothing changes in the [libraries](../libraries/README.md) or [scan](../scan/README.md) specs.** A library root and a download scratch dir are both just Arrflix-frame paths that resolve _through_ the volume layer; they don't reference it.

### The four boundaries

Every cross-frame path question is one `translate` call:

| # | Boundary | Call | Example |
| - | -------- | ---- | ------- |
| 1 | Downloader → Arrflix (import) | `translate(qbit, arrflix, content_path)` | `/downloads/torrents/X/X.mkv` → `/data/torrents/X/X.mkv` — then hardlink into `/data/movies/…` |
| 2 | Arrflix → Downloader (`SavePath`) | `translate(arrflix, qbit, save_dir)` | `/data/torrents` → `/downloads/torrents` — set as [`AddRequest.SavePath`](../downloaders/README.md#addrequest-shape) |
| 3 | Media server → Arrflix (correlation) | `translate(plex, arrflix, webhook_path)` | `/media/movies/X (2024)/X.mkv` → `/data/movies/X (2024)/X.mkv` — match `file.path` |
| 4 | Arrflix → Media server (targeted refresh) | `translate(arrflix, plex, import_dir)` | `/data/movies/X (2024)` → `/media/movies/X (2024)` — hand to Plex partial-refresh |

Boundary 3 is the case the [media-server spec](../media-server/README.md#correlation-mapping-a-server-item-back-to-our-file) currently solves with a per-`(library, server)` override. Here it is a volume lookup, so that bespoke field is **superseded** — correlation resolves the server-reported path to canonical and matches `file`, with the existing basename fallback unchanged.

### Hardlink feasibility — the first-class output

Arrflix is hardlink-first; hardlinks require one filesystem. The resolver makes feasibility a fact, not a hope:

- **Same file?** `sameFile` — `(st_dev, st_ino)` match _proves_ a candidate translation is correct, turning a heuristic into a certainty.
- **Hardlink-safe?** `device(download_dir) == device(library_root)` — equal device = hardlinks work; unequal = imports must fall back to a full copy (2× space, slow).

This is surfaced **before** the cost is paid: at routing/config time, when a downloader is paired with a library, Arrflix can report ✅ "hardlinks OK" or ⚠ "different filesystems — imports will copy." It also feeds a [hygiene](../hygiene/README.md) finding when an existing library's files turn out to be copies that _could_ have been links.

This layer answers the **predictive, single-file** question only ("will this pair link?"). The **retrospective, multi-path** question — what is currently linked to what, an inode's `nlink` reference count, which torrent holds it alive — is the [hardlinks](../hardlinks/README.md) reference graph, which _consumes_ `device()` / `sameFile()` here rather than reimplementing them. Clean split: path-mapping is stateless and before-the-fact; hardlinks is the captured graph, after-the-fact.

### Auto-detection — three tiers

Detection is an **assist**. It writes to the same table manual authoring does, only ever **fills empty** fields, and never overwrites a value the operator typed.

| Tier | What it does | Difficulty | Posture |
| ---- | ------------ | ---------- | ------- |
| 1. Detect **need** | `stat` the path each actor reports (qBit save path, Plex section location) against Arrflix's FS. Exists → shared mount, identity, nothing to configure. | trivial | always, riding the [connectivity-health](../../patterns/connectivity-health/README.md) probe |
| 2. **Propose** | Plex exposes its section locations directly → suffix-align to library roots. Downloaders correlate a real torrent's `content_path` + size/name against Arrflix's roots, confirm by inode → propose the prefix swap. | low (Plex) → medium (downloader) | when tier 1 fails: a pre-filled "Apply?" suggestion |
| 3. **Auto-apply** | Accept a proposal with no confirmation. | — | **only** when inode-_proven_; a suffix-only guess always requires confirmation, because a wrong mapping silently misplaces files |

The probes in tiers 1–2 are the **same API calls the connectivity-health worker already makes** — incremental cost is a couple of `stat`s and a suffix/inode helper, not a new subsystem.

**Hard parts** (where "fully automatic" frays, handled honestly rather than hidden):

- **Cold start** — no downloads yet → nothing to correlate for a downloader. Tier 1 still runs; if it fails, defer to "paths auto-verify on your first grab" or a one-time prompt, rather than guessing.
- **Ambiguous suffix** — non-distinctive trailing components could mis-propose. Mitigated by requiring the proposed local path to exist + be a directory, and preferring inode-proof over suffix-guess.
- **Permissions / cross-device** — proposed path un-stat-able, or resolves on a different device. Both are _detectable_ → a warning, never a silent break.

### Manual authoring — first-class

Auto-detection can be ignored entirely. Manual authoring writes the same rows and is exactly as authoritative.

The create flow is a blank form (no detection forced): name, the required stat-validated **Arrflix base**, and per-actor alias rows added by hand. Affordances that respect an operator who wants to drive:

- **Live translation preview** — as a base is typed, show what a sample actor path resolves to and whether it exists.
- **A `Test` action that _proves_, not assumes** — given a sample path, run the `sameFile`/`device` check and report ✓ "same file, same device, hardlinks OK" / ⚠ "resolves but different device → copy mode" / ✗ "doesn't exist." The system _confirms_ the operator's work; it doesn't second-guess it.
- **Overlap warnings** — two volumes with overlapping Arrflix bases are flagged rather than silently disambiguated.
- **Two entry points, one model** — author on the Path-mapping page, or declare an alias inline when configuring a connection ("qBittorrent saves to `/downloads`, which is volume `media`"). Both edit the same volume.

The operator therefore chooses among three postures over the _same_ data: **fully manual** (author + `Test` by hand), **assisted** (detection proposes, operator confirms), **hands-off** (detection + inode-proof auto-applies).

### Surfacing in the UI

- A dedicated sidebar entry, titled by **function** — leaning **"Path Mapping"** (matches *arr muscle memory; describes the job) — with each entry presented as a **shared location**. "Volume" is the model/code term, demoted from the headline so nobody must learn it to use the page. (Naming is an [open question](#open-questions); the audience runs Docker, so "Volumes" as the literal title is defensible.)
- The page is **detection-first**: green and mostly empty for the common case, an explicit `[+ Add location]` for the rare second mount.
- **Inline on each connection's Test/health result** — "✓ paths resolve, hardlinks OK" or "⚠ can't resolve save path → fix mapping," linking to the page. Path-resolution folds into the connectivity-health story rather than being a separate checkbox.

### Edge cases

- **Identity (true `/data`)** — all bases equal; `translate` is a no-op; feature invisible.
- **Path outside every volume** — admin-added torrent saving to `/tmp/foo`; `toCanonical` finds no volume → surfaces an "unmapped path" [error](../../patterns/errors/README.md) (a hold-fix-retry case, not a silent break).
- **Cross-device after mapping** — resolves but `device()` differs → import still works via copy; warn, don't block.
- **Windows actor** — a downloader on Windows saving `D:\torrents\X` needs a `flavor` (separator `\`, drive-letter, case-insensitive) on its alias so `toCanonical` normalizes before emitting a forward-slash Arrflix path.
- **Nested / overlapping bases within one actor** — resolved by longest-prefix; exact duplicates flagged at save.

## What path-mapping does NOT own

- **The destination decision** — where media of a type goes, and what the scanner walks ([libraries](../libraries/README.md)).
- **The rendered path of a file** — folder/filename structure ([name-templates](../name-templates/README.md)).
- **Hardlink/copy _mechanics_** — the actual `link()`/copy during import (import, near [scan](../scan/README.md)). This spec supplies the _verdict_ (will it link?) and the resolved local path; import does the work.
- **The connectivity-health pattern itself** — worker, hysteresis, audit hook ([pattern](../../patterns/connectivity-health/README.md)). Path-resolution is an _input_ to a producer's probe, not a new pattern.
- **The downloader/media-server connection rows** — those are owned by their specs; a volume alias _references_ them.
- **Routing decisions** — routing picks downloader + library + template; it may _read_ the hardlink verdict (see below) but the volume layer doesn't make routing choices.

## Interactions

| Neighbor | How it interacts |
| -------- | ---------------- |
| [Downloaders](../downloaders/README.md) | Aliases reference a downloader. Import resolves `content_path` (boundary 1); acquisition expresses `AddRequest.SavePath` (boundary 2) via `translate`. A downloader whose paths don't resolve is reachable-but-unusable. |
| [Media-server](../media-server/README.md) | Aliases reference a server. Correlation (boundary 3) and targeted refresh (boundary 4) go through `translate`. **Supersedes** the spec's per-`(library, server)` path-mapping override. |
| [Libraries](../libraries/README.md) | Library roots are Arrflix-frame paths resolved _through_ volumes by inference — no FK added. Hardlink verdict pairs a download dir against a library root. Interacts with libraries' multi-root question. |
| [Scan](../scan/README.md) / Import | Import resolves the downloader's reported path to canonical before hardlinking; uses `device()` to choose link-vs-copy. Correlation reuses scan's path knowledge. |
| [Acquisition](../acquisition/README.md) | Builds `SavePath` so downloads land same-device as the target library. A planned grab whose path can't resolve is held, not failed. |
| [Routing](../routing/README.md) | May read the hardlink verdict to avoid pairing a downloader + library that span filesystems (a validation or condition — decision lives with routing). |
| [Connectivity-health](../../patterns/connectivity-health/README.md) | Path-resolution is an input to the downloader/media-server probe; detection rides the same API calls. |
| [Errors](../../patterns/errors/README.md) | Unresolvable path → typed error (hold-fix-retry). |
| [Audit](../../patterns/audit/README.md) | Import decision log records the translation applied (`qbit:/downloads/X → /data/torrents/X via volume "media"`) — turns the hardest-to-debug *arr problem into a glance. |
| [Name-templates](../name-templates/README.md) | Deterministic output paths make boundary-3 correlation reliable. |
| [Users](../users/README.md) | `path_mapping.*` (or `volumes.*`) permissions gate CRUD/test, per the [permission-key model](../users/README.md#permissions). |

## Tables

**Owned by this spec** (shapes indicative; column types + alias storage deferred to iteration 2):

- **`volume`** — `{ id, name, arrflix_base, created_at, updated_at }`. The shared-location anchor.
- **`volume_alias`** — `{ volume, actor_type, actor_id, base, flavor }`. One per actor that sees the volume. (Child table vs JSON-on-`volume` is an [open question](#open-questions).)

**Referenced, owned elsewhere:**

- **`downloader`** / **`media_server`** — [downloaders](../downloaders/README.md) / [media-server](../media-server/README.md). An alias references one.
- **`library`** root paths, `file.path`, `download_job.content_path` — resolved _through_ the volume layer; not owned here.

## Open questions

1. **Entity vs page naming.** Model term "volume"; page leaning "Path Mapping"; prose "shared location." Audience runs Docker, where "volume" is daily vocabulary _but_ means one container's mount (related, not identical). Lean: function-named page, "volume" demoted to model/code. Revisit if user testing says "Volumes" lands better as the title.
2. **Alias storage.** Child table (`volume_alias`) vs JSON array on `volume`. Lean: child table — aliases FK to connection rows and want cascade semantics; JSON loses the FK. Iteration-2 detail.
3. **Inference vs explicit FK.** Resolve a path's volume by longest-prefix (proposed) vs adding `volume_id` to libraries/download dirs. Lean: inference — keeps libraries unchanged. Revisit only if a real path legitimately belongs to two volumes (it can't, physically).
4. **Auto-apply threshold.** Tier 3 gated on inode-proof; suffix-only always confirms. Confirm that's the right line, or allow operators to opt into "trust suffix matches."
5. **Cold-start UX.** No downloads yet → can't correlate a downloader. Prompt at connection-add, or defer to first grab with a "will verify then" note? Lean: defer + note; don't block setup.
6. **Path flavor scope.** Model `flavor` now (separator/case/drive-letter) for a future Windows-side actor, or defer until a non-POSIX actor exists? Lean: reserve the field, leave it POSIX-only in v1.
7. **Cross-device after mapping — warn vs block.** Import still works via copy. Lean: warn (it's a legitimate, if wasteful, setup); never block an import that can succeed.
8. **Actor set.** v1 actors are downloaders + media servers. Do blackhole/watch-folder paths or future indexer-side paths become actors too? Lean: same model, add when those land.
9. **Relationship to libraries OQ#4** (`import_task` carries `library_root_path` string, not `library_id`). Does resolving through volumes supersede that rework, or do they land together? Lean: coordinate — both touch how import resolves paths.
10. **Multi-root libraries** ([libraries OQ#1](../libraries/README.md#open-questions)) spanning more than one volume/device complicates the hardlink verdict (which root, which device?). Flagged; resolve alongside the multi-root decision.
11. **Where the device/inode probe runs.** Health worker (continuous), on-demand `Test` (operator-triggered), and import-time (authoritative) all read it. Confirm all three are read-paths against the same helper, with no persisted staleness traps.

## What we're explicitly not deciding here

- Exact column types, the `volume_alias` storage shape, and table names (iteration 2).
- API route shapes / OperationIDs for volume CRUD / test / detect (iteration 2).
- UI layout — the page, the inline connection badge, the live-preview component (iteration 2).
- The precise suffix-alignment + inode-correlation algorithm (implementation detail).
- Windows / non-POSIX actor support beyond reserving `flavor`.
- Connectivity-health internals (worker, hysteresis, audit row shape) — owned by the [pattern](../../patterns/connectivity-health/README.md).
- The link-vs-copy import mechanics — owned by import.
- Routing's "same-volume" validation/condition grammar — owned by [routing](../routing/README.md).

## Doc neighbors

- [Downloaders](../downloaders/README.md) — boundaries 1–2; aliases reference downloader rows; reachable-but-unusable health input.
- [Media-server](../media-server/README.md) — boundaries 3–4; supersedes its per-`(library, server)` path-mapping override.
- [Libraries](../libraries/README.md) — roots resolve through volumes by inference; multi-root interaction; the hardlink verdict pairs against library roots.
- [Hardlinks](../hardlinks/README.md) — consumes this spec's `device()` / `sameFile()` primitives for the retrospective reference graph; predictive-vs-retrospective split.
- [Scan](../scan/README.md) — shares the file-on-disk truth; import resolves + chooses link-vs-copy.
- [Acquisition](../acquisition/README.md) — builds `SavePath`; holds a grab whose path can't resolve.
- [Routing](../routing/README.md) — may read the hardlink verdict to avoid filesystem-spanning pairings.
- [Connectivity-health](../../patterns/connectivity-health/README.md) — path-resolution is an input to the connection probe.
- [Errors](../../patterns/errors/README.md) — unresolvable path → typed hold-fix-retry error.
- [Audit](../../patterns/audit/README.md) — import decision log records the translation applied.
- [Name-templates](../name-templates/README.md) — deterministic paths underpin correlation.
- [Users](../users/README.md) — permission keys gate CRUD/test.
</content>
</invoke>
