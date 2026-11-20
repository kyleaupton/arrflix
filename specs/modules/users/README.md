---
status: v1
---

# Users — identity, roles, and permissions

> **Beyond what's built:** custom roles, the `plex_friends` strategy and per-strategy default roles (§Onboarding), `user_policy` quotas, §Activity-visibility scoping ("view as"), and the admin-action half of §Admin-action audit describe unbuilt design. The four-role starter set and custom roles are **superseded by [V1.md](../../V1.md#roles-and-permissions-decided)** (two perspectives, `requester | admin`, per-user auto-approve and 4K toggles, no role editor); quotas are V2.

This doc defines how Arrflix models users, how they get into the system, and what they're allowed to do. It captures the identity model, sessions, the role-and-permission system, onboarding strategies, and the "what's mine" scoping that the [audit pattern](../../patterns/audit/README.md) defers here. It does **not** own the [request entity](../requests/README.md) itself.

## TL;DR

- **Permissions** answer "can this user do X?" — bundled into **roles** and assignable directly to users. Per-user limits that *can't* be expressed as a permission (quotas) are V2; there is no `user_policy` table.
- **Authentication is session-based.** Login opens a `user_session` row; a short-lived access JWT is a stateless projection of it, and a rotating refresh token (HttpOnly cookie) mints new ones.
- **Tier and type are encoded in the permission key**, not in separate columns. `requests.create:movie:hd`, `requests.auto_approve:series:4k`. Per-library scope uses the existing `resource_id` column on the grant — enum-shaped attributes stay in keys, opaque UUIDs stay tabular.
- **Auto-approve is a permission, not a flag.** If you hold `requests.auto_approve:movie:hd`, your HD movie requests bypass the approval queue. Uniform with every other capability.
- **Roles are bundles of grants, not magic.** Built-in roles are pre-seeded; custom roles work identically. `built_in=true` just prevents deletion. Effective permission set = union of role grants ⊕ direct user grants, with deny overriding allow.
- **Onboarding strategies**: `invite_only` (default — admin issues a magic-link invite carrying a target role; the invitee redeems it) and `open` (anyone can self-register). `plex_friends` (auto-onboard anyone on the admin's Plex friends list) is unbuilt. Strategy is a system setting.
- **`auth_audit` is the home for auth and admin-action history.** It records session lifecycle events today; admin actions (who promoted whom, who approved which request, who changed strategy) are unbuilt. Different shape from the [decision-artifact pattern](../../patterns/audit/README.md): that's automated decisions; this is humans-touching-other-humans.
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

## Sessions

A **`user_session`** row is the source of truth for "this device is logged in as this user" (`SessionService`, `internal/service/session.go`).

- **Login** (`auth-login`, `auth-plex-exchange`, `auth-accept-invite`) authenticates, then opens a session. The response carries a signed HS256 **access JWT** (default TTL 24h) whose `sid` claim binds it to the session, and sets an opaque **refresh token** `<sessionId>.<secret>` in an HttpOnly cookie (session TTL `SESSION_TTL`, default 90 days).
- **Only `sha256(secret)` is stored** (`refresh_hash`). `POST /auth/refresh` rotates the secret on every use; the previous hash stays valid for a one-minute grace window so a multi-tab race doesn't trip reuse detection. A secret matching neither generation is treated as theft and revokes the session.
- **Revocation** — `auth-logout` revokes the cookie's session; `/auth/sessions` lists the caller's active sessions (joined to their push subscriptions as a devices list) and revokes one or all others. The access JWT is verified statelessly (`ChiJWT`), so a revoked session's outstanding access token stays valid until it expires.
- Sessions record a best-effort `user_agent` and `ip`; the `label` column (a user-renamable device name) is not written by any endpoint. A background sweeper (`internal/jobs/session`) deletes rows that have been revoked or expired past a retention window.
- Session create / refresh / revoke / reuse-detected events are written to [`auth_audit`](#admin-action-audit-auth_audit).

## Roles

A role is a named bundle of permission grants.

- **Roles are not magic.** A role grant and a directly-granted user permission compose the same way — they're both `permission_grant` rows with different `subject_type` values. The runtime check doesn't branch on subject kind.
- **`role.built_in = true`** marks system-seeded roles. Built-in roles can be edited (grants added/removed) but not deleted. Custom roles can be created and deleted freely. **Unbuilt:** the API exposes only `roles-list`; there is no role or grant editing, and V1.md rules out custom roles.
- **A user can have many roles.** The `user_role` join table is many-to-many. `UsersService.AssignRole` (`PUT /users/{id}/role`) replaces all of a user's roles with one; that is an API choice, not a model constraint.
- **No role inheritance.** Roles don't include other roles. If you want a "trusted_friend" role that's "requester plus 4K," create it as its own bundle. Inheritance adds resolution complexity for marginal gain at this scale.

### Starter built-in roles

The exact per-role grants are pinned in [Seed grants](#seed-grants-frozen--iteration-2) below.

| Role           | Captures                                                                            |
| -------------- | ----------------------------------------------------------------------------------- |
| `admin`        | All permissions, including `admin.users.manage`. Cannot be the last admin removed.  |
| `co_admin`     | Operational control (libraries, media, requests, jobs, settings) without `admin.users.manage`. The "spouse who can run the box but can't delete you" role. |
| `requester`    | Browse + request HD content. No 4K, no approval power, no admin. The default new-user role. |
| `viewer`       | Browse only. No requests, no admin. Read-only friend / houseguest.                  |

All four are seeded (migration `0022_rbac_catalog_freeze`). Per V1.md, `co_admin` and `viewer` stay seeded but hidden: invite and role pickers offer only `requester | admin`. **Unbuilt:** the invite API's `role` enum still accepts `requester, viewer, co_admin, admin`.

Custom-role examples sit on top of this without changing the model: a `family` role granting HD movie + series request and auto-approve; a `kids` role with `library.read` scoped to a specific library UUID; a `co_admin` variant without the `admin.settings.write` they hold by default.

### Seed grants (frozen — iteration 2)

The exact `effect='allow'`, global (`resource_id IS NULL`) grant rows seeded per built-in role. Brace shorthand (`{movie,series}`) expands to the cartesian set of real grant rows — there are **no wildcard grants** ([open question 2](#open-questions)). All entries reference the [frozen catalog](#catalog-frozen--iteration-2).

**`admin`** — the entire catalog. Every key, including `admin.users.manage`. Concretely: `requests.{create,auto_approve}:{movie,series}:{hd,4k}`, `requests.{approve,deny}:{movie,series}`, `requests.cancel.any`, `requests.view.any`, `library.{read,write,scan}`, `media.write`, `tracking.{create,cancel}.any`, `jobs.{read,manage}`, `hygiene.{read,resolve}`, `admin.users.manage`, `admin.settings.{read,write}`, `admin.{indexers,downloaders}.manage`.

**`co_admin`** — `admin` minus `admin.users.manage`. Identical to admin across the request family and all operational domains; it simply cannot manage users. This is the entire admin/co_admin difference. (Holds `admin.settings.{read,write}` by default; the "co_admin without settings-write" variant noted above is a custom role, not this seed.)

**`requester`** — `library.read` (global), `requests.create:{movie,series}:hd`, `requests.cancel.own`, `requests.view.own`. No `auto_approve` (their requests queue for review), no `4k`, no `approve` / `deny`, no admin. Tracking is spawned on their behalf by the request-approval service, so a requester needs no `tracking.*` grant to get content.

**`viewer`** — `library.read` (global) only. Browse, nothing else.

The `.any`-satisfies-own rule means `admin` / `co_admin` need only the `.any` variants of `cancel` / `view`; `requester` gets the `.own` variants. New built-in tiers (e.g. a `family` role) are added as their own seed bundles without disturbing these four.

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

### The dot/colon rule (frozen — iteration 2)

Every separator in a key is chosen by one test: **is the discriminator a closed structural scope, or an open registry enum?**

- **Dot** joins a **closed, structural** segment — domain, sub-namespace, or scope-of-action (`.own` / `.any`). Its legal values are fixed in code. It is part of the capability's *identity*.
- **Colon** prefixes an **open, registry-valued** qualifier — `:<type>` from the media-type enum, `:<tier>` from the [tier registry](../quality-profiles/README.md#tiers). New values enter without touching the grammar.

The test for any new key: fixed set of values → dot; registry-backed → colon. This is what resolves the two naming forms that drifted across the draft specs — `requests.view:all` (colon on a closed scope) is wrong and becomes `requests.view.any`; `requests.approve:movie` (colon on a registry enum) is right and stays.

**Lexical conventions.** Segments are `snake_case` (`auto_approve`). Dots separate structural segments; each enum qualifier is colon-prefixed. Qualifier order is positional and load-bearing — always `:<type>:<tier>`, never reordered — so a `grep` for a key matches it identically everywhere.

### Why structure in the key string

Two alternatives we considered:

1. **A second `attrs JSONB` column on `permission_grant`** — flexible but requires a parser at every check site, and `JSONB @>` containment queries make grants harder to reason about.
2. **Multi-row composition** (one grant per dimension) — same idea, more rows. The composition logic gets baked into the check.

Encoding in the key keeps the runtime check a string equality (`exists permission_grant where permission_key = $1 and subject in (...)`). The cost is that adding a tier means adding new keys, not new rows. That trade is right at this scale.

**Per-resource scope** (specific library UUIDs) uses the existing `resource_type` / `resource_id` columns on `permission_grant`. UUIDs don't go in the key string because they're not enumerable. A grant for "the kids library only" is `(permission_key='library.read', resource_type='library', resource_id=<kids_uuid>)`.

### Catalog (frozen — iteration 2)

This is the pinned key set. The catalog is **append-only** — adding a key is a code change, removing one is a migration. Every key obeys [the dot/colon rule](#the-dotcolon-rule-frozen--iteration-2).

| Key                                     | Meaning                                            |
| --------------------------------------- | -------------------------------------------------- |
| `requests.create:<type>:<tier>`         | Eligibility to request `<type>` at `<tier>`        |
| `requests.auto_approve:<type>:<tier>`   | Requesting `<type>` at `<tier>` auto-spawns (bypass queue) |
| `requests.approve:<type>`               | Approve a pending `<type>` request                 |
| `requests.deny:<type>`                  | Deny a pending `<type>` request                    |
| `requests.cancel.own` / `.any`          | Withdraw own / cancel anyone's requests            |
| `requests.view.own` / `.any`            | View own requests / the full admin queue           |
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

`<type>` is `movie | series`. `<tier>` is the [tier registry](../quality-profiles/README.md#tiers) — currently `hd | 4k`. A new tier there adds new `requests.create:*` / `requests.auto_approve:*` keys and an admin grant backfill (see [open question 2](#open-questions)); it is not a wildcard.

**Two calls baked into the request family**, both departing from the earlier drafts:

- **`approve` and `deny` are split, type-qualified, and distinct keys** — reconciling the users-spec `requests.approve` and the requests-spec `requests.approve:movie` / `requests.deny`. Keeping them as separate stable keys makes "approve-but-not-deny" or "TV-only approver" a **role-config** choice (which grants a role holds), never a **catalog migration** (which keys exist). The built-in roles below grant approve and deny together; splitting them is a later edit to a role, not a schema change.
- **`approve` / `deny` carry `:<type>` but deliberately not `:<tier>`** — the one place the request family breaks symmetry with `create` / `auto_approve`. Tier is the self-service differentiator (HD auto-spawns, 4K queues); tier-scoped *approval authority* instead fragments the queue (a 4K request landing in nobody's approve-set). Tier escalation is expressed the other way — hold `auto_approve:*:hd` but not `auto_approve:*:4k`, and 4K routes to whoever holds `approve:*`. Re-qualifying `approve` with `:tier` later is an expensive migration, so this is a deliberate freeze, not a default.

**`.own` / `.any` runtime semantics.** A scoped pair resolves at the check site as: `.any` grants the action on *any* subject; `.own` grants it only when the subject belongs to the caller. The check tries `.any` first, then falls back to ownership + `.own`. So a holder of `cancel.any` / `view.any` also passes on their own subjects — roles that hold the `.any` variant need not also be granted `.own`.

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

Permission checks fire on most authenticated requests. `AuthzService` (`internal/service/authz.go`) builds each user's effective grant set once (`authz.NewEffectiveSet`, the pure resolver in `internal/authz`) and caches it in-process with no TTL; `Bust(userID)` drops it, and role assignment is the only call site. The invariant is forgiving — a user has at most a few roles with a few dozen grants each, well below a hundred rows.

### Enforcement

- **Operation gate.** `internal/http/authzgate` maps every huma operation ID to a requirement — public, authenticated, one key, or any-of — and denies operations missing from the table (a coverage test keeps it complete). It runs before the body is parsed, so body- or owner-dependent checks are only a floor there.
- **Service refinement.** Services refine the floor: `requests.create:<type>:<tier>` from the request body, `requests.approve|deny:<type>` from the request's type, and `.own` / `.any` pairs via `AuthzService.CanOwnAny`.
- **Frontend.** `/auth/me` returns the user's global-scope effective keys so the UI can gate controls without a second round-trip. Resource-scoped grants never leave the server.

## `user_policy`

> **Unbuilt (V2).** No `user_policy` table exists; per-user quotas are V2 per [V1.md](../../V1.md#out--later), which reshapes them as a rolling GB quota enforced at grab time. The fields below are the earlier design.

Per-user state that **can't** be expressed as a permission:

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

How users come to exist. The active strategy is a system setting (`auth.signup_strategy`); the **default is `invite_only`**. The settings validator accepts only `invite_only` and `open`.

### Strategies

| Strategy        | Flow                                                                                       |
| --------------- | ------------------------------------------------------------------------------------------ |
| `invite_only`   | Admin creates a `user_invite` for an email with a target role (default `requester`). This mints a magic link: the raw token lives only in the link (copyable, and emailed best-effort when SMTP is configured) and only its sha256 is stored; it expires after 7 days. The invitee redeems it at `auth-accept-invite` (choosing username + password), which creates the account with the invite's role, claims the invite (`claimed_at` set), and opens a session. Plex SSO for a new user also claims an unclaimed invite by matching the Plex-verified email. Local self-signup is Forbidden. |
| `open`          | Anyone can self-register (`auth-signup`) and gets `requester`. Intended for instances behind a trusted network perimeter.            |
| `plex_friends`  | **Unbuilt.** Anyone on the admin's Plex friends list can sign in via Plex SSO and is auto-onboarded with the default role. The invite path still works alongside. |

`plex_friends` matches Overseerr's onboarding model. It depends on the admin's account being able to enumerate friends via the Plex API; the strategy fails open to "Plex SSO with no auto-onboard" if the friends API is unreachable.

### Default role per strategy

Each strategy has a configurable default role for newly-created users. `requester` is the seeded default for `invite_only` and `plex_friends`; `viewer` is the seeded default for `open`. The default can be changed per-strategy in settings. **Unbuilt:** `requester` is hardcoded for open signup and Plex onboarding; an invite carries its own role.

### Inactive accounts

`app_user.is_active = false` blocks login (local and Plex). Admins disable rather than delete when a user might come back — disabling preserves the row, all request history, and any tracking the user was a requester for. **Unbuilt:** disabling doesn't revoke existing sessions, and refresh doesn't re-check `is_active`. Deletion is a hard delete that cascades through `user_role`, identity rows, and sessions; `request.requested_by` is `ON DELETE RESTRICT`, so a user with requests can't be deleted. Deleting, disabling or demoting the last active admin is a Conflict; an inactive admin doesn't count toward keeping one.

## Activity-visibility scoping

**Unbuilt.** There is no activity feed or "view as" yet; request visibility is enforced per-request via `requests.view.own` / `.any`.

The [audit pattern](../../patterns/audit/README.md) defers "what does a non-admin see in the activity feed?" to this spec. The answer:

A user **owns** an activity row if:

- They are the `requested_by` of the originating [request](../requests/README.md), or
- They are in the requesters set of the originating [tracking](../tracking/README.md), or
- The activity acts on a media item they currently have an open want for.

Admins see everything. Admins additionally have a **"view as user X"** affordance for support — read-only scoping into another user's feed. Every "view as" session writes an admin-action audit row so the masquerade is traceable.

For users with `requests.view.any` (without being full admin), scoping is bypassed — they see everything regardless of authorship. This is how `co_admin` ends up seeing the full activity feed.

The scoping predicate is a single function the activity-view renderer calls per row, conceptually `is_visible_to(user, activity_row) -> bool`. The exact query shape is implementation.

## Admin-action audit (`auth_audit`)

The `auth_audit` table is the home for auth events and **human-on-human actions**: who promoted whom, who disabled which account, who approved which request, who changed the signup strategy. It's distinct from the per-decision audit pattern — that's automated decisions; this is administrative changes.

Each row carries `user_id` (nullable, `ON DELETE SET NULL` so the trail survives account deletion), `event` (typed name), `detail` JSONB (event-specific payload), and `created_at`. **Unbuilt:** an admin action needs both an actor and a target user, and the table has only the one `user_id`.

The only writer is `SessionService`: `session.created`, `session.refreshed` (when the IP changes), `session.revoked`, `session.reuse_detected`. **Unbuilt:** the admin-action write points:

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

## Interactions

| Neighbor                                              | How users interacts                                                                                          |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| **[Requests](../requests/README.md)**                 | Consumes `requests.create:<type>:<tier>`, `requests.auto_approve:<type>:<tier>`, `requests.approve|deny:<type>`, and the `requests.view` / `requests.cancel` `.own`/`.any` pairs. |
| **[Tracking](../tracking/README.md)**                 | Requesters set is `app_user` IDs. Tracking-level visibility scoping uses this spec's predicate.              |
| **[Quality profiles](../quality-profiles/README.md)** | Tier registry. Permission keys reference tier names; profile bindings to tiers happen there.                 |
| **[Routing](../routing/README.md)**                   | Open Q5 (per-user routing rules) reads user context — role IDs, user ID — from this spec's identity primitives. |
| **[Audit pattern](../../patterns/audit/README.md)**   | Activity-view scoping predicate defined here. Retention configuration lives there.                           |
| **[Acquisition](../acquisition/README.md)**           | `manual_override` audit rows carry the picking user's ID. No other coupling.                                 |
| **[Notifications](../notifications/README.md)**       | Per-user notification preferences and push subscriptions will live there but reference user IDs from here.   |

## Open questions

1. **Built-in role set finalization.** The starter (`admin` / `co_admin` / `requester` / `viewer`) is directional. Should `co_admin` exist as built-in, or is it a custom role most installs don't need? Lean: keep it built-in; multi-admin households are common. **Superseded by [V1](../../V1.md):** `co_admin` is hidden; only `requester` and `admin` are offered (REQ-ROLE-001).
2. **Wildcard grants vs explicit seeding.** *Resolved (iteration 2): no wildcards.* Admins get every key via explicit seed grants (see [Seed grants](#seed-grants-frozen--iteration-2)); the runtime check stays string-equality. Adding a permission key — including a new tier's `requests.*` keys — requires an admin grant backfill, documented as part of "adding a permission key" in the contributor docs. **Answered by the code:** Explicit seed grants (`0022_rbac_catalog_freeze.up.sql:54-90`).
3. **Permission-key catalog freeze.** *Resolved (iteration 2):* the [catalog](#catalog-frozen--iteration-2) and [per-role seed grants](#seed-grants-frozen--iteration-2) are pinned above, and the [dot/colon rule](#the-dotcolon-rule-frozen--iteration-2) reconciles the earlier naming drift (`approve`/`deny` split and type-qualified; `view`/`cancel` scoped with `.own`/`.any`). **Answered by the code:** The catalog is frozen in `0022_rbac_catalog_freeze.up.sql:14-30`.
4. **Cache invalidation strategy.** Role and grant changes need to propagate. Options: short JWT TTL (15min) so caches expire naturally; explicit cache-bust on change; cache keyed by `(user_id, settings_version)`. Lean: explicit bust via in-process pub/sub; we're single-instance for the foreseeable future. — *Answered by the code: an in-process per-user cache with no TTL, dropped by `AuthzService.Bust` on role assignment.*
5. **Deny grants — worth keeping?** Real use cases are narrow ("revoke 4K from this one user temporarily"). The composition rule (deny wins) is standard. Risk is admin confusion at scale. Lean: ship `deny` since the column exists and the semantics are well-trodden; surface it sparingly in UI. **Answered by the code:** Deny grants are kept and deny wins (`authz/resolve.go:47-59`); V1 surfaces none.
6. **Plex friends sync cadence.** On every login, periodic sweep, or both? Periodic catches removals (was a friend, no longer is); on-login is the natural pivot point. Lean: on-login check + a daily sweep for removals. *Frozen — beyond V1.*
7. **"View as user" mechanics.** Session-scoped impersonation (audit row + masquerade for N minutes) or per-request `?as=` parameter (audited per request)? Lean: session-scoped with a persistent visible banner so the admin can't forget they're in someone else's view. *Frozen — beyond V1.*
8. **Last-admin protection scope.** Currently delete protects against removing the last admin. Should disabling protect too? What about removing the `admin` role from the last admin (a different code path that today would orphan the system)? **Decided:** protect all three.
9. **Deletion vs disable vs tombstone.** When a user with request history is deleted, what happens to the authorship link? Options: cascade-null (history orphaned), reassign to a tombstone user, soft-delete (`deleted_at` column, row preserved). Lean: soft-delete via `is_active=false`, real delete cascades to authorship-null. Pin in iteration 2. **Answered by the code:** `request.requested_by` is `ON DELETE RESTRICT`, so users with requests can't be deleted; disable is `is_active` (`0016_tracking.up.sql:65`).
10. **Multi-role UI shape.** Today the UI assigns one role. The model already supports many. When the multi-role UI lands, is there a "primary role" for display purposes, or does the user just have a list? Lean: display shows the highest-permission role with a "+N more" indicator. **Superseded by [V1](../../V1.md):** Every user holds exactly one role (REQ-ROLE-001).

## What we're explicitly not deciding here

- API endpoint shapes for user / role / permission management
- The permission-check helper's exact Go signature
- Permission cache implementation
- Plex friends API integration details (auth, pagination, rate limits)
- Two-factor authentication or other strengthened-auth flows
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
