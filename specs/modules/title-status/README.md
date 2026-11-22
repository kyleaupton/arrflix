---
status: as-built
---

# Title status — the acquisition read model

> **Beyond what's built:** the built slice is the per-title projection (`GET /api/v1/titles/{mediaType}/{tmdbId}/status`, rendered by the movie and series focus pages), the `request` and `cancel` actions, and the `title_status` / `title_progress` events. §List surfaces (batch `TitleStatus[]`), the `obtainableAt` / `library.best` / `work.startedAt` / `work.lastCheckedAt` / season-rollup fields of §The model, operator-only fields, and `Action.effect` describe unbuilt design. Poster chips and rails still read `HydratedTitle`'s `isInLibrary` / `isDownloading`.

This doc defines **title status**: a single server-computed answer to *"what is happening with this title, for this viewer, right now."* It is the read model acquisition-facing surfaces render from — focus pages, hero controls, status cards, season grids — and the subject of the realtime events that keep those surfaces live.

It exists because, derived client-side, that question gets **a different answer on every surface**: each derives it independently from a different join across four caches. They disagree, and the disagreement is not a bug to be fixed once — it is the guaranteed steady state of client-side derivation.

This doc owns the shape of the read model, its state vocabulary, how it is computed, and when it is emitted. It does **not** own the SSE transport ([realtime](../realtime/README.md)), the acquisition pipeline that produces the underlying state ([acquisition](../acquisition/README.md)), or the durable notification of events ([notifications](../notifications/README.md)).

## TL;DR

- **One type, every surface.** `TitleStatus` is computed server-side and rendered — never re-derived — by the hero control, the status card, and the season grid (and, once [list surfaces](#list-surfaces-get-the-same-type-delivered-differently) exist, the poster chip). Identical shape everywhere is what makes those surfaces structurally incapable of disagreeing.
- **Name events for what the UI renders, not what the DB stores.** `title_status`, not `want_updated`. An event named after a table pushes a join onto every client that receives it; three clients doing that join three ways is how we got here.
- **This finishes an existing pattern, it doesn't invent one.** `HydratedTitle`/`MovieRail` already carry server-computed `isInLibrary`/`isDownloading`, and rails/search/library already consume them with no derivation. The projection extends that to the focus pages.
- **Two events on one query key.** `title_status` is a bare kick to refetch and fires on transitions; `title_progress` carries numbers and fires per tick. The split falls out of what each one is: the projection is per-viewer so it is fetched, progress is viewer-independent so it is pushed. Both land on the same TanStack key.
- **The projection is per-viewer.** Two people looking at the same title get different `TitleStatus` — different actions, different intent, and different *fields*. Operator-only data is **absent** for a requester, not hidden by the client. That is also why the transition event is a kick: a per-viewer answer is fetched by the viewer, not pushed to them.
- **State is not one enum.** A title can be *available* and *working* at once (an upgrade in flight), or partially available and still acquiring (a series mid-season). A headline state drives the chip; orthogonal facts carry the rest.
- **`actions[]` is server-computed.** What this viewer may do, whether it needs approval, and what it would cost. The client renders buttons; it does not decide which buttons exist — and an offered action ships whatever taking it requires, or the client is back to joining caches to act on it.
- **Derivation is a pure function.** Raw state tuple → `TitleState`, total, no I/O. Every requirement about *what state should show* becomes a table test with no database.
- **Explicit invalidation plus a reconciler.** Mutation sites announce changes; a periodic sweep re-announces every title whose acquisition rows changed. A forgotten call site becomes a latency bug rather than a correctness bug — which is the failure mode that produced the defect this work started from.

## Why this is its own spec

### The concrete defect

The defect that motivated this model: a user requests a movie. The poster's chip updates. The hero chip and the status card do not.

Two independent causes, producing one symptom:

1. **`want_updated` carries no title identity.** It carries the full want, whose only title reference is `mediaItemId`. The frontend keys on TMDB id. `download_job_updated` *does* carry `tmdbId`. So the poster chip — which reads the jobs cache — could locate the title, and the card — which reads the tracking cache — could not.
2. **The binding drops the event.** The `want_updated` binding in `web/src/realtime/bindings.ts` discards any want whose cached tracking entry is null. For a title that was untracked when the page loaded — exactly the case where someone just pressed Request — the cache holds `tracking: null`, so the patch is discarded and nothing refetches.

Either alone would have been a two-line fix. Together they look arbitrary, and that arbitrariness is the tell: the client is doing a join it has no business doing, with partial data, in two places, differently.

### The systemic cause

The row-level acquisition events are named after database tables. None of them means anything on its own, so a consumer that renders title state from them reconstructs meaning by joining across four caches:

| Cache | Carries |
| --- | --- |
| `trackingByTmdb` | tracking, wants, the viewer's request |
| `downloadJobsList` | live job progress |
| `mediaGetMovie` / `mediaGetSeries` | files, per-episode availability |
| `requestsList` | request lifecycle |

Before the projection, six surfaces performed that join independently — a 7-state machine in `MovieStatusCard`, a 5-branch template in the series grid, a 4-branch hero control, a shape-branching library check in `Poster`, plus two more — encoding the same concept in six vocabularies.

Surfaces independently deriving one concept will eventually disagree. Fixing each disagreement does not change that; only deleting the derivation does.

### Related problems underneath

Problems adjacent to the projection, which it either fixes or leaves standing:

- **Row events and scoping.** `download_job_updated` and `import_task_updated` target `jobs.read`, so operator-only fields such as `candidateTitle` reach operators only. `want_updated` is still `Broadcast`: every connected session receives every want update for every title.
- **Request state reaches open tabs through the projection.** No `request_*` or `tracking_*` event exists; approve, deny, and cancel change the projection, and the `title_status` kick is what tells a requester's open focus page. Other request surfaces (`requestsList`) still learn only on refetch.
- **The client debounce has no maximum wait.** `bindings.ts` coalesces its invalidate-based refetches (import tasks, media detail, proposals) with a trailing-only debounce. A sustained event stream at under the debounce interval postpones the refetch indefinitely — and a season-pack import is precisely that stream. `title_status` avoids this by coalescing server-side with a bounded interval.
- **One operation, two cache keys.** Movie callers omit `query`, series callers pass `query: {type: 'series'}`, producing two distinct cache entries for `trackingByTmdb`. Every broad invalidation works around this with a partial-match key, and one component reconstructs the argument shape conditionally to force a cache hit.

## The model

### One type, two granularities

`TitleStatus` answers the question for one title and one viewer. Series carry an episode dimension; movies do not. The wire type is `model.TitleStatus`; the pure derivation is `internal/titlestatus`, assembled by `TitleStatusService` in `internal/service/title_status.go`.

```
TitleStatus
  mediaType            movie | series
  tmdbId

  state                the headline — what the chip renders
  phase?               searching | downloading | importing (empty when nothing is in flight)
  active               work in flight, independent of state

  library              what we have
    hasFiles
    fileCount

  counts               { total, available, working } over in-scope atoms

  viewer               computed for the requesting user
    isRequester
    requestId?         the viewer's live request — what cancel acts on
    intent?            { tier, scopeRule } — this viewer's own intent
  actions[]

  episodes[]?          series only, in season/episode order
                       { episodeId, seasonNumber, episodeNumber, state, airDate? }
```

**Unbuilt fields** from the design: `obtainableAt` (when the title becomes gettable), `library.best` (quality bin of the best copy), `work.startedAt` / `work.lastCheckedAt` (freshness, for "last checked 2h ago"), and server-computed episode rollups (`inScope` / `aired` counts and a per-season `{ number, total, available, state }` array). Per-episode not-yet-aired state is derived from the air date and does reach the headline as `unreleased`.

**The episode array lives in the projection**, rather than in a second query the grid joins against. Splitting it would recreate the two-sources problem this document exists to delete. A 200-episode series is a few KB of compact tokens, and it changes rarely.

### State is a headline, not the whole truth

A single enum cannot express the states the product requires, because several are simultaneous:

- A title with a file that is being upgraded is **available and working** ([upgrades and divergent tiers](../../stories/06-upgrades-and-divergent-tiers.md)).
- A series mid-season is **partially available and still acquiring** ([series mid-season](../../stories/02-series-mid-season-auto-approve.md)).

So `state` is what the chip says, and `library` / `active` / `phase` carry the orthogonal facts. An upgrade in flight is `state: available` with `active: true` — not a distinct state, which is what keeps the vocabulary from multiplying combinatorially.

| `state` | Means |
| --- | --- |
| `not_requested` | Nothing exists for this title |
| `unreleased` | Not obtainable yet (episodes: not yet aired; movies: search date still ahead — the day before the home release, or theatrical + 30 days when none is announced). A movie carries `homeReleaseDate` when one is known. **Unbuilt:** `obtainableAt` on the wire |
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

The state vocabulary alone does not determine the answer when several inputs disagree; these rules do (see `titlestatus.Derive`).

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

- **Different actions.** A requester sees cancel; a reviewer sees approve and deny (**unbuilt** — see below); someone with no grant for this media type sees nothing.
- **Different intent.** `viewer.intent` is *this* viewer's tier and scope, not the tracking's union. It reports the ask, which is not a promise: `tracking.quality_profile_id` is single-valued, so two requesters wanting different tiers cannot both be acted on.
- **Different fields.** Operator-only data — candidate release titles, indexer names, per-file paths — is **absent** from a requester's projection. **Unbuilt:** the projection carries no operator-only fields for anyone yet; operators read them from the jobs surfaces.

A per-viewer payload means sensitive data is never sent, rather than sent and withheld by client-side rendering.

**Requester identity is never disclosed between requesters.** A viewer learns that a title is already being followed — the useful part — and not by whom.

### Actions carry their consequences

An action is not a label. It carries what will happen if taken:

```
Action
  kind              request | cancel | approve | deny | retry | pick | upgrade
  enabled
  requiresApproval?   this will need a decision before anything happens
  tiers?              [{ tier, requiresApproval }] — which tiers this viewer may choose
  effect?             { episodesAdded, bytesEstimate }   (unbuilt)
  disabledReason?
```

Only `request` and `cancel` exist as kinds (`titlestatus.ActionRequest` / `ActionCancel`); the rest are design, per **Shipped** below.

**Approval is per tier, not per user.** A viewer can be trusted to pull HD on their own and still need a decision for 4K — the grant catalog says so directly (`requests.auto_approve:<type>:<tier>`). So the flag lives on each tier, and the top-level `requiresApproval` is set only when *every* offered tier needs a decision, letting a single-tier UI read it without walking the list.

This absorbs three requirements that would otherwise each need their own endpoint (only the first is built):

- **`REQ-APPROVE-012`** — a user must know a choice requires approval *before* committing. That is `requiresApproval` on the request action.
- **`REQ-UNION-*` scope diff** — *"Season 3 is already being followed. Requesting will add Seasons 1–2."* That is `effect.episodesAdded`.
- **`REQ-SERIES-007`** — the pre-flight size of what you are asking for. That is `effect.bytesEstimate`.

It also removes client-side tier filtering and auto-approve checks from deciding the CTA's face — the server holds both the grant set and the state, so it should be the one deciding.

**Disabled actions are still sent** when the affordance needs to explain itself. An action that simply does not apply is omitted; an action that is unavailable *for a reason the user should know* is present, disabled, and carries the reason.

The line between the two is whether seeing the affordance helps. A viewer with no create grant does not get a greyed-out Request button — that permission is not a state that will change, and showing it only invites them to keep trying. A viewer looking at *their own* live request they may not withdraw does get a disabled Cancel with a reason, because the absence of that button on their own request is otherwise inexplicable.

**An offered action must carry what taking it requires.** `cancel` needs the request's id, so `viewer.requestId` is part of the projection rather than something the client re-fetches — a client that has to look up the id is back in the cross-cache join this exists to delete. The same rule is what blocks `approve`/`deny` below.

**Shipped:** `request` (with per-tier approval) and `cancel`. **Deferred, each blocked on a fact the projection cannot state honestly yet:** `approve`/`deny` need the pending request of *any* requester plus its id, and are queue affordances more than focus-page ones; `retry` needs the "found nothing" vs "indexer unreachable" distinction that lives in `want.last_error`; `pick`/`upgrade` need the candidate set and tier comparison; `effect.episodesAdded` needs the requester-union scope diff, which is deliberately never materialized; `effect.bytesEstimate` has no pre-grab size to read. Shipping a half-supported action is worse than omitting it — the client would render an affordance whose consequences the server is guessing at.

### Delivery

Two events, one TanStack query key:

| Event | Fires | Carries |
| --- | --- | --- |
| `title_status` | On state transitions | Nothing but the title's identity — a kick to refetch |
| `title_progress` | Per tick during transfer | Numbers only — progress, bytes/sec, ETA |

**`title_status` is a kick, not a payload.** The projection is per-viewer: the requester's own request governs part of the headline, and the actions offered differ by grant. Pushing the computed projection means one derivation per watching session at emit time, and a payload that can be stale before it lands. Naming the title instead lets each client refetch through the endpoint that already applies its own lens, which by construction cannot disagree with what a reload would show. The cost is one round trip on an event that fires only on transitions — the rare one.

**`title_progress` carries its payload**, because a refetch per tick would be absurd for a number that moves every few seconds. That is safe precisely because progress is viewer-independent: a percentage is the same for everyone, so there is no lens to apply and nothing to withhold. The operator-only detail — which release, which indexer, which client — stays on `download_job_updated` behind `jobs.read`. Splitting them is what lets a progress bar reach a requester without the release title going with it.

Note what this implies: the per-viewer *payload* machinery is not needed yet. Recipient scoping decides who receives both events; neither varies its content by viewer. The doctrine stays in the [realtime spec](../realtime/README.md) for when something genuinely needs it.

**The tick is decoration.** `title_progress` carries no state and never changes what the UI believes is happening — only how far along it is. A dropped or reordered tick is corrected by the next one, so no sequencing guard is required.

**Emission is coalesced server-side** by `internal/titlenotify`: at most one `title_status` per title per 250ms flush interval. A season-pack import transitions every episode of a season; that is one emit, not ten. A bounded server-side interval, unlike a trailing-only client debounce, cannot be starved indefinitely by exactly this burst.

**A sweep backs the explicit invalidations.** Every write path that moves acquisition state announces the affected media item (`titlenotify.Notifier.Notify`), but that is a contract across a dozen call sites and contracts like that decay. A sweep every 30s re-reads which media items changed and announces them regardless, which is what keeps a forgotten announcement a latency bug rather than a stale-UI bug. The sweep window overlaps the previous one by a 10s grace so a row committed late by a long transaction still lands inside a swept window.

### Frontend consumers

The movie and series focus pages render the projection through `composables/useTitleStatus.ts`: `MovieStatusCard`, `SeriesStatusCard`, `AcquisitionControl`, `SeriesAcquisitionControl`, and the series episode grid. `lib/titleStatus.ts` holds the one state → label/icon/variant table, and nothing else. Live progress arrives through `realtime/titleProgress.ts`, a store fed by `title_progress`, never the query cache.

**The line held is title vs. job vs. file.** Job status (`downloads/statusConfig.ts`) and file status (the local-files table) stay outside the projection. They are different concepts that happen to share vocabulary, and folding them into the projection would be the same conflation in reverse.

Two grant rules follow from rendering one source:

- **Tier choice follows `actions[].tiers`.** A surface offers exactly the tiers the server lists, so a viewer without `requests.create:series:4k` is never offered 4K and cannot collect a 403 for picking it.
- **Quality choice and autonomy are different questions.** In the track dialog, quality follows the tiers offered; autonomy is operator policy and follows `jobs.manage`.

### Known gaps

- **The status card does not show the release name to operators.** Showing it would mean re-joining the jobs cache in a component the projection frees from it, so it belongs on an operator surface — `AttentionCard` or the Downloads page, which both carry it.

### List surfaces get the same type, delivered differently

**Unbuilt.** No list endpoint returns `TitleStatus[]` yet; rails, search, library, and the request list use their own shapes.

A browse page with fifty posters cannot subscribe to fifty topics, and does not need per-tick progress in a grid.

**List endpoints return `TitleStatus[]`, batch-computed, refreshed by notify-and-refetch.** The shape is identical to what the focus page receives; only the delivery differs. Identical shape is the entire point — it is what makes the poster chip and the hero chip unable to disagree, since they render the same type from the same producer.

This also resolves the request-list N+1, where each row fetches full media detail for a title and a poster.

### Keeping it correct

Two mechanisms, deliberately overlapping:

1. **Explicit invalidation.** Services that mutate acquisition state announce the affected media item. Greppable, immediate, and easy to forget.
2. **A sweep.** Every 30s, `titlenotify` asks which media items had acquisition-relevant rows change since its last watermark and kicks each of them. It does not recompute or diff the projection: a kick is cheap, and the client's refetch is what applies the lens.

The sweep is what makes this robust. A missed call site becomes a bounded latency, not a permanently wrong UI — and a missed call site is *precisely* the class of defect that motivated this model. Its watermark starts at process start, so after a restart it covers work that keeps writing rows (running downloads), not changes made while the process was down; clients refetch on reconnect for those.

## Dependencies

- **Recipient scoping in the broker.** Recipients are `User(id)` / `Capability(key)` / `Broadcast`, resolved once per session at attach. Both title events target `library.read`, which is what lets a requester see progress move without the operator payload going with it.
- **A TMDB-keyed read.** `TitleStatusService` resolves `(tmdbId, mediaType)` to the media item (`GetMediaItemByTmdbIDAndType`), then its wants, files, and jobs (`ListDownloadJobsByTmdbMovieID` for movies).
- **Title identity on kicks.** Callers announce a media item id; `titlenotify` resolves it to `(mediaType, tmdbId)` before emitting, so no client-side map is needed.
- **A unified tracking cache key.** Open: the movie/series `trackingByTmdb` key split still exists on the frontend.

## Known limits

- **No score is persisted per file.** `file_origin` caches the quality bin triple, but the custom-format score exists nowhere on disk. So `library.best` can express bin-based comparison and **cannot** express score-based upgrade eligibility without recomputation. This is the concrete blocker under [upgrades and divergent tiers](../../stories/06-upgrades-and-divergent-tiers.md), and it is narrower than "upgrades are unbuilt."
- **`tracking.quality_profile_id` is single-valued.** Divergent tiers across requesters are deferred at the schema level. `viewer.intent.tier` can report what a viewer asked for; the system cannot yet act on two different answers.
- **Effective scope is not materialized.** It is recomputed live from surviving requester associations. The projection infers scope from the presence of a want at read time — correct, and worth knowing before someone adds a cache.
- **Spelling.** `want`, `tracking`, and `request` use `canceled`; `download_job` and `import_task` use `cancelled`. The projection is a new type and normalizes to **`canceled`**, matching the majority and the domain models. Noted so it is not "corrected" back.

## Requirements this satisfies

Traceability to the [stories](../../stories/README.md). This is the design half of the link; the test half is a test named for the id.

| Requirement | How |
| --- | --- |
| `REQ-SEARCH-002` — state stays stable while the want cycles | `searching` collapses the `pending`↔`searching` oscillation |
| `REQ-SEARCH-003` — distinguish "working" from "stuck" | `active` is orthogonal to `state`; `unavailable` is distinct from `searching` |
| `REQ-APPROVE-012` — know a choice needs approval before committing | `Action.requiresApproval` |
| `REQ-SERIES-007` — see the size of what you are asking for | `Action.effect.bytesEstimate` (**unbuilt**) |
| `REQ-SERIES-008` — series progress without composing per-episode state | `counts` and per-episode `state` are server-computed; per-season rollups are **unbuilt** |
| `REQ-UNION-010` — only hear about what your intent covers | Per-viewer computation (**unbuilt:** delivery is scoped to `library.read`, not to intent) |
| `REQ-UNION-011` — not told who else wants a title | Requester identity is never in the payload |
| `REQ-UNREL-*` — "not out yet" is distinct from "can't find it" | `unreleased`, distinct from `searching` (episodes by air date, movies by their stored home-release date; `obtainableAt` **unbuilt**) |

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
| [Requests](../requests/README.md) | Request lifecycle folds into `state` and `actions[]`, so a focus page learns of approve/deny/cancel without a `request_updated` row event |
| [Tracking](../tracking/README.md) | Autonomy dial produces `needs_pick` / `proposed`; requester associations produce `viewer.intent` |
| [Quality profiles](../quality-profiles/README.md) | Supplies bins and cutoff for `library.best` and upgrade eligibility |
| [Notifications](../notifications/README.md) | Sibling. Same underlying events, different bus and different guarantees |
| [Users](../users/README.md) | The grant set that produces `actions[]` |

## Open questions

1. **Coalescing interval.** Long enough to collapse a season-pack burst, short enough that a single transition feels immediate. **Lean:** around 250ms, measured against the import burst rather than guessed. — *Answered by the code: `titlenotify` flushes every 250ms.*

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
