# Notifications — typed events, templated delivery, per-user routing

**Status:** Draft, iteration 1

This doc defines how Arrflix produces, routes, and delivers notifications across multiple channels (push, in-app, email, and a future-extensible set). It captures the three-audience model that distinguishes user-facing from admin-facing from system-bypass events, the typed-constructor API that doubles as the event registry, the per-event-type × per-channel template system, the shared outbox-and-retry delivery substrate, and the bundled preference model. It does **not** pin column types, API shapes, or specific UI layouts — those are implementation, defined in iteration 2.

## TL;DR

- **One delivery substrate, three routing strategies.** Every notification flows through a single typed-enqueue API, a single outbox, a single retry-with-backoff worker, and the same set of channel adapters. What differs is how recipients are resolved: `user` events use the per-user preference matrix; `admin` events use per-admin preferences (with install-wide defaults); `system` events bypass preferences entirely and ship over a hardcoded channel.
- **Typed constructors are the API and the registry.** Each event has an exported Go constructor (`notifications.NewWantAvailable(ctx, ...)`) with a typed payload. Producers can't enqueue an event without going through one. The set of constructors is the event registry — grep finds it, the compiler enforces the payload shape, and adding an event is a structural change rather than a string.
- **Per-event-type × per-channel templates.** `text/template` for push and in-app (title + body); MJML for email, compiled to inline-CSS HTML at build time, then variable-substituted at runtime via `html/template`. Templates ship as files in the repo; no admin-edit-in-DB in v1.
- **Bundled preferences with per-event overrides.** A user toggles channels per bundle (`My requests`, `Library activity`, `Admin alerts`). Power users can override at the event-type level. The combinatorial UI problem disappears; the data model still supports surgical control.
- **Producer-owned batching.** Hygiene's weekly digest, scan's "47 new files imported" rollup — the producer owns the schedule and the payload composition. Notifications delivers individual events promptly. No central digest scheduler; no cadence column on preferences.
- **Dedup keys on enqueue.** An optional `dedup_key` on each enqueue collapses storms (50 broken-hardlink findings → one push) without producers having to coordinate. Cheap to implement, expensive to retrofit.
- **Resolvable notifications.** Events that describe a standing condition ("still searching", "indexers down") carry a resolution lifecycle: the condition is cleared by a `Resolve(dedup_key)` call or a resolving event, flipping `active` rows to `resolved` (and cancelling any not-yet-sent). No stale bell entries — and two stories stop hand-rolling "supersede."
- **Outbox doubles as notification history.** Every attempted delivery is a row. The user's notification-history UI is just a query against the outbox filtered to that user.
- **Email is first-class but optional.** The architecture treats email as a peer channel. The adapter signals "not configured" when SMTP credentials are missing; preferences hide the toggle; outbox rows park in `awaiting_config` rather than failing. Configure SMTP later, drain (or skip) the backlog.

## Why this is its own spec

Notifications cross-cut every module that has a user-facing or admin-facing event: [acquisition](../acquisition/README.md) emits `want.grabbed` and `want.available`, [tracking](../tracking/README.md) emits episode-imported and upgrade-proposed, [hygiene](../hygiene/README.md) emits error findings and weekly digests, [requests](../requests/README.md) emits pending-needs-review, [metadata](../metadata/README.md) emits renumber alerts, the [connectivity-health pattern](../../patterns/connectivity-health/README.md) emits failed-transition alerts. Without a central spec, each producer would invent its own delivery model — different retry semantics, different preference shapes, different template stories.

This spec is also where Arrflix beats the field. [Sonarr / Radarr / Overseerr's notification systems are famously fiddly](../../docs/guide/why-arrflix.md): many channels, no per-user preferences, admin-only config that broadcasts to one configured endpoint. Modern hosted services (Knock, Courier) ship per-user preferences and templated multi-channel delivery as table-stakes. The bar to beat is "actually has user preferences"; the ambition is "the notification system feels like it was designed by someone who's shipped one before."

## The model

### The three audiences

Every event is one of three audiences. The audience is fixed by the constructor — producers don't choose at enqueue time.

| Audience  | Routing source                              | Visible in user prefs UI? | Channel locked? | Examples                                                                |
| --------- | ------------------------------------------- | ------------------------- | --------------- | ----------------------------------------------------------------------- |
| `user`    | Per-user preference matrix                  | Yes                       | No              | `want.available`, `upgrade.proposed`, `tracking.episode_imported`       |
| `admin`   | Per-admin preference matrix (install defaults seed new admins) | Yes (admins only) | No | `connectivity.failed`, `hygiene.error_finding`, `metadata.renumber`     |
| `system`  | Hardcoded per event-type; usually email-only | No                        | Yes             | `auth.password_reset`, `invite.created`, `auth.2fa_code`                |

The split matters because `system` events serve flows where preferences would defeat the point — a password-reset email that the user has disabled is a broken account-recovery path. By making the audience an immutable property of the event type, the runtime never has to reason about "should this bypass preferences?"

Resolution at enqueue time:
- `user` audience → exactly one `recipient: user_id`.
- `admin` audience → expands to all users holding the event's declared recipient permission key *at enqueue time* (see [Admin recipient resolution](#admin-recipient-resolution-frozen--iteration-2)). Newly-promoted admins do not get backfilled with prior alerts; newly-demoted admins still receive in-flight ones.
- `system` audience → recipient is either a `user_id` (most common — password reset, 2FA) or a literal email/endpoint (pre-claim invite, signup verification).

### Admin recipient resolution (frozen — iteration 2)

Iteration 1 routed `admin` events to "the relevant admin permission." That phrasing assumed a coarse `admin` role; RBAC instead froze a **fine-grained keyspace** — 33 enumerated concrete keys, with no blanket "is-admin" bit (`internal/authz`, migration `0022`). Two things pin down as a result.

**Each `admin` event declares a recipient permission key** — a constant in the constructor file, next to `audience` and the default bundle. Its recipients are everyone holding that key. Routing by role name (`admin` / `co_admin`) is deliberately *not* used: the RBAC model gates on capability, not role identity, and notification fan-out is no exception.

The launch-set mapping — every key already exists in the frozen catalog, so shipping notifications needs no catalog migration:

| Event                                  | Recipient key             | Rationale                                                         |
| -------------------------------------- | ------------------------- | ----------------------------------------------------------------- |
| `connectivity.failed` / `.recovered`   | `admin.indexers.manage`   | whoever manages indexer/downloader connections owns their health  |
| `hygiene.error_finding`                | `hygiene.read`            | hygiene visibility ⇒ hygiene findings                             |
| `hygiene.digest_weekly`                | `hygiene.read`            | same audience as the findings                                     |
| `metadata.renumber`                    | `media.write`             | renumber-override review is a `media.write` capability            |
| `request.pending_review`               | `requests.approve:<type>` | the approval queue — type-qualified (below)                       |
| `acquisition.search_failure_summary`   | `jobs.manage`             | acquisition-operator surface = `jobs.manage`                      |

Callouts:

- `request.pending_review` is **type-qualified** (`requests.approve:movie` / `requests.approve:series`): a movie-only approver is not paged for a pending series request. The fine-grained catalog buys this precision for free.
- **Recipient key ≠ preference bundle.** The key answers "are you in the audience"; [preferences](#preferences-and-bundles) answer "which channel." A user receives an admin event only if they *both* hold the key *and* have the channel enabled. The admin bundles (`admin_alerts`, `admin_summaries`) are shown in the prefs UI only to users eligible for at least one event they contain.
- A future admin event with no natural existing key requires an append-only catalog migration (owned by the [users spec](../users/README.md), not here).

**The reverse resolver.** RBAC's runtime answers one direction — given a *user*, does she hold key `K` (`AuthzService.Can`). Admin fan-out needs the reverse: **given `K`, which users hold it?** That query does not exist yet; it is the single primitive notifications adds to the authz layer. Its shape:

1. **Candidate gather** — a query unioning users with a direct `user`-subject allow on `K` with the members of any role holding an allow on `K` (global grants only in v1; resource-scoped admin events don't exist yet).
2. **Deny-wins filter** — the candidate set is a *superset* (a role allow can be overridden by a direct user deny), so each candidate is resolved through the existing pure `authz.Resolve`. The naive `SELECT subject_id WHERE permission_key = K` is wrong for exactly this reason.
3. **Surface** — `AuthzService.UsersWith(ctx, key)`, called at enqueue to produce the recipient set. Resolving at enqueue time is what yields the snapshot semantics above (no backfill for new admins; in-flight rows survive demotion).

Recommended: resolve directly against the DB rather than caching the reverse direction — enqueue is a cold path, and a reverse cache would add an invalidation surface the forward per-user cache doesn't have.

### The constructor API

Each event has a typed constructor exported from the `notifications` package:

```go
func NewWantAvailable(ctx context.Context, ev WantAvailable) error

type WantAvailable struct {
    Recipient    uuid.UUID    // user id (audience: user)
    DedupKey     string       // optional — collapses repeated enqueues
    Media        MediaRef     // {ID, Title, Year, PosterPath, ...}
    PlexLink     string       // deep link to play
}
```

The constructor:

1. Validates the payload at compile time (Go's type system).
2. Knows its event-type string (`"want.available"`), audience (`user`), and default bundle (`"my_requests"`) as constants in the constructor file.
3. Resolves the recipient set (single user, all admins, etc.).
4. For each recipient × subscribed channel, writes an outbox row tagged with event_type, payload JSON, and dedup_key.
5. The delivery worker drains the outbox, loads the matching template, renders, and hands to the channel adapter.

**The constructor file *is* the event registry.** There's no separate registration step, no `init()` magic, no string-keyed map. Constructors live under `backend/internal/notifications/events_<domain>.go` — one file per producing domain (`events_tracking.go`, `events_hygiene.go`, `events_system.go`). The doc-comment header on each constructor names the event-type, audience, and default bundle so the file is self-documenting.

### Channels

The v1 channel set:

| Channel    | Adapter responsibility                                              | Notes                                                              |
| ---------- | ------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `push`     | VAPID-signed Web Push via the `push_subscription` rows for the user | Per-device; one user can have many subscriptions                   |
| `in_app`   | Writes a `notification` row the bell-icon UI reads                  | "Delivery" means the row exists; `read_at` is a separate state    |
| `email`    | SMTP send via configured relay; rendered from MJML→HTML template    | Requires SMTP config; adapter reports `IsConfigured()` to gate UI |

Each adapter implements:

```go
type ChannelAdapter interface {
    Name() string                                        // "push" | "in_app" | "email"
    IsConfigured() bool                                  // false ⇒ toggle hidden, rows park awaiting_config
    Deliver(ctx context.Context, row OutboxRow) error    // returns typed error: transient (retry) vs permanent (dead)
}
```

Future channels (Discord webhook, ntfy, Gotify, Telegram) drop in as new `ChannelAdapter` implementations. The preference system and outbox are channel-name-agnostic; adding a channel is one new file plus its template entries.

### Templates

Each event × channel pair has a template. Templates live as files under `backend/internal/notifications/templates/<event_path>/`:

```
templates/
├── want/
│   ├── available/
│   │   ├── push.title.tmpl
│   │   ├── push.body.tmpl
│   │   ├── in_app.title.tmpl
│   │   ├── in_app.body.tmpl
│   │   ├── email.subject.tmpl
│   │   └── email.mjml          → compiled at build to email.body.html.tmpl
│   └── grabbed/
│       └── …
├── hygiene/
│   ├── error_finding/
│   │   └── …
│   └── digest_weekly/
│       └── …
└── system/
    ├── password_reset/
    │   ├── email.subject.tmpl
    │   └── email.mjml
    └── invite/
        └── …
```

**Text channels (`push`, `in_app`)** use Go's `text/template`. Same DSL as [name-templates](../name-templates/README.md), so operators see a single templating syntax across the app. Title and body are separate templates rendered independently — push notification systems treat them as distinct fields.

**Email** uses MJML for layout (`<mj-section>`, `<mj-button>`, etc.) — chosen because hand-rolled email HTML for cross-client rendering is genuinely awful. MJML files are **compiled at build time** to `html/template`-compatible HTML with inline CSS, eliminating any MJML dependency at runtime. The `email.subject.tmpl` is a separate plain-text template.

**Variables available to templates** are exactly what the constructor's typed payload includes — no implicit globals, no environment-aware context unless the constructor explicitly adds it. The template can only render what the constructor declared. This keeps templates pure-functional and trivially testable.

Missing-template-at-runtime is a **loud startup error**, not a silent skip. The build also includes a verification step that confirms every registered event has templates for every channel that targets it. A new event without templates fails to compile the binary.

### Preferences and bundles

A user's preference set is two layers:

**Bundles** group event types. Bundle definitions are static (in code), per audience:

| Bundle                  | Audience | Includes (directional)                                                       |
| ----------------------- | -------- | ----------------------------------------------------------------------------- |
| `my_requests`           | user     | `want.grabbed`, `want.available`, `request.decision_made`, `request.expired` |
| `library_activity`      | user     | `tracking.episode_imported`, `match.dropped_in`, `upgrade.proposed`           |
| `admin_alerts`          | admin    | `connectivity.failed`, `hygiene.error_finding`, `metadata.renumber`           |
| `admin_summaries`       | admin    | `hygiene.digest_weekly`, `acquisition.search_failure_summary`                 |

Preferences are stored at one of two scopes per `(user, channel)`:

| Scope        | Row                                            | Meaning                                                                   |
| ------------ | ---------------------------------------------- | ------------------------------------------------------------------------- |
| Bundle-level | `(user_id, scope='bundle', value='my_requests', channel='push', enabled=true)` | "Push me on anything in my_requests"                                       |
| Event-level  | `(user_id, scope='event', value='upgrade.proposed', channel='push', enabled=false)` | "...but not upgrade proposals specifically"                                |

Resolution at enqueue: event-level row wins if present; otherwise the bundle-level row decides; otherwise the bundle default applies (every bundle has a default for each available channel, set in the bundle definition).

New users get bundle-level rows seeded from each bundle's defaults on account creation. The defaults are designed so a brand-new user has a reasonable notification experience without configuring anything — push enabled for `my_requests`, in-app on for everything.

`system` audience events ignore this entire system. They route by the hardcoded `system_routing` map in the constructor file: `password_reset → email`, period.

### The outbox

Every enqueue writes one row per `(recipient, channel)` pair to `notification_outbox`:

| Column                  | Meaning                                                                                |
| ----------------------- | -------------------------------------------------------------------------------------- |
| `id`                    | UUID                                                                                   |
| `event_type`            | `"want.available"` etc.                                                                |
| `audience`              | `user` / `admin` / `system`                                                            |
| `recipient_user_id`     | nullable — non-NULL for `user`/`admin` events and most `system` ones                  |
| `recipient_literal`     | nullable — non-NULL for `system` events to non-user emails (invites, signup verifies) |
| `channel`               | `push` / `in_app` / `email`                                                            |
| `payload`               | JSONB — the typed constructor's serialized payload                                     |
| `dedup_key`             | nullable — see [Dedup](#dedup-and-coalescing)                                          |
| `status`                | `queued` / `delivering` / `delivered` / `failed` / `dead` / `awaiting_config` / `superseded`         |
| `attempts`              | integer; incremented on each failed try                                                |
| `next_attempt_at`       | nullable timestamptz; set on retry-back-off                                            |
| `last_error`            | nullable text; the most recent failure reason                                          |
| `created_at`            | timestamptz                                                                            |
| `delivered_at`          | nullable timestamptz                                                                   |
| `read_at`               | nullable timestamptz — `in_app` only; updated by the bell-icon UI                     |
| `resolvable`            | bool — true if this event represents a standing condition with a resolution lifecycle (see [Resolvable notifications](#resolvable-notifications-resolution-lifecycle)) |
| `resolution_state`      | nullable — `active` / `resolved`; only set when `resolvable`                          |
| `resolved_at`           | nullable timestamptz — when the condition cleared                                     |

The delivery worker polls for `status='queued' AND next_attempt_at <= now()`, marks rows `delivering` while it works (so a crashed worker leaves provable in-flight state), hands the row to the adapter, and transitions to `delivered`, `failed` (transient, schedule retry), `dead` (permanent — bad subscription endpoint, malformed email), or `awaiting_config` (adapter reports not configured). A resolvable row whose condition clears before delivery transitions to `superseded` (cancelled, never sent) — see [Resolvable notifications](#resolvable-notifications-resolution-lifecycle).

**Retry semantics:** exponential back-off (1s, 4s, 16s, 64s, …) up to a max of ~1 hour, capped at ~10 attempts before transitioning to `dead`. Adapter-reported permanent failures bypass retry entirely.

**The outbox is the notification history.** A user's bell-icon UI is a query: `SELECT * FROM notification_outbox WHERE recipient_user_id = $1 AND channel = 'in_app' AND status = 'delivered' ORDER BY created_at DESC`. Mark-as-read updates `read_at`. No separate `notification_history` table.

### Dedup and coalescing

Each enqueue accepts an optional `dedup_key`. The semantics:

- At enqueue time, if a row with the same `(dedup_key, channel)` is already `queued` or `delivering` for the same recipient, **the new row is silently dropped** (or, optionally, replaces the existing payload — see Open question #4).
- `dedup_key` is opaque to the system — its content is the producer's choice. Hygiene might use `"hygiene.error_finding.<library_id>"` to collapse a storm of findings under one push; metadata might use `"metadata.renumber.<series_id>"` to avoid alerting on the same series twice.

The pattern is intentionally simple. Producers that need richer batching (rolled-up summaries with counts and per-item details) own that logic and emit a single digest event. Dedup is the *floor* of coalescing, not the ceiling.

### Resolvable notifications (resolution lifecycle)

Most notifications are **one-shot**: "your episode is available", "import finished". Delivered, read, done. But a class of notifications describe a **standing condition** that later clears:

- "Still searching for *Sentinel*" — clears when the release is found ([Story 4](../../stories/04-failed-search-recovery.md)).
- "All indexers unavailable" — clears when an indexer recovers ([Story 10](../../stories/10-indexer-health-degraded-and-recovery.md)).
- "Request pending review" — clears when an approver decides.
- "Upgrade proposed" — clears when the user accepts or declines.

For these, leaving a stale "still searching" entry in the bell after the thing was found — or a red "indexers down" banner after they recovered — is a bug. Two separate stories independently reached for a "supersede" mechanism; rather than hand-roll it twice, resolution is a **first-class lifecycle** on the outbox.

**The model.** A resolvable event type declares `resolvable: true` (a constant in its constructor, like audience) and supplies a `dedup_key` that identifies the *condition*, not the individual message. Its outbox rows carry `resolution_state: active`. The condition is cleared in one of two ways, both of which transition every `active` row matching that `dedup_key` (across all recipients and channels) to `resolved`:

1. **`notifications.Resolve(ctx, dedupKey)`** — a lightweight call with no new notification. Use when the clearing isn't itself noteworthy. _Story 4:_ the AcquisitionWorker calls `Resolve("want.<id>.search_stalled")` on `want.grabbed` — the user doesn't need a separate "we found it" message ahead of the eventual `want.available`.
2. **A resolving event that declares `Resolves: <dedupKey>`** — delivers itself _and_ resolves the prior condition. Use when the recovery is worth announcing. _Story 10:_ `connectivity.recovered` is a real admin notification ("indexers recovered") that also resolves the `connectivity.failed` rows.

**Per-channel resolution behavior:**

| Channel  | If still `queued` (not yet delivered)         | If already `delivered`                                                            |
| -------- | --------------------------------------------- | -------------------------------------------------------------------------------- |
| `in_app` | row → `superseded` (never shown)              | row → `resolved`; stays as history, drops out of the unread/attention count      |
| `push`   | row → `superseded` (don't push a stale state) | already in the tray — can't recall; in_app + app state reconcile when opened     |
| `email`  | row → `superseded`                            | already sent — can't recall                                                      |

The headline win: a "still searching" push that hasn't fired yet is *cancelled* when the want recovers, and the in-app entry quietly resolves rather than lingering. A delivered "indexers down" alert stays visible but flips to `resolved` — [Story 10](../../stories/10-indexer-health-degraded-and-recovery.md)'s "resolved + visible" choice — so "when did this happen earlier today?" stays answerable.

**Relationship to dedup (resolves [open question #4](#open-questions)).** Resolution also settles drop-vs-replace: for a **resolvable** event, a repeat enqueue under the same `(dedup_key, recipient)` while a row is still `active` **replaces** the payload (you want the latest state of the standing condition — e.g. "47 considered" → "52 considered"), keeping the original `created_at`. For a **transient** event, the original behavior holds — the repeat is **dropped**. So drop is the default; replace is what resolvable conditions get.

One-shot events are unaffected: no `resolvable` flag, no `resolution_state`, exactly today's behavior.

### Push subscriptions

Per-user, per-device. Tracked in `push_subscription`:

| Column         | Meaning                                                          |
| -------------- | ---------------------------------------------------------------- |
| `id`           | UUID                                                             |
| `user_id`      | FK to `app_user`                                                 |
| `endpoint`     | VAPID push endpoint URL                                          |
| `p256dh`       | client-provided encryption key                                   |
| `auth`         | client-provided auth secret                                      |
| `ua`           | user-agent string at registration                                |
| `device_label` | nullable — user-set "iPhone", "Work laptop"                      |
| `created_at`   | timestamptz                                                      |
| `last_seen_at` | timestamptz — bumped on each successful delivery                 |

Subscriptions that return permanent push errors (410 Gone, 404 Not Found) are deleted after the failed delivery is marked `dead`. The next time the device re-registers, it gets a fresh row.

### Read-state for in-app

The `in_app` channel uses the outbox row directly. The bell-icon UI reads recent rows; user actions on the UI update `read_at`. There is no separate read-receipts table; for the in-app surface, the outbox row is the durable record.

Unread count is `COUNT(*) WHERE channel='in_app' AND recipient_user_id=$1 AND status='delivered' AND read_at IS NULL AND resolution_state IS DISTINCT FROM 'resolved'`. Marking all as read is a single `UPDATE`. A resolved standing-condition row drops out of the attention count even if never explicitly read.

## Email and SMTP configuration

Email is architected as a peer to push and in-app — same adapter shape, same outbox flow, same template system. But unlike push (which uses VAPID with self-generated keys) and in-app (which is just a DB write), email requires an SMTP relay the admin has to configure.

The pattern handles this gracefully:

1. **Adapter reports configuration state.** `EmailAdapter.IsConfigured()` returns true only when SMTP host, port, credentials, and from-address are present in settings.
2. **Preferences UI hides email when unconfigured.** The toggle column for `email` is removed from the user-facing preferences screen if the adapter isn't configured. A discreet "Email channel requires SMTP — admins, see settings" affordance appears in its place.
3. **Outbox rows park.** Email-channel enqueues that occur while unconfigured are written with `status='awaiting_config'`. They are *not* dropped — they accumulate. The delivery worker ignores them.
4. **On configuration, the admin chooses.** When SMTP is first configured (or re-configured after a long outage), the settings save flow prompts: "Drain queued emails since *date*?" with options to drain since now, since a recent timestamp, or never (mark `dead`). This prevents a fresh SMTP setup from blasting users with a year of accumulated invites.

**System-audience events with email-only routing** are special-cased: if SMTP isn't configured, those flows surface a synchronous, in-band error to the caller ("Cannot send invite — no SMTP configured. Configure SMTP in Settings → Email."). They never park; they fail loud at the point of trigger.

## Does NOT own

- **The SSE / real-time UI update stream.** SSE is a sibling system. It powers in-flight status pills, request progress, and live view updates. Notifications and SSE may emit on overlapping events but they are different channels with different durability semantics: SSE is "tell me what's happening right now if I'm here"; notifications is "tell me about this even if I'm not."
- **Approval workflows from a notification.** "One-tap approve" pushes from `upgrade.proposed` open the in-app surface and rely on its existing approval-action endpoints. The notification carries the deep link; it doesn't carry the action.
- **Producer-side batching logic.** When [hygiene](../hygiene/README.md) emits a weekly digest, hygiene owns the schedule, the query, and the digest payload. Notifications receives one event and delivers it. Cross-event batching ("queue all my notifications into a 9am morning digest") is not in scope (see Open question #7).
- **Permission-grant resolution for `admin` audience.** [Users spec](../users/README.md) defines who counts as admin. The notification system queries the permission system at enqueue time; it doesn't reimplement admin detection.
- **Channel adapter configuration UI.** SMTP settings, Discord webhook URLs, etc. live in their respective settings screens. The adapter reads from settings; this spec doesn't dictate the settings UI.
- **Decision-artifact rows.** A notification *about* an approved request is not the audit row for that approval — the [audit pattern](../../patterns/audit/README.md) owns the durable decision record. Notifications subscribes to (some) audit events; it doesn't replace the audit table.

## Interactions

| Neighbor                                                          | How notifications interacts                                                                                                                                            |
| ----------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[Acquisition](../acquisition/README.md)**                       | Producer: `want.grabbed`, `want.available`, `upgrade.proposed`, `tracking.archived`. Acquisition calls notification constructors at the appropriate lifecycle points. |
| **[Tracking](../tracking/README.md)**                             | Producer: `tracking.episode_imported`, `tracking.archived`, `upgrade.proposed`. Emits via constructors.                                                                |
| **[Requests](../requests/README.md)**                             | Producer: `request.pending_review`, `request.decision_made`, `request.expired`. Both user-audience (the requester) and admin-audience (the approval queue).            |
| **[Quality profiles](../quality-profiles/README.md)**             | Producer: upgrade-available data feeds the `upgrade.proposed` event payload.                                                                                           |
| **[Matching](../matching/README.md)**                             | Producer: `match.dropped_in` for drop-in auto-matches that resolve open wants.                                                                                         |
| **[Metadata](../metadata/README.md)**                             | Producer: `metadata.renumber` when TMDB renumbers a series and overrides need review.                                                                                  |
| **[Hygiene](../hygiene/README.md)**                               | Producer: `hygiene.error_finding` (immediate, dedup'd) and `hygiene.digest_weekly` (producer-owned schedule).                                                          |
| **[Connectivity-health pattern](../../patterns/connectivity-health/README.md)** | Producer: `connectivity.failed` (resolvable) and `connectivity.recovered` (declares `Resolves` the failed key) on `failed`-tier transitions. Subscriber via `<resource_type>.health` SSE channels. |
| **[Audit pattern](../../patterns/audit/README.md)**               | Subscriber: some audit events trigger notifications (approval-needed, manual-override-applied). Notifications references audit rows by ID in event payloads.           |
| **[Users](../users/README.md)**                                   | Recipient resolution for `admin` audience uses the permission keys defined there. User identity (email, push subscriptions) hangs off `app_user`.                      |
| **SSE (existing system)**                                         | Sibling channel — *not* a notification channel. SSE delivers real-time UI updates; notifications delivers durable events. Both can fire on the same event.            |

## Tables

Owned here:

- `notification_outbox` — the durable record of every attempted delivery
- `notification_preference` — per-user, per-(bundle|event), per-channel toggles
- `push_subscription` — per-user, per-device VAPID endpoints

Referenced (owned elsewhere):

- `app_user`, `permission_grant` — see [users](../users/README.md)
- `auth_audit` — see [users](../users/README.md#admin-action-audit); admin-action notifications surface from here

## v1 scope and build order (frozen — iteration 2)

The full spec is large — VAPID push, an MJML email pipeline, the preference matrix, the resolution lifecycle, SMTP with drain UX. v1 proves the **spine** first, then layers channels and the admin path onto it.

**v1 — the spine.** `user` audience only; `in_app` channel only (a DB write — no VAPID, SMTP, or MJML). The [outbox](#the-outbox) with its status lifecycle and the retry worker; [typed constructors](#the-constructor-api) with build-time template verification; bundle-level [preferences](#preferences-and-bundles) for `my_requests` + `library_activity`, seeded on user creation; the bell-icon UI (outbox query, unread count, mark-read). First producers: `request.decision_made` (→ the requester) and `want.available` (→ the want's requester set) — both already carry the recipient `user_id`. Needs nothing from RBAC at runtime.

**v1.1 — the `admin` audience.** The [reverse resolver](#admin-recipient-resolution-frozen--iteration-2) and the event→key map; the admin bundles plus eligibility gating in the prefs UI; first admin producers `request.pending_review` and `connectivity.failed` / `.recovered`. This slice introduces the [resolution lifecycle](#resolvable-notifications-resolution-lifecycle) — connectivity is the first resolvable event.

**v1.2 — `push`.** `push_subscription`, the VAPID Web Push adapter, per-device fan-out, dead-endpoint cleanup on 410/404.

**v1.3 — `email`.** The SMTP adapter, `IsConfigured()` gating, `awaiting_config` parking and drain UX, the MJML→HTML build step. The system-audience email events that motivate email — `invite.created`, `auth.password_reset`, `auth.2fa_code` — only deliver once their *producers* exist: invites are RBAC P5, and password-reset / 2FA are not in the current auth build. The substrate can land ahead of them.

**Dependency gates.** v1 lands after the RBAC merge (to avoid stacking work on an unmerged branch, not for any runtime coupling). v1.1's reverse resolver builds directly on `internal/authz`. The v1.3 system email events are gated on RBAC P5 and auth flows that don't yet exist.

## Open questions

1. **Bundle catalog finalization.** The starter set (`my_requests`, `library_activity`, `admin_alerts`, `admin_summaries`) is directional. Iteration 2 should pin the full catalog and the bundle-default channels per bundle. Lean: ship the four-bundle starter; add new bundles as new event-type categories emerge. *Partially pinned (iteration 2): [v1](#v1-scope-and-build-order-frozen--iteration-2) ships `my_requests` + `library_activity`; the admin bundles arrive in v1.1. Full catalog and per-bundle default channels still to pin.*
2. **Admin install-wide defaults vs per-admin only.** When a new admin is promoted, they get bundle defaults. Should those defaults be configurable per-install ("our team wants admin_alerts on email + push by default") or hardcoded? Lean: configurable per-install, surfaced in Settings → Notifications → Admin defaults.
3. **System-event registry — do we expose this in the UI?** `system` audience events bypass user preferences entirely. Should the UI at least *show* the user "we will email you for password resets and invites" so it's not opaque? Lean: yes, a read-only "system emails" disclosure section in the preferences page.
4. **Dedup semantics: drop vs replace — resolved.** Transient events **drop** the colliding enqueue (the simple default). Resolvable events **replace** the payload (keeping the original `created_at`) so the standing condition shows its latest state. See [Resolvable notifications](#resolvable-notifications-resolution-lifecycle).
5. **`awaiting_config` drain UX.** When SMTP is first configured, the admin sees "Drain N queued emails?" — but `N` could be huge if the system has been running unconfigured for months. Default to "drain only those from the last 7 days"? Per-event-type cutoffs? Lean: 7-day default with admin override.
6. **Push subscription cleanup cadence.** Subscriptions that haven't seen activity in months are likely dead. Run a periodic prune (e.g., 6-month silence + last delivery attempt failed). Lean: yes; prune at the hygiene cadence.
7. **Quiet hours.** Per-user "don't push between 11pm and 7am" — common Tier-2 feature. Defer to v2; flag here. The model accommodates it as a delivery-side filter (rows queued during quiet hours park with `next_attempt_at` set to wake-time).
8. **Cross-event accumulator.** "Hold all notifications from the last 8 hours and deliver as a single push at 9am" — not the same as producer-owned batching. Out of v1; flag for v2. Same delivery-side filter shape as #7.
9. **Channel-level rate limits.** A push gateway might 429 us; SMTP relays have hourly caps. Today we just back off per outbox row. Worth a per-channel global throttle? Lean: defer; the back-off pattern handles spikes adequately at self-hosted scale.
10. **Localization.** All templates are English in v1. The structure (templates as files, separate subject/title/body) doesn't preclude i18n later — a future iteration adds a locale parameter to template lookup. Lean: explicitly defer; document the upgrade path.
11. **Template editing / custom branding.** Some self-hosters will want custom email branding ("from My Family Library"). Options: a single "branding" template snippet that all emails include; full per-template override via DB rows; do nothing. Lean: ship a `branding.mjml` partial in v2 that's the one customization knob.
12. **Migrating existing in-flight notifications when an event-type is renamed.** If `want.available` becomes `media.available`, what happens to outbox rows already enqueued under the old name? Lean: never rename in place — deprecate the old constructor, add the new, let outbox drain naturally.
13. **Multi-recipient single-row optimization.** A `connectivity.failed` event going to 5 admins writes 5 outbox rows. Each is a distinct delivery; status tracking is independent. Is there value in a "broadcast row" that fans out at delivery time? Lean: no — 5 rows for 5 admins is the cleaner model and the cardinalities are tiny.

## What we're explicitly not deciding here

- Exact `notification_outbox` / `notification_preference` / `push_subscription` column types and indexes
- The Go signature of the channel adapter interface (sketch is directional)
- The exact `text/template` and `html/template` function libraries available to template authors
- MJML compilation tooling (Node-based vs pure-Go) — the *output* is what's load-bearing, not the build chain
- Push gateway choice / VAPID key rotation policy
- SMTP relay recommendations or starter docs
- The preferences UI layout — a bundles-as-cards-with-channel-toggle-rows pattern is the assumed shape; exact components are implementation
- Per-event-type analytics ("how many `want.available` notifications did we send last week?") — derivable from outbox queries; no separate metrics table
- Whether to retain delivered outbox rows indefinitely or apply a retention window (lean: retention configurable via the [audit retention pattern](../../patterns/audit/README.md#retention))

## Doc neighbors

- [Story 1](../../stories/01-happy-path-auto-approve.md) — the auto-approve happy path that pre-committed to the constructor + per-user push + `push_subscription` + `notification_preference` shape
- [Users](../users/README.md) — defines the recipient model, admin permission resolution, and the related `auth_audit` admin-action stream
- [Acquisition](../acquisition/README.md) — the primary user-facing notification producer; pre-committed `NotificationService (with push channel)` in its own service list
- [Hygiene](../hygiene/README.md) — the primary producer of producer-owned batching (weekly digest of warnings + immediate push for errors)
- [Connectivity-health pattern](../../patterns/connectivity-health/README.md) — emits `<resource_type>.health` transitions that become admin notifications
- [Audit pattern](../../patterns/audit/README.md) — sibling cross-cutting system; some audit-row creations trigger notifications
- [Name templates](../name-templates/README.md) — sibling templating system; sharing `text/template` DSL means operators learn one syntax for the whole app
- [Errors pattern](../../patterns/errors/README.md) — typed error model; channel adapters return typed errors that the retry worker maps to transient vs permanent
