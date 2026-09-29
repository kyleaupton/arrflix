// Live download progress per title, kept outside the query cache.
//
// `title_progress` arrives at the downloader's poll cadence — several times a
// second across a busy queue. The projection it belongs to is a snapshot fetched
// on a kick, so writing progress into that cache entry would either be
// overwritten by the next refetch or force one per tick. It lives here instead:
// the numbers are viewer-independent (a percentage is the same for everyone), so
// there is nothing to derive and nothing to scope.
//
// Written by realtime/bindings.ts, read by composables/useTitleStatus.ts.

import { reactive } from 'vue'
import type { TitleProgressPayload } from '@/client/types.gen'

export interface TitleProgress {
  progress: number
  bytesPerSecond?: number
  etaSeconds?: number
  // receivedAt bounds how long a reading is trusted — see readTitleProgress.
  receivedAt: number
}

// A reading older than this is treated as absent. The stream is the only thing
// that clears an entry, so a download that dies mid-transfer would otherwise
// leave its last percentage on screen indefinitely.
const STALE_AFTER_MS = 60_000

const byTitle = reactive(new Map<string, TitleProgress>())

function key(mediaType: string, tmdbId: number): string {
  return `${mediaType}:${tmdbId}`
}

// recordTitleProgress stores the latest reading for a title.
export function recordTitleProgress(payload: TitleProgressPayload): void {
  byTitle.set(key(payload.mediaType, payload.tmdbId), {
    progress: payload.progress,
    bytesPerSecond: payload.bytesPerSecond,
    etaSeconds: payload.etaSeconds,
    receivedAt: Date.now(),
  })
}

// readTitleProgress returns the latest reading, or null when there is none or it
// has gone stale. Reading prunes, which is enough to bound the map: an entry is
// only reachable through the title it belongs to.
export function readTitleProgress(mediaType: string, tmdbId: number): TitleProgress | null {
  const k = key(mediaType, tmdbId)
  const entry = byTitle.get(k)
  if (!entry) return null
  if (Date.now() - entry.receivedAt > STALE_AFTER_MS) {
    byTitle.delete(k)
    return null
  }
  return entry
}
