# Story 6 — A better release appears: upgrades and divergent tiers

**Status:** Draft

Sometimes what you can get today isn't what you want to keep. A film hits the library as a 720p web rip because that's all that existed the week it came out; a proper Bluray shows up three weeks later. Somebody who already has a movie in HD now wants it in 4K.

Both are the same question — **the library has a file, and something better is possible** — and neither is answered today. [Quality profiles § upgrade detection](../modules/quality-profiles/README.md#upgrade-detection) defines what "better" means and [tracking](../modules/tracking/README.md#relationship-to-upgrade-behavior) owns the dial that decides how aggressively to act on it. This story owns what the user experiences and what must be true for it to work.

The framing that separates this from ordinary acquisition:

> Acquisition **adds** something that wasn't there. An upgrade **replaces** something that was. Everything hard about upgrades comes from that difference — the old file has to go somewhere, and something may still depend on it.

## Cast

- **Priya** — a requester. Wanted a film the week it came out.
- **Theo** — a second requester, who wants the same film in 4K.
- **Sam** — the operator.

## Preconditions

- Priya's quality profile has a **cutoff**: the bin at which the system stops looking for better.
- The film is in the library at a quality **below** that cutoff.
- The tracking's upgrade behavior is `propose`, which is the default per [autonomy and proposals](./08-autonomy-and-proposals.md#when-a-default-is-required).

## Flow — Part A: something better turns up

### Phase 1 — Settling for what exists (T+0)

**User-visible (Priya):**

- Priya requests a film released three days ago. The only release that passes her profile is a 720p web rip. It's acquired, imported, and she can watch it.
- Her request is fulfilled. As far as she's concerned, the story is over.

**Behind the scenes:**

- The file's quality is recorded in the same vocabulary a candidate release is scored in, so the two are comparable later.
- Because that quality is below cutoff, **the system's interest does not end here.** The want is satisfied but not finished — the tracking keeps a low-frequency eye out.

This is the part that does not exist today. A want that reaches available is terminal in every sense: no re-arm path admits it, and the reconciler treats "needs a want" as "has no file." Nothing in the system can ever look again.

### Phase 2 — A better release appears (T+3 weeks)

**User-visible (Sam):**

- On the film's page — and in whatever surface collects things waiting on him — an upgrade suggestion appears. It is explicit about being a **replacement**, not an addition:

  > **Better release available**
  > 1080p Bluray, 14 GB — replacing 720p WEB-DL, 3 GB

- Approve and Decline, exactly like an acquisition proposal.

**The product decision:** an upgrade proposal must read differently from an acquisition proposal, because approving it *destroys something*. An acquisition proposal that goes wrong costs a download; an upgrade that goes wrong costs the copy you already had. The confirmation has to name both sides of the swap.

### Phase 3 — Sam approves; the swap happens (T+3 weeks)

- The better release is grabbed and imported through the ordinary path, re-gated on import like any other acquisition.
- **The old file is superseded** — one file for the title, not two. The old bytes are released, subject to any obligation still standing against them.
- Priya isn't asked. She didn't request an upgrade; she requested a film, and she still has it — better.

### Phase 4 — The cutoff ends it

- Once the file is at or above cutoff, the title stops being upgrade-eligible. No further searching, no further proposals.
- **Cutoff is what makes `propose` liveable as a default** — the eligible set is "things acquired below cutoff," and it drains as they improve. Without it, every title would generate suggestions forever.

## Flow — Part B: a requester wants it better

### Phase 5 — Theo asks for 4K (T+ later)

**User-visible (Theo):**

- The film is already in the library in 1080p. Theo holds a 4K grant and requests it at 4K.
- He is told his request was successful.

**What actually happens today — and it's a defect, not a gap:**

- Theo's 4K profile is resolved, then **discarded**. The tracking keeps Priya's HD profile; the joining requester's quality target is dropped on the floor. His tier is recorded on his requester association — a value nothing anywhere reads.
- His request is marked spawned. He is told it worked. **Nothing 4K will ever be acquired, and nothing will ever tell him.**

This is worse than an unbuilt feature. A request that cannot change what the system will acquire must not report success.

### Phase 6 — What should happen instead

- Theo joining at a higher tier **raises the tracking's effective quality target**, exactly as a wider scope raises its effective scope. Per-requester intent is unioned, not first-write-wins.
- The existing 1080p file is now below the new target, which makes the title upgrade-eligible by the same rule as Part A. The upgrade path handles it — there is no separate "tier change" mechanism.
- Priya is unaffected. Nobody loses anything; the file only improves.

**The symmetry is the point.** [Multi-requester scope union](./05-multi-requester-scope-union.md) already establishes that per-requester intent is live, unioned, and recomputed. Tier is the axis where that model was specified and not implemented.

## What "replace" must mean

Replacement is where upgrades stop being a search problem and become a storage problem.

| Concern | What must be true |
| --- | --- |
| **One file wins** | After an upgrade the title has one current file. Today there is no notion of a current file — many rows may point at one title with nothing electing a winner. |
| **The old bytes go** | The superseded file is removed from the library, not merely dereferenced. A better release renders to a *different* path, so nothing collides and nothing is cleaned up by accident. |
| **Obligations survive** | Removing the library copy must not break an obligation against the source. This is what the hardlink strategy is for — the library link and the seeded copy are separate references to the same bytes — but nothing reasons about it today. |
| **Failure is safe** | If the upgrade fails to import, the original must still be there. Never remove the old file before the new one is verified in place. |

That last row is the one that decides the implementation order: **acquire, verify, then supersede** — never supersede-then-acquire.

## Postconditions

**After Part A:** one file for the title at the better quality; the superseded file gone from the library and its row no longer presented as current; the tracking still active if still below cutoff, otherwise no longer upgrade-eligible.

**After Part B:** both requesters attached to one tracking; the tracking's effective quality target is the higher of the two; the title upgrade-eligible against that target.

## What must be true (foundation requirements)

Requirements carry stable ids per [the stories contract](./README.md#conventions). Nearly all of this is unbuilt — the pure comparators exist and are tested, but they have no callers.

### The system can still be interested in a satisfied want

- **REQ-UPGRADE-001** — A want fulfilled below cutoff must remain eligible for improvement. Reaching available must not permanently end the system's interest in the title. **Not currently true:** available is terminal, every re-arm path excludes it, and the reconciler defines want-need as file absence.
- **REQ-UPGRADE-002** — A library file's quality must be comparable against a candidate release on the same terms the release was picked by — both its quality bin and its score. **Not currently true:** the file's bin is persisted but **its score is not**, so the comparison a candidate would be judged on cannot be reconstructed for a file already on disk. This is a schema gap, not a wiring gap.
- **REQ-UPGRADE-003** — Upgrade eligibility must end at the profile's cutoff. **Not currently true:** cutoff is defined, persisted, and validated on write, but nothing reads it.
- **REQ-UPGRADE-004** — A marginal improvement must not trigger an upgrade. Same-bin candidates must exceed the current file by a configurable margin. **Not currently true:** the comparator that applies a margin exists with no callers, and the margin itself has no configuration source anywhere.

### Replacement is safe

- **REQ-UPGRADE-005** — After an upgrade the title must have exactly one current file. **Not currently true:** nothing elects a current file, and because a better release renders to a different path, an upgrade would leave two live files and orphaned bytes that no scan reclaims.
- **REQ-UPGRADE-006** — The superseded file's bytes must be removed from the library.
- **REQ-UPGRADE-007** — Removing a superseded file must not break an outstanding obligation against the underlying data. **Nothing models this today** — no seeding state, no reference counting, and the inode columns that would support it are explicitly deferred.
- **REQ-UPGRADE-008** — The existing file must remain intact and current until the replacement is verified in place. An upgrade that fails must leave the library exactly as it was.
- **REQ-UPGRADE-009** — An upgraded file must be re-gated on import on the same terms as any acquisition, so a release that lied about its quality cannot displace an honest one.

### Intent is unioned, and never silently dropped

- **REQ-UPGRADE-010** — A requester joining an existing tracking at a higher quality tier must raise that tracking's effective quality target. **Not currently true:** the joining requester's profile is discarded and their recorded tier is never read.
- **REQ-UPGRADE-011** — A request that cannot change what the system will acquire must not report success. Whatever the resolution — raise the target, refuse the request, or explain that it's already covered — silently accepting is not among the options.
- **REQ-UPGRADE-012** — A tracking still watching for upgrades must not be treated as finished. **Neither side exists today:** nothing detects completion, and the upgrade dial that would keep a complete tracking alive is hardcoded off and never read.

### The proposal is honest about what it does

- **REQ-UPGRADE-013** — An upgrade proposal must state what it replaces, not only what it adds. Approving it destroys the existing copy, and the confirmation must say so.
- **REQ-UPGRADE-014** — Declining an upgrade must exclude that release from future upgrade proposals for the title, so a rejected suggestion does not return.

## Out of scope (variant stories)

- **How "better" is computed** — bin ranking, scoring, gates, the anti-flapping margin. Owned by [quality profiles](../modules/quality-profiles/README.md).
- **The autonomy dial itself** — what `auto` / `propose` / `none` mean and where defaults come from. [Autonomy and proposals](./08-autonomy-and-proposals.md).
- **Retention and cleanup** — what happens to library content over time, independent of upgrades. [Hygiene](../modules/hygiene/README.md).
- **Hardlink mechanics** — inode tracking, broken-link detection. [Hardlinks](../modules/hardlinks/README.md); this story only asserts the obligation that mechanism must honor.
- **Multi-tier storage** — keeping *both* a 1080p and a 4K copy of one title for different consumers. This story assumes one current file; simultaneous editions are a separate model.
- **Downgrades** — deliberately acquiring worse to save space.

## Open questions

1. **Does a requester learn their film improved?** An upgrade is an operator decision, but Priya's copy visibly changed. Options: silent, a passive note in her requests view, or an actual notification. **Lean:** silent by default — "your film is now 1080p" is noise to someone who never asked about quality — but surface the current quality wherever the title is shown.

2. **What if the superseded file is still seeding?** The obligation may outlast the reason for keeping the file. Options: defer removal until the obligation lapses, remove the library link and leave the source copy, or refuse to upgrade while seeding. **Lean:** remove the library link only — that is what the hardlink strategy exists to permit — but it needs the seeding state that isn't modeled yet. Blocks REQ-UPGRADE-007.

3. **Should a requester be able to trade quality for speed?** Priya took a 720p rip because it was that or nothing. A requester who would rather wait a month for a Bluray has no way to say so, and one who wants *anything now* has no way either. That's an urgency axis the tier picker doesn't capture. **Lean:** out of scope here, but it is the same decision cutoff already encodes — worth checking whether cutoff plus a "grab below cutoff or wait" flag covers it.

4. **When a higher-tier requester leaves, does the target drop?** Scope narrows on departure. Quality arguably should not — you don't downgrade a file you already have. **Lean:** the target drops for *future* acquisition, but nothing already acquired is touched and no downgrade is ever proposed. Mirrors the acquired-files-persist rule in [multi-requester scope union](./05-multi-requester-scope-union.md).

5. **Do upgrade searches run on the same cadence as acquisition?** An unfulfilled want is urgent; an upgrade is not. Running both at the same frequency wastes indexer budget on titles that are already watchable. **Lean:** a distinctly lower frequency, and one that backs off as a title ages.

6. **Is an upgrade proposal the same entity as an acquisition proposal?** They resolve identically — approve grabs, decline excludes and keeps looking — but they mean different things and one of them destroys data. **Lean:** the same entity with a kind discriminator, so the attention surface, audit, and resolution logic stay singular.
