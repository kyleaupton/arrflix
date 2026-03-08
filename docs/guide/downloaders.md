# Downloaders

A downloader is the client that actually fetches files from the sources your indexers find. Arrflix communicates with your download client to queue downloads and monitor their progress.

## Supported Clients

Currently supported:

- **qBittorrent** — For torrent downloads

## Configuration

Each downloader needs:

- **Name** — A label for your reference
- **Type** — The client type (e.g., `qbittorrent`)
- **Protocol** — `torrent` or `usenet`, determines which indexer results it can handle
- **URL** — The base URL of your download client (e.g., `http://localhost:8080`)
- **Username / Password** — Credentials, if your client requires authentication

### Default Downloaders

Each protocol (torrent, usenet) can have one downloader marked as the **default**. When no [policy](./policy-engine) specifies a downloader, the default for that protocol is used automatically.

## How Downloads Are Managed

When you select a release to download:

1. Arrflix sends the torrent or magnet link to your download client
2. A background worker polls the client for progress updates
3. Once the download is complete, the [import process](./importing-and-hardlinks) begins automatically

::: tip Same Filesystem for Hardlinks
If you want Arrflix to hardlink files instead of copying them (saving disk space), your download client's save location and your library folder must be on the same filesystem. See [Importing & Hardlinks](./importing-and-hardlinks) for details.
:::
