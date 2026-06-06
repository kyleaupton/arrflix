# Rules — the shared predicate substrate

**Status:** Draft, iteration 1

This doc defines the **rule substrate**: the shared machinery for evaluating user-authored predicates over a release. A predicate is "is this release 2160p?", "is the group blocklisted?", "does the codec match what was advertised?" — a boolean question about one release. Many parts of Arrflix ask boolean questions about releases; this is the one place that answers them.

Two modules consume it directly, each reading the boolean answer differently:

- [Routing](../../modules/routing/README.md) — predicate matches → apply a dispatch **action** (downloader / library / name template).
- [Quality profiles](../../modules/quality-profiles/README.md) — predicate matches → **reject** the release (hard gate) or **add a weight** to its score (custom format).

A third module, [name templates](../../modules/name-templates/README.md), shares the *data half* of this substrate (the Release model + field registry) without using the evaluator — it renders fields, it doesn't gate on them.

Like [errors](../errors/README.md), this is a **foundational package other modules build on, not a feature with its own surface**. Nobody "uses the rule engine"; routing, quality, and name templates stand on it. It is prescriptive shared code (`internal/rules` + the Release model + the field registry), the same genre as `internal/errors`.

It does **not** decide what a match *means* — each consumer owns its **reduction** (the thing it does with the boolean). It does not parse release titles (that's [parsing](../../modules/parsing/README.md)) and it knows nothing about persistence shapes or HTTP.

> **v0 context.** Today this logic is welded into a `policy` engine that holds a `*repo.Repository` and walks rules against the database mid-evaluation. Arrflix is v0 and we are taking the breaking change: that engine is **deleted and rebuilt** as the pure substrate described here. There is no behavior-preservation or data-migration constraint — we are building the foundation correctly, once.

## TL;DR

- **One evaluator, one Release model, one registry — many reductions.** Gate, score, and route are the same machine (a predicate over a release); only what each does with the answer differs. Building a second evaluator, a second context model, or a second condition vocabulary for quality would be the mistake.
- **The Release is the unit of evaluation.** A namespaced, `phase`-tagged object fusing indexer metadata + the parse + resolved media + (post-download) `ffprobe` attributes. `candidate.* / identity.* / quality.* / encode.* / media.* / mediainfo.*`. Every consumer evaluates against this one shape.
- **Conditions are typed, not stringly-typed.** An operand is an explicit discriminated union — `Field{path}` or `Literal{type, value}` — never a bare string the evaluator sniffs at runtime. The kind and type are *stored facts*, decided at authoring time, not reconstructed from string shape.
- **The field registry is the single source of truth.** Promoted from struct tags into a first-class registry, it drives three things from one definition: evaluation lookup, authoring-time type-checking, and the authoring UX (autocomplete, type-aware operators, value dropdowns, phase badges). This registry is what lets the [name-template editor](../../modules/name-templates/README.md) and the rule editor share one slick, validated authoring experience.
- **The evaluator is pure.** `Eval(tree, release) → (matched, trace)`. No repo, no DB, no logger, no consumer types. A rule tree is loaded once into memory and walked purely — no lazy per-node database walk.
- **Trees are phase-partitioned.** A condition over a `post_download` field (`mediainfo.*`) is statically a post-download condition. Pre-download evaluation runs the pre-download partition; the [import-time re-gate](../../modules/quality-profiles/README.md#import-time-re-gate) runs the post-download partition once `ffprobe` data exists. The re-gate is a structural property of the tree, not a runtime special case.
- **Confidence rides along, unused in v1.** The Release carries the parse's `Field[T]` (value + confidence + provenance), not flattened values. v1 reads `.Value` and ignores confidence; the shape is preserved so confidence-aware selection is additive later, never a rewrite.

## Why one substrate

Routing and quality look like different features, but strip them to their core and they run the same loop: *evaluate a boolean predicate over one release, then do something.* The "something" is all that differs:

| Consumer | The predicate asks | On match, the reduction is |
| --- | --- | --- |
| **Routing** | "does this release fit this dispatch rule?" | apply an action set (downloader, library, name template) |
| **Quality — hard gate** | "does this release violate a gate?" | reject the release |
| **Quality — custom format** | "does this release match this format?" | add the format's weight to the release's score |

Same predicate machinery, three reductions. If we built these as separate systems we'd have two condition DSLs to learn, two Release models to keep in sync, two evaluators to debug, and two authoring UIs to polish — and they'd drift. Instead: **the predicate is shared; the reduction is each module's own.** This mirrors the [work-dispatch](../work-dispatch/README.md) framing (shared contract, per-consumer behavior) — here the shared thing is real code, not just a convention.

The payoff compounds at the authoring surface. Because every consumer evaluates the same Release through the same registry, a condition built in the routing editor, a gate built in the quality editor, and a `{Quality.Full}` token dropped in the name-template editor all draw from one field catalog with one set of type rules. One registry, three editors, zero drift.

## The Release model

`Release` is the unit of evaluation: everything a rule can ask about one candidate release, in one object. It is namespaced so paths are unambiguous and self-describing, and every field is tagged with the **phase** at which it becomes known.

### Namespaces

| Namespace | What it holds | Source |
| --- | --- | --- |
| `candidate.*` | Indexer/release metadata: size, seeders, age, indexer, categories, protocol, publish date | the indexer search result |
| `identity.*` | Parsed identity: title, year, edition, and (series) season/episode numbering | [parsing](../../modules/parsing/README.md) |
| `quality.*` | Parsed quality: resolution, source, modifier, repack/proper, and the rendered bin | parsing |
| `encode.*` | Properties of this particular rip, not its identity or quality tier: release group, hardcoded subs | parsing |
| `media.*` | Resolved media: type, tmdb id, title, year, (series) season/episode/episode-title | matching against TMDB |
| `mediainfo.*` | Asserted video/audio file analysis: codec, bit depth, HDR, channels, container | `ffprobe`, **post-download only** |

A path like `quality.resolution` or `mediainfo.video_codec` is the stable address a condition (or a name-template token) uses. The address is the contract; the Go field name behind it is free to change.

**On the `encode` name.** The whole object is the `Release`, so the small parsed namespace for release-group + hardcoded-subs can't also be `release` — it's `encode` (properties of this particular rip). *arr offers no precedent here: Sonarr/Radarr keep release group, edition, and hardcoded-subs as flat properties on the parsed-info bag and have no "encode" grouping, so this namespace and its name are our own. Where *arr *does* have an opinion we follow it — the quality axes mirror its `QualityModel` = `(Source, Resolution, Modifier)` + revision, and a custom format's predicate is what *arr calls a *specification*.

### Phase

Each field declares when it becomes available:

- **`pre_download`** — known from the indexer result + the title parse + the media match. Everything except `mediainfo.*`. This is what's available when deciding whether to *grab*.
- **`post_download`** — known only after the file exists and `ffprobe` has run. The `mediainfo.*` namespace. This is the *asserted* truth used at import.

Phase is not decoration — it's load-bearing in two directions. Pre-download, a condition over a `mediainfo.*` field has nothing to read, and the evaluator must **never** reject a release because an asserted field isn't available yet. At import, the same conditions re-run with `mediainfo.*` populated — this is the [import-time re-gate](../../modules/quality-profiles/README.md#import-time-re-gate). Both behaviors come for free if the evaluator partitions the tree by phase (see [The evaluator](#the-evaluator)).

### `Field[T]` — value, confidence, provenance

The Release does **not** carry bare values. Each parsed field is a `Field[T]` carrying:

- **Value** — the parsed value (`"2160p"`, `"BluRay"`, …).
- **Confidence** — how sure the parser is. A release title is a *claim* typed by an uploader; `2160p` sitting as an explicit token is near-certain, while `UHD`/`BL`/`RM` abbreviations are guesses, and a missing token is confidence zero.
- **Provenance** — what evidence produced the value (which token / which rule), for debugging and the decision log.

v1 reads `.Value` and **ignores** confidence and provenance — selection trusts the parsed value, and the escape hatch for a wrong parse is a manual override (see [quality-profiles OQ#4/#12](../../modules/quality-profiles/README.md#open-questions)). The reason to carry the shape anyway is the cheap-vs-expensive asymmetry: flatten now and adding confidence later is a model-wide retrofit touching every consumer; carry `Field[T]` now and it's purely additive — the parser emits better numbers into a slot that already exists, and an escalation wrapper starts reading `.Confidence`, with no consumer change. See [Confidence](#confidence).

## Conditions: the typed operand model

A **condition** is the atom of a rule: a left operand, an operator, and a right operand — `quality.resolution == "1080p"`. Conditions combine into a **tree** via `and` / `or` / `not`.

### Operands are a discriminated union — no inference

An operand is **explicitly** one of:

```
Operand =
  | Field   { Path: "quality.resolution" }
  | Literal { Type: "enum",   Value: "1080p" }
  | Literal { Type: "number", Value: 5 }
  | Literal { Type: "string", Value: "DTS-HD MA 5.1" }
```

So `quality.resolution == "1080p"` is stored as:

```
{ Left: Field{Path: "quality.resolution"}, Op: "==", Right: Literal{Type: "enum", Value: "1080p"} }
```

The evaluator never guesses what an operand *is* — it reads the tag. This is the whole of "no inference."

**Why this matters — the v0 model and its bugs.** v0 stores both operands as plain strings and reconstructs their meaning at evaluation time by sniffing the string's shape: *"has a dot → it's a field path; parses as a number → numeric literal; otherwise → string literal."* That inference is ambiguous and wrong in ways that can't be patched without redesign:

- A release group literally named `5` → `encode.release_group == 5` infers a number, but the field is a string. Mismatch.
- An audio-codec literal `"DTS-HD MA 5.1"` **contains a dot**, so the sniffer treats it as a field path (`DTS-HD MA 5` . `1`) and the lookup fails. A perfectly valid literal breaks on its punctuation.
- The evaluator can't know `quality.resolution` is an enum (so `>` is meaningless on it) until it tries `>` at runtime and either errors or does something dumb.

Making the kind and type *stored facts* eliminates all three by construction, and moves type-checking from runtime to authoring time (see [registry](#the-field-registry)).

### Operators and type rules

Operators are valid only for the operand types the registry says they apply to:

| Operator | Valid on | Notes |
| --- | --- | --- |
| `==` `!=` | all types | equality |
| `>` `>=` `<` `<=` | number only | **not** valid on enums — `quality.resolution > "1080p"` is rejected at authoring time, because resolution is a string enum with no inherent order. Ordering of quality bins is structured profile data, not a condition (see [quality-profiles](../../modules/quality-profiles/README.md#bins-as-keys-not-strings)). |
| `contains` | string, list | substring / membership |
| `in` `not in` | value against a list literal | the right operand is a typed list, not a comma-split string |
| `and` `or` `not` | sub-trees | logical composition |

Type-checking is the registry's job and happens when a rule is **saved**, not when it runs. A condition that doesn't type-check can't be authored.

### Trees, loaded as a unit

A rule is a tree of conditions joined by `and` / `or` / `not`. The whole tree belongs to one owner (a routing rule, a quality gate, a custom format) and is **always loaded and written as a unit** — nothing ever queries an individual interior node. So the tree is stored and read whole (a serialized/JSONB value), not as a flat table of rows that reference each other by id.

This kills v0's worst wart directly: v0 stores conditions as flat rows where logical operators hold child UUIDs as strings, with no parent/root marker, and the evaluator re-reads the *entire* condition table from the database for every logical node it walks. Load-as-a-unit means one read produces the whole tree; the evaluator then walks it purely in memory.

## The field registry

The registry is the authoritative catalog of every addressable field: its `path`, label, type, enum values (if any), `phase`, value type, and dynamic source (if the options come from an API, e.g. configured indexers). It is **promoted from struct tags** — today the field metadata already lives as `path:`/`type:`/`enumValues:`/`phase:` tags and is reflected into a list; the registry makes that the first-class source of truth rather than an incidental projection.

One registry drives **three** things from one definition:

1. **Evaluation lookup** — resolving `Field{path}` to a value on the Release.
2. **Authoring-time type-checking** — is this operator valid on this field? is this literal a legal value for this enum? Both answered against the registry when a rule is saved.
3. **The authoring UX** — and this is where Arrflix can pull ahead of the *arr stack.

### Author-time experience

Because the editor knows the field catalog and its types, the rule/condition builder (and the [name-template editor](../../modules/name-templates/README.md), which uses the same registry) can offer:

- **Autocomplete** — type `qual` → `quality.resolution`, `quality.source`, `quality.name`, …
- **Type-aware operator menus** — pick an enum field and only `==`/`!=`/`in` are offered; `>` simply isn't there. The invalid rule is *unbuildable*, not merely rejected.
- **Value autocomplete** — after `quality.resolution ==`, a dropdown of `SD / 480p / 720p / 1080p / 2160p` straight from the field's enum values; for a dynamic field like `candidate.indexer`, options fetched live.
- **Right-widget-per-type** — number input for numbers, toggle for booleans, dropdown for enums, chip-list for `in`.
- **Live validation** — an ill-typed comparison flagged the instant it's built.
- **Phase badges** — a "post-download" marker on `mediainfo.*` fields, so the author *sees* that the condition only runs at the [import re-gate](../../modules/quality-profiles/README.md#import-time-re-gate), not at search time.

The discriminated-union operand model maps directly onto a rich-text editor's node model: a **Field** operand renders as an atomic, non-editable token/chip; a **Literal** renders as the typed widget. The explicit operand model and the slick editor are the *same* design decision, not two — which is why "type the operands" and "make the editor great" are one line of work, not a tradeoff.

## The evaluator

The evaluator is a pure function in `internal/rules`:

```
Eval(tree RuleTree, release Release) (matched bool, trace Trace)
```

No repo, no logger, no DB, no routing/quality types. This makes it trivially unit-testable (a table of `tree × release → matched`) and reusable by every consumer.

Properties:

- **Load-once, walk-pure.** The repo (or service) loads the whole tree into a `RuleTree` once; `Eval` walks it in memory. No per-node database access.
- **Phase-partitioned.** Conditions are statically split by the phase of the fields they reference. Pre-download evaluation runs the pre-download partition; the import re-gate runs the post-download partition with `mediainfo.*` populated. A `post_download` condition encountered pre-download is *indeterminate* — it never contributes a spurious reject. This replaces v0's accidental behavior (a missing `mediainfo` value silently compared against `"<nil>"` for `==`, and hard-errored for ordering ops) with a defined, structural one.
- **Neutral trace.** `Eval` returns a per-node trace (each condition's resolved operands and its boolean result). The trace is consumer-agnostic; routing adapts it into its dispatch evaluation record, quality adapts it into [decision-log](../../modules/quality-profiles/README.md#decision-log) rows. The evaluator does not know what an "action" or a "score" is.

### Layering

The pure/orchestration split follows the backend's standard layering:

- **`internal/rules`** (pure domain) — `RuleTree`, the typed operand/condition types, `Trace`, `Eval`, and the pure tree assembly. Compiles without `internal/repo`, `dbgen`, or `pgtype`.
- **repo** — loads the stored tree (one read) and returns it as a domain value.
- **service** (routing's, quality's) — loads tree(s) + the Release, calls `Eval`, applies its own reduction.

The Release model and the field registry live alongside the rules package (rather than in the generic `model` grab-bag) so the substrate is one neighborhood — relevant when specs eventually co-locate with code.

## Confidence

Confidence is **not load-bearing** for v1 of any consumer, and we are deliberately not building it now. Two separable things hide in the word:

- **The capability** — computing good, graded, per-axis confidence (a parsing-internal calibration problem). **Deferred.** No v1 surface consumes it: gate/bin/score/route all run on the parsed value, and the [import re-gate](../../modules/quality-profiles/README.md#import-time-re-gate) checks the real bytes via `ffprobe` (ground truth) — it doesn't care how confident the *title* parse was. Confidence only matters in the pre-download window where the title is all we have, and v1's answer there is "trust the parse, override manually."
- **The seam** — carrying `Field[T]` through the Release instead of flattening. **Kept now.** It's the only foundational commitment confidence demands, and it's a data-shape decision made once.

When parsing's confidence becomes real, the consumer-side change is additive: an **escalation wrapper** around gate evaluation — a low-confidence parse *softens* a hard gate (hold for interactive/manual review with a logged reason) rather than triggering a confident auto-reject. The evaluator and Release model don't change; the wrapper reads the `.Confidence` that was always there. (This is [quality-profiles OQ#12](../../modules/quality-profiles/README.md#open-questions), architecture-preserved.)

## The reductions

For clarity, the three reductions consumers apply to `Eval`'s boolean — none of which live in this package:

| Reduction | Owner | On `matched == true` | On `matched == false` |
| --- | --- | --- | --- |
| **Dispatch** | [routing](../../modules/routing/README.md) | apply the rule's action set; stop (or `continue`-chain) | try the next rule |
| **Hard gate** | [quality profiles](../../modules/quality-profiles/README.md#hard-gates-vs-soft-scoring) | reject the release, log the gate reason | release survives this gate |
| **Custom format** | [quality profiles](../../modules/quality-profiles/README.md#hard-gates-vs-soft-scoring) | add the format's weight to the release's score | weight not added |

Quality's structured v1 scorers (preferred group, codec preference, HDR/audio variant, proper/repack) are predicates too — `encode.release_group in [...]`, `mediainfo.video_codec == "H.265"`, `quality.is_repack == true`. The closed structured form is a *constrained authoring surface that emits the same trees*; it is not a second engine. This is what reconciles "v1 ships no custom-format DSL" (a UI scope decision) with "quality reuses the evaluator" (an architecture decision).

**Ranking is not a reduction here.** Bin ordering, cutoff, and per-bin size bands are structured profile data, *not* conditions — you can't `>` a string enum, and ordering an open list of releases isn't a boolean question. The rule substrate answers "does this match?"; the profile answers "which is best?" using those answers as inputs. Keeping that boundary crisp is the point of this whole separation.

## Relationship to neighbors

| Neighbor | Relationship |
| --- | --- |
| **[Parsing](../../modules/parsing/README.md)** | Produces the `Field[T]`-wrapped parse that populates `identity.*`, `quality.*`, `encode.*`. The Release consumes the full parse (with confidence/provenance), never a flattened projection. |
| **[Routing](../../modules/routing/README.md)** | Primary consumer. Evaluates dispatch rules; reduction = action set. Owns its own evaluation/audit record built from the neutral trace. |
| **[Quality profiles](../../modules/quality-profiles/README.md)** | Primary consumer. Hard gates and custom formats are trees over the Release; reductions = reject / add-weight. The re-gate is the post-download partition re-run with `ffprobe` data. Ranking stays profile-owned structured data. |
| **[Name templates](../../modules/name-templates/README.md)** | Consumes the **data half** — the Release model + field registry — to render tokens. Does not evaluate conditions. Shares the registry-driven authoring editor. |
| **[Audit pattern](../audit/README.md)** | Each consumer writes its decision rows from the neutral trace; retention and the Activity view are owned there. |
| **[Errors](../errors/README.md)** | Sibling foundational pattern: a real `internal/` package filed as a pattern because it's shared substrate. The genre precedent for this doc. |

## Open questions

1. **Tree serialization shape.** JSONB blob per owner vs a typed serialized column. Lean: a single serialized tree value per owner (routing rule / gate / custom format), since the tree is always read and written whole and never queried by interior node. Pin the exact encoding in the implementation iteration.
2. **Operator set boundaries.** The table above is the v1 lean. Do we want `matches` (glob/regex) on string fields for power users, or keep v1 to `contains`/`in` and add regex only with the advanced custom-format tier? Lean: structured-first; defer regex to the advanced tier, where it compiles down to the same condition shape.
3. **List literals.** `in`/`not in` take a typed list literal. Is the element type constrained to match the field's type (enum-in-enum-list), and how does the editor render adding/removing elements? Lean: yes, typed-homogeneous lists, chip-list widget.
4. **Field[T] in persistence.** When the parse is persisted (the [persisted parse](../../modules/parsing/README.md#persisted-parse)), do we store confidence/provenance in the blob too? Lean: yes — nearly free to store, expensive to backfill, same asymmetry as carrying it in the model.
5. **Registry generation vs hand-authored.** Continue generating the registry from struct tags, or define it as standalone data the structs conform to? Lean: keep generating from tags (one source, compiler-checked) but expose it as the first-class registry type rather than an incidental `ListContextFields` projection.
6. **Cross-field comparisons.** The operand model permits `Field == Field` (`quality.resolution == media.expected_resolution`). Is there a real v1 use, and does the editor surface it? Lean: support it in the model (it's free), don't foreground it in the v1 editor until a use appears.
7. **Where `Release` and the registry live.** `internal/rules` vs a dedicated `internal/release` package vs staying in `internal/model`. Lean: co-locate the substrate (Release + registry + evaluator) so it's one neighborhood; decide the exact package split in implementation.
8. **Indeterminate semantics surfaced to authors.** A `post_download` condition is indeterminate pre-download. Should the dry-run/preview UI show "not yet evaluable (post-download)" distinctly from "evaluated false"? Lean: yes — the trace already distinguishes them; the preview should too.

## What we're explicitly not deciding here

- Exact table/column shapes or the tree's serialized encoding — implementation.
- The full operator grammar's edge cases (regex flavor, numeric coercion rules) beyond the v1 lean.
- Each consumer's reduction details — action sets live in [routing](../../modules/routing/README.md); gate/score/ranking live in [quality profiles](../../modules/quality-profiles/README.md).
- The parse model and confidence *calculation* — owned by [parsing](../../modules/parsing/README.md).
- The name-template token grammar — owned by [name templates](../../modules/name-templates/README.md); this doc only owns the shared Release model + registry it draws from.
- The decision-log/audit storage — owned by the [audit pattern](../audit/README.md); this doc produces the neutral trace consumers turn into rows.

## Doc neighbors

- [Routing](../../modules/routing/README.md) — consumer; predicate → dispatch action
- [Quality profiles](../../modules/quality-profiles/README.md) — consumer; predicate → reject / add-score, plus profile-owned ranking
- [Name templates](../../modules/name-templates/README.md) — consumer of the Release model + registry (rendering, not evaluation)
- [Parsing](../../modules/parsing/README.md) — produces the `Field[T]` parse the Release is built from
- [Errors](../errors/README.md) — sibling foundational pattern; the precedent for a package filed under `patterns/`
- [Audit](../audit/README.md) — where consumers' decision rows live
