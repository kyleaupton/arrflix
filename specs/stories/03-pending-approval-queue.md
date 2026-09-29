# Story 3 — Pending approval: two requests, one approved, one denied

**Status:** Draft (triaged against the code)

Two people submit requests within seconds of each other. Neither auto-approves. Both land in a queue. The admin reviews them with enough context to decide, approves one, denies the other with a reason, and both requesters learn what happened.

This is the **human-in-the-loop story** — the bottleneck [the happy path](./01-happy-path-auto-approve.md) deliberately skips. It validates the queue, both decision paths, the reason flowing back to the requester, and who is allowed to see what.

> **Triage note.** The decision mechanics and the queue itself shipped and work. Everything that *tells anyone about them* did not: **no admin is notified that a request is waiting, and no requester is notified of the outcome.** That inverts the story — the flow below reads as a communication story, and the communication is the unbuilt half. Field names and mechanism have been removed per [the stories contract](./README.md).

## Cast

- **Friend** — an established user. May request HD or 4K movies; HD auto-approves, **4K does not**.
- **Cousin** — joined recently. May request HD only, with **no auto-approval**.
- **Admin** — holds approve and deny authority for movies.
- **The system** — Arrflix.

## Preconditions

- Neither title is in the library.
- Indexers and downloader are healthy — this story is not about search.
- The queue is empty.

## Flow

### Phase 1 — Friend submits a 4K request (T+0)

**User-visible:** the tier picker offers HD and 4K, and is explicit about the consequence — 4K needs approval. Friend picks 4K, and the affordance says so before they commit: *"This will need admin approval."*

They tap Request. The pill reads **"Awaiting approval"** — deliberately not the same treatment as work in progress, because nothing is progressing.

**Behind the scenes:** the create grant is checked, auto-approval is found not to apply, and the request is held pending. **An admin is notified that something needs review.**

> **Unbuilt.** Nothing is emitted when a request lands in the queue. **No admin is told anything.** The queue is real and correct; discovering that it has something in it depends entirely on someone opening it.

### Phase 2 — Cousin submits an HD request (T+10s)

**User-visible:** Cousin sees HD as their only option, with the same up-front warning that new accounts are reviewed. Same pill, same toast.

**Notifications, and the grouping question:** this is the second thing needing review inside ten seconds. Two separate alerts for what a reviewer will handle in one sitting is noise; they should arrive as one — *"2 requests need review."*

### Phase 3 — Admin reviews the queue (T+15 min)

**User-visible:** the queue lists both requests with enough context to decide **without leaving the queue**:

| Requester | Title | Tier | Storage est. | Their history | In library? |
| --- | --- | --- | --- | --- | --- |
| Friend | *Mickey 17* | 4K | ~80 GB | 2 pending, 12 approved, 0 denied | No |
| Cousin | *Longlegs* | HD | ~12 GB | New — joined 3 days ago | No |

**The context is the product here.** A reviewer deciding from a title and a name alone is rubber-stamping. Storage cost, the requester's track record, and whether the library already has it are what turn a decision into an informed one.

> **Partly unbuilt.** The queue exists, filters to pending, and scopes correctly by permission. The context columns do not — a queue row shows title, tier, status, and requester name. Everything that makes the decision *informed* is missing.

### Phase 4 — Admin decides (T+~16 min)

**Approving Friend's request:** familiar requester, good history, space available. Admin approves, optionally attaching a note — *"Approved, but 4K is heavy on storage — HD next time if you don't mind."*

The request is approved and immediately spawns the acquisition machinery. From here it is [the happy path](./01-happy-path-auto-approve.md) at 4K.

**Denying Cousin's request:** Admin is short on space this month. They deny, with a required reason — *"We're tight on storage this month. Ask me again in a few weeks and I'll get it for you."*

**Denial requires a reason and approval does not**, which is deliberate: a denial without one reads as arbitrary, and the requester has no other channel to find out why.

**The queue empties**, and the reviewer's pending-review alerts clear themselves — nobody should have to dismiss a notice about something they just handled.

> **Unbuilt:** the approver's note has nowhere to live, and the self-clearing alert depends on a notification-resolution lifecycle that **does not exist**. This story was one of three treating that lifecycle as settled; see [the stories contract](./README.md#known-speccode-divergences).

### Phase 5 — Friend learns they were approved (T+~16 min)

**User-visible:** a notification that the request was approved, carrying the admin's note. The pill moves from *Awaiting approval* to ordinary acquisition status, and the film arrives.

> **Unbuilt.** No decision notification exists. Friend's page will eventually reflect reality if they look at it; nothing tells them.

### Phase 6 — Cousin learns they were denied (T+~16 min)

**User-visible:** a notification carrying the reason, and their requests view showing the title as denied with that reason inline. In-app rather than push — a denial is not urgent, and pushing bad news feels heavier than it needs to.

The request is terminal and read-only. Cousin can ask again; nothing structurally prevents it.

> **Unbuilt.** Same as Phase 5 — the reason is stored and displayed on the request, but nothing delivers it.

## Postconditions

- Two requests, one spawned into acquisition and one terminally denied, both retained as history with who decided and when.
- One acquisition, ending available; nothing spawned for the denial.
- The reviewer's queue empty and their attention cleared.
- Both requesters informed.

## What must be true (foundation requirements)

Requirements carry stable ids per [the stories contract](./README.md#conventions).

### A decision reaches a decider

- **REQ-APPROVE-001** — A request needing review must reach someone able to decide it, without them having to check. **Not currently true:** nothing is emitted when a request enters the queue.
- **REQ-APPROVE-002** — Multiple requests arriving close together must not produce one alert each.
- **REQ-APPROVE-003** — Deciding a request must clear it from every reviewer's attention, including reviewers who did not act on it. **Unbuilt**, and dependent on the missing resolution lifecycle.

### The decision is informed

- **REQ-APPROVE-004** — A reviewer must be able to decide from the queue itself: what it costs, who asked, what their history is, and whether the library already has it. **Not currently true** — the queue shows title, tier, status, and requester.
- **REQ-APPROVE-005** — A reviewer must only see requests they are permitted to act on. *Currently true.*
- **REQ-APPROVE-006** — Two reviewers acting on the same request must not both succeed. *Currently true* — the second attempt is refused and the interface handles it.

### The outcome reaches the requester

- **REQ-APPROVE-007** — A requester must learn the outcome of their request without checking. **Not currently true** for either outcome.
- **REQ-APPROVE-008** — A denial must carry a reason, and the requester must see it. *Half true:* the reason is required and stored, and nothing delivers it.
- **REQ-APPROVE-009** — An approver's note, where given, must reach the requester. **Unbuilt** — there is nowhere to record one.
- **REQ-APPROVE-010** — Bad news must not be delivered more loudly than good news. A denial defaults to a quieter channel than an approval.

### Nothing waits forever

- **REQ-APPROVE-011** — A request must not sit pending indefinitely with no resolution and no signal. **Unbuilt** — there is no expiry, and an unreviewed request waits forever in silence.

### The requester knows before they ask

- **REQ-APPROVE-012** — A user must know that a choice will require approval **before** they commit to it. *Currently true* — the tier picker only offers tiers they hold, and the consequence is stated up front.

### Time targets (UX commitments)

- Submission → "Awaiting approval": **< 1s**
- Submission → the reviewer knows: **within a minute**, allowing for grouping
- Decision → acquisition begins: **< 1s**
- Decision → requester knows: **under a minute**

## Out of scope (variant stories)

- **The acquisition that follows approval** — [the happy path](./01-happy-path-auto-approve.md).
- **Choosing how the release gets picked at approve time** — [autonomy and proposals](./08-autonomy-and-proposals.md), which owns approve-and-choose.
- **Withdrawing before a decision** — [a requester cancels](./07-requester-cancels.md).
- **Approving a series request that joins an existing tracking** — [multi-requester, scope union](./05-multi-requester-scope-union.md).
- **Quotas** — being over a cap at submission. No quota model exists.
- **Approve-with-modification** — dropping 4K to HD rather than denying. Currently the answer is deny-with-reason; worth revisiting.
- **Bulk decisions** — approving several at once. The model supports it; the interface doesn't.
- **Re-requesting after denial** — nothing prevents it. Whether it should warn is a product question.

## Open questions

1. **How long should the grouping window be?** Long enough to collapse a burst, short enough that a lone request isn't delayed. **Lean:** around a minute, with single requests effectively immediate.

2. **Does a reviewer see other users' history for the same title?** *"Two other people also asked for this"* is useful context and a small privacy disclosure. **Lean:** yes for reviewers, never for ordinary requesters.

3. **Should denial reasons have shortcuts?** Admins denying often want canned reasons — storage, household policy, coverage. **Lean:** a small editable set with free-text override, once denial volume justifies it.

4. **Is the approver's note visible to anyone but the requester?** **Lean:** requester and reviewers, nobody else — symmetric with the denial reason.

5. **How long until a request expires, and what happens then?** A pending request is a promise nobody made. **Lean:** expire after a few weeks with a notification, and let the user re-ask — but this is the requirement that most needs a real number.

6. **What does the requester see between approval and availability?** The request is spawned and frozen while a want carries the work. The interface says "fulfilled" once the file lands. **Lean:** derive it — introducing a second source of truth on the request is how these things drift.
