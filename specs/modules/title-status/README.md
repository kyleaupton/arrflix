# Title status — the acquisition read model

**Status:** Draft, iteration 1

This doc defines **title status**: a single server-computed answer to *"what is happening with this title, for this viewer, right now."* It is the read model every acquisition-facing surface renders from — focus pages, poster chips, hero controls, status cards, season grids — and the payload of the realtime events that keep those surfaces live.

It exists because that question currently has **six different answers** in the frontend, each derived independently from a different join across four caches. They disagree, and the disagreement is not a bug to be fixed once — it is the guaranteed steady state of the current design.

This doc owns the shape of the read model, its state vocabulary, how it is computed, and when it is emitted. It does **not** own the SSE transport ([realtime](../realtime/README.md)), the acquisition pipeline that produces the underlying state ([acquisition](../acquisition/README.md)), or the durable notification of events ([notifications](../notifications/README.md)).

## TL;DR

- **One type, every surface.** `TitleStatus` is computed server-side and rendered — never re-derived — by the poster chip, the hero control, the status card, and the season grid. Identical shape everywhere is what makes those surfaces structurally incapable of disagreeing.
- **Name events for what the UI renders, not what the DB stores.** `title_status`, not `want_updated`. An event named after a table pushes a join onto every client that receives it; three clients doing that join three ways is how we got here.
- **This finishes an existing pattern, it doesn't invent one.** `HydratedTitle`/`MovieRail` already carry server-computed `isInLibrary`/`isDownloading`, and rails/search/library already consume them with no derivation. The focus pages were left behind.
- **Two events on one query key.** `title_status` carries the full projection and fires on transitions. `title_progress` carries numbers only and fires per tick. Both land on the same TanStack key.
- **The payload is per-viewer.** Two people looking at the same title get different `TitleStatus` — different actions, different intent, and different *fields*. Operator-only data is **absent** for a requester, not hidden by the client.
- **State is not one enum.** A title can be *available* and *working* at once (an upgrade in flight), or partially available and still acquiring (a series mid-season). A headline state drives the chip; orthogonal facts carry the rest.
- **`actions[]` is server-computed.** What this viewer may do, whether it needs approval, and what it would cost. The client renders buttons; it does not decide which buttons exist.
- **Derivation is a pure function.** Raw state tuple → `TitleState`, total, no I/O. Every requirement about *what state should show* becomes a table test with no database.
- **Explicit invalidation plus a reconciler.** Mutation sites announce changes; a sweep over titles with active work recomputes and emits on diff. A forgotten call site becomes a latency bug rather than a correctness bug — which is the failure mode that produced the defect this work started from.

## Why this is its own spec

### The concrete defect

A user requests a movie. The poster's chip updates. The hero chip and the status card do not.

Two independent causes, producing one symptom:

1. **`want_updated` carries no title identity.** It carries the full want, whose only title reference is `mediaItemId`. The frontend keys on TMDB id. `download_job_updated` *does* carry `tmdbId`. So the poster chip — which reads the jobs cache — could locate the title, and the card — which reads the tracking cache — could not.
2. **The binding drops the event.** `web/src/realtime/bindings.ts:92` discards any `want_updated` whose cached tracking entry is null. For a title that was untracked when the page loaded — exactly the case where someone just pressed Request — the cache holds `tracking: null`, so the patch is discarded and nothing refetches.

Either alone would have been a two-line fix. Together they look arbitrary, and that arbitrariness is the tell: the client is doing a join it has no business doing, with partial data, in two places, differently.

### The systemic cause

Every acquisition event is named after a database table. None of them means anything on its own, so each consumer reconstructs meaning by joining across four caches:

| Cache | Carries |
| --- | --- |
| `trackingByTmdb` | tracking, wants, the viewer's request |
| `downloadJobsList` | live job progress |
| `mediaGetMovie` / `mediaGetSeries` | files, per-episode availability |
| `requestsList` | request lifecycle |

Six surfaces perform that join independently: a 7-state machine in `MovieStatusCard`, a 5-branch template in the series grid, a 4-branch hero control, a shape-branching library check in `Poster`, plus two more. They encode the same concept in six vocabularies, and the status→label→variant→icon mapping is triplicated across six files.

Three surfaces independently deriving one concept will eventually disagree. Fixing the six current disagreements does not change that; only deleting the derivation does.

### What else is broken underneath

Reconnaissance turned up problems that the projection either fixes or is blocked by. They are listed here because a reader deciding whether this work is worth it should see the real scope.

- **Nothing is scoped.** `want_updated`, `download_job_updated`, and `import_task_updated` are all `Broadcast`. Every connected session receives every want and job update for every title in the system, regardless of who it concerns. The per-user scoping described in [realtime](../realtime/README.md) is specified and not implemented.
- **Operator-only data reaches requesters.** Requesters do not fetch the jobs REST baseline (`enabled: auth.canViewJobs`) but *do* receive broadcast job payloads including `candidateTitle`, which the status card deliberately declines to render. The data is in their cache; only the rendering is withheld.
- **Request state is not realtime at all.** No `request_*` or `tracking_*` event exists. Approve, deny, and cancel are invisible until a manual refetch — so an admin approving a request is something the requester's open tab cannot learn.
- **The debounce has no maximum wait.** `bindings.ts` coalesces refetches with a trailing-only debounce. A sustained event stream at under the debounce interval postpones the refetch indefinitely — and a season-pack import is precisely that stream.
- **One operation, two cache keys.** Movie callers omit `query`, series callers pass `query: {type: 'series'}`, producing two distinct cache entries for `trackingByTmdb`. Every broad invalidation works around this with a partial-match key, and one component reconstructs the argument shape conditionally to force a cache hit.

## The model

### One type, two granularities

`TitleStatus` answers the question for one title and one viewer. Series carry an episode dimension; movies do not.

```
TitleStatus
  mediaType            movie | series
  tmdbId

  state                the headline — what the chip renders
  obtainableAt?        when this becomes gettable, if it isn't yet

  library              what we have
    hasFiles
    fileCount
    best?              quality bin of the best copy

  work                 what is happening
    active
    phase?             searching | waiting | downloading | importing
    startedAt?
    lastCheckedAt?     freshness, for "last checked 2h ago"
    wants              { total, done, working }

  episodes?            series only
    inScope, available, aired, working
    seasons[]          { number, total, available, state }
    items[]            { episodeId, seasonNumber, episodeNumber, state, airsAt? }

  viewer               computed for the requesting user
    isRequester
    intent?            { tier, scopeRule } — this viewer's own intent
    actions[]
```

**The episode array lives in the projection**, rather than in a second query the grid joins against. Splitting it would recreate the two-sources problem this document exists to delete. A 200-episode series is a few KB of compact tokens, and it changes rarely.

### State is a headline, not the whole truth

A single enum cannot express the states the product requires, because several are simultaneous:

- A title with a file that is being upgraded is **available and working** ([upgrades and divergent tiers](../../stories/06-upgrades-and-divergent-tiers.md)).
- A series mid-season is **partially available and still acquiring** ([series mid-season](../../stories/02-series-mid-season-auto-approve.md)).

So `state` is what the chip says, and `library` / `work` carry the orthogonal facts. An upgrade in flight is `state: available` with `work.active: true` — not a distinct state, which is what keeps the vocabulary from multiplying combinatorially.

| `state` | Means |
| --- | --- |
| `not_requested` | Nothing exists for this title |
| `unreleased` | Not obtainable yet; `obtainableAt` says when |
| `awaiting_approval` | A request is pending a decision |
| `denied` | A request was refused |
| `searching` | Looking for an acceptable release |
| `needs_pick` | Waiting for a human to choose (manual autonomy) |
| `proposed` | A pick is waiting for approval |
| `downloading` | Transfer in progress |
| `importing` | Transferred, being placed in the library |
| `available` | Everything in scope is on disk |
| `partially_available` | Some of it is on disk; series only |
| `unavailable` | Work ended without a file |
| `canceled` | Stood down |

### Three precedence rules

Pinned while implementing the derivation, because the state vocabulary alone does not determine the answer when several inputs disagree.

**A file on disk wins.** An atom with a file is `available` whatever its want says. The file is ground truth; the want may be a stale cancellation, or an upgrade still running. Nothing about a want should be able to make an existing file appear absent.

**The viewer's request governs only into a vacuum.** A pending or denied request is the headline when there is no file and no work underway. Once either exists, what the system is *doing* outranks what was *asked* — a denial does not hide a copy someone else already got, and a pending request does not mask an acquisition already in flight.

**Activity is orthogonal, and is read from wants rather than from states.** This is the subtle one. Deriving activity from the item states masks exactly the case the model exists to express: an available atom with a searching want reports `available`, so a rollup over states would conclude nothing is happening and the in-flight upgrade would vanish. Activity is computed from the wants directly, in parallel with the states, never from them.

**Out-of-scope atoms are shown but do not speak for the title.** A season grid renders every episode, including ones nobody asked for. Those atoms get a state so the grid can draw them, and are excluded from the counts and the headline.

Without that split the headline is simply wrong, and a live library proves it. Rick and Morty carries 128 episodes across ten seasons — but season 0 is 37 specials, and nothing wants them. A title that has acquired every episode of every real season would still report `partially_available`, permanently held back by specials it was never going to get. Specials are not a data artifact that will be cleaned up; every series with a season 0 has this shape forever. A title cannot be judged against work it isn't doing.

A file outside scope still counts as library content, because it is on disk regardless of whether anything wants it.

**Scope is inferred from the presence of a want.** Wants are created eagerly for everything in scope, so having one means the title wants that atom; a file or a live hand-grab counts too. The alternative — resolving effective scope per read — means re-running the requester-union resolution on every render, since [that union is deliberately not materialized](../../stories/05-multi-requester-scope-union.md). Reading the result of it instead is cheaper and cannot drift from what the reconciler actually did.

**`searching` collapses the internal oscillation.** A want with nothing to grab cycles `pending` → `searching` → `pending` continuously as the worker claims and releases it. Any surface rendering the raw want status shows a flickering value that misrepresents a system doing exactly the right thing. The projection reports one stable state, satisfying `REQ-SEARCH-002` by shape rather than by patch.

### Derivation is pure

The mapping from raw state to `TitleState` is a **pure, total function**: given wants, jobs, files, request, and tracking, it returns exactly one state. No I/O, no error return — an unrecognized input tuple yields a defined fallback rather than a failure.

This is the load-bearing testability property. Every requirement of the form *"a title in situation X must show state Y"* becomes a table-driven test with no database, no fixtures, and no HTTP. The service that fetches the inputs is separately and more cheaply tested.

Per the [backend layering invariants](../../../.claude/rules/overview.md): the pure derivation is a domain module and must not import `internal/repo`, `internal/db/sqlc`, or `pgtype`. The service that assembles its inputs holds the repository and lives in `internal/service/`.

### The viewer lens

`TitleStatus` is computed **per viewer**, not filtered per viewer. The difference matters:

- **Different actions.** A requester sees cancel; a reviewer sees approve and deny; someone with no grant for this media type sees nothing.
- **Different intent.** `viewer.intent` is *this* viewer's tier and scope, not the tracking's union.
- **Different fields.** Operator-only data — candidate release titles, indexer names, per-file paths — is **absent** from a requester's projection.

That last point is the fix for the leak above. Today the sensitive field is broadcast to everyone and withheld by client-side rendering; a per-viewer payload means it was never sent.

**Requester identity is never disclosed between requesters.** A viewer learns that a title is already being followed — the useful part — and not by whom.

### Actions carry their consequences

An action is not a label. It carries what will happen if taken:

```
Action
  kind              request | cancel | approve | deny | retry | pick | upgrade
  enabled
  requiresApproval?   this will need a decision before anything happens
  tiers?              which tiers this viewer may choose
  effect?             { episodesAdded, bytesEstimate }
  disabledReason?
```

This absorbs three requirements that would otherwise each need their own endpoint:

- **`REQ-APPROVE-012`** — a user must know a choice requires approval *before* committing. That is `requiresApproval` on the request action.
- **`REQ-UNION-*` scope diff** — *"Season 3 is already being followed. Requesting will add Seasons 1–2."* That is `effect.episodesAdded`.
- **`REQ-SERIES-007`** — the pre-flight size of what you are asking for. That is `effect.bytesEstimate`.

It also deletes the client-side tier filtering and the auto-approve check that currently decide the CTA's face — the server holds both the grant set and the state, so it should be the one deciding.

**Disabled actions are still sent** when the affordance needs to explain itself. An action that simply does not apply is omitted; an action that is unavailable *for a reason the user should know* is present, disabled, and carries the reason.

### Delivery

Two events, one TanStack query key:

| Event | Fires | Carries |
| --- | --- | --- |
| `title_status` | On state transitions | The full projection |
| `title_progress` | Per tick during transfer | Numbers only — percent, bytes, ETA, and per-episode progress for what is actively moving |

Topics are title-scoped: `title.status:<mediaType>:<tmdbId>`. A focus page subscribes on mount and drops on navigate, which is what the [realtime](../realtime/README.md) scope qualifier exists for.

**The tick is decoration.** `title_progress` carries no state and never changes what the UI believes is happening — only how far along it is. Ordering within a stream is guaranteed by SSE, so a tick cannot overtake the transition that precedes it. For the page-load race, the REST snapshot carries the event ID it was computed at, and ticks older than that are discarded.

**Emission is coalesced server-side**, at most one `title_status` per title per interval. A season-pack import transitions nine episodes; that is one emit, not nine. This replaces the unbounded client debounce with a bounded server-side one — an important difference, because the current client debounce can be starved indefinitely by exactly this burst.

### List surfaces get the same type, delivered differently

A browse page with fifty posters cannot subscribe to fifty topics, and does not need per-tick progress in a grid.

**List endpoints return `TitleStatus[]`, batch-computed, refreshed by notify-and-refetch.** The shape is identical to what the focus page receives; only the delivery differs. Identical shape is the entire point — it is what makes the poster chip and the hero chip unable to disagree, since they render the same type from the same producer.

This also resolves the request-list N+1, where each row currently fetches full media detail for a title and a poster.

### Keeping it correct

Two mechanisms, deliberately overlapping:

1. **Explicit invalidation.** Services that mutate acquisition state announce the affected title. Greppable, immediate, and easy to forget.
2. **A reconciler.** A periodic sweep recomputes the projection for titles with active work and emits when it differs from what was last sent.

The reconciler is what makes this robust. A missed call site becomes a bounded latency, not a permanently wrong UI — and a missed call site is *precisely* the class of defect that started this work. It also recovers state after a restart, when in-process session state is gone but downloads are still running.

The progress ticker already runs on the required cadence for anything actively downloading, so the sweep is close to free where it matters most.

## What this requires that doesn't exist

Honest dependencies. The projection cannot ship without these.

- **Per-user scoping in the broker.** Specified in [realtime](../realtime/README.md), not implemented — everything is `Broadcast` today. A per-viewer payload cannot be broadcast; it is incoherent, not merely leaky. **This is the hard prerequisite.**
- **A TMDB-keyed read.** Getting from `(tmdbId, mediaType)` to wants is three hops today, and no query joins them. The download side already has `ListDownloadJobsByTmdbMovieID` as precedent.
- **Title identity on want events.** Whatever replaces `want_updated` internally must carry enough to locate the title without a client-side map.
- **A unified tracking cache key.** The movie/series key split should be closed as part of this work rather than carried forward into a new type.

## Known limits

- **No score is persisted per file.** `file_origin` caches the quality bin triple, but the custom-format score exists nowhere on disk. So `library.best` can express bin-based comparison and **cannot** express score-based upgrade eligibility without recomputation. This is the concrete blocker under [upgrades and divergent tiers](../../stories/06-upgrades-and-divergent-tiers.md), and it is narrower than "upgrades are unbuilt."
- **`tracking.quality_profile_id` is single-valued.** Divergent tiers across requesters are deferred at the schema level. `viewer.intent.tier` can report what a viewer asked for; the system cannot yet act on two different answers.
- **Effective scope is not materialized.** It is recomputed live from surviving requester associations. The projection follows suit and computes it at emit — correct, and worth knowing before someone adds a cache.
- **Spelling.** `want`, `tracking`, and `request` use `canceled`; `download_job` and `import_task` use `cancelled`. The projection is a new type and normalizes to **`canceled`**, matching the majority and the domain models. Noted so it is not "corrected" back.

## Requirements this satisfies

Traceability to the [stories](../../stories/README.md). This is the design half of the link; the test half is a test named for the id.

| Requirement | How |
| --- | --- |
| `REQ-SEARCH-002` — state stays stable while the want cycles | `searching` collapses the `pending`↔`searching` oscillation |
| `REQ-SEARCH-003` — distinguish "working" from "stuck" | `work.active` is orthogonal to `state`; `unavailable` is distinct from `searching` |
| `REQ-APPROVE-012` — know a choice needs approval before committing | `Action.requiresApproval` |
| `REQ-SERIES-007` — see the size of what you are asking for | `Action.effect.bytesEstimate` |
| `REQ-SERIES-008` — series progress without composing per-episode state | `episodes` rollups are server-computed |
| `REQ-UNION-010` — only hear about what your intent covers | Per-viewer computation; scoped delivery |
| `REQ-UNION-011` — not told who else wants a title | Requester identity is never in the payload |
| `REQ-UNREL-*` — "not out yet" is distinct from "can't find it" | `unreleased` + `obtainableAt`, distinct from `searching` |

Requirements this **does not** satisfy, listed so the gap stays visible: the notification-side requirements (`REQ-SERIES-009`, `REQ-APPROVE-001`, `REQ-APPROVE-007`) are [notifications](../notifications/README.md)' territory. A live-updating page is not a substitute for telling someone who isn't looking at it.

## Does NOT own

- **The SSE transport, broker, scoping, and resume** — [realtime](../realtime/README.md). This module is a producer.
- **The acquisition pipeline** — searching, scoring, grabbing, importing. [Acquisition](../acquisition/README.md) owns the state; this module reads it.
- **Durable notification** — [notifications](../notifications/README.md). Realtime state and durable events are different buses with different guarantees.
- **Permission definitions** — [users](../users/README.md). The projection consumes the grant set; it does not define it.
- **Quality comparison semantics** — [quality profiles](../quality-profiles/README.md) owns bins, cutoff, and scoring. The projection reports the outcome.
- **Presentation.** The server sends structured state; labels, colours, and icons are the frontend's. Copy changes must not be backend deploys, and one enum mapped to one string is rendering, not derivation.

## Interactions

| Neighbor | How |
| --- | --- |
| [Realtime](../realtime/README.md) | Emits `title_status` / `title_progress` through it; depends on its per-user scoping |
| [Acquisition](../acquisition/README.md) | The state being projected; announces changes |
| [Requests](../requests/README.md) | Request lifecycle folds into `state` and `actions[]` — which closes the missing-`request_updated` gap without a row event |
| [Tracking](../tracking/README.md) | Autonomy dial produces `needs_pick` / `proposed`; requester associations produce `viewer.intent` |
| [Quality profiles](../quality-profiles/README.md) | Supplies bins and cutoff for `library.best` and upgrade eligibility |
| [Notifications](../notifications/README.md) | Sibling. Same underlying events, different bus and different guarantees |
| [Users](../users/README.md) | The grant set that produces `actions[]` |

## Open questions

1. **Coalescing interval.** Long enough to collapse a season-pack burst, short enough that a single transition feels immediate. **Lean:** around 250ms, measured against the import burst rather than guessed.

2. **Does the episode array scale to the worst case?** A long-running daily show is thousands of episodes. **Lean:** carry them all until a real series proves it hurts, then paginate by season — the grid is already season-organised, so that shape exists if needed.

3. **How is `effect.bytesEstimate` produced before anything is searched?** [Series mid-season](../../stories/02-series-mid-season-auto-approve.md) raises this and it remains genuinely open: the episode tree may not be synced at the moment the user is deciding. **Lean:** a coarse estimate from episode count and tier, refined once known — but this needs picking before the field can be built.

4. **Is `state` enough for the chip, or does it need a severity axis?** *Denied* and *unavailable* are both terminal-without-a-file but want different colours. **Lean:** derive severity from state in the frontend; adding a second server field invites the two to drift.

5. **Should the projection carry search history?** `REQ-SEARCH-004` — *"what have you tried?"* — is the headline unbuilt requirement of [failed search](../../stories/04-failed-search-recovery.md). It is drill-down data, large, and rarely looked at. **Lean:** a separate query on expand, not in the projection.

6. **What does an unauthenticated or newly-bootstrapped viewer get?** Bootstrap sets an empty permission set, so a surface rendering before the profile loads sees no actions and briefly shows nothing. **Lean:** the projection is not the fix — the frontend should distinguish *unknown* from *empty* — but it should be resolved alongside this work, since it makes the CTA flicker on every load.

## Doc neighbors

- [Realtime](../realtime/README.md) — transport, broker, scoping; this module's delivery path
- [Acquisition](../acquisition/README.md) — the pipeline whose state this projects
- [Tracking](../tracking/README.md) — autonomy, requester associations, effective scope
- [Requests](../requests/README.md) — the lifecycle folded into `state`
- [Quality profiles](../quality-profiles/README.md) — bins, cutoff, scoring
- [Notifications](../notifications/README.md) — the durable sibling bus
- [Stories](../../stories/README.md) — the product decisions this shape encodes
