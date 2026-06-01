// The realtime connection: a single app-wide SSE stream, owned as a module
// singleton (not a Pinia store) because it isn't API state — it's a transport.
//
// It exposes connection status for the UI, a frame dispatcher consumed by the
// cache bindings (realtime/bindings.ts) and by ephemeral listeners
// (realtime/useRealtimeListener.ts), and the subscription control plane.
//
// Server state does NOT live here — it lives in the TanStack Query cache, which
// realtime/bindings.ts keeps live from these frames.

import { computed, ref } from 'vue'
import { client } from '@/client/client.gen'
import { eventsSubscriptionsAdd, eventsSubscriptionsRemove } from '@/client/sdk.gen'
import type { ReadyPayload } from '@/client/types.gen'
import { openSseStream, type SseStream } from '@/lib/sseClient'
import { useAuthStore } from '@/stores/auth'

export type ConnectionStatus =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'error'

type FrameCallback = (data: unknown) => void
type ResyncCallback = () => void

export const status = ref<ConnectionStatus>('disconnected')
export const sessionId = ref<string | null>(null)
export const isConnected = computed(() => status.value === 'connected')

let stream: SseStream | null = null

// Listeners keyed by real SSE event name (download_job_updated, scan_progress, …).
const listeners = new Map<string, Set<FrameCallback>>()
// Recovery listeners, fired on reconnect-after-drop and on resume_gap. The cache
// bindings refetch on these.
const resyncListeners = new Set<ResyncCallback>()

// on registers a listener for a real SSE event name. Returns an unsubscribe fn.
export function on(event: string, cb: FrameCallback): () => void {
  const set = listeners.get(event) ?? new Set<FrameCallback>()
  set.add(cb)
  listeners.set(event, set)
  return () => {
    const existing = listeners.get(event)
    existing?.delete(cb)
    if (existing && existing.size === 0) listeners.delete(event)
  }
}

// onResync registers a recovery listener (reconnect-after-drop or replay gap).
export function onResync(cb: ResyncCallback): () => void {
  resyncListeners.add(cb)
  return () => {
    resyncListeners.delete(cb)
  }
}

function dispatch(event: string, data: unknown) {
  const set = listeners.get(event)
  if (!set) return
  for (const cb of set) {
    try {
      cb(data)
    } catch {
      // ignore listener errors
    }
  }
}

function fireResync() {
  for (const cb of resyncListeners) {
    try {
      cb()
    } catch {
      // ignore listener errors
    }
  }
}

export function connect() {
  if (stream) return

  status.value = 'connecting'

  // Whether the transport has dropped and is re-establishing, so we fire resync
  // exactly once per recovered drop. Local to this connection.
  let reconnecting = false

  stream = openSseStream({
    // Rebuilt every attempt so a sessionId captured from `ready` is injected as
    // ?session= on reconnect, letting the broker resume via Last-Event-ID.
    buildUrl: () =>
      client.buildUrl({
        url: '/api/v1/events',
        query: sessionId.value ? { session: sessionId.value } : undefined,
      }),
    getAuthToken: () => useAuthStore().token,
    onStatus: (s) => {
      if (s === 'reconnecting') reconnecting = true
      if (s === 'connected' && reconnecting) {
        reconnecting = false
        fireResync()
      }
      status.value = s
    },
    onEvent: (frame) => {
      if (frame.event === 'ready') {
        const payload = frame.data as ReadyPayload | undefined
        if (payload?.sessionId) sessionId.value = payload.sessionId
      } else if (frame.event === 'resume_gap') {
        // Replay buffer exhausted — missed deltas are gone; refetch.
        fireResync()
      }
      if (frame.event) dispatch(frame.event, frame.data)
    },
  })
}

export function disconnect() {
  if (!stream) return
  stream.close()
  stream = null
  status.value = 'disconnected'
}

// reconnect force-drops and re-establishes, keeping the session identity so the
// reconnect resumes. An escape hatch for a wedged connection.
export function reconnect() {
  disconnect()
  connect()
}

// reset tears down and forgets the session identity. Used on logout so the next
// login starts a clean session and never presents the previous user's id.
export function reset() {
  disconnect()
  sessionId.value = null
}

// --- Dynamic subscription control plane (REST side channel) ---
// Currently unused by the app (the stream is unfiltered); this is for future
// page-bound topics added/dropped without tearing down the stream. Callers seed
// their state from the relevant query's REST baseline, so these return nothing.

export async function subscribe(topics: string[]): Promise<void> {
  if (!sessionId.value || !topics.length) return
  await eventsSubscriptionsAdd<true>({
    throwOnError: true,
    headers: { 'X-Realtime-Session': sessionId.value },
    body: { topics },
  })
}

export async function unsubscribe(topic: string): Promise<void> {
  if (!sessionId.value) return
  await eventsSubscriptionsRemove<true>({
    throwOnError: true,
    headers: { 'X-Realtime-Session': sessionId.value },
    path: { topic },
  })
}

// When a backgrounded tab regains focus, revive a dead stream. Data refresh is
// handled by TanStack Query's refetchOnWindowFocus, so this only touches the
// transport — it never pokes the cache.
function handleVisibilityChange() {
  if (document.visibilityState !== 'visible') return
  if (status.value === 'error' || status.value === 'disconnected') reconnect()
}
document.addEventListener('visibilitychange', handleVisibilityChange)
