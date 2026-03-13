# Indexers

Indexers are the sources Arrflix searches when you look for content to download. They're essentially search engines for torrent and usenet trackers.

## How Indexers Work

Arrflix uses [Prowlarr](https://prowlarr.com/) under the hood to manage indexer connections. Prowlarr is bundled with Arrflix, so you don't need to install it separately.

When you search for a movie or series:

1. Arrflix sends a search query to Prowlarr
2. Prowlarr queries all your enabled indexers in parallel
3. Results come back with metadata like seeders, size, age, and categories
4. You pick a release from the combined results

Search results are cached for 5 minutes, so repeated searches for the same content are fast.

## Managing Indexers

You can add, remove, enable, disable, and test indexers from the Arrflix settings UI. Each indexer typically needs:

- A base URL
- An API key (from the tracker)
- Protocol type (torrent or usenet)

Prowlarr supports a wide range of public and private trackers. Check the [Prowlarr documentation](https://wiki.servarr.com/prowlarr) for a full list of supported indexers.

## Indexer and Downloader Compatibility

Indexers produce results with a specific **protocol**, either torrent or usenet. Your [downloader](./downloaders) must support the same protocol to handle the result. For example, a torrent indexer's results can only be sent to a torrent-capable download client like qBittorrent.
