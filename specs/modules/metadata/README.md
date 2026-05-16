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

It does **not** pin down exact column types, indexes, or API shapes. As with the other specs, those come after the model survives more user stories.

## TL;DR

- **Identity** lives in a separate **`external_id`** registry, not as columns on `media_item`. One row per `(entity, source)` pair across `tmdb`, `imdb`, `tvdb`, future `anidb`, etc.
- **Item-level metadata** is mostly already there (migration 0012). This doc back-fills the model and adds **movie collections** as a lightweight extension.
- **Series structure sync** is new and load-bearing: a worker pulls the full season/episode tree from TMDB, including **unaired episodes**, so tracking and smart scheduling have something to operate on.
- **Local overrides** are designed in now (model + read-time precedence), UI later.
- **Refresh policy** is cadence-by-state: in-production weekly, recently-aired daily, ended monthly, manual immediate. Background worker drives the queue.
- **Images** continue to load direct from TMDB CDN in v1; local cache is deferred.
- **Provider portability** is designed for, not built: a `MetadataProvider` seam isolates TMDB-specific code so a future provider becomes a drop-in, not a rewrite.
- People (cast/crew) and per-user language preference are explicitly **out of scope**.

## Identity

Today, `media_item` has `tmdb_id` and `imdb_id` as columns, and `media_episode` has `tmdb_id` and `tvdb_id`. That works for now but doesn't scale to more sources (TVDB for series, anidb later for anime, possibly Trakt, possibly local custom IDs).

**New model:** a dedicated **`external_id` registry**:

- One conceptual table per indexed entity (likely two tables in practice — `media_item_external_id` and `media_episode_external_id` — to avoid polymorphic FKs, but the shape is the same).
- Each row carries `(entity_id, source, external_id)`.
- `source` is drawn from an **open registry**: today `tmdb`, `imdb`, `tvdb`, with `anidb` reserved. Adding a source is a config + code change in the provider layer, not a schema change.
- Unique on `(entity_id, source)` — one ID per source per entity.
- Indexed on `(source, external_id)` for the common reverse lookup ("find me the media_item with TMDB id 12345").

**Canonical identity:** in v1, **TMDB is the canonical primary source** — it owns the writes to `media_item` columns. Most `media_item`s have a TMDB external_id; absence is allowed (manually-created stubs, items whose upstream record was deleted) but unusual. Other sources (IMDB, TVDB) are read-only mappings — useful for cross-referencing at search time, but they don't write to columns.

The canonical-source designation is a **configuration choice, not a code assumption** — see [Provider abstraction](#provider-abstraction).

**Cross-source resolution:** the auto-select pipeline may need a TVDB ID at indexer-search time (some indexers index series by TVDB). This is a single SELECT against `media_item_external_id` for `(media_item_id, source: tvdb)`. The TMDBSeriesSyncWorker is responsible for populating TVDB and IMDB IDs from TMDB's `external_ids` endpoint as part of series sync.

**Migration path:** add the `external_id` tables, backfill from the existing `tmdb_id` / `imdb_id` columns, then drop those columns in a later migration once all reads have been moved.

**Optional `confidence` field:** for low-confidence auto-matches (the unmatched_file flow assigning a likely match), we may want to record confidence per mapping. Pin in iteration 2 — flagged in open questions.

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
| `status`               | TMDB                   | `Released`, `Returning Series`, `Ended`, `Canceled`, etc.   |
| `certification`        | TMDB content ratings   | US rating extracted (configurable later)                    |
| `genres`               | TMDB                   | Stored as JSONB array of `{tmdb_id, name}`                   |
| `release_date`         | TMDB (movie release / series first_air_date) |                                            |
| `last_air_date`        | TMDB (series only)     |                                                             |
| `in_production`        | TMDB                   |                                                             |
| `metadata_updated_at`  | (set by enrichment)    | Drives staleness queries                                    |
| `collection_id`        | TMDB (movie only)      | **New, lightweight**: when a movie is part of a TMDB collection, store the ID + name |
| `collection_name`      | TMDB                   | Denormalized for fast display                                |

**Movie collections:** TMDB groups movies into collections (MCU, Star Wars Saga, etc.). Per the discussion, we store **collection_id + collection_name** denormalized on `media_item` for v1 — enough to render "part of: MCU" on a focus page and to query "all MCU movies." We do **not** create a first-class `collection` table; if richer collection features emerge (collection overview, collection poster), we promote later.

**External IDs are not columns anymore** — see [Identity](#identity).

## Series structure sync (the foundation gap)

**The gap, summarized:** today `media_season` and `media_episode` rows are created only when scan/match finds files. That means our model of "what episodes exist" is "what episodes we have files for." For monitoring, tracking, smart scheduling, and "coming soon" UI, we need upstream truth: which episodes exist, when do they air, what are they called.

### What gets synced

For each series, on a sync cycle:

1. **Series-level refresh** — re-fetch `media_item` fields (already exists via `enrichmentService.enrichSeries`).
2. **Season list** — for each season TMDB reports, upsert a `media_season` row (number, air_date, name, overview, poster).
3. **Episode tree** — for each episode TMDB reports, upsert a `media_episode` row with full metadata: number, title, air_date, runtime, overview, still_path, vote_average, special flag.
4. **External IDs** — fetch TMDB's `external_ids` endpoint for the series; upsert TVDB and IMDB IDs into `media_item_external_id`. Same for each episode (TMDB → TVDB episode ID).
5. **Raw payload** — store the full TMDB response in `media_metadata_source` keyed by `(media_item_id, source: tmdb)`.

### Pre-air episodes

We **store unaired episodes**. `air_date` is in the future (or NULL for announced-but-undated). They appear in:

- "Coming soon" UI
- Tracking's scope evaluation (e.g., `future_only` rules include future episodes immediately)
- Smart scheduling's air-date awareness

A pre-air episode has **no `media_file`** and exists as a record only. When it airs, the want lifecycle picks it up via tracking; nothing about the episode row changes.

### Handling renumbering

TMDB occasionally renumbers — moves an episode from `S01E13` to `S02E01`, etc. Sonarr famously struggles with this. The way out is **keying on stable TMDB episode IDs**, not `(season, episode)`:

- The unique constraint on `media_episode` is on the TMDB episode external ID (via `media_episode_external_id`), not on `(season_id, episode_number)`.
- When sync sees a known TMDB episode ID with new `(season, episode)` numbers, **update the row** rather than insert a duplicate.
- Surfaced to the user via a notification: "*Show X* renumbered episodes; review your overrides." (Tracking's per-episode overrides keyed on the same TMDB ID survive the renumber automatically — see [tracking open question #4](../tracking/README.md#open-questions).)

### Episode removal

If TMDB removes an episode, we **don't delete the row** — we mark it `deprecated: true`. Reasons:

- Preserves any media_files that were imported under that episode (the file still exists on disk; orphaning the row breaks scan/match)
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

Metadata sources sit behind a `MetadataProvider` seam — even though TMDB is the only implementation in v1. The seam is **architectural notation**: it locks the boundary so a future provider is a drop-in, not a rewrite. Most of the data model already permits this; what's missing is the explicit interface and the discipline of routing all TMDB-specific code through it.

### Provider responsibilities

A provider implements a small set of capabilities:

- **Lookup by external ID** — given a provider-internal ID, return raw payload + normalized fields.
- **Search** — given a title (and optionally type, year), return candidate matches.
- **Discover** — browse trending, similar/related lookups (powers discovery UI).
- **External-ID resolution** — for its records, expose mappings to other source IDs (TMDB's `external_ids` endpoint; equivalent on others).
- **Series-structure fetch** — for series, return the full season/episode tree including unaired episodes.
- **Image URL composition** — given a provider-relative image path + size, return a fetchable URL.
- **Capabilities declaration** — which entity types it supports (movie, series, anime), what config it requires (API keys, base URLs), its rate-limit shape.

The current `TmdbService` + `enrichmentService` together cover most of this — just not behind a single interface. Extracting the interface is implementation work, deferred; the data model already permits it.

### Normalization happens at the provider boundary

Provider raw payloads are stored as-is in `media_metadata_source`. The provider implementation is responsible for **normalizing** payload fields into our canonical model:

- TMDB's `status` strings (`Released`, `Returning Series`, `Ended`, `Canceled`) → our canonical status enum.
- TMDB's `release_date` / `first_air_date` ISO strings → our `release_date` / `last_air_date` typed fields.
- TMDB's `genres` array → our genre objects with provider-tagged IDs.
- TMDB's relative `poster_path` → our `poster_path` value (composition to a fetchable URL happens at render time via the provider's image URL function).

If a future provider uses a different vocabulary or shape, the translation lives inside its implementation — consumer code (UI, auto-select pipeline, tracking) never sees the provider's native shape.

### Canonical-source designation

In v1, exactly one provider is **canonical**: it owns the writes to `media_item` columns. Non-canonical providers, if added later, are **read-only at the column layer** — their raw payloads sit in `media_metadata_source`, and their external IDs sit in the registry, but they don't overwrite canonical fields.

This isolates "which provider has priority" to a configuration choice, not a code assumption. A future multi-provider mode (which provider wins per field, with per-field overrides) is deferred — it'd require a precedence config and a richer extraction layer, neither of which is needed today.

### Setup is provider-driven

The setup wizard asks for configuration declared by the *active provider*. Today: provider is TMDB, declared config = `[tmdb_api_key]`. Tomorrow: provider is X, declared config = whatever X needs. The setup UI is generic; the requirement list comes from the provider.

This is a small refactor of the current setup flow (which hardcodes "ask for TMDB key"), but locks in the right shape from the start.

### What this buys us

- A future provider replacement / addition becomes implementation work, not re-architecture.
- TMDB-specific assumptions become **localized** to the TMDB provider package. Grep-friendly.
- Multi-provider features (better images from Fanart, better ratings from IMDB) become extensions of an existing pattern, not new patterns.

### What this does NOT buy us

- A running second provider. Only TMDB ships in v1.
- Multi-provider conflict resolution (per-field precedence across canonical sources). Deferred.
- A UI affordance to "switch providers" mid-flight. Deferred.

## Refresh & staleness policy

The cadence tables above describe *what* the policy is; this section describes *how it runs*.

### Trigger sources

1. **Background sweep** — a worker periodically queries `media_item` for items whose `metadata_updated_at` plus cadence-derived TTL is in the past. Batches and processes.
2. **Manual refresh** — user-initiated, immediate enqueue.
3. **Post-import sweep** — after a successful import, we re-fetch the affected media_item's metadata (cheap, ensures the focus page shows fresh data after a new file arrives).
4. **Tracking activation** — when a series is newly tracked, an immediate full sync runs (series + episodes + external IDs).
5. **Pre-air episode aired** — when an episode's air_date passes and we haven't seen a refresh since, opportunistic re-sync to catch any late metadata.

### Rate limiting

TMDB's public API is generous but not unlimited. The sync worker:

- Limits concurrent in-flight requests
- Honors rate-limit headers
- Backs off exponentially on 429

For the v1 single-user case this is rarely hit. Documented for sanity.

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

- **Indexer release parsing** — turning a release name into quality + season/episode is the parser's job, not metadata. Metadata provides the *truth* the parser is matching against.
- **Watch state** — covered by the future Plex/Jellyfin integration.
- **People (cast, crew, directors, writers)** — explicitly deferred. When demand emerges, a separate `people` spec covers schema, search, and UI.
- **Library file layout** — what file path a movie/episode ends up at is the name-template + import-service's job.
- **Decision log** — references media_items by ID but doesn't store metadata itself.
- **Subtitle metadata** — out of scope; Bazarr-style features deferred indefinitely.

## Interactions

| Neighbor                  | How metadata interacts                                                                 |
| ------------------------- | --------------------------------------------------------------------------------------- |
| **Scan / match**          | Reads metadata to match files to media items. Also writes new media_items on first-match. Must coordinate with TMDBSeriesSyncWorker so seasons/episodes exist for matching. |
| **Tracking**              | Consumes the full episode tree (including unaired) to evaluate scope and schedule searches. Cannot function without series sync. |
| **Auto-select**           | Reads `external_id` for TVDB lookups during indexer search. Reads episode air_dates for smart scheduling. |
| **Unmatched files**       | Manual-match flow writes media_items; resolution triggers a metadata sync for newly-created items. |
| **Enrichment service (existing)** | Becomes part of the larger metadata sync system. The current `enrichMovie` / `enrichSeries` functions stay; the **series-structure sync worker** (TMDB-only impl in v1; lives behind the [provider abstraction](#provider-abstraction)) adds the structural-tree fetch on top. |
| **Decision log**          | References media_items / episodes by ID for "why didn't this download?" debugging. |
| **Plex / Jellyfin (future)** | Reads metadata to render. May consume external IDs for cross-correlation. |

## Open questions

1. **Confidence on external_id mappings.** Needed for low-confidence auto-matches (e.g., unmatched_file picks a suggested match with 70% confidence). Probably add as an optional column; pin in iteration 2.
2. **Override granularity for episodes.** Do we extend `media_item_local_override` pattern to a per-episode override table now, or defer until there's demand? Leaning defer; episode-level overrides are rare in practice.
3. **Renumber notification UX.** When TMDB renumbers, how loud should the system be? Silent migration with a log entry vs. a one-off notification vs. blocking the sync for admin approval. Probably notification + auto-migrate.
4. **Deprecated episode visibility.** Where in the UI do we show `deprecated: true` episodes? Probably hidden by default, with an admin toggle to surface. Important for not silently losing user-imported files.
5. **TMDB ID merges.** When TMDB merges two records (rare but real), how do we follow? Probably: detect via the redirect TMDB returns, log a warning, leave the data alone, surface to admin for review.
6. **Re-match flow data preservation.** When a user corrects a wrong TMDB match for a media_item (Fix Match flow), the episode tree needs to be wiped and re-synced. What happens to media_files associated with the old match — re-link or orphan? Probably re-link via the scan/match logic, fall back to unmatched on failure.
7. **Anime numbering (absolute vs season).** Out of scope for v1, but the episode-row shape should not preclude an `absolute_number` field. Reserve the concept; don't model it yet.
8. **Image cache promotion.** What's the trigger to graduate to a local image cache? Likely: when we add custom artwork uploads (so we're already serving local images anyway), the case for proxying TMDB images via the same path gets stronger.
9. **Collection-as-entity promotion.** When a `collection_id` column is no longer enough — e.g., we want a collection focus page with overview text and a poster — we promote to a `collection` table. Document this as the explicit upgrade path.
10. **Per-source priority.** If we ever pull from multiple sources (TMDB + Trakt for ratings, say), which wins on conflict? Probably configurable per-field; not v1.
11. **Canonical status enum vocabulary.** Our status enum currently mirrors TMDB's (`Released`, `Returning Series`, `Ended`, `Canceled`). A future provider may use different vocabulary — do we expand the canonical enum or map down to a smaller stable set? Probably the latter; lock the canonical set's contents before the enum gets baked into UI strings.
12. **Image source on local overrides.** Custom artwork uploaded by a user has different URL composition rules than provider paths. Does the override row need an explicit `image_source` flag (`provider` vs `local`), or is it implicit (any override path is treated as local)? Pin when custom-artwork upload UI is designed.
13. **Provider capability negotiation.** When a provider doesn't support an entity type (e.g., a movie-only provider), how does the system route around it for tracking? Pin in the iteration where multi-provider becomes real.
14. **TMDB-specific endpoints we lean on.** TMDB's `external_ids` endpoint is convenient for cross-source mapping; not every provider will have an equivalent shape. Document the contract the provider needs to fulfill so we don't accidentally bake in TMDB-only assumptions.

## What we're explicitly not deciding here

- Exact table schemas, columns, indexes
- API endpoint shapes for metadata fetch / refresh / override
- The series-structure sync worker's exact queue / scheduler implementation
- Per-user language preference (locale is a system-level setting in v1)
- Cast/crew schema
- Fanart.tv / TVDB-as-primary fallback strategies
- Custom artwork upload UI
- Watch-state schema (future Plex/Jellyfin spec)
- **Running a second metadata provider** — only TMDB ships in v1; the seam is in, additional implementations are not
- Per-field precedence rules across multiple canonical sources
- Provider-switching UI / migration flow

## Doc neighbors

- [Tracking](../tracking/README.md) — the primary consumer of series-structure data
- [Auto-select](../auto-select/README.md) — consumes external_ids for indexer search
- [Quality profiles](../quality-profiles/README.md) — orthogonal; quality decisions don't read metadata directly
- [Story 1](../stories/01-happy-path-auto-approve.md) — exercises the metadata flow from request to availability
- [Errors](../errors/README.md) — TMDB / upstream failures use `KindBadGateway`
