import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { client } from '@/client/client.gen'

type EventCallback = (data: unknown) => void

export type EventsConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'error'

export const useEventsStore = defineStore('events', () => {
  const status = ref<EventsConnectionStatus>('disconnected')
  const lastError = ref<string | null>(null)

  const wantedTypes = ref<string[]>([])

  let abort: AbortController | null = null
  const listeners = new Map<string, Set<EventCallback>>()

  const isConnected = computed(() => status.value === 'connected')

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

  function buildUrl(types?: string[]) {
    const base = '/v1/events'
    const list = types ?? wantedTypes.value
    if (!list.length) return base
    const params = new URLSearchParams()
    for (const t of list) params.append('type', t)
    return `${base}?${params.toString()}`
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

    try {
      const url = buildUrl()
      const { stream } = await client.sse.get({
        url,
        signal: abort.signal,
        onSseEvent: (ev) => {
          if (!ev.event) return
          emit(ev.event, ev.data)
        },
        onSseError: (err) => {
          lastError.value = err instanceof Error ? err.message : String(err)
        },
      })

      status.value = 'connected'

      // Keep the generator alive until aborted or error.
      ;(async () => {
        try {
          for await (const _ of stream) {
            // no-op: we use onSseEvent for dispatch
          }
        } catch (e) {
          if (abort?.signal.aborted) return
          status.value = 'error'
          lastError.value = e instanceof Error ? e.message : String(e)
        } finally {
          abort = null
          if (status.value !== 'error') {
            status.value = 'disconnected'
          }
        }
      })()
    } catch (e) {
      abort = null
      status.value = 'error'
      lastError.value = e instanceof Error ? e.message : String(e)
    }
  }

  function disconnect() {
    if (!abort) return
    abort.abort()
    abort = null
    status.value = 'disconnected'
  }

  return {
    status,
    lastError,
    isConnected,
    on,
    connect,
    disconnect,
  }
})
