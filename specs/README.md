# Specs

[**V1.md**](./V1.md) is the scope of the current release: which requirements are in, and what V1 is judged by. Start there.

The rest of this directory is reference material:

- **Stories** ([`stories/`](./stories/README.md)) describe flows from the user's side and carry the `REQ-*` requirement IDs.
- **Module specs** (`modules/`) describe one area's design.
- **Pattern specs** (`patterns/`) describe cross-cutting conventions.

## Status

Every spec carries a `status` in its frontmatter. The status describes what the document is *for*, not how complete the feature is:

| Status | Meaning |
| --- | --- |
| `as-built` | Reference for existing code. When code and spec disagree, fix the spec. |
| `v1` | [V1.md](./V1.md) names work in this area. V1.md is the only place that says which work; the spec is the design reference for it. |
| `deferred` | Frozen. Its open questions stay open until the area is picked up. |

A spec can describe more than what is built or in V1. Sections beyond that are marked as unbuilt design in the spec itself.

New work doesn't grow these specs. It gets a short per-ticket spec, and the owning module spec is updated when behavior changes.

## Index

### v1

| Spec | |
| --- | --- |
| [Acquisition](./modules/acquisition/README.md) | Orchestrating the get-it-into-the-library flow |
| [Hardlinks](./modules/hardlinks/README.md) | The reference graph behind verifiable storage (V1: inode capture only) |
| [Importer](./modules/importer/README.md) | Making a completed download real in the library |
| [Metadata](./modules/metadata/README.md) | Identity, structure, and refresh |
| [Notifications](./modules/notifications/README.md) | Typed events, templated delivery, per-user routing |
| [Path-mapping](./modules/path-mapping/README.md) | One folder, many vantage points (V1: downloader slice) |
| [Requests](./modules/requests/README.md) | The user-facing intent layer |
| [Tracking](./modules/tracking/README.md) | The ongoing-intent primitive |
| [Users](./modules/users/README.md) | Identity, roles, and permissions |
| [Story 1: Happy path](./stories/01-happy-path-auto-approve.md) | A friend requests a movie with auto-approve |
| [Story 2: Series mid-season](./stories/02-series-mid-season-auto-approve.md) | A friend requests an in-flight show |
| [Story 3: Pending approval](./stories/03-pending-approval-queue.md) | Two requests, one approved, one denied |
| [Story 4: Failed search recovery](./stories/04-failed-search-recovery.md) | The indexers don't have it yet |
| [Story 7: Requester cancels](./stories/07-requester-cancels.md) | Withdrawing a request |
| [Story 9: Not out yet](./stories/09-not-out-yet.md) | Asking for something that isn't released |

### as-built

| Spec | |
| --- | --- |
| [Downloaders](./modules/downloaders/README.md) | The clients that pull bits down |
| [Files](./modules/files/README.md) | The physical media on disk |
| [Indexers](./modules/indexers/README.md) | Release discovery via Prowlarr |
| [Libraries](./modules/libraries/README.md) | The on-disk roots where media lives |
| [Matching](./modules/matching/README.md) | File-to-identity resolution |
| [Name templates](./modules/name-templates/README.md) | Turning metadata into paths |
| [Parsing](./modules/parsing/README.md) | Release names and filenames into structured attributes |
| [Quality profiles](./modules/quality-profiles/README.md) | Which release do we grab |
| [Realtime](./modules/realtime/README.md) | SSE transport, broker, and producer API |
| [Routing](./modules/routing/README.md) | Where a grabbed release goes |
| [Scan](./modules/scan/README.md) | Keeping disk and database in sync |
| [Sources](./modules/sources/README.md) | Metadata providers and how we depend on them |
| [Title status](./modules/title-status/README.md) | The acquisition read model |
| [Errors](./patterns/errors/README.md) | Typed errors and the wire model |
| [Navigation](./patterns/navigation/README.md) | Navigation and information architecture |
| [Rules](./patterns/rules/README.md) | The shared predicate substrate |

### deferred

| Spec | |
| --- | --- |
| [Hygiene](./modules/hygiene/README.md) | Finding and fixing entropy over time |
| [Media-server](./modules/media-server/README.md) | Surfacing the library in Plex / Jellyfin (V1.1 starts here) |
| [Audit](./patterns/audit/README.md) | The system-wide decision-artifact pattern (V1.1 search history is its first producer) |
| [Connectivity health](./patterns/connectivity-health/README.md) | The runtime-liveness pattern |
| [Work dispatch](./patterns/work-dispatch/README.md) | Non-poll worker wake-ups over a durable queue |
| [Story 5: Multi-requester scope union](./stories/05-multi-requester-scope-union.md) | Two requesters, one series |
| [Story 6: Upgrades and divergent tiers](./stories/06-upgrades-and-divergent-tiers.md) | A better release appears |
| [Story 8: Autonomy and proposals](./stories/08-autonomy-and-proposals.md) | Who picks the release (V2 quotas reuse proposals) |
| [Story 10: Indexer health](./stories/10-indexer-health-degraded-and-recovery.md) | Indexers degrade mid-acquisition |
