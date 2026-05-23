# Users — identity, roles, and permissions

**Status:** Draft, iteration 1

This doc defines how Arrflix models users, how they get into the system, and what they're allowed to do. It captures the identity model, the role-and-permission system, onboarding strategies, per-user policy state, and the "what's mine" scoping that the [audit pattern](../../patterns/audit/README.md) defers here. It does **not** pin down table columns, API shapes, or the [request entity](../requests/README.md) itself — those live in iteration 2 and the sibling spec.

## TL;DR

- Two layers. **Permissions** answer "can this user do X?" — bundled into **roles** and assignable directly to users. **`user_policy`** answers "with what limits and per-user defaults?" — only quotas and state that *can't* be expressed as a permission.
- **Tier and type are encoded in the permission key**, not in separate columns. `requests.create:movie:hd`, `requests.auto_approve:series:4k`. Per-library scope uses the existing `resource_id` column on the grant — enum-shaped attributes stay in keys, opaque UUIDs stay tabular.
- **Auto-approve is a permission, not a flag.** If you hold `requests.auto_approve:movie:hd`, your HD movie requests bypass the approval queue. Uniform with every other capability.
- **Roles are bundles of grants, not magic.** Built-in roles are pre-seeded; custom roles work identically. `built_in=true` just prevents deletion. Effective permission set = union of role grants ⊕ direct user grants, with deny overriding allow.
- **Three onboarding strategies**: `invite_only` (default — admin pre-creates email; user signs up; invite is claimed), `open` (anyone with an email), and `plex_friends` (auto-onboard anyone on the admin's Plex friends list). Strategy is a system setting.
- **`auth_audit` is the home for admin-action history** — who promoted whom, who approved which request, who changed strategy. Different shape from the [decision-artifact pattern](../../patterns/audit/README.md): that's automated decisions; this is humans-touching-other-humans.
- This spec **does not own the request entity, the approval flow, or notification delivery** (see [notifications](../notifications/README.md)). It defines the permission keys those things consult.

## Why this is its own spec

Users, roles, and permissions touch every other module: the [request flow](../requests/README.md) consults them on every request; [tracking](../tracking/README.md) ties requesters to series; [routing](../routing/README.md) may eventually condition rules on user context; the [audit pattern](../../patterns/audit/README.md) scopes activity feeds against them. If each module defined its own permission shape, we'd end up with three or four parallel systems of who-can-do-what. Centralizing here means each module says "I depend on these permission keys" and the runtime check is the same everywhere.

The model also has to absorb features we know are coming: per-library access for kids' libraries, auto-onboard from Plex friends, multi-requester tracking, quota enforcement. Designing those in upfront — even when they're deferred to implementation — beats retrofitting them later.

## Identity model

A user is an `app_user` row plus zero or more `user_identity` rows.

**`app_user`** carries the canonical account state: email, username, optional password hash (for local login), avatar URL, `is_active`, timestamps. Username is unique case-insensitively; email is unique case-insensitively when non-NULL.

**`user_identity`** carries one row per external identity provider linked to the account. v1 supports two providers via the `auth_provider` enum:

- `local` — email + password authentication. The `password_hash` column on `app_user` is the durable credential.
- `plex` — Plex SSO. The identity row carries the Plex subject (numeric ID, stable) and the most recent access token. The Plex flow uses Plex's PIN/redirect handshake; the resulting subject becomes the identity row's `(provider, subject)` unique key.

A single account can have multiple identities — the canonical case is "user signed up locally, then linked Plex." The `user_identity` row carries the per-provider token state; `app_user.password_hash` is independent of any external token. When a Plex SSO login arrives for an email that already matches a local account, the Plex identity is linked to that account rather than spawning a duplicate user.

Adding a new provider (Discord, GitHub, whatever) is an `ALTER TYPE ... ADD VALUE` plus a service-layer flow; the data model already accommodates it.

## Roles

A role is a named bundle of permission grants.

- **Roles are not magic.** A role grant and a directly-granted user permission compose the same way — they're both `permission_grant` rows with different `subject_type` values. The runtime check doesn't branch on subject kind.
- **`role.built_in = true`** marks system-seeded roles. Built-in roles can be edited (grants added/removed) but not deleted. Custom roles can be created and deleted freely.
- **A user can have many roles.** The `user_role` join table is many-to-many. The current "assign role replaces all" behavior in `UsersService` is a UI choice, not a model constraint — multi-role assignment becomes available via the same join table once the UI catches up.
- **No role inheritance.** Roles don't include other roles. If you want a "trusted_friend" role that's "requester plus 4K," create it as its own bundle. Inheritance adds resolution complexity for marginal gain at this scale.

### Starter built-in roles

Directional; iteration 2 pins the exact seed grants:

| Role           | Captures                                                                            |
| -------------- | ----------------------------------------------------------------------------------- |
| `admin`        | All permissions, including `admin.users.manage`. Cannot be the last admin removed.  |
| `co_admin`     | Operational control (libraries, media, requests, jobs, settings) without `admin.users.manage`. The "spouse who can run the box but can't delete you" role. |
| `requester`    | Browse + request HD content. No 4K, no approval power, no admin. The default new-user role. |
| `viewer`       | Browse only. No requests, no admin. Read-only friend / houseguest.                  |

The current seeded roles (`admin` / `manager` / `user` / `guest`) map cleanly: `manager → co_admin`, `user → requester`, `guest → viewer`. Since there's one user today (Kyle), the existing seed migration is replaced rather than data-migrated.

Custom-role examples sit on top of this without changing the model: a `family` role granting HD movie + series request and auto-approve; a `kids` role with `library.read` scoped to a specific library UUID; a `co_admin` variant without the `admin.settings.write` they hold by default.

## Permissions

A permission is a string key naming a capability. Keys are **structured**: they encode both the verb and the qualifiers that scope it.

### Key syntax

```
<domain>.<action>[.<sub_action>][:<qualifier>[:<qualifier>...]]
```

- **`domain`** — top-level area (`requests`, `library`, `media`, `tracking`, `jobs`, `hygiene`, `admin`). Dotted sub-namespaces allowed (`admin.users`, `admin.settings`).
- **`action`** — the verb (`create`, `read`, `write`, `approve`, `cancel`, `manage`, `auto_approve`, ...).
- **`sub_action`** — when scope matters within an action (`own` vs `any`, e.g., `requests.cancel.own`).
- **`qualifier`** — colon-separated enum-shaped attributes. Order matters within a key: `:<type>:<tier>` for request keys. Position is part of the contract — grep finds the same key the same way.

### Why structure in the key string

Two alternatives we considered:

1. **A second `attrs JSONB` column on `permission_grant`** — flexible but requires a parser at every check site, and `JSONB @>` containment queries make grants harder to reason about.
2. **Multi-row composition** (one grant per dimension) — same idea, more rows. The composition logic gets baked into the check.

Encoding in the key keeps the runtime check a string equality (`exists permission_grant where permission_key = $1 and subject in (...)`). The cost is that adding a tier means adding new keys, not new rows. That trade is right at this scale.

**Per-resource scope** (specific library UUIDs) uses the existing `resource_type` / `resource_id` columns on `permission_grant`. UUIDs don't go in the key string because they're not enumerable. A grant for "the kids library only" is `(permission_key='library.read', resource_type='library', resource_id=<kids_uuid>)`.

### Starter catalog

Directional; finalized in iteration 2. The catalog is **append-only** — adding a key is a code change, removing one is a migration.

| Key                                     | Meaning                                            |
| --------------------------------------- | -------------------------------------------------- |
| `requests.create:<type>:<tier>`         | Eligibility to request `<type>` at `<tier>`        |
| `requests.auto_approve:<type>:<tier>`   | Auto-approve `<type>` at `<tier>` (bypass queue)   |
| `requests.approve`                      | Approve / deny others' requests                    |
| `requests.cancel.own` / `.any`          | Cancel own / anyone's requests                     |
| `requests.view.own` / `.any`            | View own / all requests in admin queue             |
| `library.read`                          | Browse library content (resource-scopable)         |
| `library.write`                         | Create / edit library configs                      |
| `library.scan`                          | Trigger scans                                      |
| `media.write`                           | Fix-match, manual matching, edit metadata          |
| `tracking.create.own` / `.any`          | Subscribe self / on behalf of others               |
| `tracking.cancel.own` / `.any`          | Cancel own / any tracking                          |
| `jobs.read` / `.manage`                 | View / retry / cancel jobs                         |
| `hygiene.read` / `.resolve`             | View findings / take remediation action            |
| `admin.users.manage`                    | Invite, edit, disable users; assign roles          |
| `admin.settings.read` / `.write`        | Read / change system settings                      |
| `admin.indexers.manage`                 | Manage indexer connections                         |
| `admin.downloaders.manage`              | Manage downloader connections                      |

`<type>` is `movie | series`. `<tier>` is the tier registry from [quality profiles](../quality-profiles/README.md) — initially `hd | 4k`. New tiers added there flow into the keyspace.

## Grants

A grant binds a permission to a subject, optionally scoped to a resource, with an effect.

```
permission_grant(
  subject_type   : 'role' | 'user',
  subject_id     : UUID,
  permission_key : TEXT,
  resource_type  : TEXT  (nullable — NULL = global),
  resource_id    : UUID  (nullable),
  effect         : 'allow' | 'deny',
)
```

### Resolution

To answer "does user U have permission P (on resource R)?":

1. Collect candidate grants:
   - All grants where `subject_type='role' and subject_id in (U's role IDs)`
   - All grants where `subject_type='user' and subject_id = U`
2. Filter by `permission_key = P`.
3. Filter by resource match: a global grant (`resource_id IS NULL`) matches anything; a resource-scoped grant matches only when `(resource_type, resource_id) = (R_type, R_id)`.
4. If any matching grant has `effect = 'deny'`, the answer is **no**.
5. Otherwise, if any matching grant has `effect = 'allow'`, the answer is **yes**.
6. Otherwise, the answer is **no**.

**Deny wins.** Allow grants compose freely; a single deny shuts the gate. This is the conventional shape across IAM systems.

Direct user grants are an **exception path**, not the main mechanism. The expected use is a one-off ("give just this person 4K without inventing a new role"). Most permissions flow through roles.

### Resolution caching

Permission checks fire on most authenticated requests. Re-querying the database for every check is wasteful. The expected shape: build the user's effective grant set once per request (or cache for the JWT's lifetime) and check against the in-memory set. v1's invariant is forgiving — a user has at most a few roles with a few dozen grants each, well below a hundred rows.

The cache shape and invalidation strategy are implementation. The model is what's load-bearing.

## `user_policy`

Per-user state that **can't** be expressed as a permission. v1 fields are minimal:

| Field                    | Purpose                                                                          |
| ------------------------ | -------------------------------------------------------------------------------- |
| `max_pending_requests`   | Quota: most requests in flight at once. Default from system setting `requests.max_per_user`. |
| `request_size_cap_gb`    | Optional: largest single download a user can trigger. Useful as a 4K guardrail.   |

What deliberately **isn't** here:

- **Auto-approve toggles** — those are permissions.
- **Tier eligibility** — that's a permission.
- **Notification preferences** — separate concern (see [notifications](../notifications/README.md)).
- **Push subscriptions** — same.

Future quota-shaped fields (monthly request count, monthly bytes) fit the same pattern and don't require schema redesign.

## Onboarding

How users come to exist. The active strategy is a system setting (`auth.signup_strategy`); the **default is `invite_only`**.

### Strategies

| Strategy        | Flow                                                                                       |
| --------------- | ------------------------------------------------------------------------------------------ |
| `invite_only`   | Admin adds email to `user_invite`. User signs up (local form or Plex SSO) with matching email. Invite is claimed (`claimed_at` set). Account created with the strategy's default role. |
| `open`          | Anyone can sign up. Default role assigned. Useful for fully-public setups; rare.            |
| `plex_friends`  | Anyone on the admin's Plex friends list can sign in via Plex SSO and is auto-onboarded with the default role. The invite path still works alongside. |

`plex_friends` matches Overseerr's onboarding model. It depends on the admin's account being able to enumerate friends via the Plex API; the strategy fails open to "Plex SSO with no auto-onboard" if the friends API is unreachable.

### Default role per strategy

Each strategy has a configurable default role for newly-created users. `requester` is the seeded default for `invite_only` and `plex_friends`; `viewer` is the seeded default for `open`. The default can be changed per-strategy in settings.

### Inactive accounts

`app_user.is_active = false` blocks all authentication paths. Admins disable rather than delete when a user might come back — disabling preserves the row, all request history, and any tracking the user was a requester for. Deletion is a separate path that cascades through `user_role`, identity rows, and request authorship (which becomes orphaned or reassigned to a tombstone user, TBD in iteration 2).

## Activity-visibility scoping

The [audit pattern](../../patterns/audit/README.md) defers "what does a non-admin see in the activity feed?" to this spec. The answer:

A user **owns** an activity row if:

- They are the `requested_by` of the originating [request](../requests/README.md), or
- They are in the requesters set of the originating [tracking](../tracking/README.md), or
- The activity acts on a media item they currently have an open want for.

Admins see everything. Admins additionally have a **"view as user X"** affordance for support — read-only scoping into another user's feed. Every "view as" session writes an admin-action audit row so the masquerade is traceable.

For users with `requests.view.any` (without being full admin), scoping is bypassed — they see everything regardless of authorship. This is how `co_admin` ends up seeing the full activity feed.

The scoping predicate is a single function the activity-view renderer calls per row, conceptually `is_visible_to(user, activity_row) -> bool`. The exact query shape is implementation.

## Admin-action audit (`auth_audit`)

The dormant `auth_audit` table is the home for **human-on-human actions**: who promoted whom, who disabled which account, who approved which request, who changed the signup strategy, who created which custom role. It's distinct from the per-decision audit pattern — that's automated decisions; this is administrative changes.

Conceptually each row carries:

- `actor_user_id` — who took the action
- `event` — typed event name (`role.assigned`, `user.disabled`, `invite.created`, `request.approved`, `strategy.changed`, `view_as.started`, ...)
- `target_user_id` (nullable) — who was affected, when there is one
- `detail` JSONB — event-specific payload
- `created_at`

Write points include:

- Role assignment / unassignment
- Permission grant / revoke (direct user grants)
- User activate / deactivate / delete
- Invite create / revoke
- Strategy change (system setting)
- Request approve / deny (the requests spec writes these)
- "View as" sessions start / stop

Retention follows the same configurable cadence as the [decision-artifact retention policy](../../patterns/audit/README.md#retention), but is owned by the System → Retention surface there; this spec just declares the audit type exists.

## What this spec does NOT own

- **The request entity and its lifecycle** — [requests spec](../requests/README.md).
- **Approval-policy evaluation** at request time — same (consults permissions defined here).
- **Notification preferences, delivery, and push subscriptions** — see [notifications](../notifications/README.md).
- **The tier registry** — [quality profiles](../quality-profiles/README.md). The permission-key qualifier `<tier>` references that registry by name.
- **Per-decision audit rows** — [audit pattern](../../patterns/audit/README.md).
- **Watch state** — future Plex/Jellyfin integration.
- **JWT shape and session management** — implementation, not spec.

## Interactions

| Neighbor                                              | How users interacts                                                                                          |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| **[Requests](../requests/README.md)**                 | Consumes `requests.create:<type>:<tier>` and `requests.auto_approve:<type>:<tier>` keys. Reads `user_policy.max_pending_requests` for quota enforcement. Writes admin-action audit rows on approve / deny. |
| **[Tracking](../tracking/README.md)**                 | Requesters set is `app_user` IDs. Tracking-level visibility scoping uses this spec's predicate.              |
| **[Quality profiles](../quality-profiles/README.md)** | Tier registry. Permission keys reference tier names; profile bindings to tiers happen there.                 |
| **[Routing](../routing/README.md)**                   | Open Q5 (per-user routing rules) reads user context — role IDs, user ID — from this spec's identity primitives. |
| **[Audit pattern](../../patterns/audit/README.md)**   | Activity-view scoping predicate defined here. Retention configuration lives there.                           |
| **[Acquisition](../acquisition/README.md)**           | `manual_override` audit rows carry the picking user's ID. No other coupling.                                 |
| **[Notifications](../notifications/README.md)**       | Per-user notification preferences and push subscriptions will live there but reference user IDs from here.   |

## Open questions

1. **Built-in role set finalization.** The starter (`admin` / `co_admin` / `requester` / `viewer`) is directional. Should `co_admin` exist as built-in, or is it a custom role most installs don't need? Lean: keep it built-in; multi-admin households are common.
2. **Wildcard grants vs explicit seeding.** Admins get every key via explicit seed grants. New keys added in a future migration require an admin backfill. Wildcard semantics (`requests.create:*:*`) would solve this but complicate the runtime check. Lean: no wildcards; document the backfill as part of "adding a permission key" in the contributor docs.
3. **Permission-key catalog freeze.** The starter list is directional. Iteration 2 should pin the exact set and the seed grants for each built-in role.
4. **Cache invalidation strategy.** Role and grant changes need to propagate. Options: short JWT TTL (15min) so caches expire naturally; explicit cache-bust on change; cache keyed by `(user_id, settings_version)`. Lean: explicit bust via in-process pub/sub; we're single-instance for the foreseeable future.
5. **Deny grants — worth keeping?** Real use cases are narrow ("revoke 4K from this one user temporarily"). The composition rule (deny wins) is standard. Risk is admin confusion at scale. Lean: ship `deny` since the column exists and the semantics are well-trodden; surface it sparingly in UI.
6. **Plex friends sync cadence.** On every login, periodic sweep, or both? Periodic catches removals (was a friend, no longer is); on-login is the natural pivot point. Lean: on-login check + a daily sweep for removals.
7. **"View as user" mechanics.** Session-scoped impersonation (audit row + masquerade for N minutes) or per-request `?as=` parameter (audited per request)? Lean: session-scoped with a persistent visible banner so the admin can't forget they're in someone else's view.
8. **Last-admin protection scope.** Currently delete protects against removing the last admin. Should disabling protect too? What about removing the `admin` role from the last admin (a different code path that today would orphan the system)? Lean: protect all three.
9. **Deletion vs disable vs tombstone.** When a user with request history is deleted, what happens to the authorship link? Options: cascade-null (history orphaned), reassign to a tombstone user, soft-delete (`deleted_at` column, row preserved). Lean: soft-delete via `is_active=false`, real delete cascades to authorship-null. Pin in iteration 2.
10. **Multi-role UI shape.** Today the UI assigns one role. The model already supports many. When the multi-role UI lands, is there a "primary role" for display purposes, or does the user just have a list? Lean: display shows the highest-permission role with a "+N more" indicator.

## What we're explicitly not deciding here

- Exact `user_policy` schema and column types
- API endpoint shapes for user / role / permission management
- The permission-check helper's exact Go signature
- Permission cache implementation
- Plex friends API integration details (auth, pagination, rate limits)
- Two-factor authentication or other strengthened-auth flows
- Session management beyond JWT (refresh tokens, revocation lists)
- The UI for the permission editor (a checkbox grid is the assumed shape; exact layout is implementation)
- The exact admin-action audit event vocabulary — directional only
- Whether multiple admins per install need any coordination primitives beyond audit-log visibility

## Doc neighbors

- [Requests](../requests/README.md) — the primary consumer; defines the request entity and approval flow against this spec's permission keys
- [Tracking](../tracking/README.md) — uses requesters set; multi-user semantics tie into per-user policy
- [Quality profiles](../quality-profiles/README.md) — tier registry referenced by permission key qualifiers
- [Audit pattern](../../patterns/audit/README.md) — activity-visibility scoping defined here; retention there
- [Errors](../../patterns/errors/README.md) — typed error model (`Forbidden`, `Unauthenticated`)
- [Story 1](../../stories/01-happy-path-auto-approve.md) — the auto-approve happy path that drove the model shape
