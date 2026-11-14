# Roadmap

Arrflix's goal for its first stable release is simple: a friend asks for a movie or a show, it lands in your library, and they're told. This page covers what already works, what's being finished for that release, and what comes after.

## Available Today

- **Search, download, and import.** Search your indexers, grab a release yourself, and have it [imported](./importing-and-hardlinks) into your library with your [naming](./name-templates).
- **Quality profiles and automatic selection.** Arrflix scores releases against a quality profile and grabs the best one that passes your filters, with separate HD and 4K tiers.
- **Monitoring.** Track a movie or a series and Arrflix keeps searching until it has what you asked for. Series can be tracked in full or for upcoming episodes only.
- **Requests.** Users request titles. Requests are either approved automatically or held for an admin to approve or deny, and requesters can cancel their own.
- **Routing rules.** Send grabs to different downloaders, libraries, and name templates. See [Routing Rules](./policy-engine).
- **Unmatched media resolution.** Files a [library scan](./libraries#scanning-existing-media) couldn't identify land in a matching inbox where you pick the right title.
- **Notifications.** In-app, email, and browser push notifications when requested media is ready to watch.
- **Users.** Invite links, password or Plex sign-in, and a list of signed-in devices you can revoke.

## Next: First Stable Release

- **Two simple roles.** Every user is either an admin or a requester. Admins set, per user, whether requests are auto-approved and whether 4K may be requested.
- **Path mapping.** Support setups where your download client and Arrflix see the same storage at different paths, with a test that confirms whether imports will hardlink or copy.
- **Not out yet.** A movie that's still in cinemas waits for its digital or physical release instead of searching indexers daily.
- **Shared requests.** When several people want the same title, one person cancelling doesn't cancel it for the others.
- **Clearer search status.** Titles that haven't been found yet show one consistent "searching" state everywhere.
- **Hardlink tracking groundwork.** Arrflix records link details for every file it imports or scans, so later versions can verify your storage.

## After That

- **Plex integration.** A library scan after import, and notifications that open the title directly in Plex.
- **Search history.** See what each search considered and why each release was rejected, to answer "why isn't this downloading?"

## Later

- **Storage quotas.** A per-user allowance over a rolling 30 days in place of the auto-approve switch. Grabs that don't fit are held for the owner.
- **Approvers.** Let a trusted user approve requests without being an admin.
- **Upgrades.** Replace a file when a better release appears.
- **More.** Library hygiene, Jellyfin and other media servers, indexer health tracking, a calendar, watchlists, and more download clients.
