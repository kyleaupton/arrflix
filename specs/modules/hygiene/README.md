# Library hygiene — finding and fixing entropy over time

**Status:** Draft, iteration 1

This doc defines **library hygiene**: the system for detecting, surfacing, and remediating the slow accumulation of disorder in a media library — broken hardlinks, phantom files, duplicates, naming drift, quality drift. It captures _what hygiene means in Arrflix_, _what the catalog of findings looks like_, _how findings are computed and configured_, and _how the health score works_. It does **not** pin down table names, columns, or wire formats — those come in a later iteration.

This doc complements [acquisition](../acquisition/README.md) (which gets files _into_ the library) and [metadata](../metadata/README.md) (which keeps identity correct). Hygiene is the retrospective view: what's wrong in the library _right now_, and how do we fix it.

## TL;DR

- Hygiene is the **retrospective** view — *arr stacks are forward-time-biased; nobody owns the "what's broken in my library now" question. This is the wedge.
- Five categories: **integrity** (FS ↔ DB consistency), **quality drift** (files exist but aren't ideal), **layout** (naming, duplicates), **identity** (matching problems), **lifecycle** (retention / cleanup — what's worth *removing*).
- **Retention / cleanup lives here**, not on the request. A library-wide operator policy decides what to clean up, with two Arrflix-owned triggers — **age** (works with no media server) and an **explicit done signal** — and media-server **watch-state as an optional accelerant** that makes a file cleanup-eligible sooner. Additive, never load-bearing: no server → the watch-based rule simply never fires, time-based cleanup is unaffected.
- Each finding kind is a **rule** with configurable severity (`off | info | warn | error`), fix mode (`auto | propose | observe`), and optional path-based overrides — the ESLint model.
- **Findings are first-class records**, not just dashboard counts. Each has a lifecycle (`detected → resolved | dismissed`), history, and a story.
- Computation is **nightly audit (authoritative) + opportunistic scan-time backfill (free updates)**. Dashboard reads cached findings; "Re-run audit" for impatient users.
- **Health score** is the gimmick that earns its keep — a single 0–100 number that goes up as you fix things. Only `warn` and `error` rules contribute. User-configurable presets: Recommended / Strict / Relaxed.
- Three presentation modes per finding: **auto-remediate** (silent fix), **propose** (one-tap confirm), **observe** (surface, never act).
- The differentiators are *trend lines* (it's improving), *root-cause clustering* (here's why), *hardlink intelligence* (deleting won't free space until you also …), and *"what changed" diffs*.

## Why this is the wedge

The *arr stack is built around forward motion: find things, get things, organize things. Over months and years, libraries develop entropy that no one tool surfaces:

- The torrent client was wiped six months ago. Half your hardlinks are now lonely files; nothing told you.
- You tightened your quality profile last year. Eighty movies in your library don't meet it now. There's no list.
- You renamed your template scheme. A thousand files don't match. There's no list.
- A scan failed mid-run and left orphan DB rows. They show up in the UI as broken artwork forever.

People hand-roll this with `find`, `fdupes`, custom Python scripts, and Reddit threads titled "How do I find broken hardlinks in Sonarr?" — the demand exists, no tool addresses it.

The wedge is to make this **observable, trended over time, and remediable in-app**, with a UX that rewards making the library cleaner instead of punishing the user for letting it drift.

## Categories

| Category     | What it covers                                | Stakes                              |
| ------------ | --------------------------------------------- | ----------------------------------- |
| **integrity**| Filesystem and DB don't agree                 | Data loss / silent corruption       |
| **quality**  | Files exist but don't match current standards | Suboptimal but functional           |
| **layout**   | Naming, organization, duplicates              | Cosmetic, Plex hygiene              |
| **identity** | Matching / metadata problems                  | Wrong content in the right slot     |
| **lifecycle**| Content that's a candidate for *removal* per the retention policy | Storage reclaim (opt-in, never automatic surprise) |

The four entropy categories (integrity / quality / layout / identity) are about disorder that *accumulated*; **lifecycle** is the inverse — *intentional* storage reclaim driven by the [retention policy](#retention--cleanup-the-home-for-request-retention). It reuses the same finding / remediation / destructive-preflight machinery but is opt-in by default (most users keep everything). The integrity category is where the dashboard *earns trust* — surfacing real data risk is high-stakes and high-impact. Quality drift is where it gets *addictive* — visible, fixable, satisfying. Layout is the long tail.

## The catalog

Each finding type is a **rule** with a stable ID, default severity, default fix mode, and a description that becomes its UI tooltip.

| Rule ID                          | What it detects                                                   | Default severity (Recommended) | Default fix mode | Notable options                              |
| -------------------------------- | ----------------------------------------------------------------- | ------------------------------ | ---------------- | -------------------------------------------- |
| `integrity/broken-hardlink`      | File we hardlinked to is gone (link count dropped, source nuked)  | `error`                        | `propose`        | —                                            |
| `integrity/orphan-db-row`        | DB claims file exists, `stat()` says no                           | `error`                        | `propose`        | —                                            |
| `integrity/phantom-file`         | File on disk, no DB row claims it                                 | `warn`                         | `propose`        | —                                            |
| `integrity/empty-folder`         | Empty directory under a library root                              | `info`                         | `auto`           | —                                            |
| `integrity/path-outside-library` | File tracked in DB but lives outside any configured library root  | `warn`                         | `propose`        | —                                            |
| `quality/upgrade-candidate`      | File in library below current profile cutoff                      | `warn`                         | `propose`        | (re-uses upgrade logic from quality-profiles)|
| `quality/bitrate-outlier`        | Bitrate-per-resolution outside expected range                     | `info`                         | `observe`        | `tolerance_pct: 30`                          |
| `quality/codec-preference`       | Codec/container deviates from user preference                     | `info`                         | `observe`        | `preferred_video`, `preferred_audio`         |
| `quality/format-inconsistency`   | Mixed resolutions across seasons of the same series               | `info`                         | `observe`        | —                                            |
| `quality/advertised-mismatch`    | Release-advertised attributes disagree with the file's `ffprobe` truth (e.g. false Atmos/DV claim) | `warn`        | `observe`        | confident on audio/HDR; humble on source/edition |
| `layout/naming-drift`            | File path doesn't match current name template                     | `warn`                         | `propose`        | —                                            |
| `layout/duplicate-files`         | Two or more files claim the same identity                         | `warn`                         | `propose`        | `ignore_intentional_versions: true`          |
| `identity/unmatched-file`        | Scanner found a file it couldn't identify                         | `error`                        | `propose`        | —                                            |
| `identity/wrong-match-suspect`   | Parsed title/year disagrees with stored title/year                | `warn`                         | `propose`        | `title_similarity_threshold: 0.7`            |
| `lifecycle/age-cleanup`          | File older than the configured grace window since fulfillment     | `off`                          | `propose`        | `grace_days`, scope globs                    |
| `lifecycle/watched-cleanup`      | File watched (per media-server watch-state) and past a short grace | `off`                          | `propose`        | `grace_days`, `whose_watch`                  |

The catalog is **append-only** as we grow. New rules ship with sensible defaults and a "new since last release" badge on the rules screen, so users with `error`-everything configs aren't surprised by score drops.

**`quality/advertised-mismatch` is import-time-sourced.** Unlike the integrity rules (computed by the nightly audit + scan backfill), this finding is written by the [import-time re-gate](../quality-profiles/README.md#import-time-re-gate) at placement, when the importer holds both the advertised parse and the fresh `ffprobe`. It fires only on **soft-fail** keeps — hard-fails are rejected and never become library files. Its scope follows the [`ffprobe` verifiable taxonomy](../parsing/README.md#what-ffprobe-can-and-cannot-verify): confident on audio (false Atmos/channels) and HDR/DV presence, silent on source / edition / upscale, which `ffprobe` can't adjudicate.

## The rule config model (ESLint-style)

Each rule has a config row:

```yaml
rules:
  integrity/broken-hardlink:
    severity: error              # off | info | warn | error
    fix: propose                 # auto | propose | observe

  integrity/empty-folder:
    severity: info
    fix: auto

  quality/bitrate-outlier:
    severity: info
    fix: observe
    options:
      tolerance_pct: 30

  quality/codec-preference:
    severity: warn
    options:
      preferred_video: [h264, hevc]
      preferred_audio: [aac, eac3]

  layout/naming-drift:
    severity: warn
    fix: propose
    overrides:
      - paths: ["{library}/Family Movies/**"]
        severity: off
```

Three layers of "don't bother me":

| Layer                           | Granularity                    | Persistence                       |
| ------------------------------- | ------------------------------ | --------------------------------- |
| Rule severity = `off`           | Global per rule kind           | Rule config                       |
| Rule override on path glob      | A subtree of the library       | Rule config (`overrides:` list)   |
| Per-finding dismiss             | One specific file/title        | `hygiene_finding.dismissed_at`    |

Path globs match against **library-relative paths** (e.g., `{library}/Kids/**`) so the same config keeps working through renames and reorganizations.

### Presets

Three starter configs ship in-box:

| Preset           | Stance                                                                                  |
| ---------------- | --------------------------------------------------------------------------------------- |
| **Recommended**  | Integrity = error, quality drift = warn, layout = warn, observations = info. Sensible defaults. |
| **Strict**       | Everything that can be `error` is `error`. One finding anywhere drops the score. For the OCD crowd. |
| **Relaxed**      | Only critical integrity rules = error; everything else = info or off. For users who just want to be warned about real problems. |

Once a user deviates from a preset, the selector reads "Custom (based on Recommended)." Reset / Import / Export buttons round out the affordance — exporting a Strict-mode config and sharing it on Reddit is the kind of community vibe we want.

## Findings as first-class records

A finding is **a row, not a count on a card**. Schematically (data shapes TBD):

```
hygiene_finding(
  id,
  rule_id,                          -- 'integrity/broken-hardlink', etc.
  severity,                         -- snapshot at detection time
  target_id, target_kind,           -- the media item, file, or directory
  details_jsonb,                    -- rule-specific (e.g., expected_path, actual_path)
  detected_at,
  last_seen_at,                     -- updated each audit run
  resolved_at,
  dismissed_at, dismiss_reason
)
```

This unlocks:

- **History** — *"hardlink broke on 2026-04-12; what else happened that day?"* (cross-reference with import / scan logs)
- **Trends** — broken-hardlink count over the past 90 days, charted
- **Per-finding lifecycle** — resolve (problem fixed) / dismiss (problem isn't really a problem) / snooze (revisit later)
- **Audit trail** — *"you dismissed these 3 findings on 2026-05-01. Why?"*

Same persisted-artifact pattern as everything else that writes audit rows — see the system-wide [decision-artifact pattern](../../patterns/audit/README.md). Findings ARE the dashboard; the dashboard is a rollup over findings.

## Computation model

Two production paths feed findings into the table:

1. **Nightly audit (authoritative).** A dedicated `HygieneAuditWorker` runs at a configured cron (default 3am). It walks the filesystem, joins against DB state, applies each enabled rule, and writes findings. Findings not re-detected get their `last_seen_at` left stale, then auto-resolved after a TTL. This is the source of truth.

2. **Opportunistic scan-time backfill.** The existing `ScannerService` walk already touches the filesystem. We hook in cheap rule checks (broken hardlinks, phantom files, orphan rows) as it goes. Findings detected mid-scan get the same DB rows and the same lifecycle; they just don't wait for the nightly audit. Free updates.

3. **Manual re-audit.** Top-of-dashboard "Re-run audit" button kicks the worker on demand. Useful after big remediation work, or for OCD users who want to see the green check *now*.

The dashboard **always reads cached findings**. We never block on a fresh audit. Findings show a "last audited" timestamp so users can judge freshness.

### Why not on-demand-only?

Tempting (always fresh!), but the integrity scan stat()s thousands of files. Cold-start dashboard latency in the tens of seconds. Users bounce.

### Why not in-scan-only?

Scans run when *they* are needed, not when hygiene reports are needed. Stale findings would compound between scans.

Nightly authoritative + opportunistic = fresh findings without dashboard-latency penalty.

## Health score

A single 0–100 number is the dashboard chrome. Computation principles:

- **Only `warn` and `error` rules contribute.** `info` and `off` are zero-weight. Users opt in to what counts.
- **Severity-weighted.** An error costs ~10× a warn. Exact weights tunable.
- **Library-size normalized.** 3 errors in a 50-item library should feel different than 3 errors in a 5000-item library. The math is "per-finding penalty / library size, capped."
- **Per-category breakdown shown on hover or click.** `Integrity 100% / Quality 87% / Layout 71% / Identity 100%`.
- **Trended over time.** Show today, 7-day, 30-day. Going up is the win signal.

The dashboard chrome:

```
┌───────────────────────────────────────────┐
│   Library health                          │
│                                           │
│         ╭───────╮                         │
│         │  94%  │   ↑ +2 since last week  │
│         ╰───────╯                         │
│                                           │
│   Integrity 100%   Quality 87%            │
│   Layout 91%       Identity 100%          │
└───────────────────────────────────────────┘
```

For Kyle (the user — admitted OCD): set everything to `error` via the Strict preset, fix the findings, hit 100%, see the green check. The score is *honest about your own definition of "perfect."*

For someone who doesn't care about layout: set `layout/*` to `off`, score reflects what they actually care about.

## Remediation modes

| Mode             | Behavior                                       | Best for                                              |
| ---------------- | ---------------------------------------------- | ----------------------------------------------------- |
| `auto`           | Silently fix without asking                    | Empty folders, dismissed-pattern matches              |
| `propose`        | Surface a button, require one-tap confirm      | Broken hardlinks (re-grab), naming drift, duplicates  |
| `batch propose`  | Group similar findings, one bulk action        | "Rename 47 files to current template" — one approval  |
| `observe`        | Surface, no action button                      | Bitrate outliers, format inconsistency (preference)   |

Per-rule default in the catalog, user-overridable. The dashboard's batch UI groups same-rule findings: *"47 layout/naming-drift findings — preview rename diff"* leads to a destructive-preflight screen before commit.

### Destructive preflight

Every batch action that touches the filesystem goes through a preflight:

- Show the diff (rename map, files to delete, total bytes affected)
- Show reversibility (`hardlink-only delete`, `original torrent still in cache`, `truly destructive`)
- Require explicit confirm; offer "dry run" first

Cost of a wrong batch click is too high to skip this.

## Retention & cleanup (the home for "request retention")

Iteration 1 of [requests](../requests/README.md#where-retention-went) modeled retention as a per-request flag (`keep_forever` / `cleanup_after_watch` / `keep_for_days(N)`). That made the media server **load-bearing** for a request feature — `cleanup_after_watch` did nothing without watch-state, silently inverting the user's intent into "keep forever." It also asked a casual requester to reason about a file's eventual deletion, which is an operator concern, not a request-time one.

So retention lives here, as a **library-wide policy** the operator configures once. It's the `lifecycle/*` category above. The model follows the system-wide rule: **a behavior's trigger must be Arrflix-owned; a media server can only enrich or accelerate.**

**Two Arrflix-owned triggers** (a file becomes cleanup-eligible when either fires):

- **Age** — `lifecycle/age-cleanup`: N days since fulfillment. Needs no media server; always works. This is the honest floor that `keep_for_days(N)` used to express.
- **Explicit done** — a user (or admin) marks a title "done, clean it up." Arrflix owns this signal directly.

**Watch-state is the optional accelerant**, never a sole trigger:

- `lifecycle/watched-cleanup` fires when [media-server watch-state](../media-server/README.md#watch-state-ingestion--the-legitimate-coupling) reports the title watched, past a short grace window. It's effectively the explicit-done signal, auto-clicked by the player.
- **No media server → the rule simply never has a signal**, and nothing breaks: time-based cleanup still runs, and the user can still mark done manually. This is the additive-not-load-bearing posture, mirrored exactly from the [availability decoupling](../acquisition/README.md#media-server-propagation-decoupled-from-available). Watch-state can only ever make cleanup happen *sooner*; its absence never makes a file stick around against the policy.

**Hardlink-aware, by reusing the existing machinery.** "Eligible for cleanup" ≠ "delete now." A file flagged by a `lifecycle/*` rule goes through the same [hardlink intelligence](#killer-ux-moves) and [destructive preflight](#destructive-preflight) as a broken-hardlink fix: if the file is still held alive by torrents or other links, the dashboard surfaces *"removing this won't free space until you also …"* rather than silently deleting. Default fix mode is `propose` (one-tap, with preflight); `auto` is available for the operator who trusts it.

**Defaults to off.** Most users keep everything — the `lifecycle/*` rules ship `off`, so the library never deletes anything until the operator opts in. Because they're `off`/`info`-weight, they don't drag the [health score](#health-score) (not cleaning up a watched movie isn't a health problem).

Per-user / multi-retention ("watch-once for Alice, keep-forever for Bob on the *same* hardlinked file") is **parked** — it's a richer storage feature for a later iteration, not a v1 policy. v1 is a single library-wide policy.

## Killer UX moves

Janitor screens are useful. These are what make the dashboard *cool*:

1. **Trend lines, prominently.** *"Broken hardlinks: 12 ⇩ from 47 last week."* The library is improving, visibly. Reward the user for tending it.
2. **Root-cause clustering.** Don't list 47 broken hardlinks raw — group them: *"Most of these are under `{library}/Kids` — did you move that folder?"* Surface the likely cause, not just the symptoms.
3. **Hardlink intelligence.** For each broken hardlink, show inode + ref count + which torrents currently hold the file alive: *"This file is held by 3 torrents in qBittorrent. Deleting the file won't free space until you also remove torrent X."* Nobody else does this. The engine is the [hardlinks](../hardlinks/README.md) reference graph — `st_nlink` plus the inode→torrent correlation; hygiene renders the story, it doesn't compute it.
4. **"What changed" mode.** Diff since last audit: *"+5 broken hardlinks, +12 phantom files, −8 resolved."* Find the moment things broke.
5. **Finding stories.** Each finding has a narrative pane: *"Imported 2024-03-12 from torrent abc123. Hardlink broke 2026-04-12 when the torrent was removed from qBittorrent. The original download is no longer in cache."* Debuggable history, not a mystery error.
6. **Pre-flight diffs before destructive batches.** Already covered above — non-negotiable for trust.
7. **Hygiene digest as notification.** Don't make users *check* the dashboard. Push: weekly digest of warnings, immediate push for newly-detected `error`s. Connects to the [notifications](../notifications/README.md) subsystem.

## Interactions

| Neighbor                                              | How hygiene interacts                                                                                                              |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| **[Scan](../../docs/guide/roadmap.md) (existing)**    | Source of integrity findings; opportunistically populates `hygiene_finding` during walks. Hygiene rules' detection logic _runs in scan_ for free updates. |
| **[Files](../files/README.md)**                       | Several findings are queries over the file entity: `identity/unmatched-file` is a `file` with `media_item_id` NULL, `integrity/orphan-db-row` is `file_state.exists = false`, `layout/duplicate-files` is `GROUP BY file_state.osdb_hash`. The finding's `target_kind` is the file. |
| **[Decision log](../../patterns/audit/README.md)**    | `identity/wrong-match-suspect` findings link back to the import decision that placed the file. Closes the loop between acquisition and post-hoc review.    |
| **[Quality profiles](../quality-profiles/README.md)** | `quality/upgrade-candidate` re-uses upgrade-detection; `quality/advertised-mismatch` is the [re-gate](../quality-profiles/README.md#import-time-re-gate)'s soft-fail delta. Hygiene surfaces; quality-profiles owns the definitions. |
| **[Tracking](../tracking/README.md)**                 | Tracking states ("active subscription, last episode aired 21d ago, no file") inform a future `identity/missing-aired-episode` rule. Cross-references want-debugger view. |
| **[Metadata](../metadata/README.md)**                 | `identity/wrong-match-suspect` uses parsed-title-vs-stored-title comparison; depends on metadata freshness.                       |
| **[Requests](../requests/README.md)**                 | Owns retention/cleanup that iteration-1 requests carried. Requests no longer carry a retention flag; the `lifecycle/*` policy is operator-set and library-wide. |
| **[Media-server](../media-server/README.md)**         | Supplies watch-state, the optional accelerant for `lifecycle/watched-cleanup`. Absent server → the rule never fires; time-based cleanup is unaffected. |
| **Name templates (existing)**                         | Template changes regenerate `layout/naming-drift` findings; the batch re-render reads each file's [persisted parse](../parsing/README.md#persisted-parse) (lossless for grabbed, best-effort for scanned).                |
| **Import (existing)**                                 | Hooks into import-error pathways: failed imports surface as `identity/unmatched-file`; the [re-gate](../quality-profiles/README.md#import-time-re-gate)'s soft-fail writes `quality/advertised-mismatch` at placement. |
| **[Notifications](../notifications/README.md)**       | Critical findings push immediately; warnings batched into digest. Per-user preferences.                                            |
| **[Hardlinks](../hardlinks/README.md)**               | Owns the reference graph behind `integrity/broken-hardlink` (the exact `nlink` + import-method predicate) and the hardlink-aware [cleanup preflight](#retention--cleanup-the-home-for-request-retention) (`ReclaimableBytes`). Hygiene presents; hardlinks computes. |
| **Storage intelligence (future)**                     | Free-space + hygiene combined: *"You'd free 47 GB by resolving these 12 broken hardlinks."*                                       |

## Audit cadence and lifecycle

Findings live through these states:

```
                ┌──────────┐
                │ detected │
                └─────┬────┘
                      │
        ┌─────────────┼─────────────┐
        │             │             │
        ▼             ▼             ▼
   ┌─────────┐  ┌────────────┐  ┌─────────┐
   │ resolved│  │ dismissed  │  │  open   │ ←──── re-detected each audit
   └─────────┘  └────────────┘  └─────────┘       (last_seen_at updated)
   (fixed,       (user said       │
    auto or       not a problem)  │ user takes action
    by user)                      ▼
                              ┌─────────┐
                              │ resolved│
                              └─────────┘
```

- **Auto-resolution:** if a finding isn't re-detected for N audit cycles, it's auto-resolved with `dismiss_reason: stale`. Defaults to 3 cycles (≈ 3 nights) to filter out transient flakes.
- **Dismissals are permanent until un-dismissed.** Re-detection of an identical finding (same `rule_id` + `target_id`) checks for an existing dismiss before opening a new one.
- **Dismissals are auditable.** "Dismissed (12)" link is surfaced, not buried. Users can un-dismiss.

## UI shape

Two main surfaces:

1. **Hygiene dashboard** (`/library/health` or similar).
   - Top: health score circle + trend
   - Per-category cards (Integrity / Quality / Layout / Identity) with counts by severity
   - Recent findings list (drill-down to story view)
   - "Re-run audit" button + "Last audited X ago"

2. **Settings → Hygiene → Rules.**
   - Preset selector (Recommended / Strict / Relaxed / Custom)
   - Rules table (rule ID, description, severity, fix mode, overrides count, findings count)
   - Click a row → drawer with options form + override editor
   - Footer: Reset to defaults / Import / Export

A third surface — **the finding story view** — is reached by drill-down from either of the above. Shows narrative + remediation actions for one finding.

## Open questions

1. **Config persistence shape.** JSON blob in a single settings row, or relational `hygiene_rule_config` table? JSON blob is simpler (it's pure config, no joins needed) and matches ESLint's "file, not database" feel. Lean blob; revisit if querying needs it.
2. **Reactive recompute on config change.** Flipping a rule from `error` to `warn` should update the score immediately — findings exist, just re-roll up. Flipping from `off` to `error` requires running detection. Either kick off a partial audit, or accept "score updates at next audit (tonight at 3am, or click here)."
3. **Per-user vs per-installation config.** Hygiene is about the shared library, so single config per installation feels right. Compare to [notifications](../notifications/README.md), which are per-user. Revisit if multi-tenancy use cases (separate kid libraries with different rules?) emerge.
4. **`identity/wrong-match-suspect` heuristic.** False positives here are bad UX ("you said it's wrong but it's right"). What's the right similarity threshold? Probably user-tunable via rule options, with a generous default.
5. **`integrity/empty-folder` default fix: `auto` vs `propose`.** Auto-delete is convenient but spooky. Propose with batch-confirm is safer. Lean auto with a one-line audit-log entry per deletion.
6. **Health score weights.** Exact penalty per finding by severity. Needs experimentation against real libraries — too aggressive and Strict preset can't break 80%; too loose and the score is meaningless.
7. **TTL for stale-finding auto-resolution.** 3 cycles (3 nights) is a guess. Could be configurable, or rule-specific (broken hardlinks should auto-resolve quickly once fixed; quality findings should persist longer in case they're flaky).
8. **Subtitle / audio findings — own or punt?** Subtitle gaps probably belong to a future Bazarr-style integration. Audio language mismatch is cheap to detect via ffprobe and useful — propose owning. Out of v1, in v2.
9. **Permission / ownership findings.** Plex can't read because of UID mismatch. Real, but out of scope for v1. Revisit if support load demands.
10. **Notification routing for hygiene.** Per-rule, per-severity, per-user? Or just "send me a digest"? Should align with the [notifications](../notifications/README.md) subsystem — defer to that spec.
11. **Multi-library scoping.** Per-library audit view, all-libraries view, or both? Multi-library users will want both. Probably defaults to all-libraries with a filter.
12. **`quality/advertised-mismatch` remediation.** Default is `observe` (informational — the file is fine, just over-advertised). Worth a `propose` action ("blocklist this group / re-search for an honest release")? It overlaps the upgrade path. Lean: `observe` in v1; revisit if users want a one-tap "get the real thing."
13. **`lifecycle/watched-cleanup` "whose watch" semantics.** The `whose_watch` option: does the rule fire when *anyone* watches, or a specific user? And for content multiple people care about, fire on *either* or *all* watching? Lean: simplest useful default is "anyone watched + past grace," with `whose_watch` reserved for later refinement. Pin alongside the parked per-user multi-retention work.
14. **Manual "mark done" surface.** The explicit-done trigger needs a home — a button on the media detail page, owned by the web UI. Confirm it writes the same cleanup-eligibility signal the watch-state accelerant does, so the two paths converge. UI-iteration detail.

## What we're explicitly not deciding here

- Exact table names, columns, indexes for `hygiene_finding` and rule config
- API endpoint shapes
- The audit worker's exact scheduling implementation
- The remediation handlers' implementation (each rule needs its own; defer per-rule)
- Notification routing rules (lives in [notifications](../notifications/README.md))
- Bazarr / subtitle integration details
- Storage-intelligence cross-cutting concerns
- Final preset contents (the catalog table shows current intent, but real values tune in iteration 2)

Each gets its own pass once this model holds up against more real-world libraries.
