# Audit — the system-wide decision-artifact pattern

**Status:** Draft, iteration 1

This doc defines a **cross-cutting pattern**: every consequential automated decision Arrflix makes leaves a row that explains *what was decided* and *why*. The pattern shows up in [acquisition](../../modules/acquisition/README.md) (which release got picked / rejected), [routing](../../modules/routing/README.md) (where a release was dispatched), [matching](../../modules/matching/README.md) (what identity a file resolved to), and [hygiene](../../modules/hygiene/README.md) (what problems were detected).

The producers each own their own table — the shapes diverge — but three things are centralized here: the *principle*, the *retention configuration*, and the *Activity view* that aggregates across producers.

## TL;DR

- **Principle:** every consequential automated decision writes an audit row. Append-only at the producer side. Subsystems own their tables; the pattern is shared.
- **Why separate tables:** producers have genuinely different shapes (per-(want, release) vs per-file-with-supersession vs per-rule-with-lifecycle). Collapsing into one table would either lose information or grow giant JSONB columns. The shared principle is sufficient; shared storage is not.
- **Centralized retention.** One settings page (`System → Retention`) configures TTLs per audit-trail type. No per-subsystem retention config.
- **Activity view.** A unified chronological feed reading across producers. The library has a *story*, not just a state.
- **Common conceptual fields** every audit row has: target, decision/outcome, reason, decider, timestamp. Subsystem-specific fields layer on top.

## Why this is a cross-cutting pattern, not a module

Three subsystems all face the same operational need ("audit my decisions") with the same UX requirements (look at one row, scan a timeline, set retention). If we put that logic in one of the modules, the other two would either duplicate it or awkwardly depend on a peer. If we built a shared library, we'd over-design for code reuse that has minimal payoff (the producers' schemas are too different to share storage code).

The right shape is: **producers share a principle and the operational surfaces; they don't share an implementation.** Same pattern as [errors](../errors/README.md) — typed errors are a contract every layer honors; there's no "errors service."

## The shape

Every audit row has, conceptually:

| Field          | Meaning                                                                  |
| -------------- | ------------------------------------------------------------------------ |
| `timestamp`    | When the decision was made                                               |
| `target`       | What the decision was about (media item / file / want / etc.)            |
| `outcome`      | What was decided (`grabbed`, `confident_match`, `detected`, ...)         |
| `reason`       | Structured + human-readable explanation                                  |
| `decider`      | `auto` / `user:<id>` / `rule:<id>`                                       |
| Subsystem fields | Whatever else that producer needs                                      |

The subsystem-specific fields are what differentiate. They diverge because the producers answer different questions:

| Producer                                      | Question answered                                          | Distinctive fields                                                       |
| --------------------------------------------- | ---------------------------------------------------------- | ------------------------------------------------------------------------ |
| [Acquisition](../../modules/acquisition/README.md) | "Every release we looked at, what we did with it, why" | `(want, search_run, release)`; score; gate trace; runner-up vs rejected   |
| [Routing](../../modules/routing/README.md)    | "Where did this grabbed release go and per which rule"     | `download_job`; firing rule (denormalized snapshot); applied action set   |
| [Matching](../../modules/matching/README.md)  | "The journey of identifying this file"                     | Per-file with supersession chain; resolver evidence; confidence           |
| [Hygiene](../../modules/hygiene/README.md)    | "What's wrong now, what's been wrong before"               | `(rule, target)`; lifecycle (detected → resolved/dismissed); severity     |

We deliberately do not collapse them. The producers, consumers, and read patterns are too different — and the cost of duplication is small (a few hundred lines of producer-side write code each).

## Retention

One settings page: `System → Retention`. Default policies (configurable):

| Audit type                                    | Default retention                          |
| --------------------------------------------- | ------------------------------------------ |
| Acquisition decisions — `grabbed`             | Forever (until the want is canceled)       |
| Acquisition decisions — `manual_override`     | Forever                                    |
| Acquisition decisions — `runner_up`           | 90 days                                    |
| Acquisition decisions — `rejected`            | 30 days                                    |
| Routing evaluations                           | Forever for active jobs; 90 days post-job  |
| Match decisions — current (non-superseded)    | Forever                                    |
| Match decisions — superseded                  | Forever (the supersession chain *is* the story) |
| Hygiene findings — `open`                     | Until resolved                             |
| Hygiene findings — `resolved`                 | 90 days                                    |
| Hygiene findings — `dismissed`                | Forever (we don't want to re-surface dismissed findings) |
| Scan history (`scan_run`)                     | 1 year                                     |

Knobs are per audit type, not per-subsystem-as-a-whole — because different *kinds* of rows within one producer need different retention (e.g., grabbed vs rejected acquisition decisions).

**Enforcement** is a background pruner that runs daily. Bulk delete from the settings UI is a one-click action with a destructive preflight (count + estimated bytes freed).

**Why centralized:**

- A user shouldn't have to hunt across four settings pages to set "keep my decision logs for 90 days."
- Retention defaults are part of the install's character; per-subsystem retention drift is operational sprawl.
- The retention UI is the natural home for related affordances: "export everything before pruning," "bulk-delete now," "show me how much storage decisions consume."

## The Activity view

A unified chronological view that reads across producers. URL: `/activity` (or similar). Read-only.

Shape:

```
┌──────────────────────────────────────────────────────────────────────┐
│  Activity                                          [filters]  [export]│
├──────────────────────────────────────────────────────────────────────┤
│  2026-05-22  14:38   ✅  Dune Part Two — available on Plex            │
│              14:35   📥  Dune Part Two — imported (45 GB → movies-4k) │
│              14:33   🎯  Dune Part Two — matched as TMDB 693134 (0.98)│
│              14:21   ⬇   Dune Part Two — grabbed (2160p WEB-DL, 47 GB)│
│              14:21   ▶   Dune Part Two — routed via "4K → bigdisk"    │
│                                                                       │
│  2026-05-22  09:12   ⚠   /movies/old/The Old Man.mkv — broken hardlink│
│              03:00   🧹  Nightly audit completed — 4 new findings     │
└──────────────────────────────────────────────────────────────────────┘
```

Each row is a clickable narrative summary. Clicking opens the source subsystem's drill-down: an acquisition decision goes to the decision detail; a match goes to the match-decision chain; a hygiene finding goes to the finding story view.

**Filters:**

- By target (this media item / file / library)
- By kind (acquisitions / routing / matches / findings)
- By decider (auto vs manual)
- By time range
- Free-text search across reasons

**Drill-down semantics:**

- The Activity view is *additive* over the per-subsystem dashboards. Hygiene's dashboard, the matcher inbox, the decision-log detail page — all stay. Activity is the cross-cutting timeline; they're the focused workspaces.

**Scope:**

- Admins see everything.
- Requesters (when those exist) see activity related to their own requests/wants only. Defined in the users-permissions-approval spec when it lands.

## What this pattern enables

- **"Why did this happen?"** is answerable for every automated decision. Same approach across the system, not a one-off per subsystem.
- **The library has a story.** "What happened on 2026-04-12?" is a useful question; the Activity view answers it.
- **New audit-shaped data fits without re-inventing UX.** Future subsystems (notifications? imports? scan? approval?) that want to leave decision trails just declare their table + retention type and register a row renderer for the Activity view. No new settings page; no new dashboard concept.

## What this pattern does NOT do

- **Enforce a shared schema.** Subsystems own their tables.
- **Replace per-subsystem dashboards.** Drill-down UIs stay; Activity is additive.
- **Provide compliance-grade audit trail.** Rows are observable, not signed; tamper-evident audit is out of scope.
- **Unify the *configuration* engines.** Quality, routing, hygiene, matching — all have different evaluation models and different config surfaces. This pattern unifies the *observability* of their outputs, not their inputs.

## Producer responsibilities

Each producer subsystem must:

1. Define a table that captures its decision shape (subsystem-specific fields + the common conceptual fields above).
2. Write rows synchronously with the decision (or in a tightly-coupled async batch — see the [acquisition spec](../../modules/acquisition/README.md#decision-logging) for the bulk-insert-at-end-of-Pick pattern).
3. Declare its retention type(s) — the audit-pattern config drives the actual pruning.
4. Implement a single drill-down route that takes a row ID and renders the subsystem-native detail page.
5. Implement a row renderer for the Activity view (icon, summary line, link target).

That's the full contract. No shared library, no shared base class, no shared schema — just the contract and the consumers.

## Interactions

| Neighbor                                          | How it interacts                                                              |
| ------------------------------------------------- | ----------------------------------------------------------------------------- |
| [Acquisition](../../modules/acquisition/README.md) | Producer; writes per-`(want, search_run, release)` rows                       |
| [Routing](../../modules/routing/README.md)         | Producer; writes per-`download_job` rows                                      |
| [Matching](../../modules/matching/README.md)       | Producer; writes per-file rows with supersession chains                       |
| [Hygiene](../../modules/hygiene/README.md)         | Producer; writes per-`(rule, target)` rows with lifecycle                     |
| [Errors](../errors/README.md)                      | Sibling cross-cutting pattern. An error is observability for the failure path; an audit row is observability for the decision path. They're complementary; an audit row can reference an error's `Op` for failed decisions. |
| [Notifications](../../modules/notifications/README.md) | Subscribes to audit-row creation for push-notification triggers ("upgrade proposed", "match needs review"). |

## Open questions

1. **Decider granularity.** `auto` is fine for most rows; for rule-driven decisions, do we link back to the rule ID? Lean: yes — enables "this rule fired 47 times this week" analytics.
2. **Activity-view scope for requesters.** Admin sees all; requester sees their own. What counts as "theirs" — only their request → want chain? Or also matches against media they requested? Defer to users/permissions spec.
3. **Cross-subsystem correlation.** Acquisition grabs X → matcher identifies the resulting file as Y. There's a natural causal chain. Worth storing as explicit links? Lean: no for v1; the Activity view's chronological adjacency is usually enough. Add explicit links if and when a real query needs them.
4. **Storage volume.** Busy libraries could generate thousands of decision rows per day. Retention defaults above are conservative; we may want aggressive defaults (or compression) once we see real volume.
5. **Export.** CSV/JSON export for the Activity view? Lean: yes for power users / debugging. The settings page's "before-prune export" is a related affordance.
6. **Per-target activity feed.** A "show me everything that ever happened to this media item" view is the same query shape, scoped. Probably part of the media focus page rather than the Activity view itself. Pin in the UI iteration.
7. **What about failed jobs / errored decisions?** A failed search, a routing error, an import failure — are those audit rows or just operational logs? Lean: audit rows where there's an intentional decision (e.g., "tried to route, no rule matched"); operational logs where it's a runtime failure (e.g., "indexer returned 500"). Don't conflate.
8. **Renderer registration mechanism.** The Activity view aggregates by reading from each producer table. How is the renderer registered — code-level interface, config-driven, or something else? Pin in implementation; the spec is renderer-agnostic.

## What we're explicitly not deciding here

- Exact table shapes (those live in each producer's spec)
- The Activity view's exact UI layout, beyond the sketch above
- Export formats
- Retention enforcement implementation (pruner cadence, batch sizes)
- Cross-subsystem causal-link modeling
- A formal "auditable decision" interface in code

## Doc neighbors

- [Acquisition](../../modules/acquisition/README.md) — producer
- [Routing](../../modules/routing/README.md) — producer
- [Matching](../../modules/matching/README.md) — producer
- [Hygiene](../../modules/hygiene/README.md) — producer
- [Errors](../errors/README.md) — sibling cross-cutting pattern
