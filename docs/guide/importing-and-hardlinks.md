# Importing & Hardlinks

When a download completes, Arrflix imports the file into your library, renaming it according to your [name template](./name-templates) and placing it in the correct [library](./libraries) folder.

Arrflix uses a **hardlink-first** import strategy. This has two big benefits: it saves disk space by not duplicating files, and it makes imports nearly instant since no data actually needs to be copied.

## What Is a Hardlink?

A hardlink is a filesystem feature that lets multiple file paths point to the same data on disk. Unlike a copy, creating a hardlink is almost instant regardless of file size because no bytes are actually written. It just creates a new path that points to data that already exists.

This means:
- Your download client keeps the original file for seeding
- Your library has a neatly named and organized version
- Only one copy of the data exists on disk
- A 50 GB movie imports in under a second

If you delete the file from your library, the download client's copy is unaffected (and vice versa).

## How Import Works

1. **Download completes** - Arrflix picks up the finished file
2. **Destination path** - Computed from the library root and your name template
3. **Hardlink** - Arrflix attempts to hardlink the file to the destination
4. **Fallback** - If hardlinking fails, it falls back to a byte-for-byte copy

The fallback is automatic. You don't need to configure anything. But if imports are slow or you're using more disk space than expected, you're probably falling back to copies. The cause is almost always a filesystem issue.

## When Hardlinks Fail

Hardlinks only work when the source and destination are on the **same filesystem**. If they're on different filesystems, the operating system rejects the hardlink and Arrflix falls back to copying.

Common reasons hardlinks fail:

### Different Drives or Partitions

If your downloads land on one drive and your library is on another, they're on different filesystems. Hardlinks cannot cross filesystem boundaries.

### Docker Volume Mounts

This is the most common gotcha. If your download client and Arrflix mount different host paths, Docker treats them as separate filesystems even if they're on the same physical drive.

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

Arrflix records whether each file was imported via hardlink or copy. If imports seem slow or you're seeing more disk usage than expected, verify that:

1. Your download save path and library root are on the same filesystem
2. If using Docker, both paths are under the same volume mount
3. Your filesystem supports hardlinks

If your setup can't support hardlinks, everything still works. Imports will just take longer and use more disk space.
