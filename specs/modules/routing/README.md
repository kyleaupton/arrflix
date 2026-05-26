# Routing — where a grabbed release goes

**Status:** Draft, iteration 1

This doc defines **routing**: how Arrflix decides, for a release that's been picked for download, which downloader receives it, which library it imports to, which name template applies, and any other post-pick dispatch decisions. It owns the rules engine and its UI surface.

It does **not** decide which release to grab — that's [quality profiles](../quality-profiles/README.md). It also does not own the orchestration that calls into it — that's [acquisition](../acquisition/README.md).

## TL;DR

- Routing answers one question: *given a release we're grabbing, where does it go?*
- Implemented as an ordered list of **rules**. Each rule has **conditions** (predicates over a `RoutingEvaluationContext`) and **actions** (downloader, library, name template, future tags / categories / post-import hooks). First matching rule wins, unless an explicit `continue` action chains.
- Rules evaluate against a `RoutingEvaluationContext`: the picked release + the target media item + (eventually) the quality-profile decision.
- Every evaluation writes an audit row per the [decision-artifact pattern](../../patterns/audit/README.md). One row per `download_job`, capturing the firing rule and its action set.
- v0's `policy` engine renames to `routing` throughout — package, tables, API, UI. Data is preserved through the rename.
- Routing's `stop_processing` action (v0) is deprecated: gating belongs to quality profiles, not routing. The action stays for now to avoid breaking existing configs; it'll be removed in a later iteration.

## What routing is, and isn't

Routing is **deterministic dispatch**. Given a release object and the system's configuration, it produces one of:

- `{downloader, library, name_template, ...action fields}` — the action set the caller applies.
- `(empty)` — no rule matched. Caller (acquisition / interactive grab) decides what to do; typically falls back to a default action set or surfaces an error.

Routing is **not**:

- Release picking — that's [quality profiles](../quality-profiles/README.md).
- The orchestration that triggers it — that's [acquisition](../acquisition/README.md).
- Hardlink / rename mechanics — that's import + name templates (the template *name* is chosen here; the template *content* and *application* live with import).
- Downloader management — connections, health, limits live in the downloaders spec (pending).
- Library management — root paths and storage live in the libraries spec (pending).

## The rules model

A routing config is an ordered list of rules. Each rule has three parts:

```
rule {
  name: "4K → big-disk downloader"
  conditions: [
    media.type == "movie",
    release.quality_bin matches "2160p*",
    release.size_gb > 30,
  ]
  actions: {
    downloader: "qbit-bigdisk"
    library: "movies-4k"
    name_template: "movies-default"
  }
}
```

**Conditions** are predicates over the `RoutingEvaluationContext` (see below). All conditions in a rule are AND'd; OR is expressed by writing multiple rules.

**Actions** populate the action set. Required actions in v1: `downloader`, `library`, `name_template`. Optional / future: tags, categories, post-import hooks, priority hints.

**Evaluation:** rules are evaluated top-down. The first rule whose conditions all match fires; its actions are returned, and evaluation stops. Order matters — admin orders rules from most-specific to most-general; the last rule is typically a catch-all default.

### Why ordered rules, not a scored decision

Routing decisions are small in number (handful of downloaders, handful of libraries, a few name templates) and admin-authored. Admins reason about routing as "if X then Y, else if Z then W, else default." That mental model maps cleanly to ordered rules; it maps poorly to scoring. (Scoring is correct for [quality profiles](../quality-profiles/README.md), which need to rank an open-ended list of releases.)

## RoutingEvaluationContext

The shape rules evaluate against. Builds on v0's `EvaluationContext`:

- **Release** — parsed metadata of the picked release: title, year, quality bin, codec, source, resolution, size, group, indexer, audio info, HDR flag, etc.
- **Media** — target media_item: type (movie/series), tmdb_id, title, year, genres, collection, certification, **path metadata** (existing library this content currently lives in, if any).
- **Quality decision** *(new — iteration 2)* — the quality profile result: chosen profile, quality bin selected, score, custom-format hits. Lets rules route based on *why* it was picked, not just *what* was picked.
- **System** — current time, free space per library, downloader health.

The context is **read-only** to rules. Conditions don't mutate state.

## Actions

The v1 action set:

| Action          | Required | Notes                                                             |
| --------------- | -------- | ----------------------------------------------------------------- |
| `downloader`    | yes      | ID of a configured downloader connection                          |
| `library`       | yes      | ID of a configured library root                                   |
| `name_template` | yes      | ID of a configured name template                                  |
| `continue`      | no       | If true, don't stop; merge this action set with the next match    |

Reserved for future expansion:

- `tags` — apply tags to the resulting `media_item` / `download_job`
- `category` — downloader category (qBit/etc category for organization)
- `post_import_hook` — fire a webhook / run a job after successful import
- `priority` — downloader priority hint

`continue` is the principled escape hatch when admins want to layer rules ("the 4K-specific rule sets the downloader; a general rule sets the name template"). Without it, the entire action set has to come from one rule, which is occasionally awkward. It's strictly additive — fields from a later rule fill in unset fields from an earlier one; conflicts go to the earlier match.

## Decision logging

Every routing evaluation writes an audit row, following the [decision-artifact pattern](../../patterns/audit/README.md).

Routing-specific row shape:

- The `download_job` this routes to (one row per job).
- The matching rule (rule ID + rule name as a denormalized snapshot, so renamed/deleted rules still tell their story).
- The full action set applied.
- For `continue`-chained matches: the list of rules in firing order.
- For no-match: the fallback that was used and why no rule matched.

These rows sit alongside acquisition's quality decisions in the audit table family — the [Activity view](../../patterns/audit/README.md#the-activity-view) shows both for a given grab in one timeline.

**Retention** is configured centrally in the audit spec.

## stop_processing — deprecated

v0's routing engine has a `stop_processing` action: a rule can short-circuit and reject a release outright. In the new world, **gating is the quality profile's job**, not routing's. By the time routing sees a release, the quality engine has already approved it; rejecting at routing is muddled responsibility.

The action stays in the engine for now to avoid breaking existing configs, but:

- The admin UI flags rules that use `stop_processing` with a "deprecated — express this in your quality profile" warning.
- Documentation steers new users to quality-profile gates.
- A later iteration removes the action entirely after existing configs are migrated.

## Migration from v0 (renames)

v0 calls this whole subsystem `policy`. That collides with the conversational use of "policy" for quality, approval, user, and other rule systems. **We rename everything to `routing`.** Data is preserved through the rename.

| Old                         | New                                | Notes                                        |
| --------------------------- | ---------------------------------- | -------------------------------------------- |
| `policy` (table)            | `routing_rule_set`                 | The named set of rules                       |
| `policy_rule` (table, if present) | `routing_rule`               | Individual rule rows                         |
| `policy.Engine`             | `routing.Engine`                   | Package + type rename                        |
| `PoliciesService`           | `RoutingService`                   | Service rename                               |
| `/api/v1/policies/*`        | `/api/v1/routing/*`                | Breaking; user is fine with breaking changes |
| `EvaluationContext`/`Trace` | `RoutingEvaluationContext` / `RoutingEvaluation` | Type rename for clarity        |
| `policy.evaluate` (dry-run) | `routing/evaluate`                 | Endpoint rename                              |
| Admin nav: "Policies"       | "Routing"                          | UI label                                     |

Exact table/column shapes (especially around `routing_rule` vs `routing_rule_set`) are pinned in iteration 2; the rename above is directional.

## Interactions

| Neighbor                                              | How routing interacts                                                                                                |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| **[Acquisition](../acquisition/README.md)**           | Acquisition calls routing once a release is picked. Routing has no opinion on when this happens or by what trigger.   |
| **[Quality profiles](../quality-profiles/README.md)** | Quality picks; routing dispatches. The quality decision may feed into the `RoutingEvaluationContext` (iteration 2).   |
| **Downloaders (pending spec)**                        | Routing references downloaders by ID. A rule whose downloader is unhealthy or deleted is a validation error at config time and a logged routing failure at evaluation time. |
| **Libraries (pending spec)**                          | Same shape: routing references libraries by ID; missing/offline libraries surface at evaluation.                     |
| **Name templates (existing)**                         | Routing picks the template; templates apply it during import.                                                        |
| **[Audit pattern](../../patterns/audit/README.md)**   | Every evaluation writes a row. Retention + Activity view are owned there.                                            |
| **Import (existing)**                                 | Consumes the action set indirectly (via `download_job.library_id`, `name_template_id` columns set at routing time).   |

## Open questions

1. **`stop_processing` deprecation timeline.** Warn now, remove in v2? Or remove sooner since v0 has effectively one user? Lean: warn for one release, then remove.
2. **Quality decision in context.** Adding the quality decision to `RoutingEvaluationContext` is a strict expansion. When? Probably whenever the first rule wants to route based on "what custom format hit." Pin in iteration 2.
3. **Default rule set on first install.** Ship with a single "default" rule (matches everything, routes to the first downloader + first library + a sensible name template)? Or require admins to author rules during setup? Lean: ship a default that's overridable; new installs are usable out of the box.
4. **Per-user routing.** Should a user's request influence routing? "Movies requested by kids go to the kids library." Maybe — but it's expressible today by including user metadata in the context, no engine change needed. Worth noting in [users/permissions/approval](#) when that spec exists.
5. **Routing for upgrades.** When an [upgrade](../tracking/README.md) replaces an existing file, does it re-run routing? Probably yes — it's a new release, the rules apply fresh. The destination library should typically match the original (validated as a config-level sanity check or a hygiene rule).
6. **Validation at config time.** Should we reject a rule whose downloader/library/template IDs don't resolve, or fail at evaluation? Lean reject at save time (better UX); fall back to the catch-all rule at evaluation if a referenced thing goes missing post-save.
7. **Dry-run UI.** The `evaluate` endpoint already supports dry-run. UI should expose "test this release against the rules" — paste a release name + media item, see which rule fires. Pin in the UI iteration.
8. **Action expansion criteria.** Tags, categories, post-import hooks are noted as future. What's the trigger for promoting one to v1? Probably real user demand; the engine is action-set-shaped already, so adding an action is small.

## What we're explicitly not deciding here

- Exact table names, columns, indexes for `routing_rule_set` / `routing_rule`
- API endpoint request/response shapes
- The condition-expression syntax (operators, types, error handling) — pin in iteration 2
- The exact set of conditions the v1 UI exposes (vs raw expression input)
- Migration data backfill plan when the rename lands
- The downloaders / libraries / name templates models themselves (own specs)

## Doc neighbors

- [Acquisition](../acquisition/README.md) — the orchestration layer that calls routing
- [Quality profiles](../quality-profiles/README.md) — picks the release that routing dispatches
- [Audit pattern](../../patterns/audit/README.md) — the decision-artifact pattern routing writes into
- [Errors](../../patterns/errors/README.md) — typed error model
