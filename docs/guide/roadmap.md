# Roadmap

Arrflix is under active development. This page outlines the major features planned next, roughly in the order they'll be built. Each layer builds on the one before it.

## Unmatched Media Resolution

**Status:** Planned

When Arrflix [scans a library](./libraries#scanning-existing-media), it tries to automatically match each file to a movie or series on TMDB. Files with clear naming or embedded metadata get matched instantly, but some files are ambiguous — maybe the title is generic, the year is missing, or multiple TMDB results are equally plausible.

These files are saved as **unmatched items**, each with up to 5 suggestions of what it might be. Currently this data exists in the backend but there's no UI to act on it.

The plan is a dedicated interface where you can review unmatched files and manually link them to the correct TMDB entry — similar to Plex's "Fix Match" flow. Pick from the suggestions, or search TMDB directly if none of them are right.

## Auto-Selection

**Status:** Planned

Right now, when you download something, you manually browse indexer results and pick a release. Auto-selection will let the system make that choice for you.

This means introducing:

- **Quality profiles** — A ranked list of acceptable qualities (e.g., 1080p BluRay > 1080p WEB-DL > 720p)
- **Scoring** — Bonus or penalty points for attributes like release group, codec, or whether it's a repack
- **Rejection rules** — Hard filters like minimum seeders, blocked words, or size limits

When a download is needed, the system searches your indexers, scores every result against your profile, and grabs the highest-scoring candidate that passes all filters.

Auto-selection is the foundation for everything below. Without it, the system can't act on your behalf.

## Monitoring

**Status:** Planned — depends on auto-selection

Once the system can pick releases on its own, you'll be able to **monitor** titles — telling Arrflix to track a movie or series and download it automatically.

**For movies**, this is straightforward: "I want this movie. Check periodically. Grab it when a good release appears."

**For series**, monitoring works as a subscription. You add a series and tell Arrflix what you want — all seasons, new episodes only, or a specific season. The system creates a want for each relevant episode and fulfills them as releases appear on your indexers.

A background job handles the periodic searching and downloading. You don't need to check in.

## Requests

**Status:** Planned — depends on monitoring

Requests add a social layer on top of monitoring. A non-admin user can request a movie or series, and either a human or a policy decides what happens next.

The flow:

1. A user requests a title
2. If **auto-approve** is enabled for that user, the request immediately becomes a monitored title and downloads begin automatically
3. If **manual approval** is required, the admin reviews the request and approves or denies it

With all three layers in place, the full pipeline works end-to-end: a family member requests a movie → it's auto-approved → the system finds the best release → it downloads and imports into your library → it's ready on Plex. No manual steps required.
