import { onScopeDispose } from 'vue'
import { on } from '@/realtime/connection'

// Subscribe to a realtime event for EPHEMERAL, non-cache UI (e.g. live scan
// progress) for the lifetime of the calling scope; auto-unsubscribes on
// teardown. For server state, do NOT use this — the cache bindings
// (realtime/bindings.ts) keep the query cache live, so just useQuery.
export function useRealtimeListener(event: string, cb: (data: unknown) => void) {
  const off = on(event, cb)
  onScopeDispose(off)
}
