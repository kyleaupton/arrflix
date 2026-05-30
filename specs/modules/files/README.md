# Files — the physical media on disk

**Status:** Draft, iteration 1

This doc defines the **file**: the unit of physical media Arrflix tracks on disk, the identity it carries (or doesn't yet), and the filesystem facts observed about it. It captures _what a file is_, _how identity is modeled as state rather than as a separate table_, _what the state sidecar holds_, and _how the soft-delete lifecycle keeps the audit trail intact_. It commits the core data model; exact column types, indexes, and wire formats finalize in the data-shape pass.

This doc is the canonical owner of a concept four other specs already lean on without owning: [scan](../scan/README.md) writes file rows, [matching](../matching/README.md) sets their identity, [importer](../importer/README.md) creates them on the grab path, and [hygiene](../hygiene/README.md) / [hardlinks](../hardlinks/README.md) read them. None of them owns the **entity** itself — and the absence of an owner is why the v0 matched/unmatched split (`media_file` vs `unmatched_file`) propagated into the greenfield specs unexamined. This doc names the entity once and collapses that split.

## TL;DR

- A **file** is one physical media file under a [library](../libraries/README.md) root — `(library_id, path)` — tracked whether or not Arrflix knows what it is. One row per file on disk, identified or not.
- **Identity is nullable state on the file, not a second table.** `media_item_id` + `episode_id` carry the resolved identity; both NULL means "not identified yet." This replaces the v0 `media_file` (identified) / `unmatched_file` (orphan) split — "unmatched" becomes a query (`media_item_id IS NULL`), not an entity.
- **The identity columns mean one thing each, ratified:** `media_item_id` is always the **title** (movie or series); `episode_id` is the **episode leaf**, set only when resolved to episode granularity. Four states fall out (unidentified / movie / episode / series-but-episode-unknown), so **`partial_series` is derived**, not a stored flag.
- **Filesystem facts live in a 1:1 `file_state` sidecar** — `exists`, `size`, inode triple, `osdb_hash`, `last_verified_at` — as **typed columns, not JSONB**, because every field is grouped/filtered/indexed. Split from `file` because it's the hot, every-scan-rewritten data; identity and path are cold.
- **Files soft-delete (`deleted_at`), never hard-delete.** The row is the durable spine the [decision log](../../patterns/audit/README.md) and [hygiene findings](../hygiene/README.md) anchor to, so `match_decision.file_id` can be a real FK. The universal read filter is `WHERE deleted_at IS NULL`.
- **Re-match, un-match, and detach become `UPDATE`s, not row swaps.** The file id never changes across its life, which structurally eliminates the "id drifts across transitions / supersession chain splits" bug class.
- **Naming:** `media_*` is the abstract content layer ([media_item](../metadata/README.md) / season / episode); `file*` is the physical layer (`file`, `file_state`, `file_parse`, `file_probe`, `file_import`). The prefix is the namespace.

## What a file is, and isn't

A file answers: _there are bytes at this path under this library — what do we know about them?_ It is the join point between the filesystem (which has paths and inodes) and the catalog (which has titles and episodes).

A file is **not**:

- The content it represents — that's the [media_item](../metadata/README.md) (movie/series) and its season/episode structure. A file _points at_ a media_item; many files can point at one (different editions, a re-download).
- The decision that gave it its identity — that's [matching](../matching/README.md)'s `match_decision`. The file carries the _current_ identity; the log carries the _history_.
- The filesystem mechanics that placed it — hardlink/copy is [importer](../importer/README.md); the inode reference graph is [hardlinks](../hardlinks/README.md).
- A release or a download — those are acquisition-side concepts that _produce_ files via import.

## Why this is its own module

The same "consumers, no owner" shape that earned [parsing](../parsing/README.md) and [hardlinks](../hardlinks/README.md) their modules. Every spec that touches a file on disk hangs its facts off the file row, but none defines the row:

- [scan](../scan/README.md) — discovers files, reconciles presence, captures FS facts. Filesystem-aware, explicitly _not_ identity-aware.
- [matching](../matching/README.md) — resolves identity onto a file. Identity-aware, not filesystem-aware.
- [importer](../importer/README.md) — creates a file row on the grab path, with identity already known (closed-world).
- [hardlinks](../hardlinks/README.md) — groups files by inode; owns the _queries_, declares the inode _columns_ live on the file's state row.
- [hygiene](../hygiene/README.md) — findings over file/DB consistency; `identity/unmatched-file` is a query over file state.

Each owns a _slice_; the entity itself was ownerless, so its shape diffused across five docs and the v0 two-table split survived into the greenfield design without anyone deciding it should. Naming the file once gives that shape a home and lets the neighbors reference it instead of half-redefining it.

## The `file` table

```sql
-- indicative; exact types/indexes finalize in the data-shape pass
file (
  id            uuid PRIMARY KEY,
  library_id    uuid NOT NULL REFERENCES library,    -- ON DELETE CASCADE
  path          text NOT NULL,                        -- library-relative

  -- IDENTITY (all NULL ⇒ unidentified; see invariant below)
  media_item_id uuid REFERENCES media_item,           -- the TITLE (movie or series)
  episode_id    uuid REFERENCES media_episode,        -- the episode LEAF, when resolved
  edition       text,                                  -- "directors_cut", "extended"; refines a movie

  created_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL,
  deleted_at    timestamptz,                           -- soft-delete; NULL = live

  UNIQUE (library_id, path)                            -- among live rows
)
```

The row is deliberately lean: **location** (`library_id`, `path`), **identity** (`media_item_id`, `episode_id`, `edition`), **lifecycle** (`created_at` / `updated_at` / `deleted_at`). Everything else — filesystem facts, the parse, the probe, import history — lives in sidecars keyed on `file.id`.

### Identity as state, with a ratified invariant

The identity granularities a file can have, and the column shape each needs:

| State | `media_item_id` | `episode_id` | Notes |
| ----- | --------------- | ------------ | ----- |
| **unidentified** | NULL | NULL | In the matcher inbox; nothing resolved yet. |
| **movie** | → movie | NULL | A movie file. |
| **episode** | → series | → episode | Fully resolved; series derivable from `episode → season → media_item`. |
| **partial series** | → series | NULL | Series known, episode not yet pinned. |

`media_item_id` is not redundant with `episode_id`: the **partial-series** row has nowhere to point with `episode_id` alone, so the title pointer is load-bearing, not convenience. It also collapses the otherwise-wonky `file → episode → season → media_item` walk into a single hop for "all files of series X" (`WHERE media_item_id = X`).

> **Invariant.** `media_item_id` is always the top-level title; `episode_id` refines it to a leaf only when episode granularity is resolved. When `episode_id` is set, its season's `media_item_id` **must equal** the file's `media_item_id`. Enforced by a `CHECK`/trigger or the writer ([open question](#open-questions)).

Two consequences:

- **`partial_series` is derived, not stored.** It is exactly `media_item.type = 'series' AND episode_id IS NULL`. The v0 `unmatched_file.partial_series` flag is deleted.
- **"Identified" is `media_item_id IS NOT NULL`.** Consumers that only care about identified media (the library UI, [media-server](../media-server/README.md) propagation, [tracking](../tracking/README.md)) filter on it — more honest than the v0 implicit "if it's in `media_file` it's identified."

### Identity references the internal id, not a provider

`media_item_id` is an **internal** FK. The mapping from a media_item to TMDB / IMDb / TVDB ids is owned by [metadata](../metadata/README.md)'s `external_id` table, keyed off `media_item`. A file never carries a provider id — it points at the provider-agnostic internal item, so the [external-id work](../metadata/README.md) lands without touching files. This is the same seam the matcher's `metadata.Item` already honors.

### Inbox membership is a confidence question, not an identity one

Whether a file appears in the [matcher inbox](../matching/README.md#matcher-surfaces) is read from its **current `match_decision` band**, not from identity presence: a `confident_review` file _has_ a `media_item_id` but still wants a human glance. Identity presence (`media_item_id IS NULL`) and inbox membership (current decision band `< confident`) are orthogonal; the inbox unions them. This is why the v0 split was the wrong cut — the inbox already had to span both tables.

## The `file_state` sidecar — filesystem facts

```sql
file_state (
  file_id          uuid PRIMARY KEY REFERENCES file,   -- 1:1, ON DELETE CASCADE
  exists           boolean NOT NULL,
  size_bytes       bigint,
  mtime            timestamptz,                          -- diff-scan heuristic
  osdb_hash        text,                                 -- content fingerprint
  st_dev           bigint,
  st_ino           bigint,
  st_nlink         integer,
  last_verified_at timestamptz,
  updated_at       timestamptz NOT NULL
)
-- INDEX (st_dev, st_ino)                 hardlink reference graph (hardlinks spec)
-- INDEX (osdb_hash)                      dedup + drop-in re-identification by hash
-- INDEX (file_id) WHERE exists = false   orphan-row detection (hygiene)
```

**Typed columns, never JSONB.** Every field is queried: [hardlinks](../hardlinks/README.md) does `GROUP BY (st_dev, st_ino)`, [hygiene](../hygiene/README.md) dedup does `GROUP BY osdb_hash`, [scan](../scan/README.md) reconciliation does `WHERE exists = false`. The schema-wide rule of thumb:

> **Columns** when you filter / join / group / index it. **JSONB** when you store-and-display it as an opaque blob.

**A separate row, not columns on `file`,** because it is the _hot_ data — rewritten on every scan / verify (`exists`, `last_verified_at`, `nlink`) — while the file's identity and path are cold. Splitting them keeps the identity row and its indexes from churning on every stat pass. (At single-install scale, columns-on-`file` would also work; the sidecar is chosen for the hot/cold split and to match how [scan](../scan/README.md#what-scan-writes-data-shapes-sketched) and [hardlinks](../hardlinks/README.md#the-model) already describe these facts.)

`osdb_hash` lives here, with the other stat-derived facts, because it's captured during the same read pass — and because it must exist for **every** file, identified or not, for hash-based drop-in re-identification and duplicate detection to work on inbox files (a gap the v0 `media_file.osdb_hash`-only placement left open).

The `st_dev` / `st_ino` / `st_nlink` columns are declared here but their _semantics and query surface_ are owned by [hardlinks](../hardlinks/README.md): this spec says "the inode facts are columns on `file_state`"; hardlinks says "here is the reference graph over them."

## The sidecar family

Everything that is per-file but not core to location/identity/FS-presence hangs off `file.id` and is owned by its domain:

| Table | Cardinality | Holds | Owner |
| ----- | ----------- | ----- | ----- |
| `file_state` | 1:1 | FS facts (presence, size, inode, hash, verified-at) | **this spec** |
| `file_parse` | 1:1 | persisted parse: raw title, parsed `Quality`/`Release` (JSONB), `parser_version`, `origin` (`grabbed`/`scanned`/`manual`) | [parsing](../parsing/README.md#persisted-parse) |
| `file_probe` | 1:1 (lazy) | ffprobe output: scalar quality columns hygiene filters on + raw `streams` JSONB | [scan](../scan/README.md) / quality |
| `file_import` | 1:N | per-attempt import audit: `method` (`hardlink`/`copy`), `source`, `dest`, `success`, `error` | [importer](../importer/README.md) / [audit](../../patterns/audit/README.md) |

One head table makes the ownership legible: a reader sees `file_*` and knows it's a per-file fact, and the table comment points at the owning spec. JSONB is appropriate in `file_parse` (store-and-re-render) and the `streams` portion of `file_probe` (variable-length stream lists), per the columns-vs-JSONB rule above.

## Lifecycle

The file row is created once at discovery and lives — through every identity change — until it's detached. The **id is stable for the life of the physical file**; transitions mutate columns, they never swap rows.

```
                 scan / importer discovers bytes
                              │
                              ▼
                    ┌───────────────────┐
   matcher sets ───►│       file        │◄─── un-match clears identity
   identity         │  (id never moves) │     (UPDATE → media_item_id = NULL)
                    └─────────┬─────────┘
                              │ detach
                              ▼  (UPDATE → deleted_at = now())
                    ┌───────────────────┐
                    │  soft-deleted     │  row retained as audit anchor;
                    │  deleted_at set   │  excluded from all live reads
                    └───────────────────┘
```

| Action | Effect on `file` | Notes |
| ------ | ---------------- | ----- |
| **discover** | INSERT (identity NULL or set) | scan (open-world) or importer (closed-world, identity known). |
| **match / re-match** | UPDATE identity columns | + a `match_decision` row. The file id is unchanged, so the decision chain stays continuous. |
| **un-match** | UPDATE identity → NULL | + a `no_match` decision. The file stays on disk and in the table; only identity clears. |
| **detach** | UPDATE `deleted_at` | + a `detached` decision; optional quarantine move on disk. The row survives as the audit anchor. |

This is the prize of unifying the two v0 tables: un-match was a `delete media_file → create unmatched_file (new id)` swap that severed the `match_decision.file_id` join. As an in-place `UPDATE`, the entire class of id-drift / chain-split bugs is **structurally impossible**.

## Soft-delete

Files are **soft-deleted, never hard-deleted.** A `deleted_at` timestamp marks a row as gone from the library's scope; the row itself persists.

- **Why.** The file row is the spine that `match_decision`, `hygiene_finding`, and import history anchor to. If files hard-deleted, those audit trails would dangle (or cascade away the history we want to keep). Soft-delete lets `match_decision.file_id` be a real FK with integrity intact — the reason the [audit pattern](../../patterns/audit/README.md) otherwise reaches for FK-less logical references.
- **The rule.** Every live read filters `WHERE deleted_at IS NULL`. The `UNIQUE (library_id, path)` constraint applies among live rows (a path freed by a detach can be re-discovered into a new file row).
- **Retention.** How long detached rows persist before genuine purge is a [retention](../hygiene/README.md#retention--cleanup-the-home-for-request-retention) concern, configured centrally per the [audit pattern](../../patterns/audit/README.md), not decided here.

## Relationship to the decision log

The file carries its **current** identity (denormalized columns, for the hot read path — library listings, joins); the `match_decision` log carries the **history** (append-only, superseded chain). Standard "current state on the entity, history in the log" split, and the reason the file's identity is columns rather than a derived join through the decision log.

`match_decision.file_id` is a real FK to `file.id` (safe because files soft-delete). Suggestions for an unidentified file are **not** file state — they are the current decision's `ranked_candidates`, read from `match_decision`. The v0 `unmatched_file.suggested_matches` column is deleted; it duplicated the decision's ranked output.

## Interactions

| Neighbor | How it interacts |
| -------- | ---------------- |
| [Scan](../scan/README.md) | Discovers files (INSERT), reconciles presence and FS facts into `file_state`, computes `osdb_hash`. Its two-pass diff lists _files_, no longer "media_file ∪ unmatched_file". |
| [Matching](../matching/README.md) | Sets / clears / re-points identity columns (UPDATE) and writes the `match_decision`. Consumes `file.id` as the stable `match_decision.file_id`. |
| [Importer](../importer/README.md) | Creates file rows on the grab path with identity already known; writes `file_state` + `file_import`. |
| [Hardlinks](../hardlinks/README.md) | Owns the inode reference-graph queries over `file_state.(st_dev, st_ino, st_nlink)`; this spec owns the columns. |
| [Hygiene](../hygiene/README.md) | `integrity/orphan-db-row` (over `file_state.exists`), `identity/unmatched-file` (over `media_item_id IS NULL`), `layout/duplicate-files` (over `osdb_hash`) are all queries against files. |
| [Metadata](../metadata/README.md) | Owns `media_item` and the `external_id` provider mapping the file's `media_item_id` resolves through. The file references the internal id only. |
| [Libraries](../libraries/README.md) | `file.library_id` scopes every file to a root; deleting a library cascades its files (the rows; bytes on disk untouched). |
| [Tracking](../tracking/README.md) | A drop-in file that resolves to a wanted identity satisfies the want; tracking reads identified files, never touches the file row directly. |
| [Audit](../../patterns/audit/README.md) | `match_decision` and `file_import` anchor to the soft-deleted-not-purged file row; retention is centralized. |

## Migration from v0

v0 (and the just-built matcher) carry two tables — `media_file` (identified) and `unmatched_file` (orphan) — plus `media_file_state` / `media_file_import`. Since the install has no users besides Kyle, the migration drops and recreates rather than transforming rows.

What changes:

- **`media_file` + `unmatched_file` → `file`.** Identity (`media_item_id`, `episode_id`) goes nullable; `unmatched_file` is gone.
- **`media_file_state` → `file_state`**, absorbing `osdb_hash` (was a `media_file` column) for all files.
- **`media_file_import` → `file_import`**; `media_file_parse` (planned) → `file_parse`; `media_file_probe` (planned) → `file_probe`.
- **`unmatched_file.suggested_matches` → `match_decision.ranked_candidates`.** Suggestions move to the decision log.
- **`unmatched_file.partial_series` flag → derived.** Dropped.
- **`match_decision.file_id` → real FK to `file.id`.** The matcher's persist paths thread the file id into the (now single) row rather than minting an id that never reconnects.
- **The `media_file_*` → `file_*` rename** establishes the `media_*` = content / `file*` = physical namespace split.

The content layer (`media_item`, `media_season`, `media_episode`) is **unchanged** — it isn't rebuilt, and the `media_` prefix now reads as a deliberate "content" namespace rather than noise.

## Open questions

1. **Identity-invariant enforcement.** `episode_id`'s series must equal `media_item_id`. `CHECK` constraint (can't easily reach across the season FK), trigger, or writer-enforced? Lean: writer-enforced in the match-commit path + a periodic hygiene consistency check, escalate to a trigger only if drift shows up.
2. **Current-band denormalization.** Inbox membership reads the current `match_decision` band via the existing `match_decision_current` partial index. If the inbox listing is slow, denormalize `current_outcome` / `current_confidence` onto `file`. Lean: join the decision for now (single source of truth); denormalize only on a measured need.
3. **`file_state`: columns vs companion.** Coordinated with [hardlinks OQ#1](../hardlinks/README.md#open-questions) — inode facts as columns on `file_state` (lean) vs a `file_inode` companion if the row grows unwieldy. Same row, same refresh; revisit only under pressure.
4. **`edition` shape.** Free-text in v1 (`"directors_cut"`); graduate to an enum (`theatrical`/`extended`/`directors_cut`/`unrated`/`other`) + nullable free-text companion when [edition-aware matching](../matching/README.md#killer-ux-moves) ships. Lean: enum + free-text companion, deferred.
5. **Soft-delete as a pattern.** Today only `file` soft-deletes. If the `deleted_at` + universal-filter convention spreads to other entities, _that convention_ (not this table) is a candidate for a `patterns/soft-delete` doc. Do not pre-abstract; extract on the second consumer.
6. **Cross-library hardlinks.** Same inode reachable from two library roots produces two `file` rows (one per `(library_id, path)`), correctly sharing `(st_dev, st_ino)`. The graph resolves them as one inode; whether the UI treats them as one logical file is [scan](../scan/README.md#open-questions)'s parked question, not decided here.
7. **Detach retention window.** How long soft-deleted file rows persist before genuine purge — forever (audit) or trimmed after N? Defer to the central [retention](../../patterns/audit/README.md) configuration.

## What we're explicitly not deciding here

- Exact column types, index shapes, and constraint mechanics for `file` / `file_state`.
- The `file_parse` / `file_probe` schemas — owned by [parsing](../parsing/README.md) and [scan](../scan/README.md)/quality respectively.
- API endpoint shapes for reading files or driving identity transitions ([matching](../matching/README.md) surfaces own those).
- The `media_item` / `external_id` provider-mapping contract — [metadata](../metadata/README.md).
- The retention TTL for soft-deleted rows — the central [audit](../../patterns/audit/README.md) retention surface.
- Whether `current_outcome` is denormalized onto `file` (perf-driven, OQ#2).

## Doc neighbors

- [Matching](../matching/README.md) — resolves identity onto files; `match_decision.file_id` is `file.id`.
- [Scan](../scan/README.md) — discovers files and reconciles `file_state`.
- [Importer](../importer/README.md) — creates files on the grab path; owns `file_import`.
- [Hardlinks](../hardlinks/README.md) — the inode graph over `file_state`'s inode columns.
- [Hygiene](../hygiene/README.md) — findings over file/DB consistency.
- [Metadata](../metadata/README.md) — owns `media_item` and provider id mapping.
- [Libraries](../libraries/README.md) — the roots files are scoped to.
- [Parsing](../parsing/README.md) — owns the persisted parse in `file_parse`.
- [Audit](../../patterns/audit/README.md) — the decision/import trails files anchor.
