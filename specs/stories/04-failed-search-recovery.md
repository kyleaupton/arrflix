# Story 4 — Failed search, eventual recovery: the indexers don't have it yet

**Status:** Draft (triaged against the code)

Same friend as [the happy path](./01-happy-path-auto-approve.md), same auto-approve, same HD tier — but this time the film's **home release landed yesterday**, and nothing usable is on the indexers yet. The system searches, finds nothing, comes back, finds only junk, backs off, keeps trying, and about thirty hours later a decent release surfaces. It resolves normally from there.

This is the **most common non-happy path**: the request is fine, the user is fine, the system is fine — the source material just isn't out there yet. This story pins down what the user sees during that gap, what the system records about it, and how backing off and recovering actually behave.

> **Triage notes.** Two things changed.
>
> **The premise moved.** This story originally opened on a film in cinemas. That case now belongs to [asking for something that isn't out yet](./09-not-out-yet.md) — a film in theatres isn't obtainable, and the system should not be searching for it at all. Story 4 begins **after** a title becomes obtainable. The boundary is the obtainable date: before it, story 9; after it, this one.
>
> **Mechanism was removed.** Retry curves, event names, and field names have been replaced by the requirements they were serving, per [the stories contract](./README.md). Several things this story described as working are unbuilt, and are now marked.

## Cast

- **Friend** — as in [the happy path](./01-happy-path-auto-approve.md): holds the HD create and auto-approve grants, no 4K, push enabled.
- **Admin** — passive. Their HD profile **hard-rejects** cam and telesync sources rather than merely scoring them low.
- **The system** — Arrflix.

## Preconditions

- The film's home release was yesterday. It is obtainable; the question is only whether a good release exists.
- Nobody has requested it; it isn't in the library.
- Indexers are healthy — the system can see what's out there, and what's out there is bad.
- The differentiator: **at first nothing usable exists.** Junk rips appear within hours; something acceptable takes about thirty.

## Flow

### Phase 1 — Request (T+0)

Indistinguishable from [the happy path](./01-happy-path-auto-approve.md). Friend taps Request; the pill reads *"Searching for an HD release…"*.

The only difference is a quiet note on the request affordance for very recent titles — *"this only just released; a good copy may take a day or two"* — setting expectations before the wait rather than explaining it afterward. **Unbuilt.**

### Phase 2 — First wave: nothing at all (T+~15s)

**User-visible:** the pill stays as it is. Nothing to report.

**Behind the scenes:** indexers are queried and return nothing. The attempt is recorded — including that it happened and which indexers answered — so "we did try" is later provable. Another search is scheduled.

**Notifications:** none. One empty search is not news.

### Phase 3 — Second wave: junk, all rejected (T+~2h)

**User-visible:**

- The pill gains freshness: *"Searching for an HD release · last checked 2h ago"*.
- Tapping it reveals *"5 releases considered, all rejected"* and, drilling in, a per-release table — title, indexer, why it was rejected, seeders, age.

**Behind the scenes:** five cam rips return. Every one fails a hard gate before scoring even matters. Each rejection is recorded with a structured reason, tied to the search that found it.

**Notifications:** still none. Five rejected cam rips are not actionable — the user cannot make a better release exist.

> **Unbuilt.** Nothing records searches or per-release decisions today. There is no search history, no rejection reasons, no drill-down. When a want sits unfulfilled, the system cannot answer *"what have you tried?"* — which is the single most valuable thing this story asks for.

### Phase 4 — Backing off, and the long wait (T+~2h → T+~30h)

**User-visible:**

- The pill keeps its freshness marker as checks grow further apart. The requests view shows the film as **still working, not broken** — the distinction the colour choice has to carry.
- Around the one-day mark, a message appears in-app:

  > **We're still looking for _Sentinel_.** It only just released, and good copies sometimes take a day or two. We'll keep trying. *[See what we've tried]* · *[Cancel request]*

  In-app only by default — a push here would be fatigue for something the user cannot act on, and the good news is coming later anyway.

**Behind the scenes:** searches continue, spacing out as the odds of something new appearing drop.

> **Unbuilt.** There is no still-searching message, and the mechanism it was specified to use — a notification that clears itself when the situation resolves — **does not exist**. This story was one of three that treated that lifecycle as settled; see [the stories contract](./README.md#known-speccode-divergences).

### Phase 5 — Something good surfaces, normal pipeline resumes (T+~30h)

**User-visible:** *"Found a release · Queued"* → *"Downloading 8% · ~5 min"*. Back to the happy path's tempo.

**Behind the scenes:** twelve results this time; ten still junk and hard-rejected, two acceptable, the better one picked and handed to the downloader. The still-searching message clears itself — the user should not have to dismiss a problem that solved itself.

### Phase 6 — Download, import, available (T+~30h → +8 min)

Indistinguishable from [the happy path](./01-happy-path-auto-approve.md). The file is imported and verified on disk, which is what makes it available; the requester is notified.

## What the failure phase must not do

Three failure modes this story exists to rule out:

| Must not | Why |
| --- | --- |
| Give up | Nothing is wrong. The release will exist eventually, and a request that quietly died is worse than one still waiting. |
| Look broken | "Still searching" after a day is fine *if* the user can see the system is working. Without that, patience reads as failure. |
| Look identical to "not out yet" | Waiting for a date needs no attention; waiting for a good release might. [Story 9](./09-not-out-yet.md) owns the first. |

## Postconditions

- One request, frozen; one tracking, active throughout; one want, ending available.
- One download job, created only on the successful pick — not on any earlier attempt.
- A record of every search and every rejection across the thirty hours, so the wait is explicable after the fact.
- The still-searching message resolved, retained as history rather than as an open item.

## What must be true (foundation requirements)

Requirements carry stable ids per [the stories contract](./README.md#conventions). Most of [the happy path](./01-happy-path-auto-approve.md)'s requirements carry over; these are net new.

### Patience is not failure

- **REQ-SEARCH-001** — A want must never reach a terminal failed state merely because no acceptable release exists yet. *Currently true*, and worth keeping true: failure is for things that actively went wrong, not for waiting.
- **REQ-SEARCH-002** — The state shown to a user must stay stable while the want cycles internally. **Not currently true:** a want that finds nothing returns to pending and is re-claimed on the next cycle, so it oscillates continuously between two internal states. Anything rendering the raw want status shows a flickering value that misrepresents a system doing exactly the right thing.
- **REQ-SEARCH-003** — A user must be able to distinguish *"working, just waiting"* from *"stuck, needs you."* These are different situations and only one of them wants attention.

### The system can say what it tried

- **REQ-SEARCH-004** — For any unfulfilled want, the system must be able to show what was considered and why each candidate was rejected. **Unbuilt** — no search history, no per-release decisions, no reasons. This is the story's headline requirement and the thing a user actually wants at hour twenty.
- **REQ-SEARCH-005** — A search that returns nothing must still be recorded, along with which indexers answered. *"We found nothing"* and *"we didn't look"* are different claims, and only a record distinguishes them.
- **REQ-SEARCH-006** — Any progress figure shown to a user must reflect real cumulative effort. **Not currently true:** the attempt counter resets on each unsuccessful cycle, so a count of attempts can never rise above one — and the interface copy that would display it is unreachable code.

### Searching backs off, sensibly

- **REQ-SEARCH-007** — Search frequency must reflect how likely a new release is, growing sparser as a title ages, and must be bounded so a long wait never becomes an indefinite silence. *Broadly true* — the cadence is time-since-release rather than attempt-count, which is a better model than this story originally specified.
- **REQ-SEARCH-008** — A user must be able to ask the system to look again now, subject to a rate limit. **Partly built:** retry exists at the tracking level, not for a single want, and is not rate-limited.
- **REQ-SEARCH-009** — A release that already failed to download must not be picked again for the same want. *Currently true* — this exists and the original story never mentioned it.

### The wait is communicated, once

- **REQ-SEARCH-010** — A wait that exceeds a threshold must proactively tell the requester the system is still working, without requiring them to check. **Unbuilt.**
- **REQ-SEARCH-011** — That message must clear itself when the want recovers, and must not stack on repeat. **Unbuilt**, and dependent on a notification-resolution lifecycle that does not exist.

## Out of scope (variant stories)

- **It isn't out yet** — [asking for something that isn't out yet](./09-not-out-yet.md). The boundary is the obtainable date.
- **Giving up deliberately** — the user stops waiting. [A requester cancels](./07-requester-cancels.md).
- **Accepting something worse now** — grabbing a poor release deliberately and improving later. [Upgrades and divergent tiers](./06-upgrades-and-divergent-tiers.md).
- **Picking one by hand mid-wait** — impatience resolved through interactive search. [Autonomy and proposals](./08-autonomy-and-proposals.md) owns the manual path.
- **The profile is simply too strict** — the failure isn't "not yet" but "not ever, at these settings." Different diagnosis, different advice ("loosen the profile," not "wait"). Deserves its own story.
- **Indexers are down** — the failure mode is *"we can't tell whether nothing exists or we just can't see it."* [Indexer health](./10-indexer-health-degraded-and-recovery.md).
- **Failure after the grab** — download or import failures. A different axis entirely.

## Open questions

1. **Pre-flight expectation-setting.** Worth warning on the request affordance for very recent titles? Risk: false positives read as the system making excuses. **Lean:** yes for the first week after a home release, as a quiet footnote that never blocks submission.

2. **When does "still searching" fire, and how loudly?** **Lean:** around a day, in-app only by default, push opt-in — the user can't act on it, and the good news push is coming anyway.

3. **Does a long-failing want ever fail terminally? — resolved.** No. Failure is for active failures, not for patience. This matches the implementation. Captured as REQ-SEARCH-001.

4. **Should backing off reset when fresh releases appear?** Without a reset the system stays slow after real indexer activity; with one, a flood of fresh-but-bad releases keeps it busy on a futile search. **Lean:** reset only when something clears the hard gates, not on raw new-result count.

5. **How long is search history worth keeping?** A want that fails for thirty hours and then succeeds accumulates a lot of rejection records that are mostly noise once it resolves — but they're exactly what answers *"why did this take so long?"* **Lean:** keep for a period after the want resolves, then prune. Owned by [audit](../patterns/audit/README.md).

6. **Do identical releases across indexers collapse into one record?** Three indexers carrying the same rip is one decision, not three. **Lean:** collapse on release identity with the indexers listed — but note nothing durable identifies a release across indexers today, so this needs that first.

7. **"This isn't going to resolve on its own."** After about a week of nothing but junk, the situation is qualitatively different from "wait a bit" — it probably isn't coming at this quality. Worth detecting and saying so, with a suggested action. **Lean:** valuable, but later.
