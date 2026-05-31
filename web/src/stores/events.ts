import { defineStore } from 'pinia'
import { computed, ref, onScopeDispose } from 'vue'
import { client } from '@/client/client.gen'
import { eventsSubscriptionsAdd, eventsSubscriptionsRemove } from '@/client/sdk.gen'
import type { ReadyPayload, SubscriptionSnapshots } from '@/client/types.gen'

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
  const wantedTypes = ref<string[]>([])

  // Session id from the latest `ready` event. Sent as ?session= on the next
  // connect so the broker reattaches to our prior session and replays missed
  // events instead of allocating a fresh one. Sticky across reconnects.
  const sessionId = ref<string | null>(null)
  // Resume cursor: the id of the last event we received. The generated SSE
  // client keeps its own cursor in a per-connection closure that resets on each
  // connect(), so we carry it across as the initial Last-Event-ID header.
  let lastEventId: string | undefined

  let abort: AbortController | null = null
  // Monotonic counter identifying the current connection. Bumped on every
  // connect() and disconnect(). Late callbacks from a torn-down connection
  // compare against it and bail, so they can't clobber a fresh connection's
  // status (see SSE_TRANSPORT_FINDINGS.md finding #2).
  let generation = 0
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

  function buildUrl(types?: string[]) {
    const base = '/v1/events'
    const list = types ?? wantedTypes.value
    const params = new URLSearchParams()
    for (const t of list) params.append('type', t)
    // Reattach to our prior session when we have one. The broker preserves that
    // session's subscriptions and replay ring, so resume + skip-resubscribe both
    // work. The generated client fixes this URL for the life of the connection,
    // so the very first connect (before any `ready`) is necessarily sessionless.
    if (sessionId.value) params.set('session', sessionId.value)
    const qs = params.toString()
    return qs ? `${base}?${qs}` : base
  }

  async function connect(types?: string[]) {
    // Merge new types into desired set
    if (types?.length) {
      const merged = Array.from(new Set([...wantedTypes.value, ...types]))
      const changed = merged.length !== wantedTypes.value.length
      wantedTypes.value = merged

      // If already connected but the filter set grew, reconnect with the full set.
      if (abort && changed) {
        disconnect()
      }
    }

    // Already connected/connecting with correct filters — nothing to do.
    if (abort) return

    status.value = 'connecting'
    lastError.value = null
    abort = new AbortController()

    // Only this connection's callbacks may write status. A late callback from a
    // previously torn-down connection has a stale myGen and is ignored.
    const myGen = ++generation
    const setStatus = (s: EventsConnectionStatus) => {
      if (myGen !== generation) return
      status.value = s
    }

    let hasConnectedBefore = false

    try {
      const url = buildUrl()
      const { stream } = await client.sse.get({
        url,
        signal: abort.signal,
        sseDefaultRetryDelay: 500,
        sseMaxRetryDelay: 5000,
        // Seed the resume cursor for this connection. The client overrides it
        // with its own closure value once events with ids arrive; this only
        // matters for the first request of a reconnect, where resume happens.
        headers: lastEventId ? { 'Last-Event-ID': lastEventId } : undefined,
        onSseEvent: (ev) => {
          // Track the resume cursor (ping/ready carry no id, so they don't move it).
          if (ev.id) lastEventId = ev.id
          if (!ev.event) return

          // The server sends a "ready" event on every new connection.
          // Use it as a reliable signal that we're (re)connected.
          if (ev.event === 'ready') {
            const payload = ev.data as ReadyPayload | undefined
            sessionId.value = payload?.sessionId ?? sessionId.value

            const wasDisconnected = status.value !== 'connected'
            setStatus('connected')
            lastError.value = null

            if (hasConnectedBefore && wasDisconnected) {
              // Reconnected after a drop. A clean reattach already replayed the
              // missed events; but a lost session (eviction/restart) can't, so
              // consumers refetch off _reconnected to stay correct either way.
              emit('_reconnected', null)
            }
            hasConnectedBefore = true
          } else if (ev.event === 'resume_gap') {
            // The broker's replay buffer was exhausted — missed deltas are gone.
            // Consumers refetch rather than trust incremental state.
            emit('_resume_gap', null)
          }

          emit(ev.event, ev.data)
        },
        onSseError: (err) => {
          const msg = err instanceof Error ? err.message : String(err)
          lastError.value = msg

          // If we've connected before, this is a reconnection attempt.
          // If we haven't, we're still in the initial connection phase.
          if (hasConnectedBefore) {
            setStatus('reconnecting')
          } else {
            setStatus('error')
          }
        },
      })

      setStatus('connected')

      // Keep the generator alive until aborted or the stream ends.
      ;(async () => {
        try {
          for await (const _ of stream) {
            // no-op: we use onSseEvent for dispatch
          }
        } catch (e) {
          if (abort?.signal.aborted) return
          setStatus('error')
          lastError.value = e instanceof Error ? e.message : String(e)
        } finally {
          abort = null
          if (status.value !== 'error') {
            setStatus('disconnected')
          }
        }
      })()
    } catch (e) {
      abort = null
      setStatus('error')
      lastError.value = e instanceof Error ? e.message : String(e)
    }
  }

  function disconnect() {
    if (!abort) return
    // Bump generation so in-flight callbacks from this connection go stale and
    // can't overwrite the status of whatever connects next.
    generation++
    abort.abort()
    abort = null
    status.value = 'disconnected'
  }

  /**
   * Force-drop the current connection and re-establish it.
   * Useful as an escape hatch if the connection is in a bad state.
   */
  function reconnect() {
    const types = [...wantedTypes.value]
    disconnect()
    wantedTypes.value = []
    connect(types)
  }

  // --- Dynamic subscription control plane (REST side channel) ---
  // The stream is one-way, so topic changes go over REST keyed by the session
  // header. Connect-time `?type=` filters cover static subscriptions; these are
  // for page-bound topics added/dropped without tearing down the stream. The
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
  // connection. When the tab regains focus, force a reconnect + data refresh.

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
    subscribe,
    unsubscribe,
  }
})
