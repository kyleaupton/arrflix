# Sources — metadata providers, their roles, and how we depend on them

**Status:** Draft, iteration 1

This doc defines **provider strategy**: which external systems Arrflix pulls metadata from, what *role* each plays, what each *costs the user to enable*, and how we stay independent of any one of them without paying to run several. It is the home for a concern that was diffused across [metadata](../metadata/README.md), [parsing](../parsing/README.md), and [matching](../matching/README.md) without an owner — the same "consumers, no owner" shape that earned [parsing](../parsing/README.md) and [files](../files/README.md) their modules.

The split with its closest neighbor is sharp and load-bearing:

- **[metadata](../metadata/README.md) owns the *data model*** — what we persist and keep fresh (the canonical item/season/episode tree, the `external_id` registry, raw payloads, overrides, `series_type` as an attribute, the refresh *engine*).
- **This doc owns the *provider strategy*** — the role seam each provider implements, which provider plays which role, the cost-to-enable taxonomy, the bundled-key approach, TVDB's deferral, the anime data-file-vs-API mechanics, and the portability principle. The provider *operations* the refresh engine dispatches are implemented against this doc's seams.

It does **not** pin exact interface signatures, config schemas, or wire formats. Those come after the model survives more user stories, as with the other specs.

## TL;DR

- **Classify providers by what they *cost the user to enable*, not by what they are.** Three tiers: **invisible** (keyless, strictly improves → never a setting), **baseline** (one bundled credential, on by default), **opt-in** (per-user credential, subscription, or money → surfaced by *outcome*). The rule: **a free improvement is never a setting.**
- **One canonical writer, always.** [TMDB](#the-provider-catalog) owns the writes to `media_item` columns in v1. A single canonical writer is what *prevents* the per-field reconciliation fork — two canonical writers create it. Other providers are either **replacements** (insurance) or **scoped augmenters** (one operation), never a second canonical writer.
- **Portability insurance ≠ concurrency.** The ability to *replace* TMDB if it dies is cheap and mostly already built — a clean internal domain type plus **raw-payload retention** ([`media_metadata_source`](../metadata/README.md#the-metadata-source-pattern)). Running a *second* provider concurrently is an expensive feature, deferred. Rug-pull anxiety is answered by the former; don't pay for the latter to buy insurance.
- **Roles, not one fat provider.** A provider implements a *subset* of role seams — `MetadataSource` (canonical), `StructureSource`, `ImageComposer`, `Searcher`, `DiscoverySource`. Extract a seam **on the second consumer**, not speculatively. v1 has one real seam worth taking: image-URL composition.
- **IMDb is an ID namespace, not a provider.** There is no free public IMDb API. `imdb_id` (and `tvdb_id`) live in the [`external_id` registry](../metadata/README.md#identity), arriving via TMDB's `external_ids` endpoint. No IMDb adapter, ever.
- **TMDB needs no setup step.** Ship a **bundled API key** (the Overseerr/Jellyseerr pattern); let power users **bring their own** to escape the shared rate limit. The default onboarding has no required provider field.
- **TVDB is deferred past v2.** Its headline advantage — absolute/alternate episode ordering — is overwhelmingly an *anime* concern, which the [anime path](#anime-embedded-data-vs-api) covers for free. The residual (deep-catalog completeness) serves a small, pay-willing enthusiast slice → clean opt-in, never a default-path concern.
- **Anime needs no provider and no key for the load-bearing part.** Numbering/ID mapping ships as a **bundled data file** (a `NumberingMap`, not a provider). Anime metadata is an *optional* keyless API ([AniList](#the-provider-catalog)). [AniDB](#open-questions) is avoided as a runtime dependency.
- **The only ToS risk that's concrete is one we control:** TMDB's free tier is non-commercial; the commercial clause activates if *Arrflix* monetizes, not on TMDB's whim.

## Classify by cost-to-enable

The thing that decides a provider's product surface is not its role — it's what the user has to do to turn it on.

| Cost to enable | Treatment | Examples |
| -------------- | --------- | -------- |
| **Invisible** — keyless API or bundled data file, strictly improves data | No setting at all. Just on. | AniList enrichment, the anime `NumberingMap` |
| **Baseline** — one bundled credential, ships with the app | On by default, invisible, with a BYOK escape hatch | TMDB via a bundled key |
| **Opt-in** — per-user credential, subscription, money, or rate-limit risk | Surfaced by the *outcome*, not the provider name | TheTVDB (user-supplied PIN), bring-your-own TMDB key |

The rule that falls out: **a provider earns a toggle only when enabling it costs the user something. A free improvement is never a setting** — if AniList needs no key and strictly improves anime data, there is no "enable AniList" control; the system just uses it.

### Why there are multiple providers at all

Four distinct reasons, each with a *different* treatment — which is why a single "Providers" settings matrix would be the wrong UI:

1. **Coverage/quality** — same kind of data, one source is better for a slice (TVDB for TV structure). → invisible per-content-type preference, when present.
2. **Domain specialization** — a content class needs a specialist (anime). → per-series routing via [`series_type`](../metadata/README.md#item-level-metadata), no global switch.
3. **Augmentation** — additive data the primary lacks (Fanart artwork, alternate ratings). → invisible if keyless, opt-in if credentialed.
4. **Resilience** — escape the shared-key fate (your own TMDB key) or replace a dead provider. → escape hatch / insurance, not a gate.

## Roles, not one provider

Metadata sources sit behind **role-segregated seams**, each a small capability a provider may or may not implement:

| Seam | Capability | v1 implementer |
| ---- | ---------- | -------------- |
| `MetadataSource` (canonical) | Lookup by id → raw payload + normalized item/episode fields | **TMDB** |
| `StructureSource` | Full season/episode tree incl. unaired, with ordering | TMDB (TVDB seam exists, **no impl** till >v2) |
| `Searcher` | Title (+type/year) → candidate matches | TMDB |
| `DiscoverySource` | Trending, similar/related | TMDB |
| `ImageComposer` | Provider-relative path + size → fetchable URL | TMDB |

This is interface *segregation* on purpose. A fat single `MetadataProvider` forces a `capabilities()` runtime negotiation for what subset-interfaces express at compile time — and bundles a TMDB-shaped capability like `DiscoverySource` (trending) into the contract a structure-only source (TVDB) or a discovery-only source (Trakt) can't or shouldn't honor.

**Discipline: extract a seam on the second consumer, not speculatively.** The repo's house rule ([files OQ#5](../files/README.md#open-questions), the service-layer convention in [CLAUDE.md]) is "do not pre-abstract." For v1 the one seam that *demonstrably* varies — and is therefore worth taking now — is **`ImageComposer`** (a custom-artwork override composes against `/uploads/`, TMDB against its CDN; same call site, different rule). The rest stays concrete `TmdbProvider` code until a *second* implementer (realistically TVDB `StructureSource`) tells us what actually varies. Designing the other seams against exactly one provider would guess wrong.

### One canonical writer = no fork

Exactly one provider is **canonical**: it owns the writes to [`media_item`](../metadata/README.md#item-level-metadata) columns. This is not a limitation to work around — it is the mechanism that **prevents the per-field reconciliation fork**. "Which source wins on conflict" only exists when two sources both write the same column. With one canonical writer there is nothing to reconcile.

Other providers, when they arrive, enter in one of two *non-forking* roles:

- **Replacement** — never runs concurrently with the canonical, so no fork by definition. This is the insurance path (below).
- **Scoped augmenter** — owns one narrow operation (e.g. TVDB as `StructureSource` for *series episode trees only*), blended invisibly at a single call site. One interface, two implementations, one site = a seam, not a fork threaded through the app.

A future multi-canonical mode (per-field precedence, per-field overrides) is deferred; it needs a precedence config and a richer extraction layer, neither of which any v1/v2 feature requires.

## The provider catalog

The v1/v2 reality, mapped onto role, cost, and status:

| Provider | Role | Cost to enable | Status |
| -------- | ---- | -------------- | ------ |
| **TMDB** | Canonical `MetadataSource` + `Searcher` + `DiscoverySource` + `ImageComposer` | Baseline — **bundled key**, BYOK escape | v1 |
| **AniList** | `MetadataSource` augment, scoped to `series_type = anime` | Invisible — keyless | v1/v2 with anime |
| **anime `NumberingMap`** | *Not a provider* — a bundled ID/numbering data file | Invisible — shipped data | v1/v2 with anime |
| **TheTVDB** | `StructureSource` for series, scoped | Opt-in — user-supplied subscription PIN | **deferred >v2** |
| **IMDb** | *Not a provider* — an [ID namespace](#identity-id-namespaces-vs-providers) | — (consumed via TMDB) | v1 (as a stored id) |
| **Fanart.tv** | `ImageComposer` augment | Opt-in — API key | deferred |

The shape worth noticing: **only TheTVDB has billing teeth, and it is already opt-in by design.** Everything else is free. The "diverse billing implications" worry collapses to *one free primary + free augmentation + one optional-paid enhancement*.

### TMDB — the baseline, bundled

TMDB requires an API key, but the user need never see that. Ship a **bundled key** (Overseerr and Jellyseerr both do this at far larger scale than Arrflix will hit early) so the default path has **no required provider step**. Make **bring-your-own-key** the escape hatch for users who want to dodge the shared-key rate limit or insulate against a key revocation. Honest risks of bundling — aggregate rate limits, a revoked key bricking the fleet, attribution terms — are real but survivable, and BYOK is the pressure valve.

### TheTVDB — opt-in, deferred past v2

TheTVDB v4 offers a project two mutually-exclusive access paths: a **licensed/negotiated contract** (Arrflix pays usage-scaled fees; the key is invisible to users) or **user-supported** (each end user buys a TheTVDB subscription and supplies a **PIN** alongside the project key). For an OSS self-hosted project, user-supported is the realistic default — which makes TVDB an honest opt-in enhancement framed by outcome ("better episode data for your TV shows — connect a TheTVDB subscription"), surfaced at the moment of friction.

It is deferred past v2 deliberately: TVDB's structural advantage is multiple episode **orders** (aired / DVD / absolute), and the order that actually matters — **absolute, for anime** — is delivered for free by the [anime path](#anime-embedded-data-vs-api) without TVDB. The residual benefit (deep-catalog/obscure-TV completeness) serves a small, pay-willing slice. When it lands it is a single `StructureSource` operation, never a default-path concern. Note too that TVDB's absolute order is itself unstable (frequent reordering, specials interjection); our [stable-internal-id + renumber-survival design](../metadata/README.md#handling-renumbering) neutralizes that *provider-agnostically*, which is the higher-value work and is already specced.

### Identity (ID namespaces vs providers)

There is **no free public IMDb REST API**: IMDb's official API is enterprise AWS Data Exchange; its bulk datasets are thin and non-commercial; third-party wrappers (OMDb, scrapers) are too fragile to anchor a platform on. So IMDb cannot play a provider role. What it *is* is the ecosystem's universal cross-reference — `imdb_id` is the id every indexer keys on.

That role is already filled: `imdb_id` and `tvdb_id` are **ID namespaces** living in the [`external_id` registry](../metadata/README.md#identity), populated from TMDB's `external_ids` endpoint as part of [series sync](../metadata/README.md#series-structure-sync-the-foundation-gap). **Being an ID namespace is not the same as being a provider.** No IMDb adapter is ever written; the registry is the whole of IMDb's presence in the system. [Acquisition](../acquisition/README.md) reads `(media_item, source: tvdb)` from the registry at indexer-search time — a single SELECT, no provider call.

## Portability insurance vs concurrency

Two very different purchases, routinely conflated because both are called "abstraction":

| | **Portability insurance** | **Concurrent multi-provider** |
| --- | --- | --- |
| Buys | Ability to *replace* the canonical provider if it dies | Running a second provider *alongside*, blending data |
| Cost | Cheap, one-time, mostly already built | Expensive — per-field precedence, two sync paths (the fork) |
| Paid | Only *if* the rug is pulled | Continuously |
| Needs a 2nd running provider? | **No** | Yes |

Rug-pull risk is fully answered by the **left column alone**, and the left column is two things we already have: a **provider-agnostic internal domain type** (consumers see the canonical `metadata.Item` / episode shape, never a provider's native shape — the seam [matching](../matching/README.md) already honors) plus **raw-payload retention** in [`media_metadata_source`](../metadata/README.md#the-metadata-source-pattern). Together they make "replace TMDB" a *write-one-new-adapter* job, not a rewrite. Raw-payload retention is therefore not merely forward-compat/debugging insurance — it is **the portability mechanism**, and the invariant "always store the raw payload" is load-bearing for it.

Crucially, the TMDB rug-pull risk is **correlated, not idiosyncratic**: if TMDB changes terms, the entire *arr/seerr vertical is hit at once, so (a) it is less likely (TMDB torches its reason to exist) and (b) the response would be an ecosystem-wide migration we ride along with. The correct hedge for "no real alternative + correlated risk" is to make *replacement cheap*, not to build concurrency speculatively.

## The refresh engine

The thing that keeps the metadata model fresh is **not** a "series syncer" — it is a generic **refresh engine**, and movies vs series are two instances of one policy (the two cadence tables in [metadata](../metadata/README.md#sync-cadence--series) prove it). It decomposes into two cleanly separated halves:

1. **A scheduler — provider-agnostic policy.** Its only job is "what's due?": a function `(entity_type, state) → TTL`, a due-queue, and a set of enqueuers ([the trigger sources](../metadata/README.md#trigger-sources): background sweep, manual, post-import, tracking-activation, pre-air-aired). The two per-entity cadence tables collapse into one `(entity_type, state) → interval` policy; adding anime, collections, or a new entity type later is *data*, not a new worker. This half has zero provider code.
2. **Sync operations — the only provider-specific part.** A dispatched unit of work: *fetch raw → normalize at the boundary → upsert structural rows → store raw payload*. This is where the `MetadataSource` / `StructureSource` seams live. The worker is a dumb queue-drainer dispatching `(entity, provider) → operation`.

The engine **conforms to existing patterns rather than re-specifying them**: the due-queue is a [work-dispatch](../../patterns/work-dispatch/README.md) table (claim with `FOR UPDATE SKIP LOCKED`, `LISTEN/NOTIFY` wake-up hint + fallback ticker), and provider reachability (rate-limited, unreachable) is a [connectivity-health](../../patterns/connectivity-health/README.md) resource — `rate_limited` is in that pattern's example status set, and back-off on 429 is the connectivity-health contract, not a bespoke mechanism here.

Normalization happens **at the provider boundary**: TMDB's `status` strings → the canonical [status enum](../metadata/README.md#canonical-status), ISO date strings → typed dates, provider genre arrays → canonical genres. A future provider's different vocabulary is translated inside *its* operation; consumer code never sees a native shape.

### Reconciling with the response cache

The refresh engine sits *above* the provider-layer response cache — the TTL-keyed store behind the TMDB service (its `STATIC_TTL` / `DYNAMIC_TTL` notches). That cache and the durable `media_item` + `media_metadata_source` tree are **two caches of the same upstream bytes**, each with its own freshness clock. If a scheduled sync reads *through* the response cache, the engine's cadence is silently defeated — a "daily" re-check handed a week-old body — while `metadata_updated_at` is stamped fresh anyway, violating [metadata's freshness invariant](../metadata/README.md#freshness-invariant).

The rule is **one freshness decision-maker per read path**:

- **Canonical-materializing reads** — enrichment, structure sync, born-at-spawn, manual refresh — are reads the engine has *already decided* are worth spending; the cache must not second-guess them. They **consult upstream directly, bypassing the response cache on read.** (Writing the fresh body back through is optional and buys little: under a uniform bypass it could only ever serve a *browse* read, and canonical fetches are keyed separately from the render/search fetches, so the two rarely share an entry.) Mechanically, a `fresh` variant of the service's `getOrFetchFromCache` that skips the read but keeps the write; once the [role seams](#roles-not-one-provider) are extracted, the `MetadataSource` / `StructureSource` operations simply always fetch fresh.
- **Ephemeral render reads** — search proxy, non-adopted focus pages, discover/trending, watch providers — have no canonical copy, are hammered per-render, and tolerate hours-to-a-day of staleness. This is where the response cache earns its keep, unchanged.

**The response cache stays dumb.** Making *its* TTL state-aware (so it "knows" an airing show) is rejected: it is keyed on URL-shaped strings and cannot see entity state without reaching into the entity layer — which would implement the cadence policy twice, in two layers that drift. All state-awareness concentrates in the engine. (`STATIC_TTL` / `DYNAMIC_TTL` were that same instinct born at the only layer that then existed; the engine is it moved to the layer that can see entity state.)

The cache's role therefore **shrinks rather than disappears**: from a freshness mechanism for anything canonical to purely the **fan-out shield for browse/search**. The two budgets are distinct and non-substitutable — *canonical* calls are bounded by `adopted items × cadence × fetch granularity` and minimized by the engine; *browse* calls are unbounded and repetition-heavy and minimized by the cache. Neither layer can do the other's job.

### Fetch granularity

The engine decides **what to fetch, not only when** — because a series refresh is not one call. Structure sync fans out to **one provider call per season**, and most seasons are immutable (an episode that aired years ago will not change). Fetching every season on every cadence tick re-pulls that frozen back-catalog repeatedly — the exact waste the response cache existed to prevent — and it lands hardest on the worst case: a long-running show that is *still airing* (a decades-long anime, a daily soap) is simultaneously the most-refreshed and the most-seasoned. It also inflates **born-at-spawn latency**, since that synchronous path runs the same per-season fan-out while the user waits.

So the engine fetches only what can have changed: the **series-level record + the current/most-recent season + any season carrying unaired episodes**; **ended seasons are skipped** (the engine holds the tree and air dates, so it knows which seasons are frozen). This preserves the daily-airing correctness win — new episodes and slipped air dates live in the current/future seasons and the series-level status, all kept fresh — while bounding both call volume and add latency. Fetch granularity is a *scheduler* concern (part of "what's due, and what does servicing it entail"), not a provider one: a provider operation still services whatever slice the engine hands it.

## Anime (embedded data vs API)

Anime is a [`series_type`](../metadata/README.md#item-level-metadata) classification, not a media type or a global toggle. Its provider footprint is deliberately tiny and mostly *not* a provider at all:

| Piece | Mechanism | Key? | Role |
| ----- | --------- | ---- | ---- |
| **ID/numbering mapping** (Fribb anime-lists / manami AOD) — *load-bearing* | **Bundled or periodically-fetched JSON data file** | None | `NumberingMap` (data, not a provider) |
| **Anime metadata** (AniList) — optional | Keyless GraphQL **API call** at runtime | None | `MetadataSource` augment |
| AniDB | API, but registration + brutal rate limits | Yes | *Avoided at runtime; consumed pre-baked into the mapping files* |

So anime = **one embedded data file (critical) + one optional keyless API (nice-to-have)** — no key, no billing, no rug-pull surface. The three concepts split cleanly across docs:

- **This doc** owns the `NumberingMap` (the data file: provenance, refresh, shape).
- **[metadata](../metadata/README.md#item-level-metadata)** owns `series_type` (the per-series column + auto-detection from presence in the mapping) and the [`absolute_number`](../metadata/README.md#open-questions) field it populates.
- **[matching](../matching/README.md#the-resolver-catalog)** owns the `episode-numbering` resolver that reconciles [parsing](../parsing/README.md)'s release-side numbering-namespace tag against canonical numbering, using the `NumberingMap` plus resolved series identity.

A series is classified `anime` automatically when its TMDB/TVDB id is present in the `NumberingMap`; the only UI surface is a per-series **Series Type** override for the rare miss (the proven Sonarr model). No anime toggle, no key, no setup step.

## Setup is provider-driven

The onboarding flow asks only for configuration declared by the *active* providers — and in the default configuration that list is **empty**, because TMDB ships with a bundled key and every other v1 source is keyless. BYOK and any opt-in provider (a TheTVDB PIN, a Fanart key) are surfaced later, in settings, framed by outcome — never as a required first-run gate. The setup UI is generic; the requirement list comes from the providers. This is a simplification of any flow that currently hardcodes "ask for a TMDB key."

## Commercial use — the one ToS risk we control

TMDB's API is free for **non-commercial** use with attribution (an "About"/credits notice, the "not endorsed or certified by TMDB" line, and the logo). Self-hosted open-source Arrflix is squarely non-commercial — the same footing Overseerr/Jellyseerr operate on. The commercial clause (written agreement, possible fees) activates only if **Arrflix itself monetizes** — a paid hosted tier, paid support. That makes the most concrete ToS risk a roadmap decision *we* control, not a sword TMDB holds. Flagged so a future paid offering doesn't ship before the TMDB commercial license is sorted.

## Interactions

| Neighbor | How sources interacts |
| -------- | --------------------- |
| **[Metadata](../metadata/README.md)** | Owns the data model these providers write into; this doc owns the seam they write *through*. The refresh engine keeps metadata's tree fresh; raw payloads are the portability mechanism. |
| **[Parsing](../parsing/README.md)** | Provides the release-side numbering-namespace tag the `NumberingMap` + `episode-numbering` resolver consume. Never calls a provider. |
| **[Matching](../matching/README.md)** | Consumes the provider-agnostic domain type and the `external_id` registry; owns the `episode-numbering` resolver the anime `NumberingMap` feeds. The "metadata layer interface" it references is these role seams. |
| **[Acquisition](../acquisition/README.md)** | Reads `external_id` (`tvdb` for indexer search) — a registry SELECT, not a provider call. |
| **[Tracking](../tracking/README.md)** | Consumes the synced episode tree provider-agnostically; unaffected by which provider produced it. |
| **[Connectivity-health](../../patterns/connectivity-health/README.md)** | Provider reachability/rate-limit state conforms to this pattern (`rate_limited`, `unreachable`); back-off lives there. |
| **[Work-dispatch](../../patterns/work-dispatch/README.md)** | The refresh engine's due-queue is a work-dispatch table (claim + notify-hint + fallback ticker). |
| **[Users](../users/README.md)** | Provider config (BYOK key, TheTVDB PIN) is admin settings; changes are admin-action-audit events. |

## Open questions

1. **`NumberingMap` refresh & provenance.** Ship the mapping file baked into the build, fetch periodically, or both (baked as a floor, fetched to stay current)? Lean: bundled-as-floor + periodic fetch, conforming to connectivity-health for the fetch liveness. Pin when the anime path is built.
2. **AniDB as a mapping *input*.** The mapping files derive partly from AniDB; if we ever need a field only AniDB exposes, do we take its rate-limited API as a build-time/offline input only (never a runtime dependency)? Lean: offline input only. Pin with anime.
3. **`StructureSource` interface shape.** Deliberately not designed until TVDB (the second implementer) lands — designing it against TMDB alone guesses wrong. Documented as a reserved seam, not a built one.
4. **Per-content-type source preference storage.** When TVDB-for-series-structure arrives, where does "prefer TVDB structure for this series" live — a column, a per-series setting, an install default? Pin when TVDB is real (>v2).
5. **Bundled-key operational policy.** Rotation, abuse monitoring, and the threshold at which we'd nudge users toward BYOK. Operational, not architectural; pin before public release.
6. **Discovery as a separate provider.** `DiscoverySource` (trending/similar) is bundled into TMDB today but is the seam most likely to want a *different* best-in-class source (Trakt) later. Keep it a distinct role now so that swap is an extension, not a re-cut. Pin if/when a discovery-specialist is wanted.
7. **Canonical status enum vocabulary — decided.** Locked set: `upcoming` · `released` · `continuing` · `ended` · `canceled` · `unknown`, with TMDB mapped down at the provider boundary (`model.CanonicalizeStatus`). Full mapping table and rationale in [metadata's Canonical status](../metadata/README.md#canonical-status); the refresh engine's `(state) → TTL` cadence keys on these canonical states.

## What we're explicitly not deciding here

- Exact interface signatures for the role seams (extracted on the second implementer).
- The `media_item` / `external_id` / `media_metadata_source` schemas — owned by [metadata](../metadata/README.md).
- The refresh engine's exact queue/scheduler implementation — conforms to [work-dispatch](../../patterns/work-dispatch/README.md); shape pinned with metadata.
- The `episode-numbering` resolver — owned by [matching](../matching/README.md).
- Running a second live provider (TVDB concurrency) — deferred >v2; the seam is reserved, the implementation is not.
- Per-field precedence across multiple canonical sources — deferred indefinitely.
- The setup wizard's exact UI — only its provider-driven, no-required-step *shape* is decided here.

## Doc neighbors

- [Metadata](../metadata/README.md) — the data model providers write into; owns `series_type`, `external_id`, raw payloads, the refresh engine's data shape.
- [Matching](../matching/README.md) — consumes the provider-agnostic domain type; owns the `episode-numbering` resolver.
- [Parsing](../parsing/README.md) — supplies the numbering-namespace tag the anime path consumes.
- [Acquisition](../acquisition/README.md) — reads `external_id` for indexer search.
- [Connectivity-health](../../patterns/connectivity-health/README.md) — the contract provider reachability conforms to.
- [Work-dispatch](../../patterns/work-dispatch/README.md) — the contract the refresh due-queue conforms to.
- [Errors](../../patterns/errors/README.md) — upstream provider failures use `KindBadGateway`.
