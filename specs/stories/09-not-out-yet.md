# Story 9 — Asking for something that isn't out yet

**Status:** Draft

Someone sees a trailer, or a film is in cinemas, or next week's episode hasn't aired. They ask for it anyway — which is reasonable, and the system should handle it gracefully.

The distinction this story exists to draw:

> **"We can't get this yet" and "we can't find this" are different conditions.** One resolves on a date and needs nobody's attention. The other may need intervention. They must not look the same, and the system must not behave the same way about them.

[Failed search, eventual recovery](./04-failed-search-recovery.md) owns the second. This story owns the first, and hands off to that one the moment a title becomes obtainable.

## The idea that makes this hard

**For a movie, "released" is not one date.** A film is in cinemas months before it can be obtained at all. Someone requesting it during that window has asked for something that *exists* — it's out, it's being discussed, it's on posters — but that no release will satisfy for six to twelve weeks.

So the concept the product needs is not a release date. It is an **obtainable date**: the point at which asking is no longer futile. For a movie that's the home-release date; for an episode it's the air date.

The system currently understands this in exactly one place — a computed property on the movie card, which decides "not yet obtainable" from the digital and physical dates and shows a genuinely good line:

> In theaters 3 Jul · digital release 12 Sep

That understanding is display-only, lives on the untracked branch, and becomes **unreachable the moment the title is requested.**

## Cast

- **Vic** — a requester. Saw a trailer.
- **Sam** — the operator.

## Flow — Part A: a film that's in cinemas

### Phase 1 — Before asking (T+0)

**User-visible (Vic):** the film's page says it isn't available yet, and says when it's expected. He can still request it — the wait is stated, not a barrier.

This part works today, and it sets the expectation everything after it must honor.

### Phase 2 — After asking (T+1s)

**User-visible (Vic):** the honest version — *"Requested · expected 12 Sep."* The film is on his list, waiting for a date, and no action is pending from anyone.

**What actually happens today:** the moment he requests, the status becomes *"Searching for a release."* The good copy is on a branch that only renders for untracked titles, so requesting the film replaces an accurate statement with a false one. Vic now watches a spinner for six weeks.

**And behind it, worse:** because the film's persisted date is the *theatrical* one, it is already in the past. The want lands in an ordinary search tier and **the system polls indexers for it, day after day, for months**, for a release that cannot exist. Each pass records an attempt and a "nothing found" error against a film that isn't out.

### Phase 3 — The date arrives (T+10 weeks)

- The title becomes obtainable, searching begins in earnest, and the peak-cadence window does its work.
- From here, [failed search, eventual recovery](./04-failed-search-recovery.md) takes over. **This is the handoff**: everything before the date is this story, everything after is that one.
- Crucially, a release not appearing *on* the date is normal — home releases slip, and regional dates differ. Passing the date without a grab must read as "now looking," not as a failure.

### Phase 4 — The date moves (variant)

- Films get delayed. When the expected date changes upstream, the wait must reschedule, and the stated date must change with it.
- A film that slips from September to November and silently keeps saying September is worse than saying nothing — it converts a working system into an apparently broken one.

## Flow — Part B: next week's episode

**Series already does this correctly, and is the model.** Reconcile creates a want for every in-scope episode without a file — aired or not — and stamps the future ones to become due at their air date. The want exists, is visible in the ledger, and simply doesn't come due yet. Nothing searches for an episode that hasn't aired.

The asymmetry is entirely in *creation*: the scheduling layer is symmetric and already understands pre-release anchors for both media types. Only the movie path never uses it.

## Flow — Part C: no date at all

Some titles are announced with no date — "2027", or nothing. These cannot be scheduled against anything.

- Series handles this by not creating a want until a date exists; the title stays tracked and picks up a want once enrichment supplies one.
- The product requirement is that this reads as an **indefinite watch**, not a scheduled wait. "We'll get it when it's out" is honest; a countdown to a date nobody has announced is not.

## Where the waiting is visible

A requester who has asked for three unreleased things should not have to visit three pages to remember what's coming. **The calendar is the surface where waiting becomes legible** — it converts "nothing is happening" into "here is when something will happen."

The [navigation pattern](../patterns/navigation/README.md) already stakes this out: *upcoming series releases, scheduled episodes, slated downloads — for everyone.* It has no implementation.

This story does not design that surface. It asserts that **the wait must be visible somewhere other than the title's own page**, and that the data behind it — an expected date per pending item — is the same data Phase 2 needs.

## Postconditions

**After Part A:** one request, one want, dormant until the obtainable date; no search attempts recorded before it; the expected date visible on the title and in the calendar; on the date, the want becomes ordinary searching work.

**After Part C:** one request, one tracking, and either no want or a dormant one — presented as indefinite, with no fictional date.

## What must be true (foundation requirements)

Requirements carry stable ids per [the stories contract](./README.md#conventions).

### Not-yet-obtainable is a distinct condition

- **REQ-UNREL-001** — A title that cannot yet be obtained must be presented as waiting for a date, never as searching or failing. **Not currently true:** the accurate copy is unreachable once a title is tracked, so requesting it replaces "not available yet" with "searching for a release."
- **REQ-UNREL-002** — For a movie, obtainability must be determined by the home-release date, not the theatrical one. A film in cinemas is released and not obtainable. **Not currently true anywhere but the frontend:** nothing in the backend distinguishes the two.
- **REQ-UNREL-003** — The obtainable date must be durable, not recomputed per page view. **Not currently true:** per-type release dates are extracted correctly and well tested, then discarded — they exist only on the detail response, with no persisted column, so no scheduler or worker can consult them.

### The system does not chase what cannot exist

- **REQ-UNREL-004** — No search may be issued for a title before it could plausibly be obtainable. **Not currently true, and this is the expensive one:** a theatrically-released film's persisted date is already past, so its want enters a normal search tier and polls indexers daily for months.
- **REQ-UNREL-005** — A want that is waiting for a date must record that it is waiting, not accumulate failed attempts. Attempt counts and error text describe a system that tried and failed; neither applies to a title that isn't out.
- **REQ-UNREL-006** — When the obtainable date passes, the want must become ordinary searching work under the normal cadence and recovery rules.
- **REQ-UNREL-007** — Failing to find a release once the date has passed must not read as a system failure. Home releases slip; the first days after a date are ordinary searching.

### Dates change, and some don't exist

- **REQ-UNREL-008** — An obtainable date that changes upstream must reschedule the wait and update what the user is told.
- **REQ-UNREL-009** — A title with no known date must be accepted and presented as an indefinite watch, never as a scheduled wait against an invented date.

### The wait is legible

- **REQ-UNREL-010** — The expected date must be shown wherever the wait is shown, on the title and anywhere the request is listed.
- **REQ-UNREL-011** — A requester must be able to see everything they are waiting on, with dates, without visiting each title individually. **Not currently possible:** there is no calendar and no upcoming view of any kind.

## Out of scope (variant stories)

- **Searching once it's out** — cadence, back-off, and recovery after the date. [Failed search, eventual recovery](./04-failed-search-recovery.md).
- **Designing the calendar** — layout, grouping, filters, whether it merges with any other surface. This story only requires that the wait be visible off the title page and names the data needed.
- **Discovery** — browsing what's coming out generally, unrelated to anything requested. A different product that happens to share the word "calendar."
- **Metadata refresh cadence** — how often upstream dates are re-read. Adjacent and already modelled; this story only requires that a changed date takes effect.
- **Approval of a pre-release request** — whether a request for something unreleased needs different approval treatment. [Pending approval queue](./03-pending-approval-queue.md).

## Open questions

1. **Is the calendar scoped to me or to the library?** The navigation spec says "everyone," but a requester's useful question is *"what am I waiting for?"* while an operator's is *"what is this system going to do?"* Those are the same data filtered differently. **Lean:** one surface, scoped to the viewer's own requests by default, with an operator toggle for everything — rather than two features.

2. **How early should searching start?** Releases frequently appear before their official date, and a hard gate on the date would miss them; the current pre-release behavior wakes exactly at the date, which is midnight UTC of a date-only value. **Lean:** begin searching a modest window early — a day or so — and accept a few futile passes as the cost of not missing an early release.

3. **Should there be a limit on how far ahead one may request?** A film two years out occupies a request slot, a quota position, and a row in everyone's calendar for two years. Options: a horizon cap, an expiry, or nothing. **Lean:** no cap — refusing "I want this when it's out" is user-hostile — but this is the strongest argument for the request expiry the [requests spec](../modules/requests/README.md#lifecycle) already describes and the code does not implement.

4. **Does a requester get told when the wait ends?** "The film you asked for is out today — looking for it now" is a satisfying moment and closes the loop opened at request time. It is also a notification for something that is not yet actionable. **Lean:** notify on *availability*, not on the date passing; the date is the system's business, having the file is the user's.

5. **Whose dates?** Home-release dates are resolved by region priority, so a viewer outside the priority regions may see a date that does not match their own market. It's the right default for a self-hosted tool with one operator, but it is a stated assumption rather than a neutral one. **Lean:** leave it; revisit if the region priority becomes configurable.

6. **Does an unreleased request behave differently under autonomy?** A title waiting six weeks for a date is a poor fit for a proposal that sits open that whole time. **Lean:** propose-mode trackings should not generate proposals for not-yet-obtainable wants at all — there is nothing to propose until releases exist. Pin against [autonomy and proposals](./08-autonomy-and-proposals.md).
