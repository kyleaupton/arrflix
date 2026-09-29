# Stories — what the system does, from the user's side

A story is a narrative walkthrough of one flow, told from the user's point of view, with the product decisions and open questions that flow implies. Stories are the **product** layer: they answer *what should happen and why*, not *how it's built*.

## What a story holds — and what it doesn't

Stories rot in proportion to how far ahead of implementation they run. A story written beside the code that implements it stays accurate; a story that invents a design for an unbuilt subsystem is a guess that will be wrong. The split below is what keeps that from happening.

| Layer | Artifact | Holds |
| --- | --- | --- |
| Product intent | **Story** (here) | User-visible flow, product decisions, tradeoffs, open questions |
| Requirement | **Story**, `What must be true` | "It must be true that X" — with a stable ID |
| Verification | **Test** named for the ID | Whether X is actually true |
| Design | [Module spec](../modules/) | Entities, endpoints, events, workers |
| Rationale | Module spec / story | Why this design over the alternatives |

**Do not pin mechanism in a story.** Endpoint shapes, column names, event names, and worker triggers belong to the module spec that owns them — link, don't restate.

**Use the entity name; don't describe its columns.** `want`, `tracking`, `proposal`, `download job` are ubiquitous language — say them. Circumlocutions like "unit of work" are not more durable, just harder to read, and they hide which entity a requirement is actually about. The line is between *naming* a thing and *specifying its shape*:

- ✅ "Cancelling a want must not cancel a download job that also serves other wants."
- ❌ "`CancelWant` must check `ListWantsByDownloadJob` before cancelling."

State a requirement as a requirement, not as a design:

- ✅ "The page must know the viewer's tier before rendering the CTA."
- ❌ "A `user_policy` table with a `can_request_movie` boolean."

The second one was in Story 1. The table was dropped; the requirement is still true.

## Conventions

**Cross-reference by title, not number.** Write "[Multi-requester scope union](./05-multi-requester-scope-union.md)", never "Story 5". Numbers drift — before the triage pass, story 01's out-of-scope list pointed at three *wrong* stories, and no code had changed to break it. Numbers order the reading; titles carry the reference.

**Numbers are reserved on first mention.** If a story says a topic is "covered by story NN", that number is claimed. Check before reusing.

**Requirement IDs.** Requirements under `What must be true` carry a stable `REQ-<AREA>-<NNN>` id:

```
REQ-CANCEL-003 — A non-final requester's withdrawal must not cancel wants
                 that other requesters still justify.
```

The id is permanent once assigned; renumbering breaks the link to its test. Retire an id (mark it withdrawn) rather than reusing it. Areas are short topic slugs (`CANCEL`, `UPGRADE`, `AUTONOMY`, `REALTIME`), not module paths.

Requirements are being added **gradually**, as stories are written or revised. A story without ids isn't wrong, just not yet converted.

**An open question is "resolved" only when the resolution is implemented, or specified in a module spec.** It is not resolved because another story asserts it. Before the triage pass, three stories each marked a question resolved by pointing at a notifications resolution lifecycle that does not exist in code — and each cited the others as evidence it was settled. That is how a doc set convinces you a problem is solved when nobody solved it.

**Mark unbuilt things as unbuilt.** A story describing a future flow is fine and expected — that's what stories are for. Silently describing it in the present tense is what makes the set untrustworthy.

## Index

Status reflects a drift audit of each story against the code, and describes **the document**, not the feature.

| Story | Status | Notes |
| --- | --- | --- |
| [01 — Happy path, auto-approve](./01-happy-path-auto-approve.md) | Triaged | Rewritten to this contract. Carries `REQ-FULFIL-*` ids; requirements owned by other stories are linked rather than restated. The media-server tail is marked unbuilt. |
| [02 — Series mid-season, auto-approve](./02-series-mid-season-auto-approve.md) | Triaged | Rewritten to this contract. Carries `REQ-SERIES-*` ids. Structural claims held; grouped notifications, aggregate progress, pre-flight sizing, and auto-archive are unbuilt. |
| [03 — Pending approval queue](./03-pending-approval-queue.md) | Triaged | Rewritten to this contract. Carries `REQ-APPROVE-*` ids. Decision mechanics shipped; **nobody is notified of anything** — not the reviewer, not the requester. |
| [04 — Failed search, eventual recovery](./04-failed-search-recovery.md) | Triaged | Rewritten to this contract. Carries `REQ-SEARCH-*` ids. Its premise moved: it now begins *after* a title is obtainable, handing the earlier window to [Asking for something that isn't out yet](./09-not-out-yet.md). |
| [05 — Multi-requester, scope union](./05-multi-requester-scope-union.md) | Triaged | Rewritten to this contract. Carries `REQ-UNION-*` ids. The join/union/narrow spine is real; per-requester intent is not editable, departure never reconciles, and notification audience ignores scope. |
| [06 — A better release appears: upgrades and divergent tiers](./06-upgrades-and-divergent-tiers.md) | Current | Carries `REQ-UPGRADE-*` ids. Almost entirely unbuilt — the quality comparators exist and are tested but have no callers. Includes a live defect: a request at a higher tier reports success and changes nothing. |
| [07 — A requester cancels](./07-requester-cancels.md) | Current | First story written to this contract. Carries `REQ-CANCEL-*` ids; three are confirmed defects. The UI-facing requirements have not been verified against the frontend. |
| [08 — Who picks the release: autonomy and proposals](./08-autonomy-and-proposals.md) | Current | Companion to [tracking § acquisition autonomy](../modules/tracking/README.md#acquisition-autonomy--who-picks-the-release), which owns the model. Carries `REQ-AUTO-*` ids; the trust ladder is built but nothing starts on it. |
| [09 — Asking for something that isn't out yet](./09-not-out-yet.md) | Current | Carries `REQ-UNREL-*` ids. Series defers correctly to air date and is the model; the movie path does not, and polls indexers daily for months for films still in cinemas. |
| [10 — Indexer health, degraded and recovery](./10-indexer-health-degraded-and-recovery.md) | Aspirational | Essentially no implementation behind it. The story's own caveat — that it can't be specified until indexers have a defined model — still holds. |

**Reserved but unwritten:** none. Next numbers are free — check for a prior claim before reusing one.

### Known spec/code divergences

Where a module spec describes behavior the code does not implement. Listed so the gap is visible rather than implied — each is a candidate requirement, not an accepted design.

- **Admin-configured autonomy defaults** — [tracking](../modules/tracking/README.md#who-sets-it) says defaults come from operator configuration. The per-tracking override shipped; no default setting exists, so every tracking lands on `auto`. See [Autonomy and proposals](./08-autonomy-and-proposals.md).
- **Proposal provenance** — [tracking](../modules/tracking/README.md#propose-mechanics) says an approved proposal is recorded with its approving user. Approval deletes the proposal and records nothing.
- **Approve-time re-validation** — the same section says approval re-validates a release and re-proposes if it died. Approval replays the stored pick blindly.
- **The notifications resolution lifecycle** — `Resolve(dedup_key)`, `resolvable`, resolution state. Referenced as load-bearing by [tracking](../modules/tracking/README.md#propose-mechanics) and by three stories; **does not exist in code**. This is the phantom-resolution case the conventions above warn about.

## Spelling

The codebase spells it **`canceled`** (one L). Prose in older stories and specs uses `cancelled`. Prefer the code's spelling when naming a status value; either is fine in ordinary prose.
