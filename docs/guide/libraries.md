# Libraries

A library is a folder on your server where Arrflix stores and organizes imported media. Each library has a type — either **movie** or **series** — which determines how content is structured inside it.

## Setting Up a Library

Each library needs:

- **Name** — A label for your reference (e.g., "Movies", "4K Movies", "TV Shows")
- **Type** — Either `movie` or `series`
- **Root Path** — The absolute path on your server where files will be stored (e.g., `/data/movies`)

You can create multiple libraries of the same type. For example, you might have separate libraries for standard and 4K content.

### Default Libraries

Each media type (movie and series) can have one library marked as the **default**. When no [policy](./policy-engine) specifies a library, the default is used.

## How Files Are Organized

When Arrflix imports a file, it creates a folder structure inside the library root based on your [name template](./name-templates). For example:

```
/data/movies/                          ← Library root
  └── The Matrix (1999)/               ← Movie directory (from template)
      └── The Matrix (1999) [1080p].mkv  ← File (from template)

/data/series/                          ← Library root
  └── Breaking Bad (2008)/             ← Show directory
      └── Season 01/                   ← Season directory
          └── Breaking Bad - S01E01 - Pilot [1080p].mkv
```

The exact naming is controlled by your name template — see [Name Templates](./name-templates) for details.

## Scanning Existing Media

If you already have media files on disk, Arrflix can scan a library to discover and catalog them. This is useful when you're migrating from another tool or adding files that were organized outside of Arrflix.

### How Scanning Works

A scan runs in four phases:

1. **Collect** — Walks the library directory and finds all video files (`.mkv`, `.mp4`, `.avi`, etc.). Skips sample files, featurettes, trailers, and other extras.

2. **Embedded ID lookup** — Checks for metadata embedded in the files or companion `.nfo` files. If a TMDB, TVDB, or IMDb ID is found, the file is matched immediately.

3. **Filename parsing** — For files without embedded IDs, Arrflix parses the filename to extract title, year, season, and episode information. It then searches TMDB to find a match. If exactly one result matches, it's auto-linked. Otherwise, the file is marked as **unmatched** with up to 5 suggestions for you to resolve manually.

4. **Record** — Matched files are added to the database with their metadata, and the library view updates to reflect the new content.

### Tips

- Only one scan runs per library at a time.
- Progress is shown in real-time in the UI.
- Unmatched files aren't lost — they appear in the UI for you to manually match when you're ready.
- Scanning doesn't move or rename any files. It only reads and catalogs what's already on disk.
