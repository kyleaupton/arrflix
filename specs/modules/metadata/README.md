# Metadata — identity, structure, and refresh

**Status:** Draft, iteration 1

This doc defines how Arrflix knows *what* a movie or series is. It covers:

- **Identity** — how items map to upstream IDs (TMDB, IMDB, TVDB)
- **Item-level metadata** — title, overview, poster, runtime, status, etc.
- **Series structure** — the season/episode tree, including unaired episodes (the foundation gap)
- **The metadata source pattern** — raw upstream payloads stored verbatim
- **Refresh & staleness policy** — when do we re-fetch, and what triggers it
- **Local overrides** — user-edited metadata that survives refresh
- **Image handling** — paths + remote serving for v1

**Provider strategy** — which upstream sources we use, what each costs the user to enable, and how we stay independent of any one — lives in its own doc, [sources](../sources/README.md). This doc is the provider-agnostic *data model*; sources is the *provider seam* that feeds it. Where this doc once carried a `MetadataProvider` interface and a TMDB-named sync worker, that material has moved to [sources](../sources/README.md); what remains here is what we persist.

It does **not** pin down exact column types, indexes, or API shapes. As with the other specs, those come after the model survives more user stories.

## TL;DR

- **Identity** is split: the canonical writer's id (`tmdb`) is a typed column on `media_item` — it owns it, and it's the matching/dedup key. Secondary cross-reference namespaces (`imdb`, `tvdb`, `anidb` reserved) live in a separate **`external_id`** registry, one row per `(entity, source)` pair. `tmdb` is a reserved-valid `source` value but column-canonical in v1 — not stored in the registry.
- **Item-level metadata** is mostly already there (migration 0012). This doc back-fills the model and adds **movie collections** as a lightweight extension.
- **`series_type`** (`standard | daily | anime`) is the per-series numbering-regime anchor — the canonical side of [parsing](../parsing/README.md)'s release-side namespace tag. There is deliberately **no `movie_type`**: a movie has no parts to number, so no processing fork to anchor. See [Series type](#series-type-the-numbering-regime-anchor).
- **Series structure sync** is new and load-bearing: the [refresh engine](../sources/README.md#the-refresh-engine) (provider-agnostic scheduler + dispatched provider operations) pulls the full season/episode tree, including **unaired episodes**, so tracking and smart scheduling have something to operate on.
- **Local overrides** are designed in now (model + read-time precedence), UI later.
- **Refresh policy** is cadence-by-state: in-production weekly, recently-aired daily, ended monthly, manual immediate. Background worker drives the queue.
- **Images** continue to load direct from TMDB CDN in v1; local cache is deferred.
- **Provider strategy lives in [sources](../sources/README.md).** This doc stays provider-agnostic: one canonical writer (TMDB) owns the columns, consumers see an internal domain type, and **raw-payload retention is the portability mechanism** — replacing a provider is a one-adapter job, not a rewrite. Roles are segregated seams, not one fat interface.
- People (cast/crew) and per-user language preference are explicitly **out of scope**.

## Identity

Identity splits along the canonical-writer line. The primary `tmdb_id` is categorically different from the secondary namespaces: it is both the canonical writer's handle (TMDB owns `media_item` columns) and the matching/dedup key the rest of the system keys on. The secondaries (`imdb`, `tvdb`, future `anidb`) are cross-reference mappings nothing dedupes on. The model reflects that split rather than flattening both into one registry.

**`tmdb_id` stays a typed column.** `media_item.tmdb_id` (with `UNIQUE(type, tmdb_id)`) and `media_episode.tmdb_id` (partial-unique `idx_media_episode_tmdb`) remain exactly as they are — the canonical writer owns them, and they are the matching key. `tmdb` is a **reserved-valid `source` value** in the registry vocabulary, but it is **column-canonical in v1**: it is not dual-written into the registry. No v1 consumer needs it there; adding it later is a one-time backfill against a real consumer.

**Secondary namespaces live in the `external_id` registry:**

- One conceptual table per indexed entity (likely two tables in practice — `media_item_external_id` and `media_episode_external_id` — to avoid polymorphic FKs, but the shape is the same). **In v1 only `media_item_external_id` ships** — episode-level external ids have no writer or consumer yet, so that table is deferred until an episode-cross-ref consumer appears (it adds the second table, same shape).
- Each row carries `(entity_id, source, external_id)`. `external_id` is TEXT — it generalizes imdb (`tt…` strings) and tvdb (ints rendered as strings).
- `source` is drawn from an **open registry**: today `imdb`, `tvdb`, with `anidb` reserved (and `tmdb` reserved-valid but column-canonical, above). Adding a source is a config + code change in the provider layer, not a schema change.
- Unique on `(entity_id, source)` — one ID per source per entity.
- Indexed on `(source, external_id)` for the common reverse lookup ("find me the media_item with TVDB id 12345"). The lookup *query* lands with its only consumer (acquisition); the index is ready now.

**Canonical identity:** in v1, **TMDB is the canonical primary source** — it owns the writes to `media_item` columns, including `tmdb_id`. Most `media_item`s have a TMDB id; absence is allowed (manually-created stubs, items whose upstream record was deleted) but unusual. The secondary sources (IMDB, TVDB) are read-only cross-reference mappings — useful at search time, but they don't write columns.

The canonical-source designation is a **configuration choice, not a code assumption** — see [Provider abstraction](#provider-abstraction).

**Cross-source resolution:** the [acquisition](../acquisition/README.md) pipeline may need a TVDB ID at indexer-search time (some indexers index series by TVDB). This is a single SELECT against `media_item_external_id` for `(media_item_id, source: tvdb)`. The [series-structure sync operation](../sources/README.md#the-refresh-engine) populates TVDB and IMDB IDs from TMDB's `external_ids` endpoint as part of series sync.

**ID namespaces are not providers.** IMDB and TVDB appear here as `source` values — *id namespaces* used for cross-referencing — even though neither is a [provider](../sources/README.md#identity-id-namespaces-vs-providers) we fetch metadata from. IMDB has no usable public API; its only presence in the system is an `imdb_id` in this registry, arriving via TMDB. Being an id namespace is not the same as being a provider.

**Migration path:** only the secondary `imdb` / `tvdb` columns moved into the registry — `media_item.imdb_id` and the dead `media_episode.tvdb_id` were dropped, their values now sourced into `media_item_external_id` by enrichment. `tmdb_id` stays a column on both tables; there is no "backfill then drop the tmdb columns" step. The `media_item_external_id` table ships now; the episode table ships when a consumer for episode-level cross-refs appears.

**Optional `confidence` field:** for low-confidence auto-matches (the unidentified-file flow assigning a likely match), we may want to record confidence per mapping. Pin in iteration 2 — flagged in open questions.

## Item-level metadata

The shape exists today (migration 0005 + 0012). This doc captures it for completeness.

| Field                  | Source                 | Notes                                                       |
| ---------------------- | ---------------------- | ----------------------------------------------------------- |
| `title`                | TMDB                   | Localized to system locale (`en-US` for v1)                  |
| `year`                 | TMDB release date      |                                                             |
| `type`                 | (static)               | `movie` or `series`                                         |
| `overview`             | TMDB                   |                                                             |
| `poster_path`          | TMDB (relative path)   | Served from TMDB CDN; see [Images](#image-handling)         |
| `backdrop_path`        | TMDB (relative path)   |                                                             |
| `vote_average`         | TMDB                   |                                                             |
| `vote_count`           | TMDB                   |                                                             |
| `runtime`              | TMDB (movie); TMDB episode_run_time[0] (series fallback) | Series runtime is fuzzy; per-episode is the source of truth |
| `status`               | TMDB → canonical       | **Locked** canonical set: `upcoming` · `released` · `continuing` · `ended` · `canceled` · `unknown`. TMDB's strings are mapped down at the provider boundary (`model.CanonicalizeStatus`); the column stores the canonical token. See [Canonical status](#canonical-status). |
| `certification`        | TMDB content ratings   | US rating extracted (configurable later)                    |
| `genres`               | TMDB                   | Stored as JSONB array of `{tmdb_id, name}`                   |
| `release_date`         | TMDB (movie release / series first_air_date) |                                            |
| `last_air_date`        | TMDB (series only)     |                                                             |
| `in_production`        | TMDB                   |                                                             |
| `metadata_updated_at`  | (set by enrichment)    | Drives staleness queries                                    |
| `collection_id`        | TMDB (movie only)      | **New, lightweight**: when a movie is part of a TMDB collection, store the ID + name |
| `collection_name`      | TMDB                   | Denormalized for fast display                                |
| `series_type`          | derived (anime map) / user override | **New**: `standard` / `daily` / `anime`; series only. The numbering-regime anchor — see [Series type](#series-type-the-numbering-regime-anchor) |

**Movie collections:** TMDB groups movies into collections (MCU, Star Wars Saga, etc.). Per the discussion, we store **collection_id + collection_name** denormalized on `media_item` for v1 — enough to render "part of: MCU" on a focus page and to query "all MCU movies." We do **not** create a first-class `collection` table; if richer collection features emerge (collection overview, collection poster), we promote later.

**The canonical `tmdb_id` is a column; secondary external IDs are not** — see [Identity](#identity).

### Canonical status

`status` carries a **locked, source-agnostic** lifecycle vocabulary, not TMDB's wording. The provider operation maps TMDB's strings down to this set at the boundary (`model.CanonicalizeStatus`, applied at every TMDB-status read/write site), so UI labels decouple from TMDB's phrasing and the [refresh engine](../sources/README.md#the-refresh-engine)'s `(state) → TTL` cadence policy keys on a stable set.

| TMDB string | canonical |
| ----------- | --------- |
| `Released` | `released` |
| `Returning Series` | `continuing` |
| `Ended` | `ended` |
| `Canceled` | `canceled` |
| `Planned`, `Rumored`, `In Production`, `Post Production`, `Pilot` | `upcoming` |
| `""` / anything unmapped | `unknown` |

UI labels: Upcoming / Released / Continuing / Ended / Canceled; `unknown` renders no chip. A genuinely-empty raw stores NULL (keeping "never enriched" distinguishable); a non-empty-but-unmapped raw stores `unknown`. A migration-0012 `CHECK` constraint pins the column to this set. `In Production`→`upcoming` is the one judgment call — overwhelmingly a not-yet-released signal.

### Series type (the numbering-regime anchor)

`series_type` (`standard` / `daily` / `anime`) is a per-series classification on `media_item`. It is **not a media type** — `type` stays `movie | series`. It is a second, orthogonal axis: anime and standard shows are both series, and there are anime *movies* too, so "anime" can never be a value alongside movie/series. What it captures is the **numbering regime** an episode tree uses, because that is the one thing that genuinely forks processing:

| `series_type` | Episode numbering | Example |
| ------------- | ----------------- | ------- |
| `standard`    | season/episode (`S02E05`) | Breaking Bad |
| `daily`       | date-based (`2024-01-15`)  | The Daily Show, late-night, news |
| `anime`       | absolute (`1071`)          | One Piece |

This is the canonical (series-side) declaration of which namespace a release lives in; [parsing](../parsing/README.md) tags the release-side namespace it observed, and [matching](../matching/README.md#the-resolver-catalog)'s `episode-numbering` resolver reconciles the two using the anime [`NumberingMap`](../sources/README.md#anime-embedded-data-vs-api). It pairs with the [`absolute_number`](#open-questions) column: `absolute_number` is the data, `series_type = anime` is the flag that says interpret via it.

**It lives at the series, not the library.** Mixed roots — a standard show, an anime, and a daily show side by side under `/mnt/series/` — are the norm and need no per-library config: [scan](../scan/README.md) is identity-unaware and uniform, and `series_type` is applied only *after* identity resolves. A series is classified `anime` automatically when its TMDB/TVDB id is present in the `NumberingMap`; `daily` defaults off with a possible light heuristic; the only UI is a per-series override for the rare miss (the proven Sonarr model). [Sources](../sources/README.md#anime-embedded-data-vs-api) owns the mapping mechanics.

**Why no `movie_type`.** A classification column earns its place only by driving a code path. `series_type` drives episode numbering — real and three-valued. A movie is a leaf with no parts to address, so there is no analogous fork; a `movie_type` would be a label with no behavior, which is descriptive metadata (genres/keywords), not structure. The cases that *look* like they want it are something else: "is *South Park: Post COVID* a movie or a season-0 special?" is an **identity** question (which entity does this resolve to — provider-decided, and the model already houses both a movie and a special episode), not a classification one. An anime *film* is just a movie; its only divergence — optional AniList enrichment — is detection-driven provider routing, not a stored type. The movie variation axis that *does* exist — theatrical/extended/director's cut — already lives as [`edition` on the file](../files/README.md). The asymmetry between `series_type` and no-`movie_type` is intentional, not a gap.

## Series structure sync (the foundation gap)

**The gap, summarized:** today `media_season` and `media_episode` rows are created only when scan/match finds files. That means our model of "what episodes exist" is "what episodes we have files for." For monitoring, tracking, smart scheduling, and "coming soon" UI, we need upstream truth: which episodes exist, when do they air, what are they called.

This section owns the **data shape** — *what* gets synced into the tree. *How* the sync runs (the provider-agnostic scheduler that decides what's due, dispatching provider-specific fetch operations) is the [refresh engine](../sources/README.md#the-refresh-engine) in [sources](../sources/README.md). There is no "TMDB series worker" — series and movies are two instances of one refresh policy, and the structural fetch is one dispatched operation behind the `StructureSource` seam.

### Two episode lists: the synced tree vs the focus-page view

A persistent confusion worth settling up front: *"the list of episodes"* names **two different things**, and only one of them is what this section maintains.

| Layer | What it is | Lives where | Stable internal id? |
| ----- | ---------- | ----------- | ------------------- |
| **Upstream truth** | What episodes *exist* (incl. unaired) | the provider (TMDB) — ephemeral, external | no |
| **The synced tree** | Arrflix's *durable, joinable copy* of that truth — what `file`, [want](../tracking/README.md), and [decision](../matching/README.md) rows reference | `media_season` / `media_episode` | **yes** |
| **Your files** | What you actually *have* on disk | `file.episode_id` | — |

**The synced tree is the middle layer, and it is what structure sync produces.** The series **focus page is a separate thing**: today it is a *live view* that merges **upstream truth + your files** — it fetches the season/episode list straight from the provider on each render (keyed by the provider id, so it works even for a series you don't own) and overlays an `available` flag from local files. It does **not** read the synced tree. So the focus page already shows unaired episodes — *from the provider, not from the DB*. That is why the synced tree can *look* redundant with the focus page. It isn't.

**Its consumer is not the focus page — it's [tracking](../tracking/README.md) and every other background / cross-series reasoner**, the things that cannot issue a live provider call per render:

- *"Across all tracked series, which wanted episodes aired this week but aren't acquired?"* is one DB query over the synced tree, not N live provider fetches.
- A want for an *unaired* episode must point at a durable row with a stable id; a line in a discarded provider response can't anchor it.
- Renumber-stable identity ([below](#handling-renumbering)) exists only for rows, never for render-time `(season, episode)` matching.

**Scope follows adoption.** The synced tree exists only for a series that is a `media_item` — i.e. one you've adopted by having a file or by requesting/tracking it. Browsing a *non-adopted* series is pure upstream-truth display; nothing is synced. Arrflix mirrors the slice it manages, not all of TMDB.

#### Open fork — DB-as-source-of-truth vs live-provider display

Once the synced tree is reliably populated, the focus page *could* read **it** instead of calling the provider per render (the existing `ListEpisodeAvailabilityForSeries` query is shaped for exactly this). Whether to make that flip is **undecided — either is viable**, and the structure sync is the prerequisite for *both* (and required by tracking regardless):

| Approach | Pros | Cons |
| -------- | ---- | ---- |
| **Focus page reads the synced tree** (DB as source of truth) | Page loads don't hit the provider — fast, works during a provider outage, no per-view rate-limit. Focus page sees exactly what tracking sees (one path, no drift). | Page shows the *last-synced* tree → staleness bounded by refresh cadence. A non-adopted (un-synced) series still needs a live path for browse, so you keep both anyway. |
| **Focus page stays live-provider** (synced tree serves background only) | Always perfectly fresh on the page. Browse works uniformly for adopted and non-adopted series, no special-casing. | Per-render provider dependency stays — latency, rate-limit, outage-coupling. Two episode lists maintained by different paths that can drift. |

Pinned in [open questions](#open-questions); not decided here.

### What gets synced

For each series, on a sync cycle:

1. **Series-level refresh** — re-fetch `media_item` fields (already exists via `enrichmentService.enrichSeries`).
2. **Season list** — for each season TMDB reports, upsert a `media_season` row (number, air_date, name, overview, poster).
3. **Episode tree** — for each episode TMDB reports, upsert a `media_episode` row with full metadata: number, **absolute number** (nullable — populated whenever the provider supplies it; the anime seam, see [open question #7](#open-questions)), title, air_date, runtime, overview, still_path, vote_average, special flag.
4. **External IDs** — fetch TMDB's `external_ids` endpoint for the series; upsert TVDB and IMDB IDs into `media_item_external_id`. The per-episode equivalent (TMDB → TVDB episode ID) is **deferred** — `media_episode_external_id` ships with its first consumer; the source data (`seasonDetails.Episodes[].TVDBID`) is already in the payload we fetch, so capture is cheap whenever it's wanted.
5. **Raw payload** — store the full TMDB response in `media_metadata_source` keyed by `(media_item_id, source: tmdb)`.

### Pre-air episodes

We **store unaired episodes**. `air_date` is in the future (or NULL for announced-but-undated). They appear in:

- "Coming soon" UI
- Tracking's scope evaluation (e.g., `future_only` rules include future episodes immediately)
- Smart scheduling's air-date awareness

A pre-air episode has **no `file`** and exists as a record only. When it airs, the want lifecycle picks it up via tracking; nothing about the episode row changes.

### Handling renumbering

TMDB occasionally renumbers — moves an episode from `S01E13` to `S02E01`, etc. Sonarr famously struggles with this. The way out is **keying on stable TMDB episode IDs**, not `(season, episode)`:

- The unique constraint on `media_episode` is the partial-unique on the `media_episode.tmdb_id` **column** (`idx_media_episode_tmdb`), not on `(season_id, episode_number)`.
- When sync sees a known TMDB episode ID with new `(season, episode)` numbers, **update the row** rather than insert a duplicate.
- Surfaced to the user via a [notification](../notifications/README.md): "*Show X* renumbered episodes; review your overrides." (Tracking's per-episode overrides keyed on the same TMDB ID survive the renumber automatically — see [tracking open question #4](../tracking/README.md#open-questions).)

### Episode removal

If TMDB removes an episode, we **don't delete the row** — we mark it `deprecated: true`. Reasons:

- Preserves any files that were imported under that episode (the file still exists on disk; orphaning the row breaks scan/match)
- Preserves user history (decision_log, watch state, overrides)
- Reversible if TMDB un-removes (rare but happens)

Deprecated episodes are excluded from "in-scope" tracking evaluation but remain visible in admin UI.

### Specials (season 0)

Synced by default — we want to *know* about specials for display. Whether they're **in scope** for tracking is a separate question owned by [tracking](../tracking/README.md) (probably no by default, opt-in per tracking).

### Sync cadence — series

| Series state                                              | Cadence            |
| --------------------------------------------------------- | ------------------ |
| Just-tracked (first sync)                                 | Immediate          |
| In-production, next episode within 30 days OR aired in last 14 days | Daily      |
| In-production (`Returning Series`), no recent activity   | Weekly             |
| Ended, all in-scope episodes acquired                     | Monthly            |
| Failed sync (e.g., rate-limited)                          | Exponential back-off |
| User manual refresh                                       | Immediate          |

The worker reads a "due for refresh" queue derived from `metadata_updated_at` + state.

### Sync cadence — movies

Simpler: movies don't change structurally. Refresh cadence is just staleness.

| Movie state                          | Cadence            |
| ------------------------------------ | ------------------ |
| Just-added (first sync)              | Immediate          |
| Unreleased (release_date in future)  | Weekly             |
| Released, less than 1 year old       | Monthly            |
| Released, more than 1 year old       | Quarterly          |
| User manual refresh                  | Immediate          |

## The metadata source pattern

The existing `media_metadata_source` table stores raw JSONB payloads from each source, keyed by `(media_item_id, source)`. Why:

- **Forward compatibility.** If we later realize we want a field we didn't extract, we can re-derive it from the raw payload without re-fetching.
- **Debugging.** Hard problems become "look at what TMDB actually said."
- **Auditing.** Provenance of every field we display.
- **Multi-source aggregation.** If we add IMDB or Trakt later, their raw payloads sit alongside TMDB's; the field-extraction layer chooses precedence.

**Granularity:** one row per `(media_item, source)`. Episode-level metadata lives inside the series-level payload — we don't store per-episode raw blobs. This avoids row explosion (a long-running series has hundreds of episodes; storing 200 JSONBs adds little over storing one series JSONB with an episodes array).

**Size implications:** TMDB series responses with full episode appends can be 100KB+ per series. Postgres handles this fine; just don't query the column unless you need it.

## Provider abstraction

The full provider strategy — role-segregated seams, the cost-to-enable taxonomy, the bundled-key approach, TVDB's deferral, and the anime mechanics — lives in [sources](../sources/README.md). Two pieces are data-model concerns and stay here.

**Normalization happens at the provider boundary.** Raw payloads are stored as-is in `media_metadata_source`; the provider operation **normalizes** them into our canonical model before any column is written: TMDB's `status` strings → our [canonical status enum](#canonical-status), ISO date strings → typed `release_date` / `last_air_date`, genre arrays → canonical genres, relative `poster_path` → our stored path (URL composition happens at render time via the [`ImageComposer`](../sources/README.md#roles-not-one-provider) seam). Consumer code — UI, acquisition, tracking — never sees a provider's native shape; it sees the internal domain type. This boundary is what makes TMDB-shaped *storage* an implementation detail rather than a leak: the test is not "is the table TMDB-shaped" but "does TMDB shape escape this module's domain type" — and it must not.

**One canonical writer owns the columns.** In v1 exactly one provider (TMDB) writes `media_item` columns. This is the mechanism that *prevents* a per-field reconciliation fork — there is nothing to reconcile with a single writer. Any later provider is either a *replacement* (insurance, never concurrent) or a *scoped augmenter* (one operation, blended at one call site), never a second canonical writer. The full rationale — portability insurance is cheap and already built (clean domain type + raw-payload retention); concurrency is the deferred, expensive part — is in [sources](../sources/README.md#portability-insurance-vs-concurrency).

## Refresh & staleness policy

The cadence tables above describe *what* the policy is; this section describes the staleness model and the triggers that feed it. The engine that *drains* that work — the provider-agnostic scheduler and the dispatched provider operations — is the [refresh engine](../sources/README.md#the-refresh-engine) in [sources](../sources/README.md), which conforms to [work-dispatch](../../patterns/work-dispatch/README.md) (the due-queue) and [connectivity-health](../../patterns/connectivity-health/README.md) (rate-limit / reachability state).

### Trigger sources

1. **Background sweep** — a worker periodically queries `media_item` for items whose `metadata_updated_at` plus cadence-derived TTL is in the past. Batches and processes.
2. **Manual refresh** — user-initiated, immediate enqueue.
3. **Post-import sweep** — after a successful import, we re-fetch the affected media_item's metadata (cheap, ensures the focus page shows fresh data after a new file arrives).
4. **Tracking activation** — when a series is newly tracked, an immediate full sync runs (series + episodes + external IDs).
5. **Pre-air episode aired** — when an episode's air_date passes and we haven't seen a refresh since, opportunistic re-sync to catch any late metadata.

### Rate limiting

TMDB's public API is generous but not unlimited. Concurrency limits, rate-limit-header honoring, and exponential back-off on 429 are the [refresh engine](../sources/README.md#the-refresh-engine)'s concern and conform to [connectivity-health](../../patterns/connectivity-health/README.md) (provider reachability carries a `rate_limited` status). For the v1 single-user case this is rarely hit; noted so consumers know the back-off lives at the engine, not per-field here.

### Failure handling

Failed syncs:

- Mark item with `metadata_last_attempted_at` (separate from `metadata_updated_at`)
- Record last error
- Don't keep retrying tight — exponential back-off
- Distinguish "stale and behind" from "stale and failing" in operational views

### Episode-level staleness

Per-episode `metadata_updated_at` exists alongside the series-level field. Some episode-level changes (air date slipping by a week) need to invalidate just the episode, not force a full series sync. The episode field is updated whenever a sync touches that episode's row.

## Local overrides

Some metadata, users want to override. Examples:

- Wrong title from TMDB
- Bad/missing artwork → user uploads custom
- "I want to call this 'The Office (Best Version)'"

**Model:** a sparsely-populated companion table `media_item_local_override` with the same shape as the override-able subset of `media_item` (title, overview, poster_path, backdrop_path, custom artwork URLs, etc.). At read time, the API does a left join and prefers override values where non-NULL.

Same pattern for episodes if/when needed (probably not v1).

**Properties:**

- Refresh does **not** touch overrides — they live in a separate table that the sync worker never writes.
- Reverting an override = deleting that row's column.
- The override table is small and most rows don't have one — sparse population is the expected case.

**Custom uploaded artwork** is a natural extension (poster_path could point to a local /uploads/* path instead of a TMDB-relative one). Out of scope for the v1 model in terms of *upload UI*, but the override path supports it.

## Image handling

Image paths are stored as **provider-relative paths** (e.g., TMDB's `/abc123.jpg`). Composing them into fetchable URLs is the **provider's** responsibility — the [provider abstraction](#provider-abstraction) exposes an `ImageURL(path, size)` function that knows its CDN and size variants.

This means the schema stays provider-agnostic (just `poster_path` + `backdrop_path` strings) while the rendering layer knows which provider's rules apply. In v1 the only implementation composes with the TMDB CDN base; tomorrow a different provider could compose with a different CDN, and a custom-artwork override could compose with a local `/uploads/` path — all behind the same function call.

**v1:** images load direct from the provider's CDN at view time.

**Deferred:**

- **Local image cache / proxy** — serve images through arrflix for faster loads, offline-ish use, and uniformity with custom artwork. Adds disk + invalidation complexity; not v1.
- **Custom artwork upload UI** — the data model supports it via local override; the UI for uploading + hosting is deferred.
- **Fanart.tv integration** — better artwork than TMDB for some titles. Considered for the artwork-improvements iteration.

## What metadata does NOT own

Adjacent concerns that live elsewhere:

- **Provider strategy** — which sources we use, what each costs to enable, the bundled-key approach, the role seams, the refresh engine, and the anime `NumberingMap` all live in [sources](../sources/README.md). Metadata is the provider-agnostic data model only.
- **Indexer release parsing** — turning a release name into quality + season/episode is the parser's job, not metadata. Metadata provides the *truth* the parser is matching against.
- **Watch state** — covered by the future Plex/Jellyfin integration.
- **People (cast, crew, directors, writers)** — explicitly deferred. When demand emerges, a separate `people` spec covers schema, search, and UI.
- **Library file layout** — what file path a movie/episode ends up at is the name-template + import-service's job.
- **Decision log** — references media_items by ID but doesn't store metadata itself.
- **Subtitle metadata** — out of scope; Bazarr-style features deferred indefinitely.

## Interactions

| Neighbor                  | How metadata interacts                                                                 |
| ------------------------- | --------------------------------------------------------------------------------------- |
| **Scan / match**          | Reads metadata to match files to media items. Also writes new media_items on first-match. Must coordinate with the [refresh engine](../sources/README.md#the-refresh-engine) so seasons/episodes exist for matching. |
| **Tracking**              | Consumes the full episode tree (including unaired) to evaluate scope and schedule searches. Cannot function without series sync. |
| **Auto-select**           | Reads `external_id` for TVDB lookups during indexer search. Reads episode air_dates for smart scheduling. |
| **Unmatched files**       | Manual-match flow writes media_items; resolution triggers a metadata sync for newly-created items. |
| **Enrichment service (existing)** | Becomes a TMDB `MetadataSource` operation dispatched by the [refresh engine](../sources/README.md#the-refresh-engine). The current `enrichMovie` / `enrichSeries` logic stays; the structural-tree fetch (`StructureSource`) is added on top. |
| **[Sources](../sources/README.md)** | Owns the provider seam this data model is written through: which provider plays which role, cost-to-enable, the refresh engine, the anime `NumberingMap`. Metadata owns *what* is persisted; sources owns *how it is fetched and by whom*. |
| **Decision log**          | References media_items / episodes by ID for "why didn't this download?" debugging. |
| **Plex / Jellyfin (future)** | Reads metadata to render. May consume external IDs for cross-correlation. |

## Open questions

1. **Confidence on external_id mappings.** Needed for low-confidence auto-matches (e.g., an unidentified file picks a suggested match with 70% confidence). Probably add as an optional column; pin in iteration 2.
2. **Override granularity for episodes.** Do we extend `media_item_local_override` pattern to a per-episode override table now, or defer until there's demand? Leaning defer; episode-level overrides are rare in practice.
3. **Renumber notification UX.** When TMDB renumbers, how loud should the system be? Silent migration with a log entry vs. a one-off [notification](../notifications/README.md) vs. blocking the sync for admin approval. Probably notification + auto-migrate.
4. **Deprecated episode visibility.** Where in the UI do we show `deprecated: true` episodes? Probably hidden by default, with an admin toggle to surface. Important for not silently losing user-imported files.
5. **TMDB ID merges.** When TMDB merges two records (rare but real), how do we follow? Probably: detect via the redirect TMDB returns, log a warning, leave the data alone, surface to admin for review.
6. **Re-match flow data preservation.** When a user corrects a wrong TMDB match for a media_item (Fix Match flow), the episode tree needs to be wiped and re-synced. What happens to files associated with the old match — re-link or orphan? Probably re-link via the scan/match logic, fall back to unmatched on failure.
7. **Anime numbering (absolute vs season) — seam reserved.** Full anime support (numbering-mapping, AniDB as a provider) stays out of v1, but the cheap seam is taken now rather than deferred: `media_episode` carries a **nullable `absolute_number` column from v1**, populated opportunistically whenever TMDB/TVDB returns it. It's free, and it avoids a later migration the moment anime lands. What stays deferred is the **numbering-mapping** that reconciles a release's scene/absolute number against this canonical numbering — that's a future [matching](../matching/README.md#the-resolver-catalog) `episode-numbering` resolver consuming [parsing](../parsing/README.md)'s numbering-namespace tag, not a metadata concern. Identity-side, AniDB is already reserved in the [external_id registry](#identity) and the [provider abstraction](#provider-abstraction), so adding it is an implementation, not a re-architecture.
8. **Image cache promotion.** What's the trigger to graduate to a local image cache? Likely: when we add custom artwork uploads (so we're already serving local images anyway), the case for proxying TMDB images via the same path gets stronger.
9. **Collection-as-entity promotion.** When a `collection_id` column is no longer enough — e.g., we want a collection focus page with overview text and a poster — we promote to a `collection` table. Document this as the explicit upgrade path.
10. **Per-source priority.** Moved to [sources](../sources/README.md#portability-insurance-vs-concurrency) — per-field precedence across canonical sources is the deferred multi-canonical mode; not v1.
11. **Canonical status enum vocabulary — locked & implemented.** The status column previously stored TMDB's strings raw. **Locked set:** `upcoming` · `released` · `continuing` · `ended` · `canceled` · `unknown`, with TMDB mapped down at the provider boundary (`model.CanonicalizeStatus`) — not expanded per provider. The full mapping table and rationale now live in [Canonical status](#canonical-status); a migration-0012 `CHECK` constraint pins the column. The cadence engine keys on these canonical states. Coordinated with [sources OQ#7](../sources/README.md#open-questions). (Cheap adjacent renames flagged in the same pass: `vote_*` → `rating_*`, and drop the embedded `tmdb_id` from the genre blob — both gratuitous TMDB-isms in the canonical model; still open.)
12. **Image source on local overrides.** Custom artwork uploaded by a user has different URL composition rules than provider paths. Does the override row need an explicit `image_source` flag (`provider` vs `local`), or is it implicit (any override path is treated as local)? Pin when custom-artwork upload UI is designed.
13. **Provider capability negotiation.** Moved to [sources](../sources/README.md) — handled by role-segregated seams (a provider implements the subset it supports) rather than a runtime capability negotiation.
14. **TMDB-specific endpoints we lean on.** Moved to [sources](../sources/README.md) — documenting the contract a provider must fulfil (e.g. an `external_ids` equivalent) is a provider-seam concern, not a data-model one.
15. **Focus-page episode source — DB-as-source-of-truth vs live-provider.** Once the synced tree is reliably populated, does the series focus page read it (decoupled from the provider, consistent with tracking, but bounded by refresh staleness) or keep rendering live from the provider (always fresh, but per-render provider dependency and two lists that can drift)? Open fork with pros/cons in [Two episode lists](#two-episode-lists-the-synced-tree-vs-the-focus-page-view); the structure sync is the prerequisite for either. Pin when the focus page is revisited or tracking lands.

## What we're explicitly not deciding here

- Exact table schemas, columns, indexes
- API endpoint shapes for metadata fetch / refresh / override
- The [refresh engine](../sources/README.md#the-refresh-engine)'s exact queue / scheduler implementation — owned by [sources](../sources/README.md), conforming to [work-dispatch](../../patterns/work-dispatch/README.md)
- Per-user language preference (locale is a system-level setting in v1)
- Cast/crew schema
- Fanart.tv / TVDB-as-primary fallback strategies
- Custom artwork upload UI
- Watch-state schema (future Plex/Jellyfin spec)
- Provider strategy generally (role seams, cost-to-enable, bundled key, TVDB deferral, anime mechanics) — now owned by [sources](../sources/README.md)
- **Running a second metadata provider**, per-field precedence across canonical sources, and provider-switching UI — all deferred and owned by [sources](../sources/README.md); only TMDB ships in v1

## Doc neighbors

- [Sources](../sources/README.md) — the provider seam this data model is fetched through; owns the refresh engine, the provider catalog, and the anime `NumberingMap`
- [Tracking](../tracking/README.md) — the primary consumer of series-structure data
- [Acquisition](../acquisition/README.md) — consumes external_ids for indexer search
- [Quality profiles](../quality-profiles/README.md) — orthogonal; quality decisions don't read metadata directly
- [Story 1](../../stories/01-happy-path-auto-approve.md) — exercises the metadata flow from request to availability
- [Errors](../../patterns/errors/README.md) — TMDB / upstream failures use `KindBadGateway`
