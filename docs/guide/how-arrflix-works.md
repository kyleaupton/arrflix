# How Arrflix Works

Arrflix manages the full lifecycle of media, from finding content to organizing it on disk. This page gives you the big picture before diving into each piece.

## The Flow

Everything in Arrflix follows a straightforward path:

```
Search → Evaluate → Download → Import
```

1. **Search** - A movie or series is requested or tracked, or you search for one yourself. Arrflix queries your configured indexers (via Prowlarr) for available releases, and keeps searching on a schedule until it finds one.

2. **Evaluate** - Arrflix scores the releases against a quality profile and picks the best one, or you pick one by hand. Before the download starts, [routing rules](./policy-engine) decide which downloader to use, which library to import into, and which name template to apply.

3. **Download** - The release is sent to your download client (e.g., qBittorrent). Arrflix monitors progress in the background.

4. **Import** - Once the download completes, Arrflix moves the file into your library. It [hardlinks when possible](./importing-and-hardlinks) to avoid duplicating disk space, and renames the file according to your [name template](./name-templates).

## The Building Blocks

Each step in the flow relies on a few configurable pieces:

| Concept | What It Does |
|---------|-------------|
| [Libraries](./libraries) | Define where your media lives on disk |
| [Indexers](./indexers) | Provide sources to search for content |
| [Downloaders](./downloaders) | Fetch files from indexer results |
| [Name Templates](./name-templates) | Control how imported files are named and organized |
| [Routing Rules](./policy-engine) | Automatically decide which downloader, library, and template to use |

## Defaults and Routing Rules

You don't need to configure routing rules to get started. If you set up a default downloader, library, and name template, Arrflix will use those for every download.

Routing rules are for when you want more control. For example, routing 4K content to a separate library, or using a different downloader for certain indexers.
