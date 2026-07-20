# Story 7 — A requester cancels

**Status:** Draft

Someone asks for a movie and then changes their mind. This story covers what "cancel" means at each point in the lifecycle, and — the part that carries the real weight — what it must mean when **more than one person wanted the same thing**.

The governing principle, which every requirement below follows from:

> **A request is an intent, not ownership.** Cancelling withdraws *your* intent. It does not entitle you to stop work other people still justify, and it never entitles you to remove something from the library.

The mechanics of the cascade — which rows move, in what order — belong to [requests § cancellation cascade](../modules/requests/README.md#cancellation-cascade) and [tracking](../modules/tracking/README.md). This story owns the product behavior and the requirements that behavior implies.

## Cast

- **Dana** — a requester. Asked for a movie, changed her mind.
- **Erin** — a second requester who asked for the same movie and still wants it.
- **Admin** — passive here, except where noted.
- **The system** — Arrflix.

## Preconditions

- The movie is not in the library.
- Both Dana and Erin have requested it. Per [multi-requester scope union](./05-multi-requester-scope-union.md), that produced **one** shared intent record with both of them attached — not two parallel efforts.
- The request was approved (auto or manual) and acquisition is underway.

## Flow — Part A: a non-final requester cancels

### Phase 1 — Dana changes her mind (T+0)

**User-visible (Dana):**

- Dana opens the movie's focus page, or her requests list. The status shows it's being acquired.
- She taps **Cancel request**. A confirmation appears, worded so the outcome isn't oversold:

  > *Remove your request for **Dune Part Two**? It may remain in the library if someone else requested it.*

- She confirms. Toast: *"Request removed."* The page returns to its pre-request state for her — the CTA is a Request button again.

**The product decision:** the confirmation must not promise that anything stops, because for a shared title nothing does. It also must not name Erin — requester identity is not disclosed to other requesters by default (per [multi-requester scope union § open questions](./05-multi-requester-scope-union.md#open-questions)). "Someone else" is the honest ceiling on what Dana may be told.

**Behind the scenes:**

- Dana's association with the shared intent is removed. Her request record becomes terminal and remains as history.
- Because Erin's intent survives, **the acquisition continues untouched.** No want is cancelled, no download is stopped, nothing is re-searched.

### Phase 2 — Erin is unaffected (T+0)

**User-visible (Erin):**

- Nothing. No notification, no status change, no interruption. Erin never learns Dana was there.

This is the whole point of Part A, and it is the case the system currently gets wrong in three separate ways — see [What must be true](#what-must-be-true-foundation-requirements).

## Flow — Part B: the last requester cancels

Erin now cancels too, while a download is in flight.

### Phase 3 — The last intent is withdrawn

**User-visible (Erin):**

- Same affordance, same confirmation. Toast: *"Request removed."*

**Behind the scenes:**

- Erin's association is removed. No requester remains.
- Work that nothing justifies anymore is stood down: pending and searching work stops.
- **The in-flight download is allowed to finish.** Bandwidth and time are already committed, and stopping mid-transfer can carry obligations to the source that abandoning it would break. This follows the lean already committed in [multi-requester scope union § open questions](./05-multi-requester-scope-union.md#open-questions).
- The completed file lands in the library, unrequested by anyone. It is governed from then on by library-wide [hygiene](../modules/hygiene/README.md) policy — the same as any other untracked content.

**Not built today:** there is currently no mechanism to stop an in-flight transfer at all, so this behavior holds by accident rather than by decision. The requirements below make it deliberate and bound it.

### Phase 4 — What Erin sees afterward

- Her requests list shows the request as cancelled, retained as history.
- If she re-requests later and the file already landed, the library already has it — no re-acquisition. That efficiency is a consequence of not deleting on cancel, not a special case.

## Cancel in every state

Part A and B cover the contested cases. The full surface:

| When the user cancels | What it means | What stops | What persists |
| --- | --- | --- | --- |
| Awaiting approval | Withdraw before any decision | Nothing has started | The request, as history |
| Approved, want searching | Stop looking, if no requester remains | The want is cancelled | The request, as history |
| Want downloading | Withdraw intent | Nothing — the download job runs on | The job completes; the file lands |
| A proposal awaits review | Withdraw intent **and** retire the proposal | The proposal | The request, as history |
| Already in the library | **No cancel is offered** | — | Everything |

The last row is the load-bearing one. Once the title is available, the request's purpose is served and the affordance disappears. Removing library content is an operator action against the library, not a requester action against their own past request.

## Postconditions

**After Part A** (Dana leaves, Erin remains):

- Dana's request is terminal and retained as history; her requester association on the tracking is gone.
- Erin's request, the tracking, its wants, any live download job, and any acquired files are **byte-for-byte unaffected**.
- No notification was sent to anyone.

**After Part B** (Erin leaves too):

- No requester association remains; the tracking is dormant rather than deleted.
- Wants not yet downloading are cancelled; the live download job completed.
- The resulting file is in the library, untracked, subject to hygiene policy.
- Neither cancellation recorded a release exclusion against the title.

## What must be true (foundation requirements)

Requirements carry stable ids per [the stories contract](./README.md#conventions). Several describe behavior the system **does not currently exhibit** — those are marked, and each is a defect rather than a feature gap.

### Withdrawal is scoped to the withdrawing user

- **REQ-CANCEL-001** — Withdrawing a request must remove only that user's requester association. A tracking with any remaining requester, and its wants, must continue unchanged. **Not currently true:** cancelling a shared tracking is authorized on own-intent permission alone, letting one requester end a shared effort for everyone.
- **REQ-CANCEL-002** — Cancelling a want must not cancel a download job that also serves other wants still justified. **Not currently true:** cancelling a want cancels every download job linked to it without checking the job's other wants. Latent for movies; breaks the moment a season pack serves several episode wants.
- **REQ-CANCEL-003** — Removing a requester from a tracking must trigger reconciliation of that tracking's wants, so wants only the departing requester justified are cancelled. **Not currently true:** departure removes the association but never reconciles, leaving orphaned wants live.

### Work already committed is not thrown away

- **REQ-CANCEL-004** — A download job already transferring must be allowed to complete. Only wants that have not yet reached a downloading state may be cancelled on withdrawal.
- **REQ-CANCEL-005** — Withdrawal must never delete files already acquired. They persist and fall under library retention policy.
- **REQ-CANCEL-006** — Cancelling a want must not exclude its release from future searches. Withdrawal means the requester stopped wanting the title, not that the release was unsuitable — recording an exclusion would poison later acquisition of the same title.
- **REQ-CANCEL-007** — A download job completing after its want was cancelled must not return that want to a live status. *Currently true* — worth an explicit test so it stays that way.

### The affordance is honest

- **REQ-CANCEL-008** — Cancellation must not be offered once the title is available to that user. Availability ends the request's life; removal from the library is a separate, operator-authorized action.
- **REQ-CANCEL-009** — Confirmation copy must not promise that acquisition stops, since for a shared title it does not.
- **REQ-CANCEL-010** — Cancellation must not disclose the identity or existence of specific other requesters beyond the generic possibility that some exist.
- **REQ-CANCEL-011** — Withdrawing while a proposal awaits review must retire that proposal, so no reviewer is asked to decide on behalf of a requester who left.

### Authorization

- **REQ-CANCEL-012** — Cancelling a tracking that other requesters still justify must require authority over others' requests, not merely over one's own. Removing one's own requester association requires only own-intent authority, because it cancels nothing.

## Out of scope (variant stories)

- **Operator cancels the underlying work** — an admin stopping a download or abandoning acquisition outright. Different authority, different confirmation, and it may legitimately stop work others want. Deserves its own treatment.
- **Removing content from the library** — the action that replaces cancel once a title is available. Belongs with [hygiene](../modules/hygiene/README.md).
- **Series scope narrowing** — one requester of a series reducing their scope rather than leaving entirely. Covered by [multi-requester scope union](./05-multi-requester-scope-union.md); this story deliberately uses a movie so the unit of intent is singular.
- **Denial** — an admin refusing a request. That's [pending approval queue](./03-pending-approval-queue.md); the request never spawned work.
- **Re-requesting after cancelling** — the efficiency case is noted in Phase 4, but any cooldown or anti-spam gate is a separate product question.
- **Quota refund** — whether a cancelled request returns capacity to the user's allowance. Quotas do not exist yet.

## Open questions

1. **Should the *last* requester's withdrawal ever stop a transfer?** [Multi-requester scope union](./05-multi-requester-scope-union.md) leaned "let it complete" for a shared series pack, where siblings still justified continuing. The movie case is weaker — nobody wants the result. Options: always complete (simplest, honors source obligations); abort only if barely started; abort always and accept the waste. **Lean:** always complete, and revisit if abandoned files become a real storage complaint. REQ-CANCEL-004 currently encodes that lean.

2. **If we ever do stop a transfer, is the partial data deleted or left in place?** Deleting reclaims disk; leaving it may be required to honor obligations to the source. This is the decision that must be made *before* wiring any stop capability, because the wrong default is costly and silent.

3. **Should abandoned acquisitions be surfaced to the operator?** A file that landed with no requester behind it is invisible today. A "nobody asked for this" surface would make the Part B outcome legible — but it's new UI for a rare case. **Lean:** fold into whatever surfaces untracked content generally, rather than building a bespoke view.

4. **Is anyone notified on cancellation?** Today, nobody. Candidates: the operator, when a pending review is retired out from under them (REQ-CANCEL-011); a co-requester, never. **Lean:** notify the reviewer only, and only when they had a decision queued.

5. **Does withdrawing while a suggestion is pending decline it, or leave it for others?** If Erin still wants the title, the suggestion arguably remains valid and should stand. REQ-CANCEL-011 as written retires the *user's* pending decision; whether the underlying suggestion survives depends on whether it belongs to the user or to the shared effort. **Needs pinning** against the propose-and-confirm subsystem, which has no story yet.

6. **What is the affordance actually called?** "Cancel request" reads as stopping work, which is wrong for the shared case. "Remove request" or "Withdraw" are more accurate but less familiar. **Lean:** keep "Cancel request" for recognizability and let the confirmation carry the honesty, per REQ-CANCEL-009.
