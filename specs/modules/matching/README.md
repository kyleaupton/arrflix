---
status: as-built
---

# Matching — file-to-identity resolution

> **Beyond what's built:** the registered resolvers are `path-embed` (Tier 1) and `name-parse` (Tier 3); `path-context`, `embedded-tags`, `osdb-hash` and `episode-numbering` in §The resolver catalog are unbuilt. Also unbuilt: user-selectable threshold presets (the aggregator runs Recommended), §Drop-in fulfills wants and the drop-in notification, re-match's on-disk move, undo via the decision chain, and §Killer UX moves 2–4 and 6–8.

This doc defines **matching**: how Arrflix takes a file on disk and produces a confident identity for it — a TMDB-linked media item, with episode info for series, and an audit trail. It captures _what matching is_, _what the resolver + aggregator architecture looks like_, _how confidence is modeled_, and _how re-match and un-match work_. The schema is `0014_matcher` (`match_outcome` enum, `match_decision` table).

This doc supersedes the "Unmatched Media Resolution" line item in [the roadmap](../../docs/guide/roadmap.md#unmatched-media-resolution), which was scoped narrowly to a fix-match UI. Matching is a hub system that touches scan, identity/metadata, hygiene, tracking, the decision log, and import.

## TL;DR

- Matching is its own subsystem: the pure **resolver + aggregator** pipeline in `internal/matcher/`, wired to persistence by `MatcherService` / `MatchDecisionsService` in `internal/service/`.
- **Resolvers** are independent identity signals (path-embed, name-parse, OSDb hash, embedded tags). They each emit candidate identities with their own confidence. They live in `internal/matcher/resolvers/`.
- **The string parse comes from [parsing](../parsing/README.md), not from a resolver-local parser.** The `name-parse` resolver turns the unified parser's identity hint (title/year/season/episode, with per-field confidence) into a TMDB candidate. That parser is a 1:1 port of the Sonarr/Radarr parsing engine, regression-gated against them — it's the sole string-parse signal, with no sidecar.
- **Aggregator** combines votes across resolvers, validates against the metadata provider, computes a combined confidence, and bands the outcome.
- **Tiered execution** for performance: ground-truth resolvers run first; if they produce a validated identity, more expensive resolvers are skipped. Aggregator math is unchanged — tiering is an optimization, not a correctness mechanism.
- **Match decisions are first-class records.** Every consequential decision (auto-match, manual match, re-match, un-match) writes a `match_decision` row with full evidence. Re-matches supersede prior decisions; the chain is auditable.
- Six **outcomes** by confidence band — `confident`, `confident_review`, `low_confidence`, `ambiguous`, `no_match`, `partial_series` — plus a seventh, `detached`, written only by the manual detach action ("this file doesn't belong here").
- **Drop-in files** flow through the same pipeline as scan-discovered files. A successful drop-in match may close a pending want (unbuilt).
- **v2 hooks** — the OSDb-hash lookup resolver and per-library resolver toggle — are seam-designed in v1, implemented later. The hash itself is computed and stored from v1.

## What matching is, and isn't

Matching answers one question: _given a file at this path, what identity does it represent?_ Identity = `(provider, external_id, episode_ref?, edition?)`.

Matching is **not**:

- Metadata fetching (what _is_ the identified content) — that's [metadata](../metadata/README.md)
- Hardlink / rename mechanics — that's import + name templates
- Quality decisions on releases — that's [quality profiles](../quality-profiles/README.md)
- Tracking lifecycle — that's [tracking](../tracking/README.md)

Matching's output is a decision. Everything downstream consumes that decision.

## The v0 baseline

The v0 pipeline lived in `backend/internal/service/scan.go` as a 4-phase scanner. It is recorded here because the design below is shaped against its fragilities:

1. **Walk & collect** library files, dedup against `media_file` and `unmatched_file`.
2. **Embedded ID resolution** — regex on path for `tmdb-X` / `tvdb-X` / `imdb-X`. If hit, convert to TMDB via `tmdb.FindByID()`.
3. **Sidecar parse + TMDB search** — for unresolved files, call the FastAPI parsing sidecar at `127.0.0.1:8000` for title/year/S-E parse, then `tmdb.MultiSearch()` (page 1 only), filter by year (hard exclusion if year mismatches or TMDB returns `year==0`). One candidate → identified. Zero or multiple → up to 5 ranked suggestions (100/85/70/55/40) in `unmatched_file.suggested_matches` JSONB.
4. **Persist** — write `media_item`, `media_file`, `media_file_state`, `media_file_import` rows, or upsert into `unmatched_file`.

It worked for well-named libraries but had fragilities:

- **No confidence model.** Auto-match is implicit "one TMDB result survived year filter." No way to express "we're not sure."
- **Year is a hard gate.** `year==0` candidates are dropped silently; year mismatches lose legitimate candidates.
- **TMDB page-1 only.** Right answer on page 2 is invisible.
- **First-success-wins resolvers.** Once embed succeeds, the sidecar parse isn't consulted — we lose the corroboration signal.
- **No match-decision log.** Once written, reasoning is lost. Re-matches can't trace history.
- **Suggestions are TMDB-rows, not resolver evidence.** Position-based scores tell you nothing about _why_ a candidate looked plausible.

The design below layers reasoning on top of the same external dependencies.

## Architecture: resolver + aggregator

Three roles:

```
                    ┌──────────────────────────────────────────────────────┐
   collected ─────► │  Tier 1: path-embed, embedded-tags (v2)              │
   file             └────────────────────────┬─────────────────────────────┘
                                             │
                                             ▼
                                     ┌───────────────┐
                                     │ Validate IDs  │
                                     │ against       │
                                     │ metadata prov │
                                     └───────┬───────┘
                                             │
                       ┌─────────────────────┴────────────────────┐
                       ▼                                          ▼
              identity complete?                          fall through
                       │                                          │
                       │            ┌─────────────────────────────────────────┐
                       │            │  Tier 2: osdb-hash (v2)                 │
                       │            └─────────────────────┬───────────────────┘
                       │                                  │
                       │            ┌─────────────────────────────────────────┐
                       │            │  Tier 3: name-parse, path-context       │
                       │            └─────────────────────┬───────────────────┘
                       │                                  │
                       │            ┌─────────────────────────────────────────┐
                       │            │  Aggregator combines collected candidates│
                       │            └─────────────────────┬───────────────────┘
                       │                                  │
                       └──────────────────┬───────────────┘
                                          ▼
                              write match_decision
                              + file (identity set, or left NULL)
                              + (optional) want fulfillment
```

### Resolver contract

Each resolver implements (`internal/matcher/resolver.go`, abridged):

```go
type Resolver interface {
    Name() string
    Tier() Tier
    Available(ctx) bool         // API key present, opt-in enabled, etc.
    Resolve(ctx, file FileRef) (ResolverResult, error)
}

type ResolverResult struct {
    Candidates []Candidate     // 0..N
    Evidence   any             // raw payload — persisted for the decision log
}

type Candidate struct {
    ExternalRef ExternalRef     // (provider, external_id)
    EpisodeRef  *EpisodeRef     // nullable
    Edition     *string         // nullable; "directors_cut", "extended", etc.
    Confidence  float64         // 0..1, resolver-local
    // plus display identity: Title, Year, Type, PosterPath
}
```

Two key shifts vs v0: (1) resolvers can emit multiple candidates with confidences, not "one answer or nothing", and (2) the raw evidence persists on the result so the decision log captures the why.

**`EpisodeRef` is always _canonical_** — stable provider episode identity, never a raw scene/absolute number. A Western release resolves straight to it. An anime release names episodes in a different [numbering namespace](../parsing/README.md) (absolute `1071`, or a scene number matching neither the provider's seasons nor the absolute count); converting that namespaced number into a canonical `EpisodeRef` is the job of the future **`episode-numbering`** mapping resolver below, which consumes parsing's namespace tag, the resolved series' [`series_type`](../metadata/README.md#series-type-the-numbering-regime-anchor), and the anime [`NumberingMap`](../sources/README.md#anime-embedded-data-vs-api) (XEM / AniDB-derived). Reserving it as a registry slot now — rather than a schema change later — is the anime seam on the matcher side. Until it ships, anime episode identity is best-effort from the parser's numbering and often stays unresolved (`partial_series`).

### The resolver catalog

| Resolver            | Tier         | Signal type                                                                                              | v1 / v2      | Notes                                                                                                                                                                                   |
| ------------------- | ------------ | -------------------------------------------------------------------------------------------------------- | ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `path-embed`        | 1            | TRaSH-style `tmdb-X` / `tvdb-X` / `imdb-X`                                                               | v1 (built)   | Regex `(tvdb\|tmdb\|imdb)-((?:tt)?\d+)` over the absolute path (so `{tmdb-X}` on a parent folder counts), plus `SxxEyy` for the episode; resolver-local confidence 1.0              |
| `embedded-tags`     | 1            | Container-level IDs (MKV/MP4 metadata)                                                                   | v2           | Cheap when present; relies on ffprobe                                                                                                                                                   |
| `osdb-hash`         | 2            | OpenSubtitles file fingerprint → IMDb                                                                    | v2           | Hash always computed in v1; lookup gated by user opt-in                                                                                                                                 |
| `name-parse`        | 3            | Parsed title/year/S-E from the unified [parser](../parsing/README.md)                                    | v1 (built)   | Sole string-parse signal; reads `FileRef.Parsed` (never re-parses). TMDB multi-search, page 1; score = 0.50 base + unique/year/exact-title bonuses − 0.10 per rank, × title confidence, capped at 0.85 so it can't auto-match alone under Recommended |
| `path-context`      | 3            | Sibling-file and directory inference                                                                     | v1 (unbuilt) | Low signal, useful tiebreaker                                                                                                                                                           |
| `episode-numbering` | 3            | Maps a release's scene/absolute episode number → canonical episode identity (XEM- / AniDB-style mapping) | v2 (anime)   | Future. Consumes [parsing](../parsing/README.md)'s numbering-namespace tag, the series' [`series_type`](../metadata/README.md#series-type-the-numbering-regime-anchor), and the anime [`NumberingMap`](../sources/README.md#anime-embedded-data-vs-api); emits a canonical `EpisodeRef`. Registry-shaped so it drops in without a restructure. |

Per-library resolver toggle is a v2 surface, but the catalog is **registry-shaped from v1** — resolvers register at startup (`resolvers.DefaultRegistry`), the aggregator iterates the registered set. Adding/removing resolvers later is dropping in / out of a slice, not a code restructure.

### Aggregator contract

The aggregator owns:

1. **Tier orchestration** — runs Tier 1 first; validates Tier-1 candidates against the metadata provider; if a single validated candidate dominates above the auto-match threshold, short-circuits. Otherwise descends to Tier 2/3. Only Tier-1 candidates are validated.
2. **Cross-provider ID resolution** — IMDb → TMDB, TVDB → TMDB, done during validation: `MetadataProvider.LookupByID` resolves the foreign id through TMDB's find endpoint (`FindByID`, cached in `api_cache`) and returns a TMDB-sourced item; the aggregator rewrites the candidate and records both ends in the `validation_cross_provider` evidence slot. **Unbuilt:** reading the [`media_item_external_id` registry](../metadata/README.md#identity) first (a single SELECT, no provider call — the registry being IMDb/TVDB's *only* presence, since neither is a [provider](../sources/README.md#identity-id-namespaces-vs-providers)).
3. **Candidate merging** — same `ExternalRef` from multiple resolvers compounds confidence (max plus 0.05 per extra resolver, capped at 0.99); a year mismatch against the parsed year is a ×0.9 penalty rather than a hard filter. Candidates within 0.02 of each other band as `ambiguous`.
4. **Outcome banding** — see [Confidence bands](#confidence-bands).
5. **Decision recording** — writes one `match_decision` row per consequential decision, always.

The aggregator is the only place that knows about confidence math, validation rules, and tier composition. Resolvers don't know about each other.

### Why tier + aggregate (not just one or the other)

Pure all-parallel-then-aggregate is correctness-clean but wasteful for the (frequent) TRaSH-style case where the path tells us everything. Pure tiered-fallback is fast but throws away the corroboration signal that lets us trust auto-matches.

Doing both — **tiered execution, parallel aggregation when needed** — gives us:

- TRaSH-style libraries identify in microseconds, no Tier-3 name-parse / TMDB-search load
- Imperfect libraries get full multi-resolver corroboration
- Aggregator math is the same in either case (one vote or three; same formula)

## Confidence bands

Values below are the Recommended preset the aggregator runs with (`matcher.DefaultConfig`); the other presets move the `confident` / `confident_review` boundary.

| Outcome            | Confidence                           | Side effect                                                                                                                                                                    |
| ------------------ | ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `confident`        | ≥ 0.85                               | identity written to the [file](../files/README.md) (`media_item_id` set); no review                                                                                            |
| `confident_review` | 0.7–0.85                             | identity written to the file; flagged for review in the matcher inbox                                                                                                          |
| `low_confidence`   | 0.5–0.7                              | file left unidentified (`media_item_id` NULL); decision carries one strong suggestion                                                                                          |
| `ambiguous`        | < 0.5 or multiple-candidate-tie      | file left unidentified; decision carries up to 5 ranked suggestions                                                                                                            |
| `no_match`         | nothing ≥ 0.5                        | file left unidentified; decision carries no suggestions                                                                                                                        |
| `partial_series`   | series confident, episode unresolved | series identity on the file (`media_item_id` set, `episode_id` NULL); `partial_series` is a stored outcome band on the decision (series identity resolved, episode unresolved) |

### Threshold presets

Per-installation setting, three starter configs:

| Preset          | Auto threshold | Review band | Best for                                              |
| --------------- | -------------- | ----------- | ----------------------------------------------------- |
| **Strict**      | 0.95           | 0.7–0.95    | OCD libraries; reviewer-on-the-loop                   |
| **Recommended** | 0.85           | 0.7–0.85    | Balanced default                                      |
| **Relaxed**     | 0.7            | 0.5–0.7     | Just-fill-the-library; tolerate occasional mismatches |

Same shape as the [hygiene](../hygiene/README.md#presets) preset model. The three presets are defined in `internal/matcher/outcome.go` and each decision records its band in `decided_with`. **Unbuilt:** the setting — the aggregator always runs Recommended.

### Validation modulates confidence, not resolvers

When a Tier-1 resolver returns an embedded TMDB ID, its resolver-local confidence is 1.0 — we trust our own regex. The aggregator then validates against the metadata provider (an upstream error passes the candidate through unchanged rather than punishing it for an outage):

| Validation outcome                   | Final confidence                                  |
| ------------------------------------ | ------------------------------------------------- |
| ID resolves to a valid entry         | ~0.99                                             |
| ID resolves with a redirect (merged) | ~0.95; write redirected ID; log redirect          |
| ID returns 404 / deleted             | 0.0; candidate dropped; fall through to next tier |

This is how "the ID could be stale" gracefully degrades. The resolver doesn't know about provider state; the aggregator does.

## The match-decision artifact

Every consequential decision writes a row, schematically:

```
match_decision(
  id,                            -- BIGSERIAL
  file_id,                       -- FK to file.id (the file being identified)
  outcome,                       -- match_outcome enum: 'confident', 'confident_review', 'low_confidence', 'ambiguous', 'no_match', 'partial_series', 'detached'
  chosen_source, chosen_external_id,   -- nullable (set only when a candidate won)
  chosen_season, chosen_episode,       -- nullable
  chosen_edition,                -- nullable text
  confidence,                    -- final aggregated value
  resolvers_consulted (JSONB),   -- which resolvers ran and what they returned
  evidence (JSONB),              -- the full per-resolver evidence payload, capped at 8KB
  evidence_truncated,            -- true when the cap trimmed it (largest payload first)
  ranked_candidates (JSONB),     -- up to 5 ranked suggestions, denormalized for the inbox card
  parsed_snapshot (JSONB),       -- what the parser saw at decision time (display/audit only)
  decided_with (JSONB),          -- the threshold band this decision ran under (display/audit only)
  decided_by,                    -- 'auto' | 'user:<id>' | 'rule:<id>'
  decided_at,
  superseded_at,
  superseded_by                  -- FK to a later decision
)
```

**Current match** for a file = the latest non-superseded `match_decision` row (served by the partial index `match_decision_current_idx`). Re-matches insert a new row and set the prior's `superseded_by`. A manual match writes `outcome: confident` at confidence 1.0. Un-matches insert a new row with `outcome: no_match` and `decided_by: user:<id>`.

This follows the system-wide [decision-artifact pattern](../../patterns/audit/README.md): each producer owns its own table (shape diverges) but the principle, retention, and Activity-view aggregation are shared.

## Re-match, un-match, detach

Three distinct actions, three different effects:

| Action       | What it does                                     | Filesystem effect                                                                                                                                     |
| ------------ | ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Re-match** | "This is identified as X; it's actually Y"       | File moves/renames via name templates; old hardlinks resolved. **Unbuilt:** the file stays at its old path (`TODO(import)` in `commitManualMatch`)    |
| **Un-match** | "I don't know what this is; back to the inbox"   | File stays in place; identity cleared on the `file` row (`media_item_id` → NULL). No row swap — see [files § lifecycle](../files/README.md#lifecycle) |
| **Detach**   | "This file doesn't belong in the library at all" | File optionally moved to the `matcher.quarantine_path` setting (skipped, with a warning, when unset or the move fails)                               |

The endpoints are `POST /api/v1/files/{fileId}/match` (`{source: tmdb|imdb|tvdb, externalId}` plus optional episode/edition), `/unmatch` and `/detach` (`media.write`), and `GET /api/v1/files/{fileId}/match-decision` (optionally with history). All three actions write a `match_decision` row with `decided_by: user:<id>` and supersede the prior decision. Un-match writes `outcome: no_match`; detach writes `outcome: detached` and soft-deletes the file (`deleted_at` set), so it drops out of every live read and the inbox.

Re-matches are reversible via the decision-log chain — every prior decision is preserved, so "undo" means "supersede the current decision with a copy of an earlier one." This is the antidote to Sonarr's irreversible-wrong-match horror story. **Unbuilt:** there is no undo action; the chain is preserved and readable.

## Matcher surfaces

Three UI surfaces, all backed by the same data:

1. **Inbox.** `/library/matching` (`views/MatchingInbox.vue`, backed by `GET /api/v1/unmatched-files`). Every file whose current `match_decision` banded to something other than `confident` (auto-matched, out of the inbox) or `detached` (user-rejected, soft-deleted). That keeps `confident_review` and `partial_series` — identity resolved but flagged — alongside the unidentified `low_confidence` / `ambiguous` / `no_match` files; a bare `media_item_id IS NULL` predicate would drop the flagged-but-identified ones. Filterable by outcome band, with per-band counts. Count surfaced in the dashboard chrome to make it feel actionable (unbuilt).

2. **Drill-down / decide pane** (`components/matching/DecidePane.vue`). Click an inbox item → see file path, parsed title/year, ranked suggestions with evidence per suggestion (_"name-parse says 0.78, path-context says 0.31"_), one-click match, "Search TMDB" fallback, "Match by ID" power-user input.

3. **Drop-in flow.** File appears in a watched library directory → scan picks it up → matcher runs the same pipeline. If `confident` outcome, file appears in library and a [notification](../notifications/README.md) fires (**unbuilt**; drop-in detection is a manual scan today) (_"Dropped Breaking.Bad.S01E03.mkv. Auto-matched as Breaking Bad S01E03."_). Otherwise lands in the inbox.

## Drop-in fulfills wants

**Unbuilt** (`TODO(tracking)` in `ScannerService.persistConfident`). When a drop-in match resolves to identity that satisfies an open want (an episode the user has been waiting for, a movie they requested), the matcher:

1. Closes the want, marking it `available`
2. Fires Story 1's success [notifications](../notifications/README.md) (push, etc.)
3. Logs the match decision with a back-reference to the want

This collapses Sonarr's separate "import from outside" workflow into the same surface as everything else. There's no second affordance; the matcher _is_ the manual-grab side door.

## Killer UX moves

The differentiators between _another fix-match modal_ and _a matcher people enjoy using_:

1. **Match by external ID, directly.** _"Paste a TMDB or IMDb URL."_ The most precise interface. Aligns with the future where multiple providers are supported. Built as the decide pane's "Match by ID" tab (source + id; URL paste unbuilt).
2. **"Why didn't this match" explanations.** Each unmatched item surfaces what resolvers tried, what candidates they considered, why none cleared the bar. Same decision-log philosophy.
3. **Bulk match-by-folder.** _"All files in this folder are episodes of The Office (US)."_ Pick the series once → file parses resolve episodes. Saves immense work on season packs that arrive without metadata. Implemented as a manual override that bypasses resolvers — writes one `match_decision` row per affected file with `decided_by: user:<id>`.
4. **Match preview before commit.** Show the rename diff, the destination path, the poster, before the user confirms.
5. **Drop-in fulfills want.** See above.
6. **Reverse search by content fingerprint** (v2). Files that came from torrents may have an audit trail in qBittorrent we can cross-reference. Sometimes the trace is better than the parse.
7. **Confidence-banded inbox.** _"3 items need decision, 12 auto-matched but flagged for review, 47 confidently matched this week."_ The inbox is a match-decision feed, not a "what's broken" list. Built: per-band filter and counts.
8. **Edition-aware matching.** When TMDB returns one ID but the file is clearly a director's cut, surface a "which edition?" prompt rather than silently collapsing them.

## Code structure

```
backend/internal/
  parsing/                          ← the unified string parser, a 1:1 Sonarr/Radarr
                                       engine port: title/year/S-E + quality + attributes, pure Go

  matcher/                          ← matching domain logic (pure; no repo/db deps)
    aggregator.go                   ← combine + validate + band + decide
    outcome.go                      ← Outcome bands, Thresholds presets, record types
    resolver.go                     ← Resolver interface, ExternalRef, Candidate types
    resolvers/
      registry.go                   ← DefaultRegistry: startup registration; the aggregator iterates the set
      pathembed.go                  ← TRaSH-style embedded-id regex
      nameparse.go                  ← sole string-parse signal; reads internal/parsing
      (osdbhash.go, embeddedtags.go — unbuilt)

  service/                          ← orchestration layer (holds *repo.Repository)
    matcher.go                      ← MatcherService: runs the pipeline, persists decisions
    match_commit.go                 ← commitMatch: shared identity-write path for scan + manual match
    match_decisions.go              ← match / re-match / un-match / detach flows
    unmatched_files.go              ← the matcher-inbox read surface
```

Rule of thumb: a package gets its own module only when it owns a boundary — an external service, a layer, or a cross-cutting primitive. Resolvers are internal to the matcher; they live there. The matcher domain package stays pure (engine + types); the `*Service` that wires it to persistence lives in `internal/service/`, per the service-layer rules.

`ScannerService` holds no identification logic; it calls `MatcherService.MatchBatch(files)`. The scanner is filesystem-aware; the matcher is identity-aware. Clean line.

## Relationship to v0

Against the v0 baseline above:

- Kept: the `media_item` / `media_season` / `media_episode` content schemas; the scan shape, as walk → match → enrich → persist; the TRaSH-style path regex (in `resolvers/pathembed.go`).
- Added: the `match_decision` log; `file_state.osdb_hash` on every file (owned by [files](../files/README.md#the-file_state-sidecar--filesystem-facts)); `MatcherService` and the resolver + aggregator; the `name-parse` resolver over the in-process [parser](../parsing/README.md); the [persisted parse](../parsing/README.md#persisted-parse) on scanned files (`file_origin`, `origin='scan'`); banded outcomes; threshold presets (defined, not yet a setting); match-by-ID. Bulk override is unbuilt.
- Changed: suggestions live only on `match_decision.ranked_candidates` (see [files § decision log](../files/README.md#relationship-to-the-decision-log)); year is a soft penalty. **Unbuilt:** paged-stop-on-confidence TMDB search — `name-parse` still reads page 1 only.
- Gone: the FastAPI parsing sidecar, `internal/identity/`, the first-success-wins phase gate, direct TMDB calls in scan, and the `unmatched_file` table. The physical-file model is owned by [files](../files/README.md).

## Interactions

| Neighbor                                      | How matching interacts                                                                                                                                                                                                            |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[Parsing](../parsing/README.md)**           | Provides the string parse (title/year/S-E + per-field confidence) under the `name-parse` resolver. The matcher consumes the parse; it doesn't parse. The parser is a 1:1 Sonarr/Radarr engine port — the sole string-parse signal, no sidecar.            |
| **Scan**                                      | Scan walks the filesystem and produces `FileRef`s (with the parse already attached); matcher consumes them. Scan holds no identification logic.                                                                                  |
| **[Files](../files/README.md)**               | Owns the `file` entity the matcher writes identity onto. `match_decision.file_id` is `file.id`; match/re-match/un-match are in-place identity `UPDATE`s on the file row, never row swaps, so the decision chain stays continuous. |
| **[Metadata](../metadata/README.md)**         | Matching is meant to read external IDs through the [`media_item_external_id` registry](../metadata/README.md#identity); the built cross-provider resolution (IMDb→TMDB) goes through the provider's `LookupByID`, not the registry. The resolved series' `series_type` anchors the future `episode-numbering` resolver. |
| **[Sources](../sources/README.md)**           | ID validation ("does this id resolve?") and the cross-provider fallback call the canonical [`MetadataSource`](../sources/README.md#roles-not-one-provider) role seam; the future `episode-numbering` resolver consumes the anime `NumberingMap`. Matching depends on the seam, never on TMDB directly. |
| **[Hygiene](../hygiene/README.md)**           | `identity/unmatched-file` and `identity/wrong-match-suspect` are findings over matcher state. Hygiene is rollup; matcher is drill-down.                                                                                           |
| **[Tracking / wants](../tracking/README.md)** | Drop-in match satisfying an open want closes the want (unbuilt). Reverse: when a want fulfills via grab, the importer creates the file with identity already set — no match decision is written.                              |
| **[Acquisition](../acquisition/README.md)**   | Grab/route decisions and match decisions are sibling producers under the [decision-artifact pattern](../../patterns/audit/README.md). Separate tables (shapes differ), shared retention + Activity view.                          |
| **Import**                                    | Matched files flow through import (hardlink, rename via templates). Re-matches trigger re-import for the destination change (unbuilt).                                                                                            |
| **Name templates**                            | Matcher writes identity; templates consume it. Wrong match → wrong location → re-match cascades the rename.                                                                                                                       |
| **OpenSubtitles (v2)**                        | The `osdb-hash` resolver is the integration point. Hash computed and stored in v1; lookup deferred to v2 when integration ships.                                                                                                  |

## Open questions

1. **Match-decision retention.** How long do we keep superseded `match_decision` rows? Forever (audit) or trim after N (storage)? Lean forever — they're small and tell the story of a file's journey. Revisit if `match_decision` table grows pathologically.
2. **`partial_series` behavior — resolved.** A partial-series file carries the series identity on the `file` row (`media_item_id` set, `episode_id` NULL); `partial_series` is a derived state, not a stored flag or a separate table. The half-identity that worried this OQ is fine because it lives on the file as a first-class identity granularity — see [files § identity](../files/README.md#identity-as-state-with-a-ratified-invariant). Settled by the unified file model.
3. **Edition modeling.** Is `edition` a free-text field in v1 and a proper enum later? Or do we ship with a v1 enum (`theatrical`, `extended`, `directors_cut`, `unrated`, `other`)? Lean enum + nullable free-text companion.
4. **TMDB validation caching.** A 22-episode season pack with the same embedded series ID shouldn't trigger 22 validation calls. Aggregator-level batch dedup + response cache. Probably already exists for current TMDB calls — verify and reuse. — *Answered by the code: `TmdbService` caches find/details responses in `api_cache`, so repeats hit the cache; there's no in-batch dedup.*
5. **Cross-resolver disagreement on tier short-circuit.** If Tier 1 short-circuits with high confidence and a user later re-matches manually with a different identity, the chain is auditable but the disagreement signal is lost. Should we backfill by running Tier 2/3 in the background after a short-circuit, just to populate evidence? Probably no — cheap, but storage-bloaty and not load-bearing.
6. **Stale-ID hygiene finding.** When TMDB returns a redirect for an embedded ID, the path has a stale ID. Should this surface as a hygiene finding (`identity/stale-embedded-id`) so the user can rename? Probably yes — same shape as `layout/naming-drift`.
7. **Per-library resolver toggle UX.** Eventually a user might want `osdb-hash-only` or `name-parse-only` for a given library. The registry is shaped for this; the UI is v2. Data shape: per-library resolver-enable list, falls back to global. Confirm in the data-shape iteration.
8. **Confidence calibration.** The bands (0.95 / 0.7 / 0.5) and the resolver weights (0.95 path-embed, 0.6 name-parse, etc.) are guesses. Real tuning requires the labeled corpus — the same one [parsing](../parsing/README.md#testing-strategy--parity-as-a-ci-gate) builds, which calibrates both. v1 ships with educated defaults; per-user threshold and per-resolver weight overrides come later as needed.
9. **Validation provider abstraction — clarified.** The aggregator calls the canonical [`MetadataSource`](../sources/README.md#roles-not-one-provider) seam for ID validation and reads the [`external_id` registry](../metadata/README.md#identity) for cross-provider resolution. Order of operations: metadata's external_id registry ships first, matcher v1 uses it. The only remaining open part is the exact method signatures, extracted on the second provider per [sources](../sources/README.md).
10. **Resolver evidence size cap.** A resolver's raw evidence payload or a TMDB candidate list can be sizable JSON. Hard cap on `evidence` JSONB size per decision? Probably yes, with a "truncated" flag. 8KB feels generous. — *Answered by the code: 8KB cap with `evidence_truncated`, largest payload trimmed first.*
11. **Drop-in detection latency.** Inotify on library directories vs periodic re-scan? Inotify is real-time but fragile across NFS/SMB; re-scan is reliable but laggy. Probably both, configurable. Out of scope for matcher v1 — owned by scan.

## What we're explicitly not deciding here

- Exact table names, columns, indexes for `match_decision`
- API endpoint shapes for the matcher service
- Inotify vs polling for drop-in detection
- The matcher inbox UI layout — only the data and surfaces are committed
- v2 OpenSubtitles integration details (auth, rate limits, API version) — separate concern when v2 ships
- Anime / specials / multi-version edge cases — model accommodates them but tuning is later
- Resolver confidence weights and aggregation formula — initial values are educated guesses; tuning is empirical
- Per-library resolver toggle UI — data shape committed; UI deferred

Each gets its own pass once this model holds up against real libraries.
