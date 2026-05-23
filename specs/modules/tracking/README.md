# Tracking — the ongoing-intent primitive

**Status:** Draft, iteration 1

This doc defines **tracking**: how Arrflix models the ongoing intent to keep a series current. It captures _what tracking is_, _why it's its own primitive_, _what it owns_, and _how it interacts with the rest of the system_. It does **not** pin down table names, columns, or wire formats — those come in a later iteration once we've pressure-tested the model against more user stories.

## TL;DR

- Three distinct primitives — **request**, **tracking**, **want** — with separate lifecycles and responsibilities.
- **Tracking** is per-series, holds the ongoing config (scope, quality, upgrade behavior, schedule). It is the _producer_ of wants.
- **Wants** are the leaf-level work items (one per movie file, one per episode file). They flow through the pipeline defined in [Story 1](../../stories/01-happy-path-auto-approve.md).
- **Requests** are the (optional) user-facing approval layer. A request may spawn tracking (for series) or a direct want (for movies).
- Multi-user safe: one tracking record per series, requesters union their scopes, scope narrows on departure only if no one else needs it.
- Smart scheduling is the differentiator — search frequency is biased by time-since-air, not a fixed RSS interval.
- Auto-archives when a series is `Ended` upstream and all wanted episodes are acquired.

## Why a separate primitive

Sonarr models monitoring as flags on rows: `series.monitored`, `season.monitored`, `episode.monitored`. It works, but it has well-known limitations:

1. **Provenance is lost.** Once a flag is set, you can't tell _why_ something is monitored or _who_ asked for it. Was it admin-initiated? A user request? An automatic rule? Sonarr doesn't know.
2. **"Series intent" and "file intent" are conflated.** "I want this series" (ongoing, indefinite) and "I need this episode file" (concrete, one-shot) have very different lifecycles. Flagging them the same way works but obscures the difference.
3. **Multi-user is awkward.** If two people both want a series, with different scope preferences, the flag model can't represent that.
4. **Completion is manual.** When a series ends and you've acquired everything, you have to manually unmonitor. Sonarr keeps polling forever otherwise.

Splitting into three primitives — request, tracking, want — gives each concern a clear home:

| Primitive    | Concept                                                       | Lifecycle                                                                                     |
| ------------ | ------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| **Request**  | "a user asked for this; gated by approval"                    | `pending → approved → fulfilled` _or_ `pending → denied`                                      |
| **Tracking** | "ensure this series stays current per these rules"            | `active / paused / archived / canceled`                                                       |
| **Want**     | "acquire this specific atom" (one movie file, or one episode) | `pending → searching → grabbed → downloading → imported → available` (+ `failed`, `canceled`) |

For movies, you typically have **optional request → want**.
For series, you typically have **optional request → tracking → wants per episode**.
Admin-added content skips the request layer.

## What tracking owns

A tracking record encodes five concerns:

1. **What is being tracked** — a reference to the media item (a series). Tracking for movies is the rare exception, see [Movies under this model](#movies-under-this-model).
2. **Scope** — which episodes count as "wanted." See [Scope: rule + overrides](#scope-rule--overrides).
3. **Quality profile** — which profile applies. See [the quality system](#interactions) for how this resolves to actual releases.
4. **Upgrade behavior** — `auto` (replace existing files when a better release appears), `propose` (queue an upgrade as a [notification](../notifications/README.md) for one-tap approval), or `none`.
5. **Schedule strategy** — `smart` (time-since-air bias, default) or `fixed` (poll every N minutes regardless).
6. **Requesters** — the set of users who have asked for this. Drives both lifecycle (zero requesters → paused) and scope (union of requested scopes). Admin-initiated tracking lists the admin as the sole requester.
7. **State** — `active / paused / archived / canceled`, plus a reason + timestamp on the last transition for auditability.

## Scope: rule + overrides

Scope answers "which episodes does this tracking want?" The model is **one rule + per-episode overrides**:

**Preset rules** (cover ~95% of cases):

| Rule                        | Meaning                                           |
| --------------------------- | ------------------------------------------------- |
| `all`                       | Every episode that exists or ever will exist      |
| `future_only`               | Episodes airing after the tracking was created    |
| `season(N)`                 | Only season N                                     |
| `pilot`                     | Only the first episode                            |
| `latest_season_plus_future` | The most recently aired season + everything after |

**Per-episode overrides** handle the long tail ("I want all of S1 and S3+, but not S2"):

- Explicit include — force-include an episode the rule would exclude
- Explicit exclude — force-exclude an episode the rule would include

**Composition:** an episode is wanted iff `rule says yes` _and not_ explicitly excluded, _or_ explicitly included. Rule + overrides is universal — even `rule: all` + a long exclude list expresses anything.

We deliberately avoid a full rules DSL. Five presets + overrides is enough; richer rules can be added later as new presets if real demand emerges.

## Lifecycle

```
                ┌──────────┐  user pauses / last requester leaves
                │  active  │ ─────────────────────────────────────┐
       ┌───────►│          │◄─────┐                              │
       │        └─────┬────┘ user │                              ▼
   new │              │ resumes   │                        ┌──────────┐
episode│       all    │           └──────────────────────  │  paused  │
upstream       wanted │ episodes acquired                  └─────┬────┘
       │       AND series.status=Ended                           │ user
       │              ▼                                          │ cancels
       │        ┌──────────┐                                     ▼
       └────────│ archived │                              ┌──────────┐
                └─────┬────┘                              │ canceled │
                      │ user cancels                      └──────────┘
                      └─────────────────────────────────────► (terminal)
```

**State semantics:**

- **`active`** — searches run, wants are generated as episodes are wanted but not yet acquired.
- **`paused`** — no new searches, no new wants generated. Existing in-flight wants continue (they belong to the want lifecycle, not tracking's). User can resume.
- **`archived`** — completion criteria met (TMDB marks series `Ended` _and_ all wanted episodes are at-cutoff quality). No further action expected, but state is retained for queries ("show me everything I'm tracking") and for the `archived → active` re-activation path if a new episode appears upstream.
- **`canceled`** — terminal. User explicit delete. In-flight wants are also canceled.

**Auto-transitions:**

- `active → archived` when (TMDB series ended) AND (all in-scope episodes acquired at or above cutoff).
- `archived → active` when TMDB sync detects a new episode that the scope rule includes.
- `active → paused` when the last requester unsubscribes from non-admin tracking. Admin tracking does not auto-pause.

**Manual transitions:** any user with appropriate permissions can pause / resume / cancel.

## Multi-requester semantics

When two users both want the same series, we use one tracking record, not two.

- **Requesters set** — every user who currently wants this series is in the set. Admin-initiated tracking has the admin in the set.
- **Effective scope** — the union of each requester's individual scope. Conceptually each requester has their own scope; tracking acts on the union. (Whether each requester's scope is stored separately or only the union is stored is a later-iteration decision; the _semantics_ are union, the _storage_ can collapse if we accept that narrowing-on-departure requires us to recompute.)
- **Joining** — a user requests a series that's already tracked → they're added to the requesters set; effective scope widens to include their scope.
- **Leaving** — a user cancels their request → removed from the set; effective scope narrows to the union of remaining requesters' scopes (no narrowing if their scope was already covered by others).
- **Zero requesters** — tracking moves to `paused` (not `canceled`, so admins can revive it). For purely admin-initiated tracking with no human requester, this only happens if the admin explicitly removes themselves; otherwise tracking is admin-anchored.

This avoids:

- Redundant searches (two tracking records for the same series)
- The "Alice canceled and now nobody is monitoring this" footgun
- The inverse: "Bob canceled but Alice still wants it" silently dropping Alice's data

## Smart scheduling

Sonarr's `rss_sync_interval` polls every X minutes regardless of when episodes air. We can do better: bias search frequency by **time-since-air**.

Default schedule strategy (`smart`), per in-scope episode:

| Time since air     | Search frequency                             | Rationale                                   |
| ------------------ | -------------------------------------------- | ------------------------------------------- |
| Not yet aired      | None                                         | Nothing to find                             |
| 0–1h after air     | Low (~hourly)                                | Releases rarely up yet                      |
| 1–6h after air     | High (every ~15 min)                         | Peak release window for most release groups |
| 6–24h after air    | Medium (~hourly)                             | Late drops                                  |
| 1–7d after air     | Low (~6 hours)                               | Catch-up window                             |
| >7d after air      | Very low (~daily), then exponential back-off | Cold case                                   |
| Acquired at cutoff | None                                         | Done                                        |

Refinements that fall out naturally:

- **Per-series learning** — record observed "release lag" per series; bias the peak window accordingly. A show that always lands 3h after air gets searched at 3h, not blindly at 1–6h.
- **Indexer health gating** — don't bother searching while all relevant indexers are unhealthy; resume on recovery.
- **Failure back-off** — on consecutive empty searches for the same episode, exponential back-off within each frequency tier.

Tracking _declares_ its schedule strategy. The search scheduler _implements_ it. Tracking doesn't run searches; it's just the configuration root.

`fixed` strategy exists for users who want predictable behavior (or for debugging). It does what Sonarr does: poll every N minutes regardless.

## Movies under this model

In the common case, **movies do not have tracking records.** A movie request creates a want directly; the want flows through Story 1's pipeline and terminates at `available`.

Tracking _can_ exist for a movie in two cases:

1. **Upgrade watching** — user has the movie in HD, wants to be notified when a 4K release appears. A movie-tracking record with `upgrade_behavior: propose` and scope that just identifies the one movie.
2. **Pre-release monitoring** — a movie isn't out yet (theater release, no digital), user wants it grabbed as soon as a release surfaces. The tracking lives until the want is fulfilled, then auto-archives.

Both are real but uncommon. We design tracking to support them but don't require them: a movie request can short-circuit to a direct want without ever instantiating tracking.

## What tracking does NOT own

To keep scope tight, these adjacent concerns live elsewhere:

- **Search execution** — the search scheduler reads tracking config and runs searches. Tracking itself runs no code.
- **Quality scoring** — defined by [quality profiles](../quality-profiles/README.md), not tracking. Tracking just references a profile by ID.
- **Decision log** — every accept/reject the [acquisition](../acquisition/README.md) pipeline makes is logged via the [decision-artifact pattern](../../patterns/audit/README.md). Tracking is _not_ the home for that data, though tracking-generated wants link to it.
- **Notification routing** — when a tracking event matters to the user (new episode imported, upgrade available, tracking auto-archived), [notifications](../notifications/README.md) decides how to deliver it. Tracking emits the event; it doesn't route.
- **Exact data shapes** — column types, indexes, foreign keys, API contracts. Deliberately deferred to a later iteration.

## Interactions

| Neighbor                                                          | How tracking interacts                                                                                                                                   |
| ----------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **TMDB series sync** _(prerequisite, currently a foundation gap)_ | Must know every episode that exists, with air dates, to drive scope evaluation and scheduling. Without this, smart scheduling is meaningless.            |
| **Wants**                                                         | Tracking _produces_ wants when an in-scope episode lacks an at-cutoff file. The relationship is 1:N. Each want carries a back-reference to its tracking. |
| **Requests**                                                      | A series request may spawn a new tracking record or add the requester to an existing one. Movie requests typically don't touch tracking.                 |
| **Quality profile**                                               | Referenced by ID. Tracking doesn't define profiles, just selects one.                                                                                    |
| **Decision log**                                                  | Read-only consumer in the UI: "for this tracked series, here's why nothing has grabbed in the last 24h."                                                 |
| **Watch state** _(future)_                                        | Optional input — "stop tracking once I finish the series" requires Plex/Jellyfin webhook integration. Deferred until that integration lands.             |
| **Library scan**                                                  | Existing media files satisfying an in-scope episode mean no want needs to be created. Tracking respects what scan finds.                                 |

## UI naming

Internal model name: **tracking**.
User-facing UI strings will likely say **"monitoring"** or **"following"** depending on context — those are localization/UX choices, not model concerns. Stay consistent in code with `tracking`; let the UI layer pick the right word.

## Open questions

1. **Scope storage shape.** Union-only (single computed scope on tracking) vs. per-requester (each requester has their own scope, union is computed). Per-requester is correct semantically; union-only is simpler if we accept recomputing on join/leave. To resolve when we draft the data-shape iteration.
2. **Scope rule presets — are these the right five?** "Specific episode ranges" came up in conversation but isn't in the preset list; it's expressible via `all` + excludes but unwieldy if a user wants S05E10–S05E15. May need a sixth preset, or surface "specific episodes" in the UI even though it's just sugar over overrides.
3. **Anime numbering.** Anime has dual numbering (per-season vs absolute) and the scope rules implicitly assume per-season. Anime support is out of scope for now, but the model should at least not preclude it. Worth a sanity-check pass when anime is on the table.
4. **Per-episode overrides under TMDB renumbering.** If TMDB renumbers a series (rare but happens), per-episode overrides become wrong silently. Need to decide whether overrides key on a stable TMDB episode ID or on (season, episode) numbers — the former is robust but harder to surface in UI.
5. **"Pause on storage low"** — should tracking auto-pause when free disk is below a threshold? Probably yes, but the trigger lives in the storage-intelligence subsystem, not tracking itself. Cross-cutting concern; revisit when that subsystem is designed.
6. **Tracking deletion vs cancellation.** Is `canceled` actually deletion, or does it preserve a tombstone for "previously tracked"? Tombstones help the "you've already tried this show and removed it" UX. Probably worth keeping; flesh out in data-shape iteration.
7. **Re-activation noisiness.** `archived → active` when a new episode appears could be surprising ("why is this show downloading? I thought I was done with it"). Notify on re-activation? Require confirmation? Probably notify-then-resume by default; let users opt for confirm-then-resume.
8. **Cross-tracking dedup edge cases.** If admin tracks `season(2)` and a user requests `season(3)`, are these one tracking with scope `seasons 2-3`, or one tracking with two separate scope rules? The "rule + overrides" model assumes one rule per tracking. May need to relax to "list of rules, OR'd together" if real cases emerge.

## What we're explicitly not deciding here

- Exact table names, columns, indexes
- API endpoint shapes
- The search scheduler's implementation
- Quality profile structure
- Notification routing rules (lives in [notifications](../notifications/README.md))
- Storage thresholds, retention policies

Each of those gets its own pass (or its own spec) once this model holds up against more user stories.
