# Importing & Hardlinks

When a download completes, Arrflix imports the file into your library — renaming it according to your [name template](./name-templates) and placing it in the correct [library](./libraries) folder. The import process uses a **hardlink-first** strategy to avoid duplicating disk space.

## What Is a Hardlink?

A hardlink is a filesystem feature that lets multiple file paths point to the same data on disk. Unlike a copy, a hardlink doesn't use any additional storage — both paths reference the same underlying blocks.

This means:
- Your download client keeps the original file for seeding
- Your library has a neatly named and organized version
- Only one copy of the data exists on disk

If you delete the file from your library, the download client's copy is unaffected (and vice versa).

## How Import Works

1. The download completes and Arrflix picks up the finished file
2. The destination path is computed from the library root and rendered name template
3. Arrflix attempts to **hardlink** the file to the destination
4. If hardlinking fails, it falls back to a **byte-for-byte copy**
5. The import method (hardlink or copy) is recorded for each file

The fallback is automatic — you don't need to configure anything. But if you're seeing copies where you expected hardlinks, the cause is almost always a filesystem issue.

## When Hardlinks Fail

Hardlinks only work when the source and destination are on the **same filesystem**. If they're on different filesystems, the operating system rejects the hardlink and Arrflix falls back to copying.

Common reasons hardlinks fail:

### Different Drives or Partitions

If your downloads land on one drive and your library is on another, they're on different filesystems. Hardlinks cannot cross filesystem boundaries.

### Docker Volume Mounts

This is the most common gotcha. If your download client and Arrflix mount different host paths, Docker treats them as separate filesystems — even if they're on the same physical drive.

::: danger Won't Hardlink
```yaml
services:
  arrflix:
    volumes:
      - /media/library:/data

  qbittorrent:
    volumes:
      - /media/downloads:/downloads
```

These are separate volume mounts. Even though `/media/library` and `/media/downloads` might be on the same drive, Docker sees them as different filesystems inside the containers.
:::

::: tip Will Hardlink
```yaml
services:
  arrflix:
    volumes:
      - /media:/media

  qbittorrent:
    volumes:
      - /media:/media
```

Both containers mount the same parent path. Downloads at `/media/downloads/` and library files at `/media/library/` share the same filesystem, so hardlinks work.
:::

### Network Filesystems

NFS, SMB/CIFS, and other network filesystems generally do not support hardlinks, or have limited support. If your media is on a NAS, hardlinks may not work depending on the protocol and configuration.

### MergerFS

If you use MergerFS to pool multiple drives, hardlinks will only work when the source and destination files land on the same underlying drive. Configuring MergerFS with a creation policy like `mfs` (most free space) can help, but isn't guaranteed.

## Checking Import Methods

Arrflix records whether each file was imported via hardlink or copy. If you're seeing more copies than expected, verify that:

1. Your download save path and library root are on the same filesystem
2. If using Docker, both paths are under the same volume mount
3. Your filesystem supports hardlinks

## Storage Impact

The difference matters at scale:

- **With hardlinks** — A 50 GB movie exists once on disk, accessible from both the download client and your library
- **Without hardlinks** — The same movie is copied, consuming 100 GB total

If your setup can't support hardlinks, everything still works — it just uses more disk space.
