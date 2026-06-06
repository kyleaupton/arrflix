# Rules — the shared predicate substrate

**Status:** Draft, iteration 2

This doc defines the **rule substrate**: the shared machinery for evaluating user-authored predicates over a **subject** — one prospective acquisition. A predicate is "is this release 2160p?", "is the group blocklisted?", "was this requested by a kid account?" — a boolean question about one candidate release being considered for one media item. Many parts of Arrflix ask boolean questions about a prospective grab; this is the one place that answers them.

> **What changed in iteration 2.** Iteration 1 made the *Release* the unit of evaluation. That quietly narrowed the model: facts that aren't properties of the artifact — why we're acquiring it (the requester set, the trigger), and eventually what quality concluded — had nowhere to live, and [routing](../../modules/routing/README.md)'s wider evaluation context contradicted this spec. Iteration 2: (1) the unit of evaluation is the **[Subject](#the-subject)**, and **Release narrows to the artifact half**; (2) the binary `pre_download`/`post_download` phase generalizes to an ordered **[timeline](#the-timeline)** of evaluation moments, `search < grab < import`; (3) the **`want.*`** namespace (durable intent facts from [tracking](../../modules/tracking/README.md)) is added and **`decision.*`** is reserved; (4) the [two kinds of non-value](#two-kinds-of-non-value) are named; (5) conditions and name templates commit to **[one resolver](#one-resolver)** over the registry's flat paths. Implementation phases 1–2 built `model.Release` as the subject with the binary phase — re-shaping code to this model is the first job of phase 3, before routing wires on.

Two modules consume the substrate directly, each reading the boolean answer differently:

- [Routing](../../modules/routing/README.md) — predicate matches → apply a dispatch **action** (downloader / library / name template).
- [Quality profiles](../../modules/quality-profiles/README.md) — predicate matches → **reject** the release (hard gate) or **add a weight** to its score (custom format).

A third module, [name templates](../../modules/name-templates/README.md), shares the *data half* of this substrate (the Subject + field registry) without using the evaluator — it renders fields, it doesn't gate on them.

Like [errors](../errors/README.md), this is a **foundational package other modules build on, not a feature with its own surface**. Nobody "uses the rule engine"; routing, quality, and name templates stand on it. It is prescriptive shared code (`internal/rules` + the Subject + the field registry), the same genre as `internal/errors`.

It does **not** decide what a match *means* — each consumer owns its **reduction** (the thing it does with the boolean). It does not parse release titles (that's [parsing](../../modules/parsing/README.md)) and it knows nothing about persistence shapes or HTTP.

> **v0 context.** This logic began welded into a `policy` engine that holds a `*repo.Repository` and walks rules against the database mid-evaluation. Arrflix is v0 and we are taking the breaking change: that engine is **deleted and rebuilt** on the pure substrate described here. There is no behavior-preservation or data-migration constraint — we are building the foundation correctly, once.

## TL;DR

- **One evaluator, one Subject, one registry — many reductions.** Gate, score, and route are the same machine (a predicate over a subject); only what each does with the answer differs. Building a second evaluator, a second subject model, or a second condition vocabulary for quality would be the mistake.
- **The Subject is the unit of evaluation — the situation, not just the torrent.** `Subject{ Release, Media, Want, Decision }`: the artifact under consideration, what it was matched to, why we're acquiring it, and (from grab time) what quality concluded. Every consumer evaluates this one shape; field paths stay flat (`quality.resolution`, `want.trigger`).
- **Phase is a timeline, not a binary.** Evaluation happens at three moments — `search < grab < import` — and knowledge only accumulates. Every field is tagged with the earliest moment it's knowable; evaluating at moment M, a later-phase field is *indeterminate*, never a spurious match or reject. The [import-time re-gate](../../modules/quality-profiles/README.md#import-time-re-gate) is just the same questions asked again at `import`.
- **Conditions are typed, not stringly-typed.** An operand is an explicit discriminated union — `Field{path}` or `Literal{type, value}` — never a bare string the evaluator sniffs at runtime. The kind and type are *stored facts*, decided at authoring time, not reconstructed from string shape.
- **The field registry is the single source of truth.** It drives evaluation lookup, authoring-time type-checking, and the authoring UX (autocomplete, type-aware operators, value dropdowns, phase badges) from one definition — and both conditions and name-template tokens resolve through the registry's **one resolver**, so a path that works in a rule works in a template by construction.
- **The evaluator is pure.** `Eval(tree, subject, at) → (verdict, trace)`. No repo, no DB, no logger, no consumer types. A rule tree is loaded once into memory and walked purely.
- **Context scope is registry curation, not a design crisis.** Nothing is expressible in a condition unless a field is registered. Adding situational context (`want.*` today, `decision.*` tomorrow) means adding a few typed, phase-tagged rows to the catalog — a knob turned one field at a time, forever.
- **Confidence rides along, unused in v1.** The Release carries the parse's `Field[T]` (value + confidence + provenance), not flattened values. v1 reads `.Value` and ignores confidence; the shape is preserved so confidence-aware selection is additive later, never a rewrite.

## Why one substrate

Routing and quality look like different features, but strip them to their core and they run the same loop: *evaluate a boolean predicate over one subject, then do something.* The "something" is all that differs:

| Consumer | The predicate asks | On match, the reduction is |
| --- | --- | --- |
| **Routing** | "does this grab fit this dispatch rule?" | apply an action set (downloader, library, name template) |
| **Quality — hard gate** | "does this release violate a gate?" | reject the release |
| **Quality — custom format** | "does this release match this format?" | add the format's weight to the release's score |

Same predicate machinery, three reductions. If we built these as separate systems we'd have two condition DSLs to learn, two subject models to keep in sync, two evaluators to debug, and two authoring UIs to polish — and they'd drift. Instead: **the predicate is shared; the reduction is each module's own.** This mirrors the [work-dispatch](../work-dispatch/README.md) framing (shared contract, per-consumer behavior) — here the shared thing is real code, not just a convention.

The payoff compounds at the authoring surface. Because every consumer evaluates the same Subject through the same registry, a condition built in the routing editor, a gate built in the quality editor, and a quality token dropped in the name-template editor all draw from one field catalog with one set of type rules. One registry, three editors, zero drift.

## The Subject

The **Subject** is the unit of evaluation: everything a rule can ask about one prospective acquisition, in one object. It is the *situation*, of which the release is the centerpiece but not the whole:

```
Subject
├─ Release      — the artifact under consideration
│   ├─ candidate.*    advertised: the indexer listing
│   ├─ identity.*  ┐
│   ├─ quality.*   ├─ parsed: claims read from the title
│   ├─ encode.*    ┘
│   └─ mediainfo.*    asserted: what the bytes actually are (ffprobe)
├─ Media        — media.*     what we matched it to (TMDB)
├─ Want         — want.*      why we're acquiring it (tracking)
└─ Decision     — decision.*  what quality concluded (reserved)
```

The Go nesting above is **structure**, not path grammar — field paths stay flat (`quality.resolution`, `want.trigger`, `mediainfo.video_codec`). The path is the stable address a condition or a name-template token uses; the struct shape behind it is free to change. The registry spans the whole Subject, so the editors can still *group* fields under "Release / Media / Want" headings without the paths paying a nesting tax.

**On the names.** `Subject` is deliberately role-named — it's the one abstract noun in a system of concrete ones, and it's accurate at every moment (search evaluates many subjects, routing dispatches the winner, import re-gates it). `Release` is reserved for what it means everywhere else in the *arr world: the artifact — and it now tells a self-contained story of *increasing verified truth*: **advertised** (what the indexer listing says) → **parsed** (what we read from the title — an uploader's claims) → **asserted** (what `ffprobe` measures off the actual bytes). Facts outside the Release answer different kinds of questions: not "what is this?" but "what's it for?", "why are we here?", "what did we decide?".

### Namespaces

| Namespace | What it holds | Source | Phase |
| --- | --- | --- | --- |
| `candidate.*` | Indexer/release metadata: size, seeders, age, indexer, categories, protocol, publish date | the indexer search result | `search` |
| `identity.*` | Parsed identity: title, year, edition, and (series) season/episode numbering | [parsing](../../modules/parsing/README.md) | `search` |
| `quality.*` | Parsed quality: resolution, source, modifier, repack/proper, and the rendered bin | parsing | `search` |
| `encode.*` | Properties of this particular rip, not its identity or quality tier: release group, hardcoded subs | parsing | `search` |
| `media.*` | Resolved media: type, tmdb id, title, year, (series) season/episode/episode-title | [matching](../../modules/matching/README.md) against TMDB | `search` |
| `want.*` | Acquisition intent: trigger, requester set, tier | [tracking](../../modules/tracking/README.md) | `search` |
| `decision.*` | The quality outcome: profile, bin, score, format hits | [quality profiles](../../modules/quality-profiles/README.md) — **reserved, not in v1** | `grab` |
| `mediainfo.*` | Asserted video/audio file analysis: codec, bit depth, HDR, channels, container | `ffprobe` | `import` |

**On the `encode` name.** The artifact object is the `Release`, so the small parsed namespace for release-group + hardcoded-subs can't also be `release` — it's `encode` (properties of this particular rip). *arr offers no precedent here: Sonarr/Radarr keep release group, edition, and hardcoded-subs as flat properties on the parsed-info bag and have no "encode" grouping, so this namespace and its name are our own. Where *arr *does* have an opinion we follow it — the quality axes mirror its `QualityModel` = `(Source, Resolution, Modifier)` + revision, and a custom format's predicate is what *arr calls a *specification*.

### `want.*` — durable intent facts

`want.*` is what lets a rule ask *why* we're acquiring — and it is deliberately **tracking-derived, not request-derived**. Per the [requests](../../modules/requests/README.md) model, a request is frozen once it spawns; the durable home for "who wants this" is the tracking record's requester rows, and a tracking can have **zero, one, or many** requesters. Two consequences are load-bearing:

- **There is no singular "requesting user."** The durable fact is a *set*: `want.requesters contains <user>` is the natural condition ("any requester is a kid"). An RSS- or admin-driven grab has an *empty* set — and `contains` over an empty set is definitively `false`, not an error and not indeterminate (see [two kinds of non-value](#two-kinds-of-non-value)).
- **Durability makes upgrades consistent.** An automatic upgrade grab months later has no triggering request — but it has the same tracking. A rule over `want.requesters` evaluates the same way for the original grab and the upgrade; a rule over an ephemeral "the request that triggered this" would silently re-route upgrades. (The requester set itself can still change over time — placement was decided from the set *as of grab time*; later additions don't move files. v1 accepts that; [hygiene](../../modules/hygiene/README.md) can flag drift later.)

The v1 starter catalog is intentionally tiny:

| Path | Type | Notes |
| --- | --- | --- |
| `want.trigger` | enum: `request \| rss \| upgrade \| manual` | what put this acquisition in motion |
| `want.requesters` | list\<user\> (dynamic) | the tracking's requester set; empty for untriggered/automatic wants |
| `want.tier` | enum (admin's tier catalog, dynamic) | the tier the want resolves under |

This is the whole answer to "how much context do we expose?" — **scope control is registry curation**. Nothing is expressible unless a field is registered; every entry is typed (so the editor offers only valid operators) and phase-tagged (so it behaves correctly at every moment). If a field feels too spicy for v1, don't register it yet.

> **Population status.** [Acquisition](../../modules/acquisition/README.md) — the Subject's assembler — is not built yet, so `want.*` ships *registered but unassembled*: `Want` is nil until acquisition populates it. The evaluator's population backstop (see [The evaluator](#the-evaluator)) reads an unassembled half as **indeterminate** ("we don't know who wants this"), which is distinct from a populated-but-empty requester set ("definitively no one") — the [two-kinds-of-non-value](#two-kinds-of-non-value) distinction carrying the transition safely. The editor should keep unwired fields un-foregrounded until they're live.

One division of labor worth stating explicitly: **per-user *quality* never enters conditions** — it's handled upstream by tier → profile *selection* ([requests](../../modules/requests/README.md#tier)). **Per-user *placement* enters routing** as conditions over `want.requesters`. One mechanism each, no overlap.

### `decision.*` — reserved

From `grab` onward, the quality outcome is itself a fact: which profile ran, which bin was selected, the score, which custom formats hit. Routing wants to route on *why* a release was picked, not just *what* was picked ([routing OQ](../../modules/routing/README.md#open-questions)). The binary phase model couldn't even express this — the decision is neither pre- nor post-download in a useful sense; on the timeline it's simply a `grab`-phase namespace. Causality is encoded in the tag: quality evaluates at `search`, so its own rules structurally *cannot* reference `decision.*`; routing evaluates at `grab`, so it can. The exact field set is pinned with routing's iteration 2 — the substrate just reserves the namespace and its phase.

### What stays out: `system.*`

Routing's iteration 1 sketched a `System` context (free space per library, downloader health, current time). It is **deliberately excluded** from the Subject:

- **Volatility.** Every registered field is reproducible from the audit trace; system facts make dry-run and re-evaluation non-deterministic in a way nothing else does.
- **Shape.** "Free space per library" is keyed by the very thing routing is choosing — a condition on the decision's *output* is a smell, closer to action-set validation than predicate input.
- The actual needs are served elsewhere: an unhealthy/deleted downloader is a config-time validation error and an evaluation-time fallback ([routing](../../modules/routing/README.md#open-questions)), not a predicate.

### `Field[T]` — value, confidence, provenance

The parsed namespaces do **not** carry bare values. Each parsed field is a `Field[T]` carrying:

- **Value** — the parsed value (`"2160p"`, `"BluRay"`, …).
- **Confidence** — how sure the parser is. A release title is a *claim* typed by an uploader; `2160p` sitting as an explicit token is near-certain, while `UHD`/`BL`/`RM` abbreviations are guesses, and a missing token is confidence zero.
- **Provenance** — what evidence produced the value (which token / which rule), for debugging and the decision log.

v1 reads `.Value` and **ignores** confidence and provenance — selection trusts the parsed value, and the escape hatch for a wrong parse is a manual override (see [quality-profiles OQ#4/#12](../../modules/quality-profiles/README.md#open-questions)). The reason to carry the shape anyway is the cheap-vs-expensive asymmetry: flatten now and adding confidence later is a model-wide retrofit touching every consumer; carry `Field[T]` now and it's purely additive — the parser emits better numbers into a slot that already exists, and an escalation wrapper starts reading `.Confidence`, with no consumer change. See [Confidence](#confidence).

## The timeline

Evaluation happens at three **moments** in an acquisition's life, and knowledge only accumulates — it's a ratchet, never a reset:

```
search ──────────────► grab ──────────────► import
"which candidate        "where does the      "is it what it
 do we take?"            winner go?"          claimed to be?"
 quality profiles        routing              quality re-gate +
                                              name templates
```

| Namespace | `search` | `grab` | `import` |
| --- | :-: | :-: | :-: |
| `candidate.*` `identity.*` `quality.*` `encode.*` `media.*` `want.*` | ✓ | ✓ | ✓ |
| `decision.*` | — | ✓ | ✓ |
| `mediainfo.*` | — | — | ✓ |

Every field carries one tag — its **phase**, the earliest moment it's knowable — and the whole evaluation contract is one sentence:

> Evaluating at moment M, a field with phase ≤ M resolves normally; a field with phase > M is **indeterminate** — it never contributes a spurious match *or* a spurious reject.

Phase is structural, not decorative. Two things that were special cases under the old binary fall out as ordinary consequences:

- **The [import-time re-gate](../../modules/quality-profiles/README.md#import-time-re-gate) stops being a feature.** It is the same stored trees asked again at `import`, when `mediainfo.*` has become knowable. Nothing is special-cased.
- **Routing seeing the quality decision becomes expressible.** `decision.*` is a `grab`-phase namespace; quality (evaluating at `search`) structurally can't see it, routing (evaluating at `grab`) can.

Note that phase and structure are **orthogonal axes** — the tempting shortcut "Release = early, situation = late" is false: `mediainfo.*` is about the *artifact* but arrives *last*; `want.*` is situational but knowable *first*. A field's namespace says what the fact is about; its phase says when it's knowable.

Name templates render at `import` — the last moment — so every field is knowable at render time; the template editor uses phase tags only as informational badges.

### Two kinds of non-value

The timeline forces a distinction the substrate must keep crisp, because conflating them produces wrong answers in both directions:

- **Not yet knowable** — a field whose phase is later than the current moment (`mediainfo.video_codec` at `search`). The condition is **indeterminate**: unanswerable now, answerable later. It must never contribute a reject or a match at the earlier moment.
- **Knowably empty** — a field that is present and simply has no value: an empty `want.requesters` on an RSS grab, a missing `encode.release_group`. This is an ordinary value; the condition evaluates **definitively** (`contains` over an empty set is `false` — now and forever).

Get this wrong and "route kids' requests to the kids library" becomes mysteriously indeterminate for RSS grabs instead of cleanly non-matching. The registry distinguishes the cases by construction: phase handles the first; optional/empty-valued fields are just values.

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
  | Literal { Type: "bool",   Value: true }
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
| `contains` | string, list | substring / membership — over an empty list, definitively `false` (see [two kinds of non-value](#two-kinds-of-non-value)) |
| `in` `not in` | value against a list literal | the right operand is a typed list, not a comma-split string |
| `and` `or` `not` | sub-trees | logical composition |

Type-checking is the registry's job and happens when a rule is **saved**, not when it runs. A condition that doesn't type-check can't be authored.

### Trees, loaded as a unit

A rule is a tree of conditions joined by `and` / `or` / `not`. The whole tree belongs to one owner (a routing rule, a quality gate, a custom format) and is **always loaded and written as a unit** — nothing ever queries an individual interior node. So the tree is stored and read whole (a serialized/JSONB value), not as a flat table of rows that reference each other by id.

This kills v0's worst wart directly: v0 stores conditions as flat rows where logical operators hold child UUIDs as strings, with no parent/root marker, and the evaluator re-reads the *entire* condition table from the database for every logical node it walks. Load-as-a-unit means one read produces the whole tree; the evaluator then walks it purely in memory.

## The field registry

The registry is the authoritative catalog of every addressable field on the Subject: its `path`, label, type, enum values (if any), `phase`, value type, and dynamic source (if the options come from an API, e.g. configured indexers or users). It is **promoted from struct tags** — the field metadata lives as `path:`/`type:`/`enumValues:`/`phase:` tags and is reflected into the first-class registry, the single source of truth rather than an incidental projection.

One registry drives **three** things from one definition:

1. **Evaluation lookup** — resolving `Field{path}` to a value on the Subject.
2. **Authoring-time type-checking** — is this operator valid on this field? is this literal a legal value for this enum? Both answered against the registry when a rule is saved.
3. **The authoring UX** — and this is where Arrflix can pull ahead of the *arr stack.

### One resolver

The registry's path lookup is **one function with two consumers**: condition evaluation resolves `Field{path}` through it, and name-template rendering builds its template data from it (walk every registered path, resolve against the Subject, unwrap `Field[T]` → `.Value`, nest by namespace).

```
registry path ──► resolve(subject, path) ──┬─► Eval        (conditions)
                                           └─► template data (rendering)
```

This upgrades "one catalog" to "one lookup": it is *impossible by construction* for a path to work in a condition but not a template, or vice versa. Go's `text/template` traverses lowercase map keys fine, so the registry's flat paths are usable verbatim as token names (`{{.quality.resolution}}`); whether name templates keep raw `text/template` syntax or their own token grammar is [their call](../../modules/name-templates/README.md) — either way the vocabulary is the registry's paths, resolved by this function.

### Author-time experience

Because the editor knows the field catalog and its types, the rule/condition builder (and the [name-template editor](../../modules/name-templates/README.md), which uses the same registry) can offer:

- **Autocomplete** — type `qual` → `quality.resolution`, `quality.source`, `quality.name`, …
- **Type-aware operator menus** — pick an enum field and only `==`/`!=`/`in` are offered; `>` simply isn't there. The invalid rule is *unbuildable*, not merely rejected.
- **Value autocomplete** — after `quality.resolution ==`, a dropdown of `SD / 480p / 720p / 1080p / 2160p` straight from the field's enum values; for a dynamic field like `candidate.indexer` or `want.requesters`, options fetched live.
- **Right-widget-per-type** — number input for numbers, toggle for booleans, dropdown for enums, chip-list for `in`.
- **Live validation** — an ill-typed comparison flagged the instant it's built.
- **Phase badges** — a moment marker on later-phase fields (`mediainfo.*` → "import", `decision.*` → "grab"), so the author *sees* when a condition becomes answerable — e.g. that a `mediainfo.*` gate only resolves at the [import re-gate](../../modules/quality-profiles/README.md#import-time-re-gate), not at search time.

The discriminated-union operand model maps directly onto a rich-text editor's node model: a **Field** operand renders as an atomic, non-editable token/chip; a **Literal** renders as the typed widget. The explicit operand model and the slick editor are the *same* design decision, not two — which is why "type the operands" and "make the editor great" are one line of work, not a tradeoff.

## The evaluator

The evaluator is a pure function in `internal/rules`:

```
Eval(tree RuleTree, subject Subject, at Moment) (verdict {true|false|indeterminate}, trace Trace)
```

No repo, no logger, no DB, no routing/quality types. This makes it trivially unit-testable (a table of `tree × subject × moment → verdict`) and reusable by every consumer.

Properties:

- **Load-once, walk-pure.** The repo (or service) loads the whole tree into a `RuleTree` once; `Eval` walks it in memory. No per-node database access.
- **Three-valued, by the timeline.** A condition over a field whose phase is later than the evaluation moment is *indeterminate* — it never contributes a spurious match or reject. Each consumer maps indeterminate onto its own safe default. This replaces v0's accidental behavior (a missing `mediainfo` value silently compared against `"<nil>"` for `==`, and hard-errored for ordering ops) with a defined, structural one.
- **The moment is an explicit parameter, not derived.** Callers know their moment intrinsically (quality = `search`, routing = `grab`, importer = `import`), dry-run can preview any moment over the *same* subject without fabricating partially-populated ones, and `(tree, subject, at)` fully determines the verdict — a replayed evaluation can't drift because some half of the Subject happened to be populated since. Population stays as a *defensive backstop*, not the signal: a field resolves iff its phase ≤ the moment **and** its half of the Subject is actually populated; an asserted moment with missing data yields indeterminate, never a panic. (An unassembled half — nil — is "not knowable," distinct from a populated half holding empty values, which evaluates definitively; see [two kinds of non-value](#two-kinds-of-non-value).)
- **Neutral trace.** `Eval` returns a per-node trace (each condition's resolved operands and its tri-state result, distinguishing indeterminate from false). The trace is consumer-agnostic; routing adapts it into its dispatch evaluation record, quality adapts it into [decision-log](../../modules/quality-profiles/README.md#decision-log) rows. The evaluator does not know what an "action" or a "score" is.

### Layering

The pure/orchestration split follows the backend's standard layering:

- **`internal/rules`** (pure domain) — `RuleTree`, the typed operand/condition types, `Trace`, `Eval`, and the pure tree assembly. Compiles without `internal/repo`, `dbgen`, or `pgtype`.
- **repo** — loads the stored tree (one read) and returns it as a domain value.
- **service** (routing's, quality's) — loads tree(s) + the Subject, calls `Eval`, applies its own reduction. [Acquisition](../../modules/acquisition/README.md) is the natural assembler of the Subject — at each moment it holds exactly the halves that exist (candidate + parse + media + want at `search`; + decision at `grab`; + mediainfo at `import`).

The Subject and the field registry live alongside the rules package (rather than in the generic `model` grab-bag) so the substrate is one neighborhood — relevant when specs eventually co-locate with code.

## Confidence

Confidence is **not load-bearing** for v1 of any consumer, and we are deliberately not building it now. Two separable things hide in the word:

- **The capability** — computing good, graded, per-axis confidence (a parsing-internal calibration problem). **Deferred.** No v1 surface consumes it: gate/bin/score/route all run on the parsed value, and the [import re-gate](../../modules/quality-profiles/README.md#import-time-re-gate) checks the real bytes via `ffprobe` (ground truth) — it doesn't care how confident the *title* parse was. Confidence only matters in the pre-download window where the title is all we have, and v1's answer there is "trust the parse, override manually."
- **The seam** — carrying `Field[T]` through the Release instead of flattening. **Kept now.** It's the only foundational commitment confidence demands, and it's a data-shape decision made once.

When parsing's confidence becomes real, the consumer-side change is additive: an **escalation wrapper** around gate evaluation — a low-confidence parse *softens* a hard gate (hold for interactive/manual review with a logged reason) rather than triggering a confident auto-reject. The evaluator and Subject don't change; the wrapper reads the `.Confidence` that was always there. (This is [quality-profiles OQ#12](../../modules/quality-profiles/README.md#open-questions), architecture-preserved.)

## The reductions

For clarity, the three reductions consumers apply to `Eval`'s verdict — none of which live in this package — and the moment each runs at:

| Reduction | Owner | Evaluates at | On `true` | On `false` |
| --- | --- | --- | --- | --- |
| **Dispatch** | [routing](../../modules/routing/README.md) | `grab` | apply the rule's action set; stop (or `continue`-chain) | try the next rule |
| **Hard gate** | [quality profiles](../../modules/quality-profiles/README.md#hard-gates-vs-soft-scoring) | `search`, re-run at `import` | reject the release, log the gate reason | release survives this gate |
| **Custom format** | [quality profiles](../../modules/quality-profiles/README.md#hard-gates-vs-soft-scoring) | `search`, re-run at `import` | add the format's weight to the release's score | weight not added |

Quality's structured v1 scorers (preferred group, codec preference, HDR/audio variant, proper/repack) are predicates too — `encode.release_group in [...]`, `mediainfo.video_codec == "H.265"`, `quality.is_repack == true`. The closed structured form is a *constrained authoring surface that emits the same trees*; it is not a second engine. This is what reconciles "v1 ships no custom-format DSL" (a UI scope decision) with "quality reuses the evaluator" (an architecture decision).

**Ranking is not a reduction here.** Bin ordering, cutoff, and per-bin size bands are structured profile data, *not* conditions — you can't `>` a string enum, and ordering an open list of releases isn't a boolean question. The rule substrate answers "does this match?"; the profile answers "which is best?" using those answers as inputs. Keeping that boundary crisp is the point of this whole separation.

## Relationship to neighbors

| Neighbor | Relationship |
| --- | --- |
| **[Parsing](../../modules/parsing/README.md)** | Produces the `Field[T]`-wrapped parse that populates `identity.*`, `quality.*`, `encode.*` on the Release. The Subject consumes the full parse (with confidence/provenance), never a flattened projection. |
| **[Tracking](../../modules/tracking/README.md) / [requests](../../modules/requests/README.md)** | Produce the durable intent facts surfaced as `want.*` — the requester set, trigger, and tier live on tracking (requests are frozen after spawn). |
| **[Acquisition](../../modules/acquisition/README.md)** | Assembles the Subject at each moment and hands it to the evaluating consumer. |
| **[Routing](../../modules/routing/README.md)** | Primary consumer, at `grab`. Evaluates dispatch rules; reduction = action set. Owns its own evaluation/audit record built from the neutral trace. |
| **[Quality profiles](../../modules/quality-profiles/README.md)** | Primary consumer, at `search` and again at `import`. Hard gates and custom formats are trees over the Subject; reductions = reject / add-weight. The re-gate is the same trees re-run at `import` with `mediainfo.*` knowable. Ranking stays profile-owned structured data. |
| **[Name templates](../../modules/name-templates/README.md)** | Consumes the **data half** — the Subject + registry, through the [one resolver](#one-resolver) — to render tokens at `import`. Does not evaluate conditions. Shares the registry-driven authoring editor. |
| **[Audit pattern](../audit/README.md)** | Each consumer writes its decision rows from the neutral trace; retention and the Activity view are owned there. |
| **[Errors](../errors/README.md)** | Sibling foundational pattern: a real `internal/` package filed as a pattern because it's shared substrate. The genre precedent for this doc. |

## Open questions

1. **Tree serialization shape.** JSONB blob per owner vs a typed serialized column. Lean: a single serialized tree value per owner (routing rule / gate / custom format), since the tree is always read and written whole and never queried by interior node. Pin the exact encoding in the implementation iteration.
2. **Operator set boundaries.** The table above is the v1 lean. Do we want `matches` (glob/regex) on string fields for power users, or an `exists`/`is present` operator for optional scalars — or keep v1 to `==`/`contains`/`in` and add the rest with the advanced custom-format tier? Lean: structured-first; defer regex to the advanced tier, where it compiles down to the same condition shape.
3. **List literals.** `in`/`not in` take a typed list literal. Is the element type constrained to match the field's type (enum-in-enum-list), and how does the editor render adding/removing elements? Lean: yes, typed-homogeneous lists, chip-list widget.
4. **Field[T] in persistence.** When the parse is persisted (the [persisted parse](../../modules/parsing/README.md#persisted-parse)), do we store confidence/provenance in the blob too? Lean: yes — nearly free to store, expensive to backfill, same asymmetry as carrying it in the model.
5. **Registry generation vs hand-authored.** Continue generating the registry from struct tags, or define it as standalone data the structs conform to? Lean: keep generating from tags (one source, compiler-checked) but expose it as the first-class registry type rather than an incidental `ListContextFields` projection.
6. **Cross-field comparisons.** The operand model permits `Field == Field` (`quality.resolution == media.expected_resolution`). Is there a real v1 use, and does the editor surface it? Lean: support it in the model (it's free), don't foreground it in the v1 editor until a use appears.
7. **Where `Subject` and the registry live.** `internal/rules` vs a dedicated package vs staying in `internal/model` (where phases 1–2 built `model.Release`). Lean: co-locate the substrate (Subject + registry + evaluator) so it's one neighborhood; decide the exact package split when phase 3 does the re-shape.
8. **Indeterminate semantics surfaced to authors.** A `grab`- or `import`-phase condition is indeterminate at earlier moments. Should the dry-run/preview UI show "not yet evaluable (import)" distinctly from "evaluated false"? Lean: yes — the trace already distinguishes them; the preview should too, per moment.
9. **`want.requesters` quantifier and identity.** `contains` gives "any requester matches" — is an "all requesters" quantifier ever needed (e.g. route to kids library only if *every* requester is a kid)? And is the element a user id or a name (lean: id, rendered via the dynamic source)? Lean: ship `contains` only; add a quantifier if a real rule needs it.
10. **Placement drift.** Routing decisions driven by `want.*` are made from the facts *as of grab time*; the requester set can change afterward. v1 accepts the drift. Should [hygiene](../../modules/hygiene/README.md) eventually surface "this file's placement no longer matches its routing rules"? Defer.
11. **Moment: parameter vs derived. (Answered: explicit parameter.)** `Eval` takes `at Moment`. Deriving the moment from which Subject halves are populated (phase 2's `MediaInfo == nil`) stops scaling at three moments and makes verdicts depend on incidental population state — bad for replay and dry-run. Population remains a defensive backstop only (unassembled half → indeterminate). Phase 3 updates the phase-2 evaluator accordingly; see [The evaluator](#the-evaluator).
12. **`decision.*` field set.** Reserved namespace, `grab` phase. Exact fields (profile, bin, score, format hits) are pinned with routing's iteration 2.

## What we're explicitly not deciding here

- Exact table/column shapes or the tree's serialized encoding — implementation.
- The full operator grammar's edge cases (regex flavor, numeric coercion rules) beyond the v1 lean.
- Each consumer's reduction details — action sets live in [routing](../../modules/routing/README.md); gate/score/ranking live in [quality profiles](../../modules/quality-profiles/README.md).
- The parse model and confidence *calculation* — owned by [parsing](../../modules/parsing/README.md).
- The name-template token grammar — owned by [name templates](../../modules/name-templates/README.md); this doc owns the shared Subject + registry + resolver it draws from.
- The `want.*` *production* — requester rows, tier resolution, trigger semantics are owned by [tracking](../../modules/tracking/README.md) / [requests](../../modules/requests/README.md); this doc only surfaces them as fields.
- The decision-log/audit storage — owned by the [audit pattern](../audit/README.md); this doc produces the neutral trace consumers turn into rows.

## Doc neighbors

- [Routing](../../modules/routing/README.md) — consumer; predicate → dispatch action, at `grab`
- [Quality profiles](../../modules/quality-profiles/README.md) — consumer; predicate → reject / add-score at `search`/`import`, plus profile-owned ranking
- [Name templates](../../modules/name-templates/README.md) — consumer of the Subject + registry via the one resolver (rendering, not evaluation)
- [Parsing](../../modules/parsing/README.md) — produces the `Field[T]` parse the Release half is built from
- [Tracking](../../modules/tracking/README.md) / [Requests](../../modules/requests/README.md) — produce the durable intent facts behind `want.*`
- [Errors](../errors/README.md) — sibling foundational pattern; the precedent for a package filed under `patterns/`
- [Audit](../audit/README.md) — where consumers' decision rows live
