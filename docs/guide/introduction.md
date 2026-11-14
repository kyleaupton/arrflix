# What Is Arrflix?

## Introduction

Arrflix is an open source, self-hosted media manager for movies and series. Browse what's trending, search for something specific, download it, and have it organized in your library automatically.

Setting up and maintaining a media automation stack today often means stitching together multiple services, learning their individual quirks, and keeping everything in sync. Arrflix takes a different approach. One app, one container, one UI. Movies and series managed together instead of across separate tools.

## What It Does Today

Arrflix handles the core media management workflow:

- **Search** indexers for movies and series via bundled Prowlarr integration
- **Download** releases through your existing download client (qBittorrent)
- **Import** completed downloads into your library with customizable naming
- **Scan** existing media on disk and match it against TMDB metadata
- **Organize** content across multiple libraries with flexible routing rules
- **Monitor** movies and series and grab the best release automatically, based on your quality profiles
- **Take requests** from other users, auto-approved or held for your approval, and notify them when it's ready

## What’s Coming

Arrflix is under active development. The core workflows are solid, and the next features are on the [roadmap](./roadmap).

## Who This Is For

Arrflix is for people who want a simpler media management setup without running and coordinating multiple applications. If you’re comfortable being an early adopter and don’t mind reporting the occasional bug, give it a try.

Feedback and bug reports are welcome on [GitHub](https://github.com/kyleaupton/arrflix/issues).
