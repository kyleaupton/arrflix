# Quality profiles — which release do we grab

**Status:** Draft, iteration 1

This doc defines how Arrflix decides *which release* to grab when multiple are available. It captures the **tier ↔ profile** split, the selection algorithm at a conceptual level, the hard-gate / soft-score distinction, and the interactive-search escape hatch. It does **not** pin down data shapes, custom-format syntax, or wire formats — those come in later iterations.

This doc is the companion to [tracking](../tracking/README.md). Tracking decides *when* to grab; quality profiles decide *what* to grab.

## TL;DR

- Two concepts: a **tier** is the user-facing simple choice (HD, 4K); a **profile** is the admin-managed config behind it.
- A tier resolves to a profile **per media type** — an HD-movie profile and an HD-series profile are distinct, because movie and episode releases have incompatible size ranges, release-group ecosystems, and quality lists. One user-facing label, type-scoped configs behind it. Profiles can also exist outside of tier designation, for admin / tracking use.
- Profile owns: quality list + ordering, cutoff, hard gates, soft scoring (custom formats), indexer scoping.
- **Bin vocabulary is per domain, rendered by parsing.** [Parsing](../parsing/README.md#the-quality-attribute-core) is now domain-dispatched, so it both detects the quality core AND renders the bin (`Bluray-2160p Remux` for series, `Remux-2160p` / `BR-DISK` for movies). The profile consumes the rendered bin; it doesn't own the vocabulary tables. Same detection, two vocabularies; safe because every consumer (and parsing itself) is now domain-scoped. See [Per-domain quality vocabulary](#per-domain-quality-vocabulary).
- Three admin-UX tiers: **preset** (90% of users), **profile editor** (power), **custom formats** (advanced). Defaults must be great; advanced surfaces stay hidden.
- Selection algorithm: filter by hard gates → pick best allowed quality bin with a release → pick highest-scored release in that bin → deterministic tie-break.
- **Hard gates remove**, **soft scores order**. Mixing them is the TRaSH-guides trap.
- **The same gates and scores run twice:** on the release's _advertised_ attributes to decide the grab, then on the file's _asserted_ (`ffprobe`) attributes at import — the **import-time re-gate**. Hard-fail on the real file → reject + blocklist + re-search; soft-fail → keep, penalize, log a hygiene finding. The file's recorded quality stays the **advertised** parse — the re-gate just validates it (a resolution lie hard-fails; `Source` is unverifiable either way).
- Interactive search is always one click away and bypasses automated selection, but still writes audit rows.
- Requesters get zero in-tier control. Admins get all the knobs.

## Why "tier + profile" and not one knob

Different audiences need different surfaces:

- **Requesters** want one decision: "movie quality, please." Forcing them to think about Bluray vs WEB-DL or x265 vs x264 is hostile UX.
- **Admins** running monitoring need precise control: which release groups, which codecs, what's the cutoff, when do we upgrade.

A single "quality knob" can't serve both. So:

- **Tier** is the requester-facing concept. Today: `HD`, `4K`. The label tells a user what they're getting; the system handles the rest.
- **Profile** is the admin-facing config. The HD tier *resolves to* a specific profile that the admin has tuned — resolved **per media type**, so HD-for-movies and HD-for-series are separately tunable configs behind the one label. The 4K tier resolves to its own profiles. Profiles outside of tier designation can also exist — for example, a tracking record that wants a "best effort 720p" or a more aggressive "anime profile."

The tier is a **stable contract** with users. The profile is its **mutable implementation**. Admins can change what "HD" means without changing the request UX.

### The TRaSH-Guides cautionary tale

[TRaSH Guides](https://trash-guides.info/) exists because Sonarr/Radarr's quality system is too configurable. Most users can't write a custom format that scores releases the way they want. They copy/paste community-curated profiles and pray. That's a system that exported its UX work to a wiki.

The takeaway: **defaults must be great**, and **advanced configuration must be opt-in and visually separated**.

Three admin-UX tiers:

| Tier              | Surface                                                                                 | Audience                                 |
| ----------------- | --------------------------------------------------------------------------------------- | ---------------------------------------- |
| **Preset**        | "HD / 4K / Best available" radio buttons; picks a curated profile under the hood        | 90% of users, including the *arr-replacement use case |
| **Profile editor**| Quality list ordering, cutoff, size limits, preferred release groups (as structured data)| Power users, on a clearly-marked "Advanced" tab |
| **Custom formats**| Full regex / rule-level scoring                                                         | Users who'd run TRaSH today              |

The profile editor should be expressive enough that the "Custom formats" tier is rarely needed.

## What a profile owns

A profile encodes the following concerns:

1. **Quality list with ordering** — a ranked list of acceptable bins (e.g., `2160p Bluray > 2160p WEB-DL > 1080p Bluray > 1080p WEB-DL > 1080p WEBRip`). Position = preference; first match wins. The list is drawn from the profile's domain bin vocabulary — a movie profile orders `Remux-2160p` / `BR-DISK`, a series profile orders `Bluray-2160p Remux`; the vocabularies themselves live in [parsing](../parsing/README.md#the-quality-attribute-core) and the profile just references entries by name.
2. **Cutoff** — the quality at which we stop searching for upgrades. Once we have a file at or above cutoff, no further auto-search for this item.
3. **Hard gates** — filters that reject a release outright (min seeders, min/max size, blocklisted groups, indexer eligibility). See [Hard gates vs soft scoring](#hard-gates-vs-soft-scoring).
4. **Soft scoring** — preferences that contribute to a release's score within its quality bin (preferred release groups, codec preferences, audio, HDR variants, proper/repack). See same section.
5. **Indexer scoping** — which indexers are eligible for searches under this profile. Default: all healthy indexers. Power-user feature, important for anime / region-specific content.

**Notably *not* in the profile:**

- **Upgrade behavior (`auto` / `propose` / `none`)** lives on tracking, not profile. The profile defines what "better" *means*; tracking decides how aggressively to *act* on betterness. Different trackings can use the same profile with different upgrade strategies.
- **Search schedule** — also tracking's concern.

## Tiers

A tier is a small, system-recognized set of designations. A tier **resolves to a profile per media type** — the binding is keyed on `(tier, media_type)`, so the `HD` label points at one profile for movies and another for series, and profiles are correspondingly **type-scoped**. The binding is extensible to further keys (per requester-group, per library) but those are deferred — see [open questions](#open-questions).

This split is not cosmetic. Movie and episode releases have incompatible per-file size ranges (a sane movie size gate would reject every episode, and vice-versa), different release-group ecosystems, and TV-only concepts like season packs — it's the reason Sonarr and Radarr ship as separate quality systems.

Initial tier set:

- **HD** — the default tier. Resolves to the admin's HD movie / HD series profiles.
- **4K** — opt-in tier. Resolves to the admin's 4K profiles if 4K is enabled; otherwise absent from the request UI.

Tier registry is extensible (admins may want SD, 8K, "Source" eventually), but iteration 1 keeps it to HD and 4K. Adding tiers later doesn't break existing requests because tier is a forward-compatible enum, not a freeform string.

Requesters select tier at request time, gated by their permissions (`can_request_movie_hd`, `can_request_4k`, etc. — see [Story 1](../../stories/01-happy-path-auto-approve.md)).

## Per-domain quality vocabulary

Movies and series **name the same release differently** — and the difference is domain-meaningful, not cosmetic. [Parsing](../parsing/README.md#the-quality-attribute-core) is domain-dispatched: it takes the domain at the call site, runs the domain-appropriate detection logic (Sonarr's for series, Radarr's for movies), produces the orthogonal **attribute core** `(Source, Resolution, Modifier, Revision)` as its internal representation, and renders the per-domain **bin name** as a parse output. The profile consumes the rendered bin to build its quality list:

```
input + domain  ──►  parsing detection (per upstream, verbatim)
                          │
                          ▼
                     QualityCore (Source, Resolution, Modifier, Revision)   [orthogonal, lossless]
                          │
                     parsing: bin(core, domain)  (rendered at parse time)
                          ├──► series bin vocabulary (Sonarr names)  ──► series quality lists
                          └──► movie  bin vocabulary (Radarr names)  ──► movie  quality lists
```

This is **not two quality systems** — the orthogonal core is the lossless interlingua (Sonarr's flat model is a degenerate projection of it). It's a presentation/binning layer: same core, two vocabulary tables, domain selected at parse time.

### Why the vocabularies diverge

Movies have tiers TV effectively doesn't. **Remux** is the sweet-spot tier for movie collectors (disc quality, no full-disc bloat), so Radarr promotes it to a **first-class top-level bin** that movie profiles routinely order on (`Remux-2160p > Bluray-2160p > …`); **BR-DISK** (full untouched disc) is likewise a real movie tier. TV rarely ships as remux or full disc, so Sonarr nests remux as a **modifier suffix** on the Bluray bin and never foregrounds either. So the two bin *structures* track what each domain's collectors actually gate on — flattening movies into Sonarr's nested naming loses information movie profiles need.

### The two vocabularies

Same core, projected two ways. The rows that **agree** (shared heritage — WEB-DL, WEBRip, HDTV, plain Bluray, resolutions) are most of the corpus; the divergence is concentrated in the disc modifiers:

| `QualityCore` | Series bin (Sonarr) | Movie bin (Radarr) |
| --- | --- | --- |
| `(BluRay, 2160p, REMUX)` | `Bluray-2160p Remux` | `Remux-2160p` |
| `(BluRay, 1080p, REMUX)` | `Bluray-1080p Remux` | `Remux-1080p` |
| `(BluRay, 2160p, BR-DISK)` | `Bluray-2160p` *(folded)* | `BR-DISK` *(own tier)* |
| `(BluRay, 1080p, NONE)` | `Bluray-1080p` | `Bluray-1080p` *(agree)* |
| `(WEB-DL, 1080p, NONE)` | `WEBDL-1080p` | `WEBDL-1080p` *(agree)* |
| `(HDTV, 1080p, RAWHD)` | `Raw-HD` | `Raw-HD` *(agree)* |

`seriesBin` appends the modifier as a suffix and folds `BR-DISK` into plain Bluray; `movieBin` promotes `REMUX` and `BR-DISK` to top-level bins. Both are pure functions of `(core, domain)`.

### Why per-domain bins are safe

Quality profiles are already `(tier, media_type)`-scoped — a profile applies to a movie library or a series library, never both (see [Tiers](#tiers)). And parsing is now also domain-scoped at the call site. So per-domain bins create **zero** cross-domain inconsistency: every step from search candidate to import knows the domain authoritatively. A movie profile speaks `Remux-2160p`, a series profile speaks `Bluray-1080p`, the parse already rendered the right one, and that's exactly what a Sonarr + Radarr user expects.

### Bins as keys, not strings

A profile **references** bins to order and gate them, so a bin is a stable per-domain **key** (with a display name as a separate render), mirroring Sonarr/Radarr's quality-definition tables — not a free string the parser emits. The two vocabulary tables (the bin set + default ordering per domain) are profile-owned data, and they are what makes **per-tool community config interop** (TRaSH-style custom formats, written separately for Sonarr and Radarr) possible at all.

### Ownership

- **Parsing owns** the attribute core (`Source`, `Resolution`, `Modifier`, `Revision`), its per-domain detection logic, the bin rendering `bin(core, domain)`, and the two vocabulary tables (`seriesBins`, `movieBins`).
- **Quality profiles own** the ranked allowed-bin list, the cutoff, hard gates, soft scoring (custom formats), and indexer scoping. The profile consumes a `ParsedRelease` and reads `Quality.Name` / `Quality.Source` / etc. directly — it doesn't project, it doesn't own the vocabulary.

Per-domain dispatch shrinks the static parity allowlist to one principled class: identity-independent quality (parsing OQ#13, retained as a class-shaped `allowlistPredicate`). The ~17 Radarr `.mkv` extension-default entries disappear by construction (per-domain extension tables); the bin field is `enforced=true` against both tools.

## The selection algorithm

Given a list of indexer search results for one want, the quality engine runs:

1. **Hard-gate filter.** Each release is checked against the profile's hard gates (seeders, size, blocklist, indexer eligibility). Any failure → reject, log reason.
2. **Quality detection.** Each surviving release carries a parsed [attribute core](../parsing/README.md#the-quality-attribute-core) AND a pre-rendered bin name (parsing knew the domain at the call site). Releases whose bin isn't in the profile's allowed list → reject. The profile reads `quality.name`; no projection step here.
3. **Bin grouping.** Surviving releases are grouped by quality bin per the profile's ranked list.
4. **Soft scoring.** Within each bin, every release gets a score from the custom format rules.
5. **Pick best bin.** Highest-ranked bin (per the profile's ordering) that contains at least one release.
6. **Pick best release in bin.** Highest-scored release in the chosen bin.
7. **Tie-break (deterministic).** If multiple releases tie on score, fall back to: seeders descending → size closest to per-quality target → release date descending → infohash lexicographic. Last resort is arbitrary but deterministic so we don't flap on repeat searches.

**Critical property: bin-first, not score-mixed.** A 720p with crazy good scoring should NOT beat a 1080p WEB-DL just because of custom formats. Quality bin dominates; scoring orders *within* bins. This matches Sonarr/Radarr default behavior.

(Radarr has a "format priority over quality" option for users who do want score-mixed. Adding that later is fine; default is bin-first.)

## Hard gates vs soft scoring

This distinction is load-bearing. Mixing them is what makes Sonarr quality configs unreadable.

| Behaviour              | Hard gate                                  | Soft score                                  |
| ---------------------- | ------------------------------------------ | ------------------------------------------- |
| Seeders                | Below min → reject                         | Higher seeders → small score bump           |
| Size                   | Above max → reject; below min → reject     | Closer to ideal → small bump                |
| Release group          | In blocklist → reject                      | In preferred list → big bump                |
| Quality bin            | Not in profile's allowed list → reject     | Position in list determines bin pick        |
| Codec                  | (Rare) banned codec → reject               | Preferred codec → bump                      |
| Indexer                | Not in profile's allowed set → reject      | (Not scored)                                |
| Audio / HDR variant    | (Rare) require Atmos? Probably soft only   | HDR10+/Atmos → bumps                        |
| Repack / Proper        | Not gated                                  | Bump for proper/repack                      |

**Heuristic for which is which:** "If I see a release that fails this rule, do I want it to *never grab* (gate) or just to *rank lower* (score)?" Most preferences are scores. Most safety/sanity checks are gates.

## Import-time re-gate

The selection algorithm above runs on the release's **advertised** attributes — the [parsed](../parsing/README.md) title is all we have before download. Advertised attributes are a _claim_. The same profile is therefore run a **second time at import**, against the file's **asserted** attributes (`ffprobe`/mediainfo on the actual bytes), to confirm we got what we grabbed.

The re-gate reuses the existing gate + score machinery — no new comparison system. The outcome maps onto the same gate-vs-score distinction:

| Re-gate outcome on the asserted file | Action |
| ------------------------------------ | ------ |
| **Passes** | Place the file normally. |
| **Hard-gate fail** (e.g., advertised 2160p, the stream is 1080p; or a quality bin we don't accept) | **Reject the import, blocklist the release, return the want to `searching`** so the pipeline re-grabs. The auto-recovery loop. |
| **Soft-score drop** (e.g., the "Atmos" claim is false but it's a fine 1080p) | **Keep the file**, apply the score penalty (lowering its recorded score), and log a [`quality/advertised-mismatch`](../hygiene/README.md) finding. No user notification — soft mismatches are common; the finding is there for anyone who cares. |

**What the re-gate can and cannot reject.** It only re-evaluates attributes `ffprobe` can actually assert — resolution, codec, HDR/DV, audio format, channels, bit depth. It **cannot** assert **source** (BluRay vs WEB-DL), edition, or release group (see the [parsing verifiable taxonomy](../parsing/README.md#what-ffprobe-can-and-cannot-verify)). So a hard gate on resolution _can_ reject a downloaded file; a hard gate on source _cannot_ — source-based gates stay advertised-trusted at import. The reject-vs-penalize partition is an [open question](#open-questions).

**The file's recorded quality is the advertised parse, validated.** Arrflix persists the advertised `Quality` (the [parse snapshot](../parsing/README.md#persisted-parse)) as the file's recorded quality — there is **no** separate asserted bin, and `Quality` is never reconciled or mutated. The re-gate is what makes it trustworthy: a resolution mismatch hard-fails *before* the file is recorded, and `Source` is advertised-only (`ffprobe` can't see it). This recorded quality is what [upgrade detection](#upgrade-detection) compares and [name templates](../name-templates/README.md) render.

**Ownership.** The re-gate's pass/fail _logic_ lives here (it's the profile's gates and scores). The _pipeline step_ that invokes it, the blocklist, and the want-back-to-`searching` transition are [acquisition](../acquisition/README.md)'s; the [importer](../importer/README.md) supplies the asserted attributes and calls the re-gate before it hardlinks; the mismatch finding is [hygiene](../hygiene/README.md)'s. The profile only says "pass, penalize, or hard-fail."

## Upgrade detection

Once a want has been fulfilled (file imported), should we keep looking for better releases?

A release is **strictly better** than the current file if:

- It's in a higher-ranked quality bin than the current file, **or**
- It's in the same bin AND its score exceeds the current file's score by a configurable delta (anti-flapping).

The current file's bin and score are its recorded **advertised** values, validated by the [import-time re-gate](#import-time-re-gate) — so a soft-fail penalty (an over-advertised current file) correctly makes an honest release upgrade-eligible.

A want is **upgrade-eligible** if:

- Current file is below cutoff (the profile says "keep looking until you hit X").
- Tracking's `upgrade_behavior` is not `none`.

The action taken depends on tracking's `upgrade_behavior`:

- **`auto`** — the system replaces the current file. Existing hardlink is removed, new release is grabbed and imported.
- **`propose`** — the system queues an upgrade [notification](../notifications/README.md) ("Better release available — 4K Bluray 65GB, replace current 1080p WEB-DL 8GB?"). Requires one-tap approval. Default for most users.
- **`none`** — search stops once at-cutoff or as soon as any acceptable release is acquired (depending on whether cutoff is met).

`propose` is the recommended default. `auto` is for power users who trust the system; the failure mode (file swapped silently) is what makes Sonarr's auto-upgrade feel scary.

## Interactive search escape hatch

Auto-select is for monitoring and request-driven grabs. **Interactive search is for "I want to pick this one specifically"** — your manual workflow today.

Properties:

- Always available regardless of profile config.
- Shows *all* eligible results from all healthy indexers. The profile's hard gates and scoring still inform the *display* (rejected releases are shown but marked, scored releases are sorted), but the user can pick any release.
- A user-picked release still flows through the same download → import pipeline.
- The choice is logged in the decision log as a manual override, with the user ID.

Interactive search bypasses the automated selection algorithm; it does not bypass the system. Hardlinks, name templates, library logic all still apply.

## Decision log

Every release considered by the quality engine produces an audit row, written by [acquisition](../acquisition/README.md) as part of the system-wide [decision-artifact pattern](../../patterns/audit/README.md). This is what powers the "why didn't this download?" debugger.

Fields (conceptual — not data shape):

- The want this decision belongs to
- The search run identifier (so we can show "the last search saw N releases")
- Release identity (indexer, guid / infohash, title)
- Decision: `grabbed`, `runner_up`, `rejected`, `manual_override`
- Reason: structured (`gate: min_seeders`, `actual: 2`, `threshold: 5`) plus a human-readable summary
- Score (if scored)
- Timestamp

The decision log is **append-only**, retained for some bounded window (TBD — likely 30–90 days). It's not the source of truth for "what got grabbed" (the want and download_job are); it's the source of truth for "why."

## What quality profile does NOT own

To keep scope tight, these adjacent concerns live elsewhere:

- **Search execution** — the search scheduler reads tracking config, runs searches, and hands results to the quality engine. The profile is consulted; it doesn't run.
- **Tracking lifecycle** — tracking owns `active / paused / archived / canceled`.
- **Upgrade behavior strategy** — tracking decides `auto / propose / none`.
- **Notification routing** — when an upgrade is proposed or a grab happens, [notifications](../notifications/README.md) routes the event. Profile just provides the data ("here's the proposed release").
- **Quality detection / release-name parsing** — owned by [parsing](../parsing/README.md), which produces the domain-agnostic [attribute core](../parsing/README.md#the-quality-attribute-core). The profile consumes that core (and asserted `ffprobe` attributes); it parses neither. The profile **does** own the per-domain [bin projection](#per-domain-quality-vocabulary) on top of the core — parsing emits no bin.
- **The asserted-attributes extractor** (`ffprobe`/mediainfo) — a sibling extractor ([importer](../importer/README.md) / [scan](../scan/README.md)); the profile consumes `MediaInfo`, it doesn't probe.
- **Re-gate orchestration and want recovery** — the import-time re-gate's _logic_ lives here, but the _pipeline step_, the blocklist, and the want-back-to-`searching` transition are [acquisition](../acquisition/README.md)'s.
- **The advertised-vs-asserted mismatch finding** — surfaced by [hygiene](../hygiene/README.md) (`quality/advertised-mismatch`); the profile produces the delta, hygiene presents it.
- **Indexer management** — adding, removing, health-checking indexers. Profile references indexers by ID.
- **Decision log storage** — profile produces events; decision log persists them.
- **Exact data shapes** — column types, indexes, foreign keys, API contracts. Deferred.

## Interactions

| Neighbor                | How quality profile interacts                                                                 |
| ----------------------- | --------------------------------------------------------------------------------------------- |
| **Tracking**            | Tracking references a profile by ID. Same profile can be used by many tracking records.       |
| **Requests**            | Request specifies a tier; request service resolves `(tier, media type) → profile` when creating the want (media type is known at want-spawn). |
| **Wants**               | Each want carries the resolved `quality_profile_id` from its origin (tier resolution or tracking). |
| **Auto-select worker**  | Reads the profile to gate + score search results.                                             |
| **Indexer service**     | Profile references the indexer set (full or scoped) for searches.                             |
| **[Parsing](../parsing/README.md)** | Produces advertised quality from titles and (via the `ffprobe` extractor) asserted `MediaInfo`; the profile consumes both, parses neither. |
| **[Importer](../importer/README.md)** | Runs the import-time re-gate before placement: provides asserted attributes, calls the profile, acts on pass / penalize / hard-fail. |
| **[Hygiene](../hygiene/README.md)** | Surfaces the advertised-vs-asserted delta as `quality/advertised-mismatch`. |
| **Decision log**        | Every gate/score decision becomes a log entry.                                                |
| **Interactive search**  | Bypasses gating/scoring for selection but still displays them and logs the override.          |
| **[Notifications](../notifications/README.md)** | Profile + tracking together produce upgrade-available events that notifications routes. |

## UI naming

Internal model name: **profile** (with optional **tier** designation).
User-facing UI: requesters see "**Quality**" with options "HD" / "4K"; admins see "**Quality profiles**" and edit them. The word "profile" is admin vocabulary; the word "tier" is internal vocabulary.

## Open questions

1. **Custom format syntax.** Sonarr-style regex strings vs higher-level structured rules (group: X, codec: Y, audio: Z) vs both? Regex is most flexible but writes UX off; structured is most usable. Probably ship structured-first with a regex escape hatch; structured rules compile to regex under the hood.
2. **Profile scoping per library.** Are profiles global, or do they scope to a library (e.g., the kids library uses a different HD profile)? Tracking already references profiles by ID, so per-library could just be a default mapping. Decide when multi-library UX gets more attention.
3. **Predefined vs admin-editable quality list.** Ship a predefined set (Bluray-2160p, Bluray-1080p, WEB-DL-1080p, etc.) and allow admins to add. Sonarr does this. Probably correct; confirm in iteration 2. The predefined set is **per domain** now — the movie default list includes `Remux-*` / `BR-DISK`, the series default doesn't (see [Per-domain quality vocabulary](#per-domain-quality-vocabulary)).
4. **Quality detection (release-name parsing).** How robust is our parser? Sonarr's gets it wrong sometimes; users override individual releases manually. We likely need the same fallback ("mark this release as 1080p Bluray even though we parsed it as 1080p WEB-DL"). Surface in UI.
5. **Score-based selection mode (Radarr-style).** Some users want "format preferred over quality" — a high-scored 720p beats a low-scored 1080p. Default is bin-first; this is an opt-in mode. Add later if demand emerges.
6. **Indexer health and tier interaction.** What if all indexers for the 4K tier are unhealthy? Pause 4K tier searches? Notify admin? Or just keep retrying with back-off? Cross-cutting concern with indexer-health subsystem.
7. **Upgrade anti-flapping delta.** "Better by N score" — what's N? Configurable per profile? Single global? Likely a sane default (e.g., +50 score on a 100-point scale) with opt-in override. Pin in iteration 2.
8. **Tracking-specific scoring overrides.** Could a tracking record override scoring rules for a specific series? "For this anime, prefer x265 +500 even though the global profile prefers x264." Maybe later; iteration 1 says no — profile is the unit.
9. **Decision log retention.** Owned centrally by the [audit pattern](../../patterns/audit/README.md). The trade-off (forever for grabbed, short for rejected) is captured there; nothing for quality-profiles to decide.
10. **Tier mismatch UX (Story 3).** When a user requests 4K but only HD is permitted, do we: (a) hide the 4K option entirely, (b) show it disabled with a "why?", or (c) show it enabled but route to admin approval? This is exactly Story 3's job to resolve — flagging it here as a quality-profile-adjacent question.
11. **Import-time hard-fail boundary.** Which gate failures on the _asserted_ file justify rejecting an already-downloaded release (blocklist + re-search) vs. merely penalizing the score? Resolution below floor is a clear reject; a false Atmos claim is a clear penalize. And `ffprobe` can't assert **source**, so source-based gates can't reject at import — they stay advertised-trusted. Lean: ffprobe-verifiable hard gates can reject; non-verifiable gates can only penalize. Pin the partition + the soft-fail penalty magnitude (vs the upgrade anti-flapping delta) in iteration 2.

## What we're explicitly not deciding here

- Exact table names, columns, indexes
- API endpoint shapes
- Custom format DSL / regex / rule grammar specifics
- The release-name parser implementation (owned by [parsing](../parsing/README.md))
- The reject-vs-penalize partition and soft-fail penalty magnitude for the import-time re-gate (open question)
- Indexer-management UX
- The exact set of preset profiles shipped out of the box
- Cost-of-storage / disk-aware policies (separate subsystem)

Each of those gets its own pass (or its own spec) once this model holds up against more stories.
