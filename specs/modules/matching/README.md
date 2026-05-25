# Matching — file-to-identity resolution

**Status:** Draft, iteration 1

This doc defines **matching**: how Arrflix takes a file on disk and produces a confident identity for it — a TMDB-linked media item, with episode info for series, and an audit trail. It captures _what matching is_, _what the resolver + aggregator architecture looks like_, _how confidence is modeled_, and _how re-match and un-match work_. It does **not** pin down table names, column types, or wire formats — those come in a later iteration.

This doc supersedes the "Unmatched Media Resolution" line item in [the roadmap](../../docs/guide/roadmap.md#unmatched-media-resolution), which was scoped narrowly to a fix-match UI. The greenfield design treats matching as a hub system that touches scan, identity/metadata, hygiene, tracking, the decision log, and import.

## TL;DR

- Matching is its own subsystem in `internal/matcher/`. The existing identification logic (scattered across `scan.go`, `internal/identity/`, and the guessit client) gets restructured into a clean **resolver + aggregator** pipeline.
- **Resolvers** are independent identity signals (path-embed, name-parse, OSDb hash, embedded tags). They each emit candidate identities with their own confidence. They live in `internal/matcher/resolvers/`.
- **The string parse comes from [parsing](../parsing/README.md), not from a resolver-local parser.** The `name-parse` resolver turns the unified parser's identity hint (title/year/season/episode, with per-field confidence) into a TMDB candidate. **guessit** is demoted to an _optional fallback_ resolver — it supplies an alternate parse only when the primary parse is low-confidence, is gated by `Available()`, and is removed once Tier-1 identity parity holds (see [parsing § guessit disposition](../parsing/README.md#guessit-disposition)).
- **Aggregator** combines votes across resolvers, validates against the metadata provider, computes a combined confidence, and bands the outcome.
- **Tiered execution** for performance: ground-truth resolvers run first; if they produce a validated identity, more expensive resolvers are skipped. Aggregator math is unchanged — tiering is an optimization, not a correctness mechanism.
- **Match decisions are first-class records.** Every consequential decision (auto-match, manual match, re-match, un-match) writes a `match_decision` row with full evidence. Re-matches supersede prior decisions; the chain is auditable.
- Six **outcomes** by confidence band: `confident`, `confident_review`, `low_confidence`, `ambiguous`, `no_match`, `partial_series`.
- **Drop-in files** flow through the same pipeline as scan-discovered files. A successful drop-in match may close a pending want.
- **v2 hooks** — OSDb hash storage and per-library resolver toggle — are designed in v1, implemented later.

## What matching is, and isn't

Matching answers one question: *given a file at this path, what identity does it represent?* Identity = `(provider, external_id, episode_ref?, edition?)`.

Matching is **not**:
- Metadata fetching (what *is* the identified content) — that's [metadata](../metadata/README.md)
- Hardlink / rename mechanics — that's import + name templates
- Quality decisions on releases — that's [quality profiles](../quality-profiles/README.md)
- Tracking lifecycle — that's [tracking](../tracking/README.md)

Matching's output is a decision. Everything downstream consumes that decision.

## The v0 baseline (what exists today)

The existing pipeline lives in `backend/internal/service/scan.go` as a 4-phase scanner. Briefly:

1. **Walk & collect** library files, dedup against `media_file` and `unmatched_file`.
2. **Embedded ID resolution** — regex on path for `tmdb-X` / `tvdb-X` / `imdb-X`. If hit, convert to TMDB via `tmdb.FindByID()`.
3. **Guessit + TMDB search** — for unresolved files, call the FastAPI sidecar at `127.0.0.1:8000` for title/year/S-E parse, then `tmdb.MultiSearch()` (page 1 only), filter by year (hard exclusion if year mismatches or TMDB returns `year==0`). One candidate → identified. Zero or multiple → up to 5 ranked suggestions (100/85/70/55/40) in `unmatched_file.suggested_matches` JSONB.
4. **Persist** — write `media_item`, `media_file`, `media_file_state`, `media_file_import` rows, or upsert into `unmatched_file`.

The v0 pipeline works for well-named libraries but has fragility worth fixing en route:

- **No confidence model.** Auto-match is implicit "one TMDB result survived year filter." No way to express "we're not sure."
- **Year is a hard gate.** `year==0` candidates are dropped silently; year mismatches lose legitimate candidates.
- **TMDB page-1 only.** Right answer on page 2 is invisible.
- **First-success-wins resolvers.** Once embed succeeds, guessit isn't consulted — we lose the corroboration signal.
- **No match-decision log.** Once written, reasoning is lost. Re-matches can't trace history.
- **Suggestions are TMDB-rows, not resolver evidence.** Position-based scores tell you nothing about *why* a candidate looked plausible.

These aren't blockers — the v0 pipeline ships files to libraries. The greenfield design layers reasoning on top of the same external dependencies.

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
                              + media_file OR unmatched_file
                              + (optional) want fulfillment
```

### Resolver contract

Conceptually each resolver implements:

```go
type Resolver interface {
    Name() string
    Available(ctx) bool         // sidecar up, API key present, etc.
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
}
```

Two key shifts vs v0: (1) resolvers can emit multiple candidates with confidences, not "one answer or nothing", and (2) the raw evidence persists on the result so the decision log captures the why.

### The resolver catalog

| Resolver         | Tier | Signal type                                 | v1 / v2 | Notes                                                |
| ---------------- | ---- | ------------------------------------------- | ------- | ---------------------------------------------------- |
| `path-embed`     | 1    | TRaSH-style `tmdb-X` / `tvdb-X` / `imdb-X`  | v1      | Folds in the existing `internal/identity/` regex     |
| `embedded-tags`  | 1    | Container-level IDs (MKV/MP4 metadata)      | v2      | Cheap when present; relies on ffprobe                |
| `osdb-hash`      | 2    | OpenSubtitles file fingerprint → IMDb       | v2      | Hash always computed in v1; lookup gated by user opt-in |
| `name-parse`     | 3    | Parsed title/year/S-E from the unified [parser](../parsing/README.md) | v1 | Primary string-parse signal; reads `internal/parsing` (no sidecar) |
| `guessit`        | 3 (fallback) | Alternate parse when `name-parse` identity confidence is low | v1 → removed | Optional, gated by `Available()`; wraps `internal/guessit`; retained longest for anime, then deleted |
| `path-context`   | 3    | Sibling-file and directory inference        | v1      | Low signal, useful tiebreaker                        |

Per-library resolver toggle is a v2 surface, but the catalog is **registry-shaped from v1** — resolvers register at startup, the aggregator iterates the registered set. Adding/removing resolvers later is dropping in / out of a slice, not a code restructure.

### Aggregator contract

The aggregator owns:

1. **Tier orchestration** — runs Tier 1 first; validates Tier-1 candidates against the metadata provider; if a candidate is complete and validated above the auto-match threshold, short-circuits. Otherwise descends to Tier 2/3.
2. **Cross-provider ID resolution** — IMDb → TMDB, TVDB → TMDB. Today done inline via `tmdb.FindByID()`; in the greenfield world, defers to whatever the metadata layer provides ("whatever the pattern is at the time").
3. **Candidate merging** — same `ExternalRef` from multiple resolvers compounds confidence; soft year disambiguation as a penalty rather than a hard filter.
4. **Outcome banding** — see [Confidence bands](#confidence-bands).
5. **Decision recording** — writes one `match_decision` row per consequential decision, always.

The aggregator is the only place that knows about confidence math, validation rules, and tier composition. Resolvers don't know about each other.

### Why tier + aggregate (not just one or the other)

Pure all-parallel-then-aggregate is correctness-clean but wasteful for the (frequent) TRaSH-style case where the path tells us everything. Pure tiered-fallback is fast but throws away the corroboration signal that lets us trust auto-matches.

Doing both — **tiered execution, parallel aggregation when needed** — gives us:
- TRaSH-style libraries identify in microseconds, no guessit / TMDB-search load
- Imperfect libraries get full multi-resolver corroboration
- Aggregator math is the same in either case (one vote or three; same formula)

## Confidence bands

| Outcome              | Confidence | Side effect                                                              |
| -------------------- | ---------- | ------------------------------------------------------------------------ |
| `confident`          | ≥ 0.95     | `media_file` written; no review                                          |
| `confident_review`   | 0.7–0.95   | `media_file` written; flagged for review in matcher inbox                |
| `low_confidence`     | 0.5–0.7    | `unmatched_file` written with one strong suggestion                      |
| `ambiguous`          | < 0.5 or multiple-candidate-tie | `unmatched_file` written with up to 5 suggestions       |
| `no_match`           | nothing ≥ 0.5 | `unmatched_file` written; no suggestions                              |
| `partial_series`     | series confident, episode unresolved | `unmatched_file` with `partial_series` flag             |

### Threshold presets

Per-installation setting, three starter configs:

| Preset       | Auto threshold | Review band | Best for                                              |
| ------------ | -------------- | ----------- | ----------------------------------------------------- |
| **Strict**   | 0.95           | 0.7–0.95    | OCD libraries; reviewer-on-the-loop                   |
| **Recommended** | 0.85        | 0.7–0.85    | Balanced default                                      |
| **Relaxed**  | 0.7            | 0.5–0.7     | Just-fill-the-library; tolerate occasional mismatches |

User-configurable. Same shape as the [hygiene](../hygiene/README.md#presets) preset model.

### Validation modulates confidence, not resolvers

When a Tier-1 resolver returns an embedded TMDB ID, its resolver-local confidence is 1.0 — we trust our own regex. The aggregator then validates against the metadata provider:

| Validation outcome              | Final confidence |
| ------------------------------- | ---------------- |
| ID resolves to a valid entry    | ~0.99            |
| ID resolves with a redirect (merged) | ~0.95; write redirected ID; log redirect |
| ID returns 404 / deleted        | 0.0; candidate dropped; fall through to next tier |

This is how "the ID could be stale" gracefully degrades. The resolver doesn't know about provider state; the aggregator does.

## The match-decision artifact

Every consequential decision writes a row, schematically:

```
match_decision(
  id,
  file_id,                       -- the file being identified
  outcome,                       -- 'confident', 'confident_review', 'low_confidence', 'ambiguous', 'no_match', 'partial_series'
  chosen_external_ref,           -- nullable (set only on success)
  chosen_episode_ref,            -- nullable
  chosen_edition,                -- nullable text
  confidence,                    -- final aggregated value
  resolvers_consulted (JSONB),   -- which resolvers ran and what they returned
  evidence (JSONB),              -- the full per-resolver evidence payload
  decided_by,                    -- 'auto' | 'user:<id>' | 'rule:<id>'
  decided_at,
  superseded_at,
  superseded_by                  -- FK to a later decision
)
```

**Current match** for a file = the latest non-superseded `match_decision` row. Re-matches insert a new row and set the prior's `superseded_by`. Un-matches insert a new row with `outcome: no_match` and `decided_by: user:<id>`.

This follows the system-wide [decision-artifact pattern](../../patterns/audit/README.md): each producer owns its own table (shape diverges) but the principle, retention, and Activity-view aggregation are shared.

## Re-match, un-match, detach

Three distinct actions, three different effects:

| Action       | What it does                                          | Filesystem effect                                       |
| ------------ | ----------------------------------------------------- | ------------------------------------------------------- |
| **Re-match** | "This is identified as X; it's actually Y"            | File moves/renames via name templates; old hardlinks resolved |
| **Un-match** | "I don't know what this is; back to the inbox"        | File stays in place; `media_file` row removed; `unmatched_file` row created |
| **Detach**   | "This file doesn't belong in the library at all"      | File optionally moved to a configured quarantine path   |

All three write a `match_decision` row with `decided_by: user:<id>` and supersede the prior decision.

Re-matches are reversible via the decision-log chain — every prior decision is preserved, so "undo" means "supersede the current decision with a copy of an earlier one." This is the antidote to Sonarr's irreversible-wrong-match horror story.

## Matcher surfaces

Three UI surfaces, all backed by the same data:

1. **Inbox.** `/library/matching` (or similar). All files in `unmatched_file`, plus all `confident_review`-banded matches. Grouped by outcome and source (scan / drop-in / re-match). Count surfaced in the dashboard chrome to make it feel actionable.

2. **Drill-down / decide pane.** Click an inbox item → see file path, parsed title/year, ranked suggestions with evidence per suggestion (*"guessit says 0.78, path-context says 0.31"*), one-click match, "Search TMDB" fallback, "Match by ID" power-user input.

3. **Drop-in flow.** File appears in a watched library directory → scan picks it up → matcher runs the same pipeline. If `confident` outcome, file appears in library and a [notification](../notifications/README.md) fires (*"Dropped Breaking.Bad.S01E03.mkv. Auto-matched as Breaking Bad S01E03."*). Otherwise lands in the inbox.

## Drop-in fulfills wants

When a drop-in match resolves to identity that satisfies an open want (an episode the user has been waiting for, a movie they requested), the matcher:

1. Closes the want, marking it `available`
2. Fires Story 1's success [notifications](../notifications/README.md) (push, etc.)
3. Logs the match decision with a back-reference to the want

This collapses Sonarr's separate "import from outside" workflow into the same surface as everything else. There's no second affordance; the matcher *is* the manual-grab side door.

## Killer UX moves

The differentiators between *another fix-match modal* and *a matcher people enjoy using*:

1. **Match by external ID, directly.** *"Paste a TMDB or IMDb URL."* The most precise interface. Aligns with the future where multiple providers are supported.
2. **"Why didn't this match" explanations.** Each unmatched item surfaces what resolvers tried, what candidates they considered, why none cleared the bar. Same decision-log philosophy.
3. **Bulk match-by-folder.** *"All files in this folder are episodes of The Office (US)."* Pick the series once → file parses resolve episodes. Saves immense work on season packs that arrive without metadata. Implemented as a manual override that bypasses resolvers — writes one `match_decision` row per affected file with `decided_by: user:<id>`.
4. **Match preview before commit.** Show the rename diff, the destination path, the poster, before the user confirms.
5. **Drop-in fulfills want.** See above.
6. **Reverse search by content fingerprint** (v2). Files that came from torrents may have an audit trail in qBittorrent we can cross-reference. Sometimes the trace is better than the parse.
7. **Confidence-banded inbox.** *"3 items need decision, 12 auto-matched but flagged for review, 47 confidently matched this week."* The inbox is a match-decision feed, not a "what's broken" list.
8. **Edition-aware matching.** When TMDB returns one ID but the file is clearly a director's cut, surface a "which edition?" prompt rather than silently collapsing them.

## Code structure

```
backend/internal/
  parsing/                          ← the unified string parser (see parsing spec)
                                       title/year/S-E + quality + attributes, pure Go

  guessit/                          ← thin HTTP client for the FastAPI sidecar
                                       (optional fallback only; removed once parity holds)

  matcher/                          ← all matching logic
    service.go                      ← MatcherService (the public surface)
    aggregator.go                   ← combine + decide
    resolver.go                     ← Resolver interface, ExternalRef, Candidate types
    resolvers/
      pathembed.go                  ← folds in the existing internal/identity/ regex
      nameparse.go                  ← primary string-parse signal; reads internal/parsing
      guessit.go                    ← optional fallback; uses internal/guessit
      osdbhash.go                   (v2 stub)
      embeddedtags.go               (v2 stub)
```

Rule of thumb: a package gets its own module only when it owns a boundary — an external service, a layer, or a cross-cutting primitive. Resolvers are internal to the matcher; they live there.

`ScannerService` shrinks dramatically: it loses its Phase 2/3 identification logic and calls `MatcherService.MatchBatch(files)` instead. The scanner is filesystem-aware; the matcher is identity-aware. Clean line.

## Migration from v0

What stays:
- `media_item`, `media_file`, `media_file_state`, `media_file_import` schemas
- `unmatched_file` table (with richer `suggested_matches` JSONB shape)
- `internal/guessit/` client and the FastAPI sidecar — **transitionally**, as an optional fallback resolver only; removed once the unified [parser](../parsing/README.md) clears Tier-1 identity parity (see [parsing § guessit disposition](../parsing/README.md#guessit-disposition))
- The 4-phase scan structure (walk → identify → enrich → persist), reframed as walk → match → enrich → persist
- The TRaSH-style path regex (relocated to `internal/matcher/resolvers/pathembed.go`)

What's new:
- `match_decision` table — the decision log artifact
- `media_file.osdb_hash` column — populated unconditionally from v1; consumed by v2 OS resolver and v1 hygiene dedup
- `MatcherService` with the resolver + aggregator architecture
- The `name-parse` resolver backed by the unified [parser](../parsing/README.md), the primary identity signal (replacing guessit)
- The [persisted parse](../parsing/README.md#persisted-parse) on scanned files (`origin: scanned`, parsed from the filename, best-effort), written alongside the `media_file`
- Confidence model + banded outcomes
- Threshold presets in settings (Strict / Recommended / Relaxed)
- Match-by-ID and bulk-override surfaces

What evolves:
- `unmatched_file.suggested_matches` grows from position-rank to per-suggestion `{external_ref, confidence, contributing_resolvers, evidence}` — backwards-compatible (old shape is a subset)
- Year handling moves from hard filter to soft penalty
- TMDB pagination becomes paged-stop-on-confidence rather than page-1-only

What dies:
- `internal/identity/` as a top-level package (regex relocates into matcher/resolvers)
- The first-success-wins phase gate in scan.go (replaced by tiered aggregation)
- Direct TMDB calls scattered across scan.go (consolidated in the aggregator)

Since we have zero users besides Kyle, the migration is "drop the old code, run the new scan." No data preservation gymnastics needed.

## Interactions

| Neighbor                                              | How matching interacts                                                                                                              |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| **[Parsing](../parsing/README.md)**                   | Provides the string parse (title/year/S-E + per-field confidence) under the `name-parse` resolver. The matcher consumes the parse; it doesn't parse. guessit is an optional fallback parse, removed once parity holds. |
| **Scan**                                              | Scan walks the filesystem and produces `FileRef`s; matcher consumes them. Scan loses identification logic entirely.                 |
| **[Metadata](../metadata/README.md)**                 | Matching writes external IDs via the metadata layer's interface. Cross-provider resolution (IMDb→TMDB) goes through metadata.       |
| **[Hygiene](../hygiene/README.md)**                   | `identity/unmatched-file` and `identity/wrong-match-suspect` are findings over matcher state. Hygiene is rollup; matcher is drill-down. |
| **[Tracking / wants](../tracking/README.md)**         | Drop-in match satisfying an open want closes the want. Reverse: when a want fulfills via grab, the matcher gets a pre-confirmed identity. |
| **[Acquisition](../acquisition/README.md)**           | Grab/route decisions and match decisions are sibling producers under the [decision-artifact pattern](../../patterns/audit/README.md). Separate tables (shapes differ), shared retention + Activity view. |
| **Import**                                            | Matched files flow through import (hardlink, rename via templates). Re-matches trigger re-import for the destination change.        |
| **Name templates**                                    | Matcher writes identity; templates consume it. Wrong match → wrong location → re-match cascades the rename.                          |
| **OpenSubtitles (v2)**                                | The `osdb-hash` resolver is the integration point. Hash computed and stored in v1; lookup deferred to v2 when integration ships.    |

## Open questions

1. **Match-decision retention.** How long do we keep superseded `match_decision` rows? Forever (audit) or trim after N (storage)? Lean forever — they're small and tell the story of a file's journey. Revisit if `match_decision` table grows pathologically.
2. **`partial_series` behavior.** Series resolved, episode unresolved. Do we write the series-level `media_item` and create a stub `media_file` waiting for episode resolution? Or hold it entirely in `unmatched_file` until episode is identified? Lean: hold in `unmatched_file` — half-identity creates confusing UX downstream.
3. **Edition modeling.** Is `edition` a free-text field in v1 and a proper enum later? Or do we ship with a v1 enum (`theatrical`, `extended`, `directors_cut`, `unrated`, `other`)? Lean enum + nullable free-text companion.
4. **TMDB validation caching.** A 22-episode season pack with the same embedded series ID shouldn't trigger 22 validation calls. Aggregator-level batch dedup + response cache. Probably already exists for current TMDB calls — verify and reuse.
5. **Cross-resolver disagreement on tier short-circuit.** If Tier 1 short-circuits with high confidence and a user later re-matches manually with a different identity, the chain is auditable but the disagreement signal is lost. Should we backfill by running Tier 2/3 in the background after a short-circuit, just to populate evidence? Probably no — cheap, but storage-bloaty and not load-bearing.
6. **Stale-ID hygiene finding.** When TMDB returns a redirect for an embedded ID, the path has a stale ID. Should this surface as a hygiene finding (`identity/stale-embedded-id`) so the user can rename? Probably yes — same shape as `layout/naming-drift`.
7. **Per-library resolver toggle UX.** Eventually a user might want `osdb-hash-only` or `no-guessit` for a given library. The registry is shaped for this; the UI is v2. Data shape: per-library resolver-enable list, falls back to global. Confirm in the data-shape iteration.
8. **Confidence calibration.** The bands (0.95 / 0.7 / 0.5) and the resolver weights (0.95 path-embed, 0.6 name-parse, etc.) are guesses. Real tuning requires the labeled corpus — the same one [parsing](../parsing/README.md#testing-strategy--parity-as-a-ci-gate) builds, which calibrates both. v1 ships with educated defaults; per-user threshold and per-resolver weight overrides come later as needed.
9. **Validation provider abstraction.** The aggregator calls "the metadata provider" for ID validation. The metadata spec lays out a provider seam. The matcher writes against whatever the metadata layer exposes — exact contract resolves when external_id work lands. Order of operations: metadata's external_id pattern ships first, matcher v1 uses it.
10. **Resolver evidence size cap.** A `guessit` raw response or a TMDB candidate list can be sizable JSON. Hard cap on `evidence` JSONB size per decision? Probably yes, with a "truncated" flag. 8KB feels generous.
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
