---
status: as-built
---

# Routing — where a grabbed release goes

This doc defines **routing**: how Arrflix decides, for a release that's been picked for download, which downloader receives it, which library it imports to, which name template applies, and any other post-pick dispatch decisions. It owns the dispatch rules and their UI surface. The condition machinery is not routing's: conditions are trees over the shared **Subject**, evaluated by the [rules substrate](../../patterns/rules/README.md) at the `grab` moment, so this spec defines no context shape or expression syntax of its own.

> **Beyond what's built:** §Decision logging is unbuilt (the evaluation is returned by dry-run/preview, not persisted), `want.*` conditions are registered but no grab path assembles them, and `decision.*` is reserved only.

It does **not** decide which release to grab — that's [quality profiles](../quality-profiles/README.md). It also does not own the orchestration that calls into it — that's [acquisition](../acquisition/README.md).

## TL;DR

- Routing answers one question: *given a release we're grabbing, where does it go?*
- Implemented as an ordered list of **rules**. Each rule has **conditions** (a [rule-substrate](../../patterns/rules/README.md) tree over the shared Subject) and **actions** (downloader, library, name template, future tags / categories / post-import hooks). First matching rule wins, unless an explicit `continue` action chains.
- Rules evaluate the **[Subject](../../patterns/rules/README.md#the-subject) at the `grab` moment**: the picked release + matched media, plus (unassembled) the want's intent facts (`want.trigger`, `want.requesters`, `want.tier`) and (reserved) the quality decision (`decision.*`). `mediainfo.*` is `import`-phase and therefore indeterminate here — routing never sees it.
- Every evaluation is designed to write an audit row per the [decision-artifact pattern](../../patterns/audit/README.md) — one row per `download_job`, capturing the firing rule and its action set (unbuilt).
- Any slot no rule fills falls back to the configured default (downloader, library, name template), so an empty rule list still dispatches every grab.
- There is no `stop_processing` action: gating belongs to quality profiles, not routing. The action set is exactly `{downloader, library, name_template, continue}`.

## What routing is, and isn't

Routing is **deterministic dispatch**. Given a release object and the system's configuration, it produces one of:

- `{downloader, library, name_template}` — the action set the caller applies. The pure `routing.Dispatch` may leave slots unset (no rule matched, or a matched rule's target was deleted); `RoutingService.Dispatch` fills each unset slot from the configured default (the `torrent` default downloader; the per-media-type default library and name template) and records which slots fell back. A missing default surfaces as an error.

Routing is **not**:

- Release picking — that's [quality profiles](../quality-profiles/README.md).
- The orchestration that triggers it — that's [acquisition](../acquisition/README.md).
- Hardlink / rename mechanics — that's import + name templates (the template *name* is chosen here; the template *content* and *application* live with import).
- Downloader management — connections, health, limits live in [downloaders](../downloaders/README.md).
- Library management — root paths and storage live in [libraries](../libraries/README.md).

## The rules model

A routing config is an ordered list of rules. Each rule has three parts:

```
rule {
  name: "4K → big-disk downloader"
  conditions: [
    media.type == "movie",
    quality.resolution == "2160p",
    candidate.size_gb > 30,
  ]
  actions: {
    downloader: "qbit-bigdisk"
    library: "movies-4k"
    name_template: "movies-default"
  }
}
```

**Conditions** are a [rule-substrate](../../patterns/rules/README.md#conditions-the-typed-operand-model) tree — typed operands, registry-validated at save time. The substrate supports full `and`/`or`/`not` trees; routing's v1 authoring UI presents a rule as an AND'd list (OR is expressed by writing multiple rules), which simply emits a tree whose root is `and`.

**Actions** populate the action set: `downloader`, `library`, `name_template`, each a nullable FK on the `routing_rule` row. Optional / future: tags, categories, post-import hooks, priority hints.

**Evaluation:** rules are evaluated top-down by `position`; disabled rules are skipped. A condition that evaluates `Unknown` (not knowable at `grab`) counts as not-matched. The first rule whose conditions match fires; its actions are returned, and evaluation stops. Order matters — admin orders rules from most-specific to most-general; the last rule is typically a catch-all default.

### Why ordered rules, not a scored decision

Routing decisions are small in number (handful of downloaders, handful of libraries, a few name templates) and admin-authored. Admins reason about routing as "if X then Y, else if Z then W, else default." That mental model maps cleanly to ordered rules; it maps poorly to scoring. (Scoring is correct for [quality profiles](../quality-profiles/README.md), which need to rank an open-ended list of releases.)

## What rules evaluate

Routing rules evaluate the shared **[Subject](../../patterns/rules/README.md#the-subject)** at the **`grab`** moment — this spec defines no context shape of its own. What that makes available, in routing's terms:

- **The release** (`candidate.*`, `identity.*`, `quality.*`, `encode.*`) — indexer metadata + the parse of the picked release.
- **The media** (`media.*`) — the matched media item: type, tmdb id, title, year, …
- **The intent** (`want.*`) — why this acquisition exists: `want.trigger` (`request|rss|upgrade|manual`), `want.requesters` (the tracking's durable requester *set* — "movies requested by kids go to the kids library" is `want.requesters contains <user>`; empty and definitively non-matching for RSS grabs), `want.tier`. Registered but **unassembled**: neither the [acquisition](../acquisition/README.md) worker nor the interactive grab sets `Subject.Want`, so conditions over `want.*` are indeterminate (never match), and the rule editor's field catalog omits them; see the substrate's [population status](../../patterns/rules/README.md#want--durable-intent-facts).
- **The quality decision** (`decision.*`) *(reserved, unbuilt)* — the profile result: chosen profile, bin, score, custom-format hits. A `grab`-phase namespace in the [registry](../../patterns/rules/README.md#the-field-registry); lets rules route on *why* it was picked, not just *what*. Exact field set pinned here when first needed.

Not available, by design:

- **`mediainfo.*`** — `import`-phase; indeterminate at `grab`. Routing decides before the file exists.
- **System state** (free space, downloader health, current time) — **excluded from conditions** per the [substrate's rationale](../../patterns/rules/README.md#what-stays-out-system) (volatile, non-reproducible from the audit trace, and partly keyed by routing's own output). Unhealthy/missing targets are config-time validation + evaluation-time fallback (see [open questions](#open-questions)).

The Subject is **read-only** to rules. Conditions don't mutate state.

## Actions

The action set:

| Action          | Notes                                                                                   |
| --------------- | --------------------------------------------------------------------------------------- |
| `downloader`    | ID of a configured downloader connection; unset → the `torrent` default downloader      |
| `library`       | ID of a configured library root; unset → the media type's default library               |
| `name_template` | ID of a configured name template; unset → the media type's default template             |
| `continue`      | If true, don't stop; merge this action set with the next match                          |

Reserved for future expansion:

- `tags` — apply tags to the resulting `media_item` / `download_job`
- `category` — downloader category (qBit/etc category for organization)
- `post_import_hook` — fire a webhook / run a job after successful import
- `priority` — downloader priority hint

`continue` is the principled escape hatch when admins want to layer rules ("the 4K-specific rule sets the downloader; a general rule sets the name template"). Without it, the entire action set has to come from one rule, which is occasionally awkward. It's strictly additive — fields from a later rule fill in unset fields from an earlier one; conflicts go to the earlier match.

## Decision logging

**Unbuilt.** Today `RoutingService.Dispatch` returns a `routing.Evaluation` (every rule's tri-state result + trace, the fired rule ids, the final action set, and which slots fell back) that the dry-run and preview endpoints return and the grab paths discard. Every routing evaluation is designed to write an audit row, following the [decision-artifact pattern](../../patterns/audit/README.md).

Routing-specific row shape:

- The `download_job` this routes to (one row per job).
- The matching rule (rule ID + rule name as a denormalized snapshot, so renamed/deleted rules still tell their story).
- The full action set applied.
- For `continue`-chained matches: the list of rules in firing order.
- For no-match: the fallback that was used and why no rule matched.

These rows sit alongside acquisition's quality decisions in the audit table family — the [Activity view](../../patterns/audit/README.md#the-activity-view) shows both for a given grab in one timeline.

**Retention** is configured centrally in the audit spec.

## No stop_processing

Routing has no action that rejects a release. **Gating is the quality profile's job**, not routing's — by the time routing sees a release, the quality engine has already approved it; rejecting at routing is muddled responsibility. The action set is exactly `{downloader, library, name_template, continue}`; gating belongs in quality-profile gates.

## Naming and surface

The subsystem is called `routing`, not `policy` — "policy" collides with the conversational use for quality, approval, user, and other rule systems.

| Piece | Name |
| --- | --- |
| Table | `routing_rule` — one global ordered list (`position`, unique and deferrable so a reorder can permute), `enabled`, `conditions` (canonical condition-tree JSONB), nullable `downloader_id` / `library_id` / `name_template_id` (`ON DELETE SET NULL`), `continue`. There is no rule-set table. |
| Pure engine | `internal/routing` — `routing.Dispatch(reg, rules, subject) → (ActionSet, Evaluation)` |
| Service | `RoutingService` — rule CRUD with save-time validation, `Dispatch` with default fallbacks, the editor's field catalog |
| API | `/api/v1/routing/rules` (list, create, get, update, delete), `/api/v1/routing/rules/positions` (reorder), `/api/v1/routing/fields`, `/api/v1/routing/evaluate` (dry-run) |
| Grab preview | `/api/v1/movie/{id}/candidate/preview`, `/api/v1/series/{id}/candidate/preview` return the evaluation for a cached candidate |
| UI | Settings ▸ Routing (`RoutingSettings.vue`), with a dry-run panel |

## Interactions

| Neighbor                                              | How routing interacts                                                                                                |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| **[Acquisition](../acquisition/README.md)**           | Acquisition calls routing once a release is picked. Routing has no opinion on when this happens or by what trigger.   |
| **[Quality profiles](../quality-profiles/README.md)** | Quality picks; routing dispatches. The quality decision is reserved to surface as `decision.*` on the Subject.   |
| **[Downloaders](../downloaders/README.md)**           | Routing references downloaders by ID. A rule naming a nonexistent downloader is a validation error at save time; a downloader deleted after save nulls the slot, which falls back to the default. Health is not consulted. |
| **[Libraries](../libraries/README.md)**               | Same shape: routing references libraries by ID, validated at save, `SET NULL` + default fallback after.            |
| **[Name templates](../name-templates/README.md)**     | Same shape. Routing picks the template; the importer applies it.                                                     |
| **[Audit pattern](../../patterns/audit/README.md)**   | Every evaluation is designed to write a row (unbuilt). Retention + Activity view are owned there.                    |
| **[Importer](../importer/README.md)**                 | Consumes the action set indirectly (via `download_job.library_id`, `name_template_id` columns set at routing time).   |

## Open questions

1. **`stop_processing` deprecation timeline. (Answered: removed.)** No config was carried over, so there was nothing to deprecate gradually — the action doesn't exist; gating is quality's.
2. **`decision.*` field set.** The namespace and its `grab` phase are reserved in the [registry](../../patterns/rules/README.md#decision--reserved); adding it is a strict expansion (a few registry rows). When? Probably whenever the first rule wants to route based on "what custom format hit." Pin the exact fields then.
3. **Default rule set on first install.** Ship with a single "default" rule (matches everything, routes to the first downloader + first library + a sensible name template)? Or require admins to author rules during setup? Lean: ship a default that's overridable; new installs are usable out of the box. — *Answered by the code: no rule ships; unfilled slots fall back to the configured defaults, so an empty rule list dispatches every grab.*
4. **Per-user routing. (Answered.)** Yes — via conditions over **`want.requesters`**, the tracking's durable requester *set* (not the frozen request: there is no singular "requesting user," and upgrades have no request at all). RSS/admin grabs have an empty set and cleanly don't match. Quantifier semantics (any vs all requesters) and placement drift when the set changes later are [substrate open questions](../../patterns/rules/README.md#open-questions). Per-user *quality* stays out of conditions entirely — that's tier → profile selection ([requests](../requests/README.md#tier)).
5. **Routing for upgrades.** When an [upgrade](../tracking/README.md) replaces an existing file, does it re-run routing? Probably yes — it's a new release, the rules apply fresh. Because `want.*` facts are tracking-derived (durable), an upgrade evaluates the same intent conditions as the original grab. The destination library should typically match the original (validated as a config-level sanity check or a hygiene rule).
6. **Validation at config time.** Should we reject a rule whose downloader/library/template IDs don't resolve, or fail at evaluation? Lean reject at save time (better UX); fall back to the catch-all rule at evaluation if a referenced thing goes missing post-save. — *Answered by the code: rejected at save (`Validation`); post-save deletion nulls the slot, which falls back to the configured default, not a catch-all rule.*
7. **Dry-run UI.** The `evaluate` endpoint already supports dry-run. UI should expose "test this release against the rules" — paste a release name + media item, see which rule fires. Pin in the UI iteration. — *Answered by the code: the Routing settings page has a dry-run panel (release title + media type).*
8. **Action expansion criteria.** Tags, categories, post-import hooks are noted as future. What's the trigger for promoting one to v1? Probably real user demand; the engine is action-set-shaped already, so adding an action is small.

## What we're explicitly not deciding here

- Indexes beyond `routing_rule`'s position constraint
- API endpoint request/response shapes
- The condition model (operands, operators, types, evaluation semantics) — owned by the [rules substrate](../../patterns/rules/README.md)
- The exact set of fields the v1 UI foregrounds (vs the full registry catalog)
- The downloaders / libraries / name templates models themselves (own specs)

## Doc neighbors

- [Rules](../../patterns/rules/README.md) — the predicate substrate routing's conditions are built on (Subject, registry, evaluator)
- [Acquisition](../acquisition/README.md) — the orchestration layer that calls routing
- [Quality profiles](../quality-profiles/README.md) — picks the release that routing dispatches
- [Audit pattern](../../patterns/audit/README.md) — the decision-artifact pattern routing writes into
- [Errors](../../patterns/errors/README.md) — typed error model
