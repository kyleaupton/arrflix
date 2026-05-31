import { onScopeDispose } from 'vue'
import { useQueryClient, type QueryKey } from '@tanstack/vue-query'
import { useEventsStore } from '@/stores/events'

// Bridges the SSE event bus to TanStack Query, implementing the spec's two
// state-delivery patterns. Both register a listener for the lifetime of the
// calling scope (component/composable) and tear it down automatically.
//
// They also re-sync on the store's internal lifecycle events — `_reconnected`
// (a drop that may have outrun the replay window), `_resume_gap` (the replay
// buffer was exhausted), and `_visibility_restored` (the tab woke and may have
// missed events) — by invalidating the key so the cache refetches a fresh
// baseline. Delta consumers can't safely apply deltas across such a gap, so
// they fall back to a refetch too.
const RESYNC_EVENTS = ['_reconnected', '_resume_gap', '_visibility_restored'] as const

// useSSEMutation — Pattern A (snapshot + delta). Applies each event payload to
// the cached query data via `applyDelta`, mutating in place without a refetch.
// `applyDelta(prev, payload)` returns the next cache value; `prev` is undefined
// until the snapshot lands, so handle that case.
export function useSSEMutation<TData, TPayload>(
  queryKey: QueryKey,
  eventName: string,
  applyDelta: (prev: TData | undefined, payload: TPayload) => TData,
) {
  const events = useEventsStore()
  const queryClient = useQueryClient()

  const offDelta = events.on(eventName, (data) => {
    queryClient.setQueryData<TData>(queryKey, (prev) => applyDelta(prev, data as TPayload))
  })

  const resync = () => queryClient.invalidateQueries({ queryKey })
  const offResync = RESYNC_EVENTS.map((e) => events.on(e, resync))

  onScopeDispose(() => {
    offDelta()
    offResync.forEach((off) => off())
  })
}

// useSSEInvalidation — Pattern B (notify + refetch). Treats the event purely as
// a kick: on each one, invalidate the key so every observer refetches through
// the REST endpoint (which already does the right auth filtering).
export function useSSEInvalidation(queryKey: QueryKey, eventName: string) {
  const events = useEventsStore()
  const queryClient = useQueryClient()

  const invalidate = () => queryClient.invalidateQueries({ queryKey })

  const offEvent = events.on(eventName, invalidate)
  const offResync = RESYNC_EVENTS.map((e) => events.on(e, invalidate))

  onScopeDispose(() => {
    offEvent()
    offResync.forEach((off) => off())
  })
}
