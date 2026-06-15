# Navigation & information architecture (V1)

This is the spec for how arrflix's frontend is organized: where each screen lives, why,
and the rules that keep new features from accreting into the wrong place. It is a
design-time document — read it before adding a new page or settings area.

## TL;DR

- The app has **four spaces**. Every screen belongs to exactly one. The space decides where it lives in the nav.

  | Space | Mental model | Audience | Surface |
  | --- | --- | --- | --- |
  | **Browse** | "What's in / could be in my collection" | everyone | Titlebar (left) |
  | **Activity** | "What needs attention / is happening now" — time-sensitive | mixed | Titlebar (right) |
  | **App Settings** | "How the system behaves" — system-wide config | admin | Sidebar (entered via avatar) |
  | **Account** | "My personal stuff" — per-user | everyone | Avatar menu |

- The line between **Account** and **App Settings** is the line between *personal preferences*
  and *system configuration*. V1 has **multi-user with real roles**, so this split is **strict and
  role-gated**, not cosmetic.
- **App Settings is a grouped left sidebar**, not horizontal tabs. Horizontal tabs are reserved for
  **sub-sections within a single settings page** (the Quality Profiles pattern).
- **Matching is not its own destination.** It folds into Settings → Libraries, scoped per library,
  with unmatched counts that bubble up as badges.
- A feature that connects to an external service (Plex, Jellyfin, TMDB, OpenSubtitles, notification
  channels) lives under **Integrations**, never bolted onto the General page.

## Why this exists

When the settings tabs were built there were a handful of them. There are now 7, at least 5 more are
planned (Hygiene, Path Mapping, Requests config, Media Servers, plus the Integrations group), and one
page (Quality Profiles) already needed a second level of tabs. A flat horizontal tab bar does not
survive that growth, and ad-hoc decisions about "is this a top-level page or a settings tab or an
avatar-menu item" produce an inconsistent app. The four-spaces model is the rule that makes those
decisions mechanical instead of case-by-case.

## The four spaces

### Browse — titlebar, left

The collection-consumption experience. Stateless to look at; you're reading, not deciding.

- Home, Library, media detail (Movie / Series / Person), Search (⌘K dialog).

### Activity — titlebar, right

Time-sensitive things you *check* and *act on*. These are activities, not configuration — you don't
"set up" your downloads, you watch them.

- **Calendar** — upcoming series releases / scheduled episodes / slated downloads. Everyone.
- **Requests** — the admin **approve/reject queue**, with a pending-count badge. Admin only.
- **Downloads** — live acquisition status. Admin / power users.

> **Three faces of "requests" — keep them straight.** (1) *Submitting* a request happens inline from a
> detail/search page (Browse). (2) The *approval queue* is an Activity page (this section). (3) *Request
> configuration* — quotas, auto-approve rules — is an App Setting. Same word, three spaces.

### App Settings — sidebar, entered via the avatar

System-wide configuration. Admin only. See [App Settings layout](#app-settings-layout).

### Account — avatar menu

Strictly *this user's own* stuff. Every authenticated user has these regardless of role.

```
┌─ Jane Doe ──────────────┐
│  jane@example.com        │
├──────────────────────────┤
│  Preferences             │  personal: profile/details, my notification prefs, theme
│  Settings        (admin) │  gateway to the App Settings sidebar — admin only
├──────────────────────────┤
│  Log out                 │
└──────────────────────────┘
```

What changed from today's avatar menu and why:

- **Matching** leaves — it folds into Settings → Libraries ([below](#matching-folds-into-libraries)).
- **Users** leaves — user management is admin system config, so it moves into the App Settings sidebar.
  This keeps the avatar menu purely personal.

## Titlebar

```
Arrflix   Home   Library          [⌘K search]      Calendar   Requests•   Downloads   [avatar]
          └── Browse ──┘                            └─────────── Activity ───────────┘
```

Entries are **role-aware**: the Requests queue and Downloads render only for the appropriate roles;
Calendar is for everyone. Browse entries are always present.

## App Settings layout

A **grouped left sidebar**. The two-level system is:

- **Sidebar = which settings area.** Items are organized into labeled groups.
- **Horizontal tabs = facets of one area.** Used only inside a page that has genuine sub-sections
  (e.g. Quality Profiles → Profiles / Tiers / Custom Formats). This is the existing pattern in
  `QualityProfilesSettings.vue`; keep it, don't replace it.

Never use horizontal tabs as the top-level settings switcher again, and never nest a third level —
if a page needs more than one tab row, it's two pages.

Groups follow the **acquisition lifecycle**, not a generic "management" bucket. The load-bearing
distinction is **Sourcing** (decide *what release* to grab) vs **Delivery** (decide *where it lands
and how*). Routing is the orchestrator of Delivery — it dispatches a chosen release to a downloader,
a name template, and a library — so those three are its outputs and live alongside it. Indexers and
Quality Profiles are the two halves of Sourcing (where to look, which to pick), so they sit together.

```
SETTINGS                         (★ exists today   ＋ planned)
│
├─ General ★                     instance name, defaults, app-wide behavior (NOT connections)
│
├─ USERS & REQUESTS              people and what they can ask for (front-of-house)
│   ├─ Users ★(moved)            admin user management
│   └─ Requests ＋               request config: quotas, auto-approve rules
│
├─ SOURCING                      what release to grab
│   ├─ Indexers ★                where to search
│   └─ Quality Profiles ★        which release to pick   [Profiles · Tiers · Custom Formats] ← sub-tabs
│
├─ DELIVERY                      where it lands and how — Routing + its outputs
│   ├─ Routing ★                 dispatches a release → downloader, template, library
│   ├─ Downloaders ★
│   ├─ Name Templates ★
│   ├─ Libraries ★  •            routing target + content home; scan + folded-in Matching, unmatched badge
│   └─ Path Mapping ＋           download-client → host path translation (coupled to Downloaders)
│
├─ MAINTENANCE
│   └─ Hygiene ＋                library upkeep
│
└─ INTEGRATIONS ＋               external service connections (credentials + test button)
    ├─ Media Servers ＋          Plex / Jellyfin
    ├─ Metadata ＋               TMDB
    ├─ Subtitles ＋              OpenSubtitles
    └─ Notifications ＋          Discord / SMTP / webhooks — channel *connections*
```

### Placement rules

- **Group along the lifecycle, not by a generic noun.** A "Media Management" catch-all re-scatters
  Sourcing and Delivery across two boxes (e.g. Quality Profiles drifting away from Indexers,
  Downloaders away from Routing). Each lifecycle phase is one group.
- **General is for instance-level preferences, not connections.** Anything with credentials and a
  "test connection" affordance belongs in **Integrations**, for consistency — including TMDB and
  OpenSubtitles. Do not tuck a single connection onto General because it feels small.
- **Integrations is a group, not a page.** Each connected service is its own item with its own form.
- **Users & Requests sits high, under General** — it's front-of-house policy (who's in, what they can
  request), not an afterthought at the bottom.
- New settings areas slot into the phase they belong to. A genuinely new function gets a new group,
  not a new top-level scatter.

## Matching folds into Libraries

Matching is currently a standalone page (`/library/matching`) reachable only from the avatar menu.
This breaks the natural loop: you scan a library in Settings → Libraries, then have to leave to
remedy the unmatched items it produced. Instead:

- Matching becomes a view **inside Settings → Libraries, scoped per library**. Scan and remedy happen
  in one place.
- **Unmatched counts bubble up as badges** so it stays discoverable despite being "deeper":
  - per-library badge in the Libraries list,
  - a roll-up badge on the **Libraries** sidebar item,
  - a roll-up badge on the **Settings** entry in the avatar menu.
- No titlebar entry. The badge bubbling is the discoverability mechanism; matching is occasional
  cleanup, not a standing activity.

> **Tradeoff on record:** folding matching into settings assumes unmatched items are an occasional
> task. If real-world usage shows a steady stream (large/messy libraries, frequent imports), revisit
> whether it needs faster access than badge-bubbling provides — but do not give it titlebar space
> without that evidence.

## Roles & the Account / App Settings split

V1 is **multi-user with real roles**, so the split is enforced, not stylistic:

- **Account (avatar menu)** items render for every authenticated user.
- **App Settings**, the **Requests queue**, **Users**, and **Downloads** are admin-gated — both hidden
  in the UI for non-admins *and* enforced server-side. UI hiding is convenience, not security.

### Notifications span both spaces — design both up front

Notifications are the canonical example of the split, and the easiest to get wrong:

- **Connections** — "here is our Discord webhook / SMTP server" → **App Settings → Integrations →
  Notifications** (admin).
- **Preferences** — "email *me* when *my* request is approved" → **Account → Preferences** (per-user).

Build both seams from the start; retrofitting the per-user side later is painful.

## Implementation notes (non-binding)

- Settings tabs are nested routes today (`/settings/:tab`) driven by `SettingsLayout.vue`. The sidebar
  is a presentation change over the same nested-route model — sidebar items map to the same child
  routes; groups are display-only. This keeps deep links bookmarkable.
- Sub-section tabs (Quality Profiles) stay local component state, as today.
- Badge counts reuse the existing `useInboxCount()` composable, re-scoped per library.

## Open questions

Not yet decided; resolve before building the affected area:

- Exact role taxonomy beyond admin / regular user (e.g. a "request-only" role, per-library scoping).
- Whether Calendar is admin/power or truly everyone (assumed everyone here).
- Account "Preferences" page internal layout (profile vs notifications vs UI prefs as tabs or sections).
