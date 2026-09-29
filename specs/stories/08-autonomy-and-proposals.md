# Story 8 — Who picks the release: autonomy and proposals

**Status:** Draft

Scope decides *which* things are wanted. **Autonomy decides who picks the release that fulfills each want** — the system, or a person. [Tracking § acquisition autonomy](../modules/tracking/README.md#acquisition-autonomy--who-picks-the-release) owns the model and the three behaviors; this story owns what it feels like to use, and the requirements that follow.

The framing that spec sets out is a **trust ladder**:

> `manual` is "just tell me what I'm missing." `propose` is "show me your work first." `auto` is "I trust the system."
> Users start low, watch the system's picks match what they'd have chosen, and graduate.

**The ladder is built. Nothing starts on it.** Every tracking created through the normal request-and-approve path lands on `auto` for both segments, from a hardcoded literal — there is no admin-configured default to come from. New installs begin at full automation and can only step *down*, per title, by hand. That inversion is the central gap this story records.

## Cast

- **Sam** — the operator. Runs the instance, holds job-management authority.
- **Robin** — a requester. Has no autonomy controls and shouldn't.
- **The system** — Arrflix.

## Preconditions

- Indexers and a downloader are configured and healthy.
- Sam holds the operator capability; Robin does not.

## Flow — Part A: Sam adds a series on the middle rung

### Phase 1 — Choosing a posture (T+0)

**User-visible (Sam):**

- Sam adds a series he cares about the quality of. The add dialog asks three separate questions: how much of it he wants, how the back catalogue should be acquired, and how new episodes should be.
- He picks **Suggest first** for the back catalogue and **Automatic** for new episodes.

**The product decision:** back catalogue and new episodes are asked separately because the trust economics genuinely differ. Backfill is a batch commitment — season packs, tens of gigabytes, quality trade-offs across a whole run. Ongoing is a low-stakes drip where a bad pick costs one episode. `backfill: manual, ongoing: auto` — *"I'll curate the back catalogue myself; keep me current automatically"* — is the canonical mixed posture, and it is a thing the \*arr ecosystem cannot express at all.

Sam is not asked how much he trusts the system in the abstract. He's asked two concrete questions about two different kinds of risk.

### Phase 2 — A proposal arrives (T+ minutes)

**User-visible (Sam):**

- On the series page, a **Needs your attention** panel appears with a suggested download: the release name, its quality badge, size, seeders, and — because it's a season pack — that it covers eight episodes. Two buttons: **Approve** and **Decline**.
- The affected episodes show a **Suggested** pill in the ledger. They haven't stalled; they're waiting on him.

**Behind the scenes:**

- `propose` runs the *entire* `auto` path — same schedule, same gating, same scoring, same winner. It stops one step short of grabbing, and parks the winner as a proposal against the wants it covers.
- A proposal is **per pick, not per want**. One season pack proposes once and names the eight wants it would satisfy.

### Phase 3 — Sam approves (T+ minutes)

- He taps **Approve**. Toast: *"Approved — grabbing now."* The pills flip from Suggested to the normal acquiring state.
- Approving grabs the exact release that was proposed, through the identical path `auto` would have taken. No second search, no re-scoring — the pick was already made and ratified.

### Phase 4 — Sam declines a different one (T+ hours)

- A later proposal is a release Sam doesn't want. He taps **Decline**. Toast: *"Declined — searching for a different release."*
- The wants return to searching. That release is excluded from future picks for those wants, so the next search cannot re-propose the thing he just rejected.

**Decline means "not this one," never "give up."** This is the behavior that makes `propose` safe to sit in — a decline costs nothing but a little time, so there's no pressure to accept a mediocre pick.

### Phase 5 — A better release appears (T+ hours)

- If a later search finds a strictly better candidate while a proposal is still open, the open proposal **updates in place**. Sam sees one current suggestion, never a stack of stale ones to work through.

## Flow — Part B: Robin requests a movie

### Phase 6 — The request goes through the queue

**User-visible (Robin):**

- Robin requests a movie. She chooses a quality tier. **She is not asked about autonomy, and should not be** — how a release gets picked is operator policy, not requester preference.

**User-visible (Sam):**

- The request appears in his approval queue. He approves it.

**What actually happens — and the gap:**

- The tracking is created on **`auto` / `auto`**, because that is hardcoded into the approval path. Sam had no field to choose otherwise, and no configured default was consulted, because none exists.
- Sam's only route to a different posture is to approve first, then find the title and change it after the fact — and for a **movie**, no interface offers that at all.

**Not built today,** and the three pieces are separable:

1. **Admin-configured defaults.** The tracking spec says *"defaults come from admin-configured tracking defaults."* The per-tracking override shipped; the defaults did not. There is no setting anywhere.
2. **Approve-time autonomy.** The approval path deliberately ignores any autonomy the requester's submission carried, on the correct reasoning that it's the operator's call — but never gives the operator the call.
3. **Movie autonomy in the interface.** The backend is fully media-agnostic: movies segment, hold, propose, and render `Suggested` / `Needs your pick` states correctly. **Only the frontend is series-only.** Movie autonomy is a dialog away, not a feature build.

## When a default is required

Three paths start an acquisition, and only one of them needs a default at all:

| Path | Who chooses the autonomy |
| --- | --- |
| An operator adds a title directly | The add dialog asks |
| An operator approves a request | The approve dialog asks (per REQ-AUTO-002) |
| **A request is auto-approved** | **Nobody is asked** |

A default is therefore not "the instance's posture." It is narrower and more precise: **what happens when acquisition begins with no human in the loop.**

That narrowing settles the value. Auto-approval means *"this requester is trusted — don't make me look at this."* A tracking born on `propose` would then park a proposal in the operator's attention panel, moving the decision from approval time to pick time rather than removing it — and making it worse, since "is this the right release" is a poorer question to hand someone than "should Robin get this movie." **Auto-approve plus propose is the approval queue with extra steps.**

The principle generalizes past that one path:

> **Automatic for what was asked for. Ask about what the system decides on its own initiative.**

A requester who chose scope `all` asked for the back catalogue as surely as for next week's episode. Backfill and ongoing are both consented-to, so both acquire automatically. The class nobody asked for is **upgrades** — the system spending bandwidth and disk on its own judgment about quality — and that is the class to ask about. So the principle maps onto exactly two values: acquisition `auto`, upgrades `propose`.

Upgrade volume is bounded by the profile's **cutoff**, not by library size: a title stops being upgrade-eligible once its file is at or above cutoff, so the eligible set is "things acquired below cutoff" and it drains as they improve. A configurable anti-flapping delta bounds churn within a bin. The failure mode to watch is an *aspirational* cutoff — set it to the best bin that exists and nothing ever reaches it, so every title stays permanently eligible and the attention panel becomes noise. That is a configuration problem with configuration levers, not something a default should be compensating for.

Defaulting upgrades to `none` was considered and rejected. It contradicts the principle — `propose` **is** asking; `none` is not even asking — and it fails the case that motivates upgrades most: someone who grabbed a poor release because it was all that existed does not know upgrade-watching is a setting, so an opt-in default means they are never offered the better copy.

**Cost is not the autonomy lever.** If the concern is a requester pulling an eighty-episode back catalogue, the control is which scopes they may request — not making an operator approve eight season packs one at a time. Gate what can be *asked for*; don't gate what has already been granted. Approving packs is slow, per-title, and ends in rubber-stamping.

**Two jobs share one word.** A "default" can be the value a dialog *starts* on — convenience, no correctness weight — or the value used when nobody is asked, which is load-bearing. They may be the same setting, but only the second one must exist.

## The three rungs

| Behavior | What the system does | What the user does | What they see |
| --- | --- | --- | --- |
| **Automatic** | Searches, gates, scores, picks, grabs | Nothing | Normal acquisition status |
| **Suggest first** | Everything except the grab | Approves or declines one pick | A **Suggested** pill and an attention panel |
| **I'll pick** | Creates and tracks the want; never searches it | Runs an interactive search and picks | A **Needs your pick** pill |

`manual` is emphatically **not** Sonarr's unmonitored flag, and the difference is the whole argument. An unmonitored episode is *invisible* — no ledger entry, no status, no reminder that it's missing. A manual want is a first-class work item: it appears in the have/lack ledger, renders a pill, and asks to be dealt with. Manual is also not paused — the ledger stays current, wants keep being created, they just never get searched.

## Postconditions

**After Part A:** one tracking with different autonomy per segment; the approved pack's wants acquired through the same machinery an automatic grab uses; the declined release excluded for the wants it targeted; at most one open proposal per want set at any moment.

**After Part B:** one tracking on `auto`/`auto` that nobody chose, and a movie whose posture cannot be changed through the product.

## What must be true (foundation requirements)

Requirements carry stable ids per [the stories contract](./README.md#conventions). Those describing behavior the system does not currently exhibit are marked.

### The ladder has a bottom rung

- **REQ-AUTO-001** — A tracking created without a human choosing its autonomy must take that autonomy from operator-configured defaults, not from hardcoded values. The defaults are **acquisition `auto`, upgrades `propose`**, per [when a default is required](#when-a-default-is-required). **Not currently true:** the approval path passes `auto`/`auto` as literals, upgrade behavior is hardcoded to `none` and never read, and no default setting exists anywhere.
- **REQ-AUTO-002** — An operator approving a request must be able to choose that tracking's autonomy as part of approving. **Not currently true:** the approval path has no autonomy input. This is the missing rung behind "approve and pick the release myself" — approving into `manual` or `propose` *is* that feature, and needs no new acquisition machinery.
- **REQ-AUTO-003** — Autonomy must be settable for movies through the interface, not only for series. **Not currently true:** the backend is media-agnostic and the movie status card already renders proposal and needs-pick states, but no movie surface can set the dial — so those states are unreachable.

### A held want is a visible want

- **REQ-AUTO-004** — A want held for a manual pick must remain fully visible in the ledger with its own status, never hidden or silently skipped. *Currently true* — and it is the core product claim distinguishing `manual` from an unmonitored flag, so it warrants an explicit test.
- **REQ-AUTO-005** — A want awaiting a manual pick must offer the pick **in context**, on the surface that says it's waiting. **Not currently true for movies:** the movie status card renders "waiting for a release to be chosen" with no control attached; the series episode row correctly renders a Download button when its want is held.
- **REQ-AUTO-006** — A want held under manual autonomy must never be searched or grabbed autonomously, including after a retry, a failed download, or a re-arm. *Currently true*, but enforced by re-applying the hold at four separate call sites — a fifth path added later would leak a manual want into automatic search without any test failing.

### Proposals are decisions, and decisions leave records

- **REQ-AUTO-007** — Approving a proposal must record who approved it and what was approved. **Not currently true:** the proposal is deleted on approval and no provenance survives. Requests record their decider; proposals do not. The trust ladder's premise is that a user reviews the system's track record before graduating — with no record, there is nothing to review.
- **REQ-AUTO-008** — Declining a proposal must exclude that release for the wants it covered and resume searching. *Currently true* — and load-bearing, since it's what makes declining cheap.
- **REQ-AUTO-009** — At most one proposal may be open per want set; a strictly better candidate supersedes it in place rather than stacking. *Currently true.*
- **REQ-AUTO-010** — Approving a release that has become ungrabbable must not silently produce a failed download. **Not currently true:** approval replays the stored pick without re-validating, so a dead release approved becomes a download that fails and recovers through normal retry. The tracking spec calls for re-searching and re-proposing instead.
- **REQ-AUTO-011** — Proposals must be discoverable without visiting each title's page. **Not currently true:** proposals surface only in-context per tracking, so an operator running several titles on `propose` has no way to find what's waiting.

### Autonomy is operator policy

- **REQ-AUTO-012** — Setting autonomy must require operator authority. Requesters must never be offered the control. **Partly true:** changing autonomy after the fact requires operator authority, but setting it at creation is gated on holding auto-approval for the request instead — a different gate for the same capability.
- **REQ-AUTO-013** — Each behavior must be named the same way everywhere it appears. **Not currently true:** the picker says *Automatic / Suggest first / I'll pick*; the summary elsewhere says *Automatic / Suggested / Manual*.

## Out of scope (variant stories)

- **Upgrades** — the third class of want-work, carrying its own dial. [Reserved as story 06](./05-multi-requester-scope-union.md); whether its vocabulary unifies with autonomy's is an open question in [tracking](../modules/tracking/README.md#open-questions).
- **The interactive search itself** — how candidates are listed, previewed, and grabbed. Owned by [acquisition](../modules/acquisition/README.md).
- **Approval of the request** — whether a request is granted at all. That's [pending approval queue](./03-pending-approval-queue.md); this story starts once acquisition is authorized.
- **Scope** — which atoms are wanted. Orthogonal by design; "skip the back catalogue" is a scope choice, not an autonomy one.
- **Quality profiles** — what counts as a good release. Autonomy decides *who ratifies* the pick, not how it's scored.

## Open questions

1. **What should the default posture be? — resolved.** Acquisition defaults to `auto`; upgrades default to `propose`. A default only applies where no human was asked, which is the auto-approve path — and on that path `propose` for acquisition would contradict the auto-approval that just happened. Upgrades are the one class nobody requested, so they are the one to ask about; cutoff bounds how often that asking happens. This matches [quality profiles § upgrade detection](../modules/quality-profiles/README.md#upgrade-detection), which already named `propose` the recommended default. Per-segment *defaults* are unnecessary; the per-segment per-tracking **override** remains, since `backfill: manual, ongoing: auto` is still a posture worth expressing. Captured in REQ-AUTO-001.

2. **Should defaults be global, per-library, or per-requester-group?** A movies library and a shows library plausibly want different postures. "Requests from this group always propose" is a third axis that arguably belongs to permissions rather than to tracking defaults. **Lean:** global first; per-library if demand appears.

3. **Should an operator be able to constrain which scopes a requester may choose?** This is the cost lever that autonomy defaults should *not* be doing — limiting a requester to `future_only` prevents an eighty-episode back catalogue at the point of asking, rather than burying an operator in pack approvals afterward. Probably a permissions question rather than a tracking one, but it needs an owner. Currently a requester may choose any supported scope.

4. **Does approving into `manual` make sense as the approve-and-choose flow, or should approval open the release picker directly?** Approving into `manual` is the composable answer and needs no new machinery. Opening the picker inline is fewer taps but couples two features. **Lean:** approve into `manual` first, add the inline shortcut later if the extra step annoys.

5. **Should a proposal expire?** A proposal nobody acts on holds its wants indefinitely — the title quietly never arrives. Options: expire and fall back to `auto`, expire and re-propose, or nag. **Lean:** don't expire, but surface age in the attention panel; silently grabbing something the operator declined to approve would betray the mode.

6. **Who receives a proposal when several operators exist?** Today it's discoverable by anyone who visits the title. With a proper inbox (REQ-AUTO-011) it needs an audience rule. **Lean:** all operators see all proposals; first to act wins.

7. **Does the ledger distinguish "waiting on you" from "waiting on the system"?** Both `Suggested` and `Needs your pick` mean a human is the blocker, while searching and downloading mean the system is. A single count of "things waiting on me" across all trackings would make `propose` and `manual` viable at scale. Related to REQ-AUTO-011 and probably the same surface.
