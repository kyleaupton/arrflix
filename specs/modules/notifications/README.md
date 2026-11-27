---
status: v1
---

# Notifications — typed events, templated delivery, per-user routing

> **Beyond what's built:** the `admin` audience and its [reverse resolver](#admin-recipient-resolution), the [resolution lifecycle](#resolvable-notifications-resolution-lifecycle) beyond `request.pending`, [dedup coalescing](#dedup-and-coalescing), event-level preference overrides, the admin bundles, and every event except `want.available` (user) and `invite.created` (transactional email) describe unbuilt design. Password-reset and 2FA producers don't exist. V1's slice is the `want.available` notification in [Happy path](../../V1.md#request-a-movie-get-it-be-told--happy-path).

This doc defines how Arrflix produces, routes, and delivers notifications across multiple channels (push, in-app, email, and a future-extensible set). It captures the three-audience model that distinguishes user-facing from admin-facing from system-bypass events, the typed-event API that doubles as the event registry, the per-event-type × per-channel template system, the shared outbox-and-retry delivery substrate, and the bundled preference model. It does **not** pin column types, API shapes, or specific UI layouts — those are implementation.

## TL;DR

- **One delivery substrate, three routing strategies.** Every notification flows through a single typed-enqueue API, a single outbox, a single retry-with-backoff worker, and the same set of channel adapters. What differs is how recipients are resolved: `user` events use the per-user preference matrix; `admin` events use per-admin preferences (with install-wide defaults; unbuilt); `system` events bypass preferences entirely and ship over a hardcoded channel (built as *transactional* email to a literal address).
- **Typed events are the API and the registry.** Each event is a Go struct in `internal/notifications` implementing the `Event` interface (e.g. `notifications.WantAvailable`), handed to `NotificationService.Enqueue`. Producers can't enqueue without one. The `notifications.Registered` list is the event registry — grep finds it, the compiler enforces the payload shape, and adding an event is a structural change rather than a string.
- **Per-event-type × per-channel templates.** `text/template` for push and in-app (title + body); MJML for email, compiled to inline-CSS HTML by `just gen-email` (the output is committed and embedded), then variable-substituted at runtime via `html/template`. Templates ship as files in the repo; no admin-edit-in-DB.
- **Bundled preferences.** A user subscribes per bundle (`my_requests`, `library_activity`); subscription is the in-app bell, and email/push are per-bundle amplifiers on top. **Unbuilt:** per-event overrides and the admin bundles.
- **Producer-owned batching.** Hygiene's weekly digest, scan's "47 new files imported" rollup — the producer owns the schedule and the payload composition. Notifications delivers individual events promptly. No central digest scheduler; no cadence column on preferences.
- **Dedup keys on enqueue.** An optional `dedup_key` on each enqueue collapses storms (50 broken-hardlink findings → one push) without producers having to coordinate. **Unbuilt:** the column and a partial unique index exist, but no event sets a key and the enqueue has no conflict handling.
- **Resolvable notifications.** Events that describe a standing condition ("still searching", "indexers down") carry a resolution lifecycle: the condition is cleared by a `Resolve(dedup_key)` call or a resolving event, flipping `active` rows to `resolved` (and cancelling any not-yet-sent). No stale bell entries — and two stories stop hand-rolling "supersede." **Unbuilt.**
- **Outbox doubles as notification history.** Every attempted delivery is a row. The user's notification-history UI is just a query against the outbox filtered to that user.
- **Email is first-class but optional.** The architecture treats email as a peer channel. The adapter signals "not configured" when no enabled email provider exists; preferences mark the toggle unavailable; outbox rows park in `awaiting_config` rather than failing. Saving an enabled provider requeues the last 7 days of parked mail.

## Why this is its own spec

Notifications cross-cut every module that has a user-facing or admin-facing event: [acquisition](../acquisition/README.md) emits `want.grabbed` and `want.available`, [tracking](../tracking/README.md) emits episode-imported and upgrade-proposed, [hygiene](../hygiene/README.md) emits error findings and weekly digests, [requests](../requests/README.md) emits pending-needs-review, [metadata](../metadata/README.md) emits renumber alerts, the [connectivity-health pattern](../../patterns/connectivity-health/README.md) emits failed-transition alerts. Without a central spec, each producer would invent its own delivery model — different retry semantics, different preference shapes, different template stories.

This spec is also where Arrflix beats the field. [Sonarr / Radarr / Overseerr's notification systems are famously fiddly](../../docs/guide/why-arrflix.md): many channels, no per-user preferences, admin-only config that broadcasts to one configured endpoint. Modern hosted services (Knock, Courier) ship per-user preferences and templated multi-channel delivery as table-stakes. The bar to beat is "actually has user preferences"; the ambition is "the notification system feels like it was designed by someone who's shipped one before."

## The model

### The three audiences

Every event is one of three audiences. The audience is fixed by the event type — producers don't choose at enqueue time.

| Audience  | Routing source                              | Visible in user prefs UI? | Channel locked? | Examples                                                                |
| --------- | ------------------------------------------- | ------------------------- | --------------- | ----------------------------------------------------------------------- |
| `user`    | Per-user preference matrix                  | Yes                       | No              | `want.available`, `upgrade.proposed`, `tracking.episode_imported`       |
| `admin`   | Per-admin preference matrix (install defaults seed new admins) | Yes (admins only) | No | `connectivity.failed`, `hygiene.error_finding`, `metadata.renumber`     |
| `system`  | Hardcoded per event-type; usually email-only | No                        | Yes             | `auth.password_reset`, `invite.created`, `auth.2fa_code`                |

The split matters because `system` events serve flows where preferences would defeat the point — a password-reset email that the user has disabled is a broken account-recovery path. By making the audience an immutable property of the event type, the runtime never has to reason about "should this bypass preferences?"

Resolution at enqueue time:
- `user` audience → exactly one `recipient: user_id`.
- `admin` audience → expands to all users holding the event's declared recipient permission key *at enqueue time* (see [Admin recipient resolution](#admin-recipient-resolution)). Newly-promoted admins do not get backfilled with prior alerts; newly-demoted admins still receive in-flight ones.
- `system` audience → recipient is either a `user_id` (password reset, 2FA — unbuilt) or a literal email (pre-claim invite, signup verification).

The built `system` path is **transactional email**: `NotificationService.EnqueueTransactionalEmail(eventType, recipientEmail, payload)` writes one `audience='system'`, `channel='email'`, `transactional=true` row addressed by `recipient_email`. Transactional events (`invite.created`, and `email.test`, which is rendered and sent synchronously rather than enqueued) are listed in `notifications.RegisteredTransactionalEmail`; they don't implement `Event` (no bundle, no recipients) and never pass through the preference gate.

### Admin recipient resolution

RBAC gates on a **fine-grained keyspace** — 33 enumerated concrete keys, with no blanket "is-admin" bit (`internal/authz`, migration `0022`). Two things follow for `admin` events. **Unbuilt:** no `admin` event, and no reverse resolver, exists yet.

**Each `admin` event declares a recipient permission key** — declared on the event type, next to `audience` and the default bundle. Its recipients are everyone holding that key. Routing by role name (`admin` / `co_admin`) is deliberately *not* used: the RBAC model gates on capability, not role identity, and notification fan-out is no exception.

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

### The typed-event API

Each event is a struct in the `notifications` package that implements `Event`:

```go
type Event interface {
    EventType() string                    // "want.available"
    Audience() model.NotificationAudience // user | admin | system
    Bundle() string                       // "my_requests"
    Recipients() []uuid.UUID              // user-audience targets
    DedupKey() string                     // "" = no coalescing
    Payload() any                         // exactly the template variables
}

type WantAvailable struct {
    Recipient uuid.UUID `json:"-"`
    Media     MediaRef  `json:"media"`              // {title, year, tmdbId, type, posterPath}
    PlexLink  string    `json:"plexLink,omitempty"` // reserved; unset until V1.1 Plex work
}
```

Enqueueing:

1. The payload shape is checked at compile time (Go's type system).
2. The event's type string, audience, and bundle are fixed by its methods — producers don't choose them.
3. `NotificationService.Enqueue` resolves the recipient set (v1: the user IDs the event carries).
4. For each subscribed recipient it writes the `in_app` row, plus one row per enabled outbound channel, tagged with event_type, payload JSON, and dedup_key.
5. The delivery worker drains the outbox, loads the matching template, renders, and hands to the channel adapter.

Producers usually go through a service helper — the import worker calls `NotificationService.NotifyWantAvailable(want)`, which emits one `WantAvailable` per requester of the want's tracking.

**The event files *are* the registry.** There's no `init()` magic and no string-keyed map; `notifications.Registered` lists every event so template verification can walk it. Events live under `backend/internal/notifications/events_<domain>.go` — one file per producing domain (`events_acquisition.go`, `events_transactional.go`). The doc comment on each event names its event type, audience, and bundle so the file is self-documenting.

### Channels

The v1 channel set:

| Channel    | Adapter responsibility                                              | Notes                                                              |
| ---------- | ------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `push`     | VAPID-signed Web Push to the `push_subscription` rows of the user's live sessions | Per-device; one subscription per session, so a user can have many |
| `in_app`   | No-op: the outbox row itself is what the bell-icon UI reads         | "Delivery" means the row is `delivered`; `read_at` is a separate state |
| `email`    | SMTP send via the configured `email_provider`; rendered from MJML→HTML template | Requires an enabled provider; adapter reports `IsConfigured()` to gate delivery |

Each adapter implements (`internal/notifications/adapter.go`):

```go
type ChannelAdapter interface {
    Name() model.NotificationChannel                              // push | in_app | email
    IsConfigured(ctx context.Context) bool                        // false ⇒ rows park awaiting_config
    Deliver(ctx context.Context, row model.NotificationOutbox) error // retryable (apperrors.IsRetryable) vs permanent (dead)
}
```

The VAPID keypair self-generates at first boot into the `vapid_config` singleton, so push is always configured.

Future channels (Discord webhook, ntfy, Gotify, Telegram) drop in as new `ChannelAdapter` implementations. The preference system and outbox are channel-name-agnostic; adding a channel is one new file plus its template entries.

### Templates

Each event × channel pair has a template. Templates live as files under `backend/internal/notifications/templates/<event_path>/`, embedded into the binary; MJML sources live beside them under `emailsrc/`:

```
notifications/
├── templates/
│   ├── want/available/
│   │   ├── push.title.tmpl
│   │   ├── push.body.tmpl
│   │   ├── in_app.title.tmpl
│   │   ├── in_app.body.tmpl
│   │   ├── email.subject.tmpl
│   │   └── email.body.html.tmpl   ← generated from emailsrc
│   ├── invite/created/
│   │   ├── email.subject.tmpl
│   │   └── email.body.html.tmpl
│   └── email/test/
│       └── …
└── emailsrc/
    ├── partials/                  head / header / footer
    ├── want/available/email.body.mjml
    ├── invite/created/email.body.mjml
    └── email/test/email.body.mjml
```

**Text channels (`push`, `in_app`)** use Go's `text/template`. Same DSL as [name-templates](../name-templates/README.md), so operators see a single templating syntax across the app. Title and body are separate templates rendered independently — push notification systems treat them as distinct fields.

**Email** uses MJML for layout (`<mj-section>`, `<mj-button>`, etc.) — chosen because hand-rolled email HTML for cross-client rendering is genuinely awful. MJML files are **compiled ahead of time** (`just gen-email`, using the web toolchain's `mjml`) to `html/template`-compatible HTML with inline CSS; the generated `.html.tmpl` is committed, so there's no MJML dependency at runtime or in the Go build. `just email-preview` renders every email through the real renderer for eyeballing. The `email.subject.tmpl` is a separate plain-text template.

**Variables available to templates** are exactly what the event's `Payload()` includes — no implicit globals, no environment-aware context unless the event explicitly adds it. The template can only render what the event declared. This keeps templates pure-functional and trivially testable.

Missing-template-at-runtime is a **loud startup error**, not a silent skip. The delivery worker's constructor runs `Renderer.Verify` over `notifications.Registered` for every wired channel (and over `RegisteredTransactionalEmail` for email), so a new event without templates fails API startup.

### Preferences and bundles

A user's preference set is two layers:

**Bundles** group event types. Bundle definitions are static (in code), per audience:

| Bundle                  | Audience | Includes (directional)                                                       |
| ----------------------- | -------- | ----------------------------------------------------------------------------- |
| `my_requests`           | user     | `want.grabbed`, `want.available`, `request.decision_made`, `request.expired` |
| `library_activity`      | user     | `tracking.episode_imported`, `match.dropped_in`, `upgrade.proposed`           |
| `admin_alerts`          | admin    | `connectivity.failed`, `hygiene.error_finding`, `metadata.renumber`           |
| `admin_summaries`       | admin    | `hygiene.digest_weekly`, `acquisition.search_failure_summary`                 |

Only `want.available` exists today (in `my_requests`). The bundle catalog in code (`notifications.bundles`) populates the two user bundles; the admin bundle names are declared constants only.

`notification_preference` holds one row per `(user_id, bundle)` with three nullable flags:

| Column       | Meaning                                                                                      |
| ------------ | -------------------------------------------------------------------------------------------- |
| `subscribed` | The master. Subscribed ⇒ the event shows in the in-app bell; unsubscribed ⇒ silent everywhere. `in_app` is never a stored or separately toggled channel. |
| `email`      | Outbound amplifier, meaningful only while subscribed.                                        |
| `push`       | Outbound amplifier, meaningful only while subscribed.                                        |

Resolution at enqueue (`notifications.Subscribed` / `ChannelEnabled`): a non-NULL stored flag wins; NULL or an absent row defers to the bundle's in-code default. Storage is lazy — only genuine overrides are written, and there is no seeding step on account creation, so retuning a default reaches every user who never overrode it. Defaults: both user bundles subscribed; push on for `my_requests`, off for `library_activity`; email off everywhere. An unknown bundle resolves false (fail closed).

**Unbuilt:** event-level overrides ("push me on `my_requests`, but not upgrade proposals"). The row shape would need an event-scope key to carry them.

`system` audience events ignore this entire system: transactional email never consults preferences.

### The outbox

Every enqueue writes one row per `(recipient, channel)` pair to `notification_outbox`:

| Column                  | Meaning                                                                                |
| ----------------------- | -------------------------------------------------------------------------------------- |
| `id`                    | UUID                                                                                   |
| `event_type`            | `"want.available"` etc.                                                                |
| `audience`              | `user` / `admin` / `system`                                                            |
| `recipient_user_id`     | nullable — non-NULL for `user`/`admin` events and user-addressed `system` ones        |
| `recipient_email`       | nullable — literal address for `system` email to someone with no account (invites). A CHECK requires this or `recipient_user_id` |
| `transactional`         | bool — bypasses preference gating and never parks as `awaiting_config`                 |
| `channel`               | `push` / `in_app` / `email`                                                            |
| `payload`               | JSONB — the typed event's serialized payload                                           |
| `dedup_key`             | nullable — see [Dedup](#dedup-and-coalescing)                                          |
| `status`                | `queued` / `delivering` / `delivered` / `failed` / `dead` / `awaiting_config` / `superseded` (`failed` is reserved; nothing writes it. `superseded` is written by `request.pending` resolution) |
| `attempts`              | integer; incremented on each failed try                                                |
| `next_attempt_at`       | timestamptz, default `now()`; pushed out on retry back-off                             |
| `claimed_at`            | nullable timestamptz — when the worker claimed the row into `delivering`               |
| `last_error`            | nullable text; the most recent failure reason                                          |
| `created_at`            | timestamptz                                                                            |
| `delivered_at`          | nullable timestamptz                                                                   |
| `read_at`               | nullable timestamptz — `in_app` only; updated by the bell-icon UI                     |
| `resolvable`            | **Unbuilt.** bool — true if this event represents a standing condition with a resolution lifecycle (see [Resolvable notifications](#resolvable-notifications-resolution-lifecycle)) |
| `resolution_state`      | **Unbuilt.** nullable — `active` / `resolved`; only set when `resolvable`             |
| `resolved_at`           | nullable timestamptz — `in_app` only; when the condition cleared. A resolved row doesn't count as unread. How it cleared isn't stored: the inbox reads it from the subject (for `request.pending`, the request's status) |

The delivery worker (`internal/jobs/notification`, 5s poll, batches of 50) selects `status='queued' AND next_attempt_at <= now()`. If the channel's adapter isn't configured, a non-transactional row parks as `awaiting_config` before being claimed. Otherwise it claims the row with a compare-and-swap to `delivering` (stamping `claimed_at`), hands it to the adapter, and settles it: `delivered`, back to `queued` with back-off (transient), or `dead` (permanent — no adapter for the channel, a malformed payload, a rejected address, or retries exhausted). A reaper returns rows stuck in `delivering` for over 5 minutes to the queue, which is the crash-window self-heal. A transactional row on an unconfigured channel fails as retryable and eventually dies, so it never waits silently. A resolvable row whose condition clears before delivery transitions to `superseded` (cancelled, never sent) — see [Resolvable notifications](#resolvable-notifications-resolution-lifecycle).

**Retry semantics:** exponential back-off (2s, 4s, 8s, 16s, …) capped at 1 hour, with at most 10 attempts before `dead`. Non-retryable errors (`apperrors.IsRetryable` false) bypass retry entirely.

**The outbox is the notification history.** A user's bell-icon UI is a query: `SELECT * FROM notification_outbox WHERE recipient_user_id = $1 AND channel = 'in_app' AND status = 'delivered' ORDER BY created_at DESC`. Mark-as-read updates `read_at`. No separate `notification_history` table.

### Dedup and coalescing

Each enqueue accepts an optional `dedup_key`. **Unbuilt:** the partial unique index on `(recipient_user_id, channel, dedup_key)` over `queued`/`delivering` rows exists, but no event returns a non-empty key and `EnqueueOutbox` has no `ON CONFLICT` clause, so a colliding enqueue would error rather than drop. The semantics:

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

**The model.** A resolvable event type declares `resolvable: true` (declared on the event type, like audience) and supplies a `dedup_key` that identifies the *condition*, not the individual message. Its outbox rows carry `resolution_state: active`. The condition is cleared in one of two ways, both of which transition every `active` row matching that `dedup_key` (across all recipients and channels) to `resolved`:

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

**What's built.** Only `request.pending` resolves, keyed on its dedup key (`request:<id>:request.pending`), via `NotificationService.ResolveRequestPending` — called after a request is approved, denied or withdrawn while pending, best-effort. It differs from the generic model above in one place: every recipient's `in_app` row is resolved whatever its delivery status (a still-queued one is delivered resolved, not superseded), so the bell shows the request dimmed with its outcome. The outcome is read from the request when the inbox is read: approved or later reads "approved", denied "denied", canceled "withdrawn". Undelivered `push`/`email` rows (`queued`, `awaiting_config`, or claimed `delivering`) become `superseded`. The worker's mark-delivered and reschedule transitions apply only to a row still `delivering`, so a row superseded mid-send stays superseded; that one in-flight attempt may still arrive, and nothing retries it. Delivered rows are left alone. There is no `resolvable` flag, `Resolves:` declaration, or replace-on-repeat yet.

### Push subscriptions

A push subscription is a capability of a login: each row belongs to one `user_session`, and a session holds at most one. The owner is derived through the session, and fan-out joins only live sessions (not revoked, not expired), so revoking a session silently stops its push. Logins and push devices are one list in the devices UI. Tracked in `push_subscription`:

| Column             | Meaning                                                              |
| ------------------ | -------------------------------------------------------------------- |
| `id`               | UUID                                                                 |
| `session_id`       | FK to `user_session` (CASCADE), unique                               |
| `endpoint`         | push service delivery URL, globally unique                           |
| `p256dh`           | client-provided encryption key                                       |
| `auth`             | client-provided auth secret                                          |
| `user_agent`       | best-effort device label                                             |
| `created_at`       | timestamptz                                                          |
| `last_used_at`     | timestamptz — last (re)subscribe                                     |
| `last_notified_at` | nullable timestamptz — set on each successful push                   |

The user-set device name lives on `user_session.label`. Registering clears any prior row for the session or endpoint first, so a browser that signs into a new session re-homes its endpoint.

One outbox row fans out to every device. A 404/410 endpoint is pruned immediately and ignored. If any device received it, the row is `delivered`. If none did, a transient failure retries the whole row, and a permanent one marks it `dead`. A user with no devices is a successful no-op. The device re-registers to get a fresh row.

### Read-state for in-app

The `in_app` channel uses the outbox row directly. The bell-icon UI reads recent rows; user actions on the UI update `read_at`. There is no separate read-receipts table; for the in-app surface, the outbox row is the durable record.

Unread count is `COUNT(*) WHERE channel='in_app' AND recipient_user_id=$1 AND status='delivered' AND read_at IS NULL`. Marking all as read is a single `UPDATE`. With the resolution lifecycle (unbuilt), the count also excludes `resolution_state = 'resolved'`, so a resolved standing-condition row drops out of the attention count even if never explicitly read.

The API is `GET /api/v1/notifications` (rendered inbox), `GET …/unread-count`, `POST …/{id}/read`, `POST …/read-all`, and `GET`/`PUT …/preferences`; push lives under `/api/v1/notifications/push/…` (public key, subscriptions, per-device test).

## Email and SMTP configuration

Email is architected as a peer to push and in-app — same adapter shape, same outbox flow, same template system. But unlike push (which uses VAPID with self-generated keys) and in-app (which is just a DB write), email requires an SMTP relay the admin has to configure.

The relay is the `email_provider` singleton (Settings ▸ Email, `/api/v1/settings/email`): provider `smtp`, from-address/name, reply-to, host, port, security (`starttls` / `implicit_tls` / `none`), optional auth, and an `enabled` flag. The password is write-only on the wire. Settings offers a synchronous "send test email" and a test-config probe.

The pattern handles this gracefully:

1. **Adapter reports configuration state.** `IsConfigured()` is true when an `email_provider` row exists and is enabled.
2. **Preferences UI marks email unavailable when unconfigured.** The preferences read reports each channel's `available` flag, and the `email` toggle is unavailable while no enabled provider exists.
3. **Outbox rows park.** Email rows are written normally at enqueue. When the worker finds the adapter unconfigured, it moves them to `status='awaiting_config'`. They are *not* dropped — they accumulate. The delivery worker ignores them.
4. **On configuration, the recent backlog drains.** Saving an enabled provider requeues `awaiting_config` rows created in the last 7 days (`AwaitingConfigDrainWindow`); older rows stay parked. This keeps a fresh SMTP setup from blasting users with a year of accumulated mail. **Unbuilt:** the interactive "Drain queued emails since *date*?" prompt with drain-since / never options.

**Transactional (system-audience) email** is special-cased: `EnqueueTransactionalEmail` checks configuration synchronously and, if SMTP isn't configured, enqueues nothing and reports `enqueued=false` so the caller can respond in-band. Invite creation succeeds either way and returns a copyable magic link, with `emailed` telling the admin whether the email was also sent. Transactional rows never park.

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
| **[Acquisition](../acquisition/README.md)**                       | Producer: `want.grabbed`, `want.available`, `upgrade.proposed`, `tracking.archived`. Acquisition enqueues typed events at the appropriate lifecycle points; today the import worker emits `want.available`, and the rest are unbuilt. |
| **[Tracking](../tracking/README.md)**                             | Producer: `tracking.episode_imported`, `tracking.archived`, `upgrade.proposed`. Emits typed events (unbuilt).                                                                |
| **[Requests](../requests/README.md)**                             | Producer: `request.pending_review`, `request.decision_made`, `request.expired`. Both user-audience (the requester) and admin-audience (the approval queue).            |
| **[Quality profiles](../quality-profiles/README.md)**             | Producer: upgrade-available data feeds the `upgrade.proposed` event payload.                                                                                           |
| **[Matching](../matching/README.md)**                             | Producer: `match.dropped_in` for drop-in auto-matches that resolve open wants.                                                                                         |
| **[Metadata](../metadata/README.md)**                             | Producer: `metadata.renumber` when TMDB renumbers a series and overrides need review.                                                                                  |
| **[Hygiene](../hygiene/README.md)**                               | Producer: `hygiene.error_finding` (immediate, dedup'd) and `hygiene.digest_weekly` (producer-owned schedule).                                                          |
| **[Connectivity-health pattern](../../patterns/connectivity-health/README.md)** | Producer: `connectivity.failed` (resolvable) and `connectivity.recovered` (declares `Resolves` the failed key) on `failed`-tier transitions. Subscriber via `<resource_type>.health` SSE channels. |
| **[Audit pattern](../../patterns/audit/README.md)**               | Subscriber: some audit events trigger notifications (approval-needed, manual-override-applied). Notifications references audit rows by ID in event payloads.           |
| **[Users](../users/README.md)**                                   | Recipient resolution for `admin` audience uses the permission keys defined there. User identity (email, push subscriptions) hangs off `app_user`.                      |
| **[Realtime](../realtime/README.md)** (SSE)                           | Sibling channel — *not* a notification channel. SSE delivers real-time UI updates; notifications delivers durable events. Both can fire on the same event.            |

## Tables

Owned here:

- `notification_outbox` — the durable record of every attempted delivery
- `notification_preference` — per-user, per-bundle subscription + email/push toggles
- `email_provider` — the singleton SMTP relay config
- `vapid_config` — the singleton VAPID keypair and `sub` contact URI
- `push_subscription` — per-session Web Push endpoints

Referenced (owned elsewhere):

- `app_user`, `permission_grant` — see [users](../users/README.md)
- `auth_audit` — see [users](../users/README.md#admin-action-audit); admin-action notifications surface from here

## Build slices

The full spec is large — VAPID push, an MJML email pipeline, the preference matrix, the resolution lifecycle, SMTP with drain UX. The build proves the **spine** first, then layers channels and the admin path onto it.

**Spine (built).** `user` audience; the [outbox](#the-outbox) with its status lifecycle, the retry worker and the stale-claim reaper; [typed events](#the-typed-event-api) with startup template verification; bundle-level [preferences](#preferences-and-bundles) for `my_requests` + `library_activity`; the bell-icon UI (inbox query, unread count, mark-read). The one producer is `want.available` (→ each requester of the want's tracking), emitted by the import worker. **Unbuilt:** `request.decision_made`.

**Push (built).** `push_subscription` bound to sessions, the VAPID Web Push adapter, per-device fan-out, dead-endpoint pruning on 410/404.

**Email (built).** The `email_provider` SMTP adapter, `IsConfigured()` gating, `awaiting_config` parking with the 7-day automatic drain, the MJML→HTML generation step, and transactional literal-recipient email. `invite.created` is the first transactional producer. **Unbuilt:** `auth.password_reset` and `auth.2fa_code`, whose auth flows don't exist.

**Admin audience (unbuilt).** The [reverse resolver](#admin-recipient-resolution) and the event→key map; the admin bundles plus eligibility gating in the prefs UI; first admin producers `request.pending_review` and `connectivity.failed` / `.recovered`. This slice introduces the [resolution lifecycle](#resolvable-notifications-resolution-lifecycle) — connectivity is the first resolvable event. The reverse resolver builds directly on `internal/authz`.

## Open questions

1. **Bundle catalog finalization.** The starter set (`my_requests`, `library_activity`, `admin_alerts`, `admin_summaries`) is directional. The full catalog and the bundle-default channels per bundle. Lean: ship the four-bundle starter; add new bundles as new event-type categories emerge. *Partially pinned: the code ships `my_requests`, `library_activity` and `admin_alerts` with their default channels ([Preferences](#preferences-and-bundles)); `admin_summaries` is not built. The full catalog is still to pin.* **Decided:** An `admin_alerts` bundle ships in V1 carrying REQ-APPROVE-001, push on and email off by default.
2. **Admin install-wide defaults vs per-admin only.** When a new admin is promoted, they get bundle defaults. Should those defaults be configurable per-install ("our team wants admin_alerts on email + push by default") or hardcoded? Lean: configurable per-install, surfaced in Settings → Notifications → Admin defaults. *Frozen — beyond V1.*
3. **System-event registry — do we expose this in the UI?** `system` audience events bypass user preferences entirely. Should the UI at least *show* the user "we will email you for password resets and invites" so it's not opaque? Lean: yes, a read-only "system emails" disclosure section in the preferences page. *Frozen — beyond V1.*
4. **Dedup semantics: drop vs replace — resolved.** Transient events **drop** the colliding enqueue (the simple default). Resolvable events **replace** the payload (keeping the original `created_at`) so the standing condition shows its latest state. See [Resolvable notifications](#resolvable-notifications-resolution-lifecycle). **Answered by the code:** `dedup_key` with constructor-owned policy (`db/queries/notification.sql:10-23`).
5. **`awaiting_config` drain UX.** When SMTP is first configured, the admin sees "Drain N queued emails?" — but `N` could be huge if the system has been running unconfigured for months. Default to "drain only those from the last 7 days"? Per-event-type cutoffs? Lean: 7-day default with admin override. — *Answered by the code in part: saving an enabled provider automatically drains the last 7 days; there is no prompt or override yet.*
6. **Push subscription cleanup cadence.** Subscriptions that haven't seen activity in months are likely dead. Run a periodic prune (e.g., 6-month silence + last delivery attempt failed). Lean: yes; prune at the hygiene cadence. *Frozen — beyond V1.*
7. **Quiet hours.** Per-user "don't push between 11pm and 7am" — common Tier-2 feature. Defer to v2; flag here. The model accommodates it as a delivery-side filter (rows queued during quiet hours park with `next_attempt_at` set to wake-time). *Frozen — beyond V1.*
8. **Cross-event accumulator.** "Hold all notifications from the last 8 hours and deliver as a single push at 9am" — not the same as producer-owned batching. Out of v1; flag for v2. Same delivery-side filter shape as #7. *Frozen — beyond V1.*
9. **Channel-level rate limits.** A push gateway might 429 us; SMTP relays have hourly caps. Today we just back off per outbox row. Worth a per-channel global throttle? Lean: defer; the back-off pattern handles spikes adequately at self-hosted scale. *Frozen — beyond V1.*
10. **Localization.** All templates are English in v1. The structure (templates as files, separate subject/title/body) doesn't preclude i18n later — a future iteration adds a locale parameter to template lookup. Lean: explicitly defer; document the upgrade path. *Frozen — beyond V1.*
11. **Template editing / custom branding.** Some self-hosters will want custom email branding ("from My Family Library"). Options: a single "branding" template snippet that all emails include; full per-template override via DB rows; do nothing. Lean: ship a `branding.mjml` partial in v2 that's the one customization knob. *Frozen — beyond V1.*
12. **Migrating existing in-flight notifications when an event-type is renamed.** If `want.available` becomes `media.available`, what happens to outbox rows already enqueued under the old name? Lean: never rename in place — deprecate the old event type, add the new, let outbox drain naturally. *Frozen — beyond V1.*
13. **Multi-recipient single-row optimization.** A `connectivity.failed` event going to 5 admins writes 5 outbox rows. Each is a distinct delivery; status tracking is independent. Is there value in a "broadcast row" that fans out at delivery time? Lean: no — 5 rows for 5 admins is the cleaner model and the cardinalities are tiny. **Answered by the code:** One outbox row per recipient per channel (`recipient_user_id` in `db/queries/notification.sql:13`).

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

- [Story 1](../../stories/01-happy-path-auto-approve.md) — the auto-approve happy path that pre-committed to the typed-event + per-user push + `push_subscription` + `notification_preference` shape
- [Users](../users/README.md) — defines the recipient model, admin permission resolution, and the related `auth_audit` admin-action stream
- [Acquisition](../acquisition/README.md) — the primary user-facing notification producer; pre-committed `NotificationService (with push channel)` in its own service list
- [Hygiene](../hygiene/README.md) — the primary producer of producer-owned batching (weekly digest of warnings + immediate push for errors)
- [Connectivity-health pattern](../../patterns/connectivity-health/README.md) — emits `<resource_type>.health` transitions that become admin notifications
- [Audit pattern](../../patterns/audit/README.md) — sibling cross-cutting system; some audit-row creations trigger notifications
- [Name templates](../name-templates/README.md) — sibling templating system; sharing `text/template` DSL means operators learn one syntax for the whole app
- [Errors pattern](../../patterns/errors/README.md) — typed error model; channel adapters return typed errors that the retry worker maps to transient vs permanent
