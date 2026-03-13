# How Arrflix Works

Arrflix manages the full lifecycle of media, from finding content to organizing it on disk. This page gives you the big picture before diving into each piece.

## The Flow

Everything in Arrflix follows a straightforward path:

```
Search → Evaluate → Download → Import
```

1. **Search** - You search for a movie or series. Arrflix queries your configured indexers (via Prowlarr) and returns a list of available releases.

2. **Evaluate** - You pick a release. Before the download starts, the [Policy Engine](./policy-engine) evaluates the candidate and produces a plan: which downloader to use, which library to import into, and which name template to apply.

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
| [Policy Engine](./policy-engine) | Automatically decides which downloader, library, and template to use |

## Defaults and Policies

You don't need to configure policies to get started. If you set up a default downloader, library, and name template, Arrflix will use those for every download.

Policies are for when you want more control. For example, routing 4K content to a separate library, or using a different downloader for certain indexers.
