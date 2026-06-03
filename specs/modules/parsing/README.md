# Parsing — release names and filenames into structured attributes

**Status:** Draft, iteration 1

This doc defines **parsing**: the subsystem that turns a string — an indexer release title or an on-disk filename — into a structured set of **advertised** attributes (identity hints, quality, release metadata), each carried with a confidence and a provenance trail. It captures _what parsing is_, _why it's a foundational module below matching_, _the canonical attribute model it produces_, _the single-engine design_, _the disposition of guessit_, and _the corpus + parity-harness testing strategy that keeps us honest against Sonarr/Radarr_. It does **not** pin down struct field names, regex tables, or wire formats — those land in code, with this spec as the contract.

Parsing is **advertised-only**. It never opens the file. The ground-truth counterpart — `ffprobe`/mediainfo reading the actual bytes — is a separate _asserted_ extractor (see [Advertised vs asserted](#advertised-vs-asserted-the-output-boundary)); the comparison of the two is a downstream concern owned by [quality profiles](../quality-profiles/README.md) and [acquisition](../acquisition/README.md).

## TL;DR

- Parsing is a **pure, deterministic, Go-native function**: `Parse(input, domain) → ParsedRelease`. No DB, no network, no sidecar. One engine per domain (series / movie), two input flavors (release title, on-disk path).
- It is a **layer below** [matching](../matching/README.md), [quality profiles](../quality-profiles/README.md), [name templates](../name-templates/README.md), [routing](../routing/README.md), and the [importer](../importer/README.md) — all of them consume the parse; none of them own it.
- **Domain is required at the call site.** Every consumer already knows it: candidate searches are TMDB-id-anchored, the importer reads the job's media type, scan inherits from the library, the policy test endpoint inherits from the UI tab. So parsing takes `Domain` as an argument, not a hint. The auto-detect path is removed.
- **One parser, finished — not two patched together.** v0 ported the _quality half_ of Sonarr/Radarr's parser (`internal/release/`) for name templates, then bolted on a **guessit** Python sidecar for the identity half. Green-field, we finish the port into a single engine and **retire guessit**.
- The output is the **canonical advertised attribute model** — identity hints (title, year, season/episode, absolute, edition), quality (source, resolution, modifier, the rendered **bin name**, proper/repack), and release attributes (group, codec, audio, HDR-claim, languages). It populates the advertised namespaces of the shared `EvaluationContext`.
- **Every field carries confidence + provenance.** Not "source = BluRay" but "source = BluRay (0.9), token `BluRay` @ idx 6". This feeds the matcher's confidence bands, the advertised-vs-asserted check, and an explainable "why did we parse it this way" UI.
- **Parity is a CI gate and a published number.** A version-stamped, labeled **corpus** is the source of truth. Tier-1 CI diffs the parser against committed **golden** expected-output every PR (hermetic, fast). Tier-2 regenerates goldens from **pinned live Sonarr/Radarr** containers on a schedule. An **intentional-divergence allowlist** keeps "we differ on purpose" from breaking the build — and per-domain dispatch shrinks that allowlist to one principled class (identity-independent quality, OQ#13).
- **Sonarr and Radarr are separate codebases** with shared quality heritage and diverged domain parsing. We test both, by domain: series releases → Sonarr, movie releases → Radarr.

## Why parsing is its own module

Today the parser has no home. Four other specs reference it as something that exists "somewhere":

- [name templates](../name-templates/README.md#what-name-templates-does-not-own): _"The name-parser that produces `Quality` and `Release` from a release title (lives with quality / parsing infrastructure)."_
- [quality profiles](../quality-profiles/README.md#what-quality-profile-does-not-own): _"Quality detection / release name parsing — the parser is a sibling system."_
- [importer](../importer/README.md#open-questions) OQ#5: _"Promote the filename-marker parser into a package both the importer and matching consume."_
- [matching](../matching/README.md) frames `guessit` as one resolver, but the _string parse_ underneath every resolver is unowned.

That's four consumers pointing at a "sibling system" that no spec defines. Parsing is genuinely foundational — it sits **below** matching (identity), quality profiles (gate/score), name templates (render), routing (rules), and the importer (closed-world assignment). Giving it its own module makes the dependency explicit and the engine a single, testable thing instead of logic smeared across `internal/release/`, `internal/identity/`, the guessit client, and a private copy in the importer.

## The history this corrects

v0's two parsers are an accident of build order, not a design:

1. The first flow was **manual download selection**, which needed [name templates](../name-templates/README.md), which needed quality attributes. So we ported the **quality half** of Sonarr/Radarr's parser into `internal/release/release.go` — and built a 310-title parity harness (`cmd/quality-test/`) against live Sonarr to prove it.
2. Then **identity detection for existing libraries** needed title/year/season/episode parsing — the _other half_ of the same Sonarr/Radarr parser. Instead of finishing the port, we added **guessit** (a Python sidecar) as a patch.

Sonarr/Radarr's parser is **one engine** that extracts everything. We ported a slice and prosthetised the rest. Green-field, the move is to **finish the port into one Go engine** and remove the prosthetic.

## What parsing owns

- The **string → attributes** transformation, for both release titles and filenames, dispatched per domain.
- The **canonical advertised attribute model** (`ParsedRelease`) and its per-field confidence + provenance.
- **Quality detection AND bin rendering**, both per domain. The internal representation is the orthogonal **attribute core**: source, resolution, modifier (`REMUX` / `BR-DISK` / `RAWHD` / …), and revision (proper / repack / real). The per-domain **bin** (`Bluray-1080p Remux` for series, `Remux-1080p` for movies) is rendered from `(core, domain)` at parse time and populated on `ParsedRelease.Quality.Name`. See [The quality attribute core](#the-quality-attribute-core).
- **Per-domain detection latitude.** Each domain's detection logic can follow its upstream's `QualityParser` / `Parser` verbatim — series follows Sonarr, movies follow Radarr. Both still terminate in the same orthogonal core (Sonarr's flattened pair is a lossy projection of Radarr's triple — see [Why the core stays orthogonal](#why-the-core-stays-orthogonal-even-with-per-domain-detection)).
- **Identity-hint extraction** (parsed title, year, season, episode(s), absolute number, daily/air-date, multi-episode range, edition) — the port that replaces guessit.
- **Release-attribute extraction** (release group, codec, audio format/channels, HDR _claim_, dual-audio _claim_, language(s), hardcoded-subs claim — movie-only). Dual-audio is reserved as an advertised attribute now because it's a first-class anime quality axis (sub/dub) and, unlike source/edition, `ffprobe` can _assert_ it. Fansub group is the anime case of `release group`; no separate field. Hardcoded-subs is movie-domain only and is fed by Radarr's `ParseHardcodeSubs`.
- The **labeled corpus** and the **parity-harness contract** (Tier-1 golden diff, Tier-2 live regeneration).
- The **per-domain bin vocabulary tables** (Sonarr `Quality.cs` for series, Radarr `Quality.cs` for movies). Verbatim ports of the upstream tables; their ordering matches upstream so default quality lists drawn from them stay faithful.

## What parsing does NOT own

- **Identity resolution** — turning a parsed title into a real `media_item` / TMDB entity. That's [matching](../matching/README.md); parsing supplies the hint, matching validates and bands it.
- **Asserted file analysis** — `ffprobe`/mediainfo reading the actual bytes. Separate extractor; see below. It fills the `MediaInfo` namespace, not parsing.
- **Gating and scoring** — [quality profiles](../quality-profiles/README.md) consume the parse (including the rendered bin); they don't parse and don't own the bin vocabulary tables.
- **The advertised-vs-asserted re-gate** — comparing parse to ffprobe at import. Owned by [quality profiles](../quality-profiles/README.md) (logic) and [acquisition](../acquisition/README.md) (orchestration); parsing only defines the advertised half of the comparison.
- **Path/folder mechanics, name rendering, routing decisions** — downstream consumers.

## The canonical attribute model

`Parse` produces a `ParsedRelease`. Its fields map onto the **advertised** namespaces of the shared `EvaluationContext` (`backend/internal/model/context.go`), the same struct [routing](../routing/README.md) and [name templates](../name-templates/README.md) already read:

| Group        | Fields (directional)                                                                 | Maps to                          |
| ------------ | ------------------------------------------------------------------------------------ | -------------------------------- |
| **Identity** | parsed title, year, type hint (movie/series), season, episode(s), absolute #, daily/air-date, multi-ep range, edition | feeds [matching](../matching/README.md) → `Media`; feeds importer assignment |
| **Quality**  | source, resolution, modifier (`REMUX`/`BR-DISK`/`RAWHD`/…), revision (`version`/`real`/`isRepack`) — the orthogonal **attribute core** — plus **bin name** (`name`, e.g. `"Bluray-1080p"`) and **full** (`name` + revision render, e.g. `"Bluray-1080p Proper"`) rendered from `(core, domain)` at parse time | `Quality` namespace; `quality.name` mirrors *arr's `Quality.Name` API field, `quality.full` mirrors *arr's `{Quality Full}` naming-token semantics |
| **Release**  | release group, codec, audio format, audio channels, HDR-claim, dual-audio-claim, languages | `Release` namespace (+ extensions) |

Three boundary notes:

- **Identity is a _hint_, not a resolution.** Parsing says "this string looks like *The Office* S03E05"; [matching](../matching/README.md) decides whether that's TMDB id N with the right confidence. The `Media` namespace is populated by matching/metadata, **not** by parsing.
- **Numbering is parsed in the release's _own namespace_, not the canonical one.** A release numbers episodes however its scene/fansub convention does — Western `S03E05`, or anime-style **absolute** (`One Piece - 1071`), or a scene numbering that matches neither the provider's seasons nor the absolute count. Parsing emits the number(s) it sees **tagged with which namespace they're in** (`season_episode` / `absolute` / `daily`) and never silently coerces an absolute number into a `(season, episode)`. Reconciling that namespace against the provider's canonical episode identity is [matching](../matching/README.md#the-resolver-catalog)'s job — a future `episode-numbering` resolver that reads the resolved series' [`series_type`](../metadata/README.md#series-type-the-numbering-regime-anchor) (the canonical, series-side declaration of which namespace its episodes live in) and the anime [`NumberingMap`](../sources/README.md#anime-embedded-data-vs-api) — so parsing only has to _preserve the distinction_. This is the load-bearing seam for anime: collapsing numbering to a single `(season, episode)` shape bakes in "release-numbering == provider-numbering," which is exactly the assumption anime breaks.
- **The raw `Candidate` namespace** (indexer result: size, seeders, indexer, GUID, …) is **not** parsed — it's passed through from the search result. Parsing reads the candidate's _title_; it doesn't own the candidate.

### The quality attribute core

The internal representation of quality is the orthogonal **core** — three independent axes plus a revision:

```
QualityCore = (Source, Resolution, Modifier, Revision)
  Source     — BluRay | WEB-DL | WEBRip | HDTV | DVD | CAM | Telesync | … (the medium)
  Resolution — 2160p | 1080p | 720p | 576p | 480p | SD
  Modifier   — NONE | REMUX | BR-DISK | RAWHD | SCREENER | REGIONAL
  Revision   — version (proper count) | real | isRepack
```

**Why orthogonal.** Sonarr and Radarr both descend from the same quality heritage but decompose it differently, and one is strictly richer:

- **Radarr** keeps three orthogonal axes: `Remux-2160p` is `(BluRay, 2160p, REMUX)`, `BR-DISK` is `(BluRay, _, BR-DISK)`, `Raw-HD` is `(HDTV, _, RAWHD)`.
- **Sonarr** has no modifier axis — it folds "raw/remux-ness" into extra _source_ values (`BlurayRaw` for remux, `TelevisionRaw` for raw-HD), so `Bluray-2160p Remux` is just `(BlurayRaw, 2160p)`.

Sonarr's model is a **degenerate projection** of Radarr's: the orthogonal core can render *both* vocabularies losslessly, but Sonarr's flattened pair cannot reconstruct Radarr's. So the core adopts **Radarr's decomposition** internally, regardless of which domain a parse is running for. The two missing axes this adds over v0 — a first-class `Modifier` (generalizing the old `IsRemux` bool) and **`BR-DISK` / full-disc detection** (absent in v0) — are exactly what Radarr's parser already detects.

**The bin IS parsed, at the end.** A *bin* — `Bluray-1080p Remux` (series vocabulary) vs `Remux-1080p` (movie vocabulary) — is a render of `(core, domain)`. Because the caller passes the domain to `Parse`, parsing knows it authoritatively and can render the bin name at parse time. The output `ParsedRelease.Quality.Name` is the rendered bin; the orthogonal axes (`Source`, `Resolution`, `Modifier`) remain available alongside it as the underlying truth. The bin vocabulary tables (`seriesBins`, `movieBins`) are verbatim ports of upstream `Quality.cs` and live in the parsing package.

#### Why the core stays orthogonal even with per-domain detection

Detection can follow each upstream verbatim (and probably should, for parity), but the **internal representation** stays unified on Radarr's orthogonal triple for three reasons:

1. **Rule schema uniformity.** A routing rule like `quality.source == "BluRay"` should work identically across domains. Sonarr's flat model would force series rules to enumerate `[BluRay, BlurayRaw]` and movie rules `[BluRay]` — same concept, different syntax per domain. The orthogonal core keeps the rule surface uniform.
2. **Modifier axis preservation for series.** A series `BR-DISK` release has nowhere to live in Sonarr's flat model (Sonarr folds it into plain Bluray). The orthogonal core preserves the distinction on the `Modifier` axis even when the bin can't express it.
3. **Persistence shape.** The persisted parse is the orthogonal triple, never a rendered bin string. A vocabulary tweak or a hypothetical library re-type needs no backfill.

**Methodology.** Quality detection is the one sub-parser still on Go's stdlib `regexp` (lookarounds unrolled into Go branching); the re-port brings it onto `regexp2` with `Field[T]` provenance, matching the rest of the engine. `BR-DISK`'s lookahead-heavy regex is itself a reason the re-port needs `regexp2`. Per-domain dispatch splits the extension-fallback table (`extensionQualityMap`) into Sonarr-faithful and Radarr-faithful variants — the table mismatch was the dominant class in today's intentional-divergence allowlist, and per-domain dispatch eliminates it by construction.

### Per-field confidence + provenance

The differentiator over Sonarr's binary parse-or-fail. Every field is more than a value:

```go
type Field[T any] struct {
    Value      T
    Confidence float64 // 0..1
    Evidence   string  // provenance, e.g. "token 'BluRay' @ idx 6"
}
```

This single shift pays off in three places at once:

1. **Matching** consumes per-field confidence directly into its [confidence bands](../matching/README.md#confidence-bands) — the parser's certainty about the title is an input to the aggregator, not a thrown-away boolean.
2. **The advertised-vs-asserted check** needs to know which advertised fields are claims vs. facts. Confidence + the [verifiable taxonomy](#what-ffprobe-can-and-cannot-verify) tell it.
3. **Explainability.** A "Parse Inspector" surface — paste any release name or click any file, see the structured parse with per-token highlighting and per-field confidence — is the "why did we read it this way" debugger Sonarr never had. It's a power-user trust feature, a support tool, and the visible face of the model.

## Advertised vs asserted — the output boundary

Parsing emits **advertised** attributes only: what the string _claims_. The actual file's properties — read by `ffprobe` — are **asserted**, and they live in a separate extractor that fills the `MediaInfo` namespace ([importer](../importer/README.md#rendering-the-destination) at grab time; [scan](../scan/README.md#ffprobe-metadata--lazy-out-of-band)'s `MediaProbeWorker` for on-disk files). One asserted-attributes model, two entry points; parsing is neither of them.

The two are deliberately distinct namespaces (`Quality`/`Release` = advertised, `MediaInfo` = asserted) so a consumer can ask for either. The _comparison_ of the two — the import-time re-gate and the mismatch finding — is parked and owned elsewhere; this spec only fixes the contract that parsing produces the advertised side and never the asserted side.

### What ffprobe can and cannot verify

This taxonomy bounds both the mismatch feature and how much trust each advertised field deserves:

| ffprobe **can** assert (advertised value is checkable) | ffprobe **cannot** assert (advertised value is the only source) |
| ------------------------------------------------------- | --------------------------------------------------------------- |
| resolution, video codec, HDR/DV presence, audio format, audio channels, audio track count / languages (dual-audio), bit depth, runtime, container | **source** (BluRay vs WEB-DL vs HDTV), release group, edition, proper/repack, remux |

"Source" is not a stream property — nothing in a container says "I came from a Blu-ray." Like release group and edition, it is inherently a name-derived claim. Parsing is the _only_ source for the right-hand column; for the left-hand column it produces a claim that a later step may confirm or contradict.

## Persisted parse

The parse is computed pre-download, but consumers need it long after — most importantly to **re-render name templates over an existing library** (a template change, a mass-rename) without re-downloading. The advertised side is the only re-render input that isn't already durable: `Media` lives in the DB, `MediaInfo` lives in the [probe table](../scan/README.md#ffprobe-metadata--lazy-out-of-band), but `Quality`/`Release` are derived from a release title the `download_job` eventually purges. So Arrflix persists the parse, per `file`:

- **Raw source string** — the release title (grabbed) or the filename (scanned). The source of truth: enables re-parse, and is the audit trail for "what produced this file."
- **Parsed fields** — the advertised `Quality` (the orthogonal [attribute core](#the-quality-attribute-core) — `Source`, `Resolution`, `Modifier`, `Revision` — **not** a rendered bin string) + `Release`, as a snapshot. Redundant with re-parsing, but cheap, and it's the working value for fast render/list, a stable record across parser versions, and the home for [correction-loop](#open-questions) overrides. The per-domain bin (`name`, `full`) is re-rendered from the stored core + the media item's known domain at read time, so it isn't frozen here — a vocabulary tweak (renaming a bin, adding a bin) needs no backfill.
- **`parser_version`** — so we know whether a re-parse would differ, and can offer an explicit "re-parse" (to pick up parser improvements) rather than silently changing output on every read.
- **`origin`** — `grabbed` | `scanned` | `manual`. Tells consumers how much to trust the parse and whether the raw string is a release title or a filename.

**Grabbed → lossless re-render; scanned → best-effort.** A grabbed file has a real release title, so its persisted parse is complete and a mass-rename reproduces the original name exactly. A scanned file's parse comes from its existing filename: reliable where the namer encoded the info (most Sonarr/Plex names carry quality + group + edition), `Unknown` only for whatever the old name omitted — the same limitation Sonarr has renaming files it didn't grab. `origin` lets the mass-rename preflight say so honestly ("source/group unknown for these — they'll drop from the new name").

This — plus `MediaInfo` (probe table) and `Media` (DB) — is the complete set of re-render inputs. Persisting `MediaInfo` rather than re-probing is what keeps a 5,000-file mass-rename fast.

## The engine

One pure function, domain-dispatched:

```go
func Parse(input string, domain Domain, opts ...Option) ParsedRelease
//                       ^^^^^^^^^^^^^                                   required
//                                       ^^^^^^^^^^^^                    AsPath() switches to filename + folder-context mode
```

- **No DB, no network, no sidecar.** Pure and deterministic — the cheapest, most valuable unit-test surface in the system (the [importer](../importer/README.md#file-assignment--closed-world) already prizes this property for its filename-marker parser; here it covers the whole engine).
- **Domain is a required argument.** Every production consumer knows the domain at the call site (TMDB-anchored search, importer's media-type-typed job, library-scoped scan, UI-scoped policy test endpoint). There is no auto-detect path. The series patterns and Sonarr-faithful detection run for `DomainSeries`; the movie patterns and Radarr-faithful detection run for `DomainMovie`.
- **Two input flavors, one output.** A release title from an indexer and an on-disk path go through the same code. Path mode additionally reads **folder context** (series name from the show folder, season from the season folder) — exactly how Sonarr reuses one parser for releases and filenames.
- **Embeddable.** A single Go binary's worth of parsing. Removing the Python sidecar is also a deployment win for the [first-run experience](../../docs/guide/) — one fewer process to run, crash, and health-check.

### Package structure (directional)

```
backend/internal/parsing/
  parsing.go      // Parse(input, domain, ...opt) ParsedRelease — public surface
  domain.go       // Domain enum (Series | Movie); the only typed dispatcher
  series.go       // series identity (Sonarr port: title, season, episode, daily, absolute)
  movie.go        // movie identity (Radarr port: title, year, edition, hardcoded-subs)
  quality.go      // detection per domain → orthogonal core (Source, Resolution, Modifier, Revision)
  bin.go          // bin(core, domain) → name + full (formerly internal/quality.BinFor)
  vocab.go        // seriesBins / movieBins — verbatim Quality.cs ports
  group.go        // release group (split per domain — Sonarr vs Radarr exception lists)
  language.go     // languages (split per domain — Sonarr vs Radarr LanguageParser)
  clean.go        // simplify pipeline per domain
  attributes.go   // codec / audio / channels / HDR-claim / proper / repack (EXTEND)
  types.go        // ParsedRelease + Field[T] (confidence + provenance)
  testdata/       // labeled corpus + sonarr.golden.json / radarr.golden.json
```

`internal/release/` folds into `parsing/`; `internal/identity/`'s embedded-ID regex relocates to [matching](../matching/README.md)'s `pathembed` resolver (per the matching spec); the importer's private filename-marker copy is replaced by a call into this package. **`internal/quality/` folds into `parsing/` as `bin.go` + `vocab.go`** — the projection now lives where the detection does, and consumers stop having to import two packages.

### What's already done vs. to port

| Area                                   | Status                                                              |
| -------------------------------------- | ------------------------------------------------------------------- |
| Quality core (source/resolution/modifier/revision) | **Done** — `regexp2`, Radarr-faithful base, emits the orthogonal core with `Modifier` axis + `BR-DISK` detection |
| Quality bin rendering (`name`, `full`) | **To port** — `bin(core, domain)` + the two vocabulary tables, folded in from `internal/quality/` |
| Per-domain dispatch | **To port** — splits detection (extension table, source regex precedence) and the rest of the engine; eliminates ~17 allowlist entries by construction |
| HardcodedSubs (movie-only) | **To port** — Radarr `ParseHardcodeSubs`; field exists today but never populates |
| Sonarr reversed-path recovery | **To port** — closes 3 remaining Sonarr allowlist entries |
| Release group, edition, proper/repack  | **Mostly done** — present, not yet diffed against Sonarr per-field  |
| Title + year                           | **To port** (Sonarr series / Radarr movie title extraction)        |
| Season/episode, daily, **absolute (anime)**, multi-ep | **To port** (Sonarr); replaces guessit                  |
| Languages                              | **To port** (LanguageParser)                                        |
| Codec / audio / channels / HDR-claim   | **Extend** — needed for [scoring](../quality-profiles/README.md) and naming |
| Per-field confidence + provenance      | **New** — wrap all of the above                                     |

The port is **bounded, GPL-clean, and oracle-backed**: Sonarr/Radarr are GPL (so is Arrflix — we comply, we can port directly), the parsers are finite readable codebases, and the live `/api/v3/parse` endpoint is a ready-made oracle. It's high-volume mechanical work with a built-in regression net — the kind of task that parallelizes well.

## guessit disposition

**Target: removed.** The unified engine subsumes guessit's role (title/year/season/episode/codec/audio), and we keep one parser, one language, no sidecar.

**Transition: optional fallback resolver, time-boxed.** Until the port clears the corpus parity threshold on identity fields — especially the long tail guessit is good at (anime absolute numbering, unusual international titles) — guessit may remain as a **single optional fallback resolver** behind [matching](../matching/README.md)'s resolver seam, gated by `Available()`. It runs only when the Go parser's identity confidence is low, never on the quality/scoring path, and is **not a hard dependency**. Once Tier-1 identity parity holds without it, the sidecar and its client are deleted.

This respects the "delete it entirely is fine" position while not betting the migration on a big-bang rewrite: the seam already exists, so demotion-then-deletion costs nothing structurally.

## Testing strategy — parity as a CI gate

### The corpus is the asset

A single, version-stamped, ground-truth-**labeled** corpus is the crown jewel — harder to replicate than the parser itself, and it does triple duty: parity measurement, [matcher confidence calibration](../matching/README.md#open-questions) (their open Q8, currently guesses), and the regression net. It graduates the corpus out of today's embedded Go slices (duplicated across `release_test.go` and `cmd/quality-test/main.go`) into `parsing/corpus/` as data.

Each entry: the input string, the expected parse, the Sonarr/Radarr **version** that produced it, and an optional `divergence: { reason }` mark.

### Two tiers

**Tier 1 — every PR, hermetic, fast (a normal `go test`).** Run the parser over the corpus, diff against the committed **golden** expected-output, compute per-field/per-tool compat %, fail on **regression vs baseline** or an **un-allowlisted divergence**. No network, no containers — consistent with the suite's existing "no live external calls" invariant ([tmdbtest](../../backend/internal/test/tmdbtest/) is the pattern: fakes, not real services). A `-update` flag regenerates goldens locally from Tier 2.

**Tier 2 — scheduled / on-demand, live (the refresh job).** Spin up **pinned** Sonarr + Radarr containers using the same `testcontainers-go` the suite already depends on for postgres (generic container + linuxserver image, a pre-seeded `config.xml` with a fixed API key and auth disabled, wait-strategy on the API port). Run the corpus through `GET /api/v3/parse?title=…` (auth via `X-Api-Key`; `golift.io/starr` already wraps this), regenerate goldens, diff against committed. Drift opens a PR ("Sonarr 4.0.x changed; 7 parses moved"). This is where the pinned version is bumped deliberately and the corpus grows.

### Compat % is two numbers, with an allowlist

- **Per tool, by domain:** series-flavored corpus → Sonarr, movie-flavored → Radarr. Editions are Radarr-only; absolute numbering is Sonarr-only. Quality heritage is shared, so the quality corpus largely validates both.
- **Per field and overall:** report compat for quality bin, resolution, source, group, season/episode, edition, year, languages — not just an aggregate.
- **Allowlist over 100%.** You will not want byte-identical parity: sometimes Sonarr is wrong, sometimes we differ on purpose (e.g., the asserted-quality-bin case parked in [quality profiles](../quality-profiles/README.md)). compat % = matches / (total − allowlisted); CI fails only on _un-allowlisted_ mismatches. This retires the current harness's brittle exit-1-on-any-mismatch behavior.

The published parity number ("99.x% with Sonarr 4.0.x / Radarr 5.x") falls out of Tier 1 for free.

## Interactions

| Neighbor                                              | How parsing interacts                                                                                                          |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| **[Matching](../matching/README.md)**                 | Primary identity resolver consumes the parse + its per-field confidence. guessit demotes to an optional fallback resolver, then is removed. |
| **[Quality profiles](../quality-profiles/README.md)** | Consumes `Quality` + `Release` for gating/scoring. Owns the advertised-vs-asserted re-gate (parked); parsing supplies the advertised half. |
| **[Name templates](../name-templates/README.md)**     | Reads `Quality`/`Release` (advertised) for rendering; `MediaInfo` (asserted) tokens come from the ffprobe extractor, not parsing. |
| **[Routing](../routing/README.md)**                   | Candidate-time rules read advertised fields; `MediaInfo` rules only fire post-download.                                        |
| **[Importer](../importer/README.md)**                 | Uses the same engine (path flavor) for closed-world season-pack assignment; replaces its private filename-marker copy.         |
| **[Scan](../scan/README.md)**                         | Path-flavor parse of discovered files; the asserted `MediaProbeWorker` is a sibling extractor, not parsing.                    |
| **[Metadata](../metadata/README.md)**                 | Matching turns parsing's identity hints into resolved `Media` via the metadata/external-id layer. The resolved series' `series_type` is the canonical anchor parsing's numbering-namespace tag is reconciled against. |
| **[Sources](../sources/README.md)**                   | Owns the anime `NumberingMap`; together with metadata's `series_type` it is the canonical side that parsing's `absolute`/scene namespace tag is reconciled against (in matching's `episode-numbering` resolver). Parsing never calls a provider. |
| **Asserted extractor (ffprobe)**                      | Sibling, not owned here. Fills `MediaInfo`. Parsing emits advertised only.                                                     |

## Open questions

1. **guessit: delete now or transitional?** Spec position is transitional-then-deleted, gated on Tier-1 identity parity (esp. anime). Confirm the deletion trigger — a specific compat threshold on identity fields, or a date.
2. **Confidence calibration source.** Per-field confidence values are educated guesses until the corpus is labeled. Same dependency the [matcher](../matching/README.md#open-questions) has — the corpus calibrates both. Sequence corpus labeling early.
3. **Provenance granularity.** Token-index evidence per field is the ambition; is a coarser "which sub-parser fired" enough for v1, with token-level as a follow-up? Lean: coarse first, token-level for the Parse Inspector.
4. **Anime absolute numbering.** The hardest port and guessit's strongest area. Is anime in the v1 parity claim or explicitly out (consistent with [tracking](../tracking/README.md#open-questions))? Lean: out of the v1 _claim_, parsed best-effort, guessit-fallback retained longest here.
5. **Corpus sourcing.** Hand-curated (today's ~310) vs. harvested from real indexer results vs. public release-name datasets. Lean: seed from the existing set, grow via Tier-2 harvesting, label ground-truth identity so it doubles as matcher calibration.
6. **Pinned-version policy.** Which Sonarr/Radarr versions are the parity targets, and what's the bump cadence? Lean: pin latest stable, bump deliberately via a Tier-2 PR.
7. **Path-flavor folder context shape.** How much directory context does path mode consume (immediate parent only, or full ancestry)? Sonarr uses limited context. Pin against the [scan](../scan/README.md) / [matching](../matching/README.md) needs.
8. **`Field[T]` ergonomics.** Generics make per-field confidence clean but ripple through every consumer that reads a value. Do consumers see `ParsedRelease.Quality.Resolution.Value` or a flattened view with confidence on the side? Lean: a flattened "values" view for consumers + a parallel provenance map, so most call sites stay simple.
9. **Correction loop.** User overrides ("this PROPER is fake", "this group always ships x265") improving future parses — keyed on group/pattern. In scope for the parsing module or a matching/hygiene concern? Lean: design the override store here (it's parse-shaped), surface it via matching's re-match UI.
10. **Persisted-parse storage shape.** Raw string + orthogonal parsed snapshot + `parser_version` + `origin`, per `file` — a 1:1 companion table (`file_parse`) or columns on `file`? Lean: companion table, so the advertised namespaces stay grouped and nullable for pre-existing rows. The bin (`name`, `full`) is **not** persisted; it is re-rendered from the core + the media item's known domain on read. Shape lands with [libraries](../libraries/README.md) / [matching](../matching/README.md) data-shape work.
11. **Modifier set scope.** **Resolved.** The [core](#the-quality-attribute-core) detects the full Radarr modifier set: `REMUX`, `BR-DISK`, `RAWHD`, `SCREENER`, `REGIONAL`. The Sonarr-flavored NTSC/PAL → DVD source signal is also wired into the shared source regex.
12. **Pre-release sources.** **Resolved.** Ported and wired: `CAM` / `Telesync` / `Telecine` / `Workprint` are first-class sources matching Radarr; Sonarr's `NTSC` / `PAL` DVD-source signal is applied as a fallback when no stronger source matched (kept out of the shared SourceRegex so a trailing NTSC token doesn't override an earlier explicit `DVD-R` / `DVD5` / `DVD9` match).
13. **Identity-independent quality. Resolved.** Arrflix extracts quality even when no title/episode parses; Sonarr/Radarr return `Unknown` on identity-parse failure. We keep ours (strictly more information; matching is a separate layer downstream) as the single principled parity divergence, retained as a class-shaped `allowlistPredicate`. With per-domain dispatch, this becomes the *only* remaining bin allowlist class.

## What we're explicitly not deciding here

- Exact `ParsedRelease` / `Field[T]` field names beyond `quality.name` and `quality.full`, and the regex tables themselves.
- User-customizable per-bin titles (`QualityDefinition.Title` in *arr). Deferred until that feature is built; for now `quality.name` is both the stable identifier and the rendered display value.
- The advertised-vs-asserted **re-gate** mechanism and the mismatch hygiene rule (parked; owned by [quality profiles](../quality-profiles/README.md) / [acquisition](../acquisition/README.md) / [hygiene](../hygiene/README.md)).
- The `ffprobe` extractor's field schema (owned with [scan](../scan/README.md) / hygiene's data-shape pass).
- The Parse Inspector UI.
- The corpus file format and the golden-diff harness's exact CLI flags.
- The testcontainers wiring details (image tags, config.xml seeding, wait strategies) — directional only.
- Migration ordering for folding `internal/release/` + `internal/identity/` into `internal/parsing/`.

## Doc neighbors

- [Matching](../matching/README.md) — the primary consumer of identity hints; resolver + aggregator sits on top of parsing
- [Quality profiles](../quality-profiles/README.md) — consumes `Quality`/`Release`; owns the parked re-gate
- [Name templates](../name-templates/README.md) — renders advertised + asserted attributes into paths
- [Importer](../importer/README.md) — reuses the engine (path flavor) for closed-world assignment
- [Scan](../scan/README.md) — path-flavor parsing of discovered files; sibling ffprobe extractor
- [Routing](../routing/README.md) — reads advertised at candidate time, asserted post-download
- [Errors](../../patterns/errors/README.md) — parsing is pure and total; unparseable input yields low-confidence fields, not errors
