import { defineStore } from 'pinia'
import { computed, ref, onScopeDispose } from 'vue'
import { client } from '@/client/client.gen'
import { eventsSubscriptionsAdd, eventsSubscriptionsRemove } from '@/client/sdk.gen'
import type { ReadyPayload, SubscriptionSnapshots } from '@/client/types.gen'
import { openSseStream, type SseStream } from '@/lib/sseClient'
import { useAuthStore } from '@/stores/auth'

type EventCallback = (data: unknown) => void

export type EventsConnectionStatus =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'error'

export const useEventsStore = defineStore('events', () => {
  const status = ref<EventsConnectionStatus>('disconnected')
  const lastError = ref<string | null>(null)

  // Session id from the latest `ready` event. The transport sends it as
  // ?session= on the next (re)connect so the broker reattaches to our prior
  // session and replays missed events instead of allocating a fresh one. Sticky
  // across reconnects; cleared only by reset() (logout).
  const sessionId = ref<string | null>(null)

  // The single live transport, or null when disconnected. The transport owns the
  // reconnect loop, the resume cursor (Last-Event-ID), and backoff, and stops
  // emitting once close() is called — so there's no stale-callback race for the
  // store to guard against (the old generation-counter is gone).
  let stream: SseStream | null = null

  const listeners = new Map<string, Set<EventCallback>>()

  const isConnected = computed(() => status.value === 'connected')

  // --- Pub/sub for event listeners ---

  function on(type: string, cb: EventCallback) {
    const set = listeners.get(type) ?? new Set<EventCallback>()
    set.add(cb)
    listeners.set(type, set)
    return () => {
      const existing = listeners.get(type)
      existing?.delete(cb)
      if (existing && existing.size === 0) {
        listeners.delete(type)
      }
    }
  }

  function emit(type: string, data: unknown) {
    const set = listeners.get(type)
    if (!set) return
    for (const cb of set) {
      try {
        cb(data)
      } catch {
        // ignore listener errors
      }
    }
  }

  // --- Connection management ---

  // The app opens one unfiltered stream that carries every event the user is
  // eligible for; page-scoped narrowing happens via subscribe()/unsubscribe(),
  // not a connect-time filter. The URL is rebuilt by the transport on every
  // attempt so a sessionId captured from `ready` is injected on reconnect.
  function connect() {
    if (stream) return

    status.value = 'connecting'
    lastError.value = null

    // Tracks whether the transport has dropped and is re-establishing, so the
    // store can fire `_reconnected` exactly once per recovered drop (consumers
    // refetch on it as a correctness backstop, since a clean replay covers the
    // happy path but a lost/evicted session can't). Local to this connection.
    let reconnecting = false

    stream = openSseStream({
      buildUrl: () =>
        client.buildUrl({
          url: '/api/v1/events',
          query: sessionId.value ? { session: sessionId.value } : undefined,
        }),
      getAuthToken: () => useAuthStore().token,
      onStatus: (s) => {
        if (s === 'reconnecting') reconnecting = true
        if (s === 'connected') {
          lastError.value = null
          if (reconnecting) {
            reconnecting = false
            emit('_reconnected', null)
          }
        }
        if (s === 'error') lastError.value = 'SSE connection error'
        status.value = s
      },
      onEvent: (frame) => {
        if (frame.event === 'ready') {
          const payload = frame.data as ReadyPayload | undefined
          if (payload?.sessionId) sessionId.value = payload.sessionId
        } else if (frame.event === 'resume_gap') {
          // The broker's replay buffer was exhausted — missed deltas are gone.
          // Consumers refetch rather than trust incremental state.
          emit('_resume_gap', null)
        }
        if (frame.event) emit(frame.event, frame.data)
      },
    })
  }

  function disconnect() {
    if (!stream) return
    stream.close()
    stream = null
    status.value = 'disconnected'
  }

  /**
   * Tear the connection down and forget the session identity. Used on logout:
   * unlike disconnect() (which keeps sessionId so a reconnect can resume),
   * reset() clears it so the next login starts a clean session and never
   * presents the previous user's session id. The resume cursor lives in the
   * transport and is discarded with it on close.
   */
  function reset() {
    disconnect()
    sessionId.value = null
  }

  /**
   * Force-drop the current connection and re-establish it, keeping the session
   * identity so the reconnect resumes. An escape hatch for a wedged connection.
   */
  function reconnect() {
    disconnect()
    connect()
  }

  // --- Dynamic subscription control plane (REST side channel) ---
  // The stream is one-way, so topic changes go over REST keyed by the session
  // header. Currently unused by the app (the stream is unfiltered); this is for
  // future page-bound topics added/dropped without tearing down the stream. The
  // 200 body of a subscribe carries the initial snapshot for any topic that has
  // one (download-jobs today) so callers can seed their cache.

  async function subscribe(topics: string[]): Promise<SubscriptionSnapshots | null> {
    if (!sessionId.value || !topics.length) return null
    const res = await eventsSubscriptionsAdd<true>({
      throwOnError: true,
      headers: { 'X-Realtime-Session': sessionId.value },
      body: { topics },
    })
    return res.data ?? null
  }

  async function unsubscribe(topic: string): Promise<void> {
    if (!sessionId.value) return
    await eventsSubscriptionsRemove<true>({
      throwOnError: true,
      headers: { 'X-Realtime-Session': sessionId.value },
      path: { topic },
    })
  }

  // --- Visibility change handling ---
  // When a tab is backgrounded for a long time, the browser/OS may kill the
  // connection. When the tab regains focus, force a reconnect or a data refresh.

  function handleVisibilityChange() {
    if (document.visibilityState !== 'visible') return

    if (status.value === 'error' || status.value === 'disconnected') {
      // Connection is dead — reconnect.
      reconnect()
    } else if (status.value === 'connected' || status.value === 'reconnecting') {
      // Connection may be alive but we could have missed events while
      // backgrounded. Emit _visibility_restored so consumers can refresh.
      emit('_visibility_restored', null)
    }
  }

  document.addEventListener('visibilitychange', handleVisibilityChange)

  onScopeDispose(() => {
    document.removeEventListener('visibilitychange', handleVisibilityChange)
    disconnect()
  })

  return {
    status,
    lastError,
    isConnected,
    sessionId,
    on,
    connect,
    disconnect,
    reconnect,
    reset,
    subscribe,
    unsubscribe,
  }
})
