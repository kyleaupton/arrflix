// Session refresh: the single choke point that trades the HttpOnly refresh
// cookie for a fresh in-memory access token. Used by the 401 interceptor
// (queue-and-replay) and the resume keepalive, so it must be safe to call
// concurrently from anywhere.
//
// Two layers of coordination keep concurrent refreshes from racing two
// rotations of the same secret (which would trip the backend's reuse
// detection, see HANDOVER_sessions_auth.md §4.2):
//   1. in-tab single-flight — N simultaneous 401s await one refresh, not N.
//   2. cross-tab mutual exclusion via the Web Locks API — at most one tab in
//      the browser refreshes at a time; the others run afterward against the
//      always-current cookie, so each rotation is a valid sequential one.

import { authRefresh } from '@/client/sdk.gen'
import { useAuthStore } from '@/stores/auth'

const REFRESH_LOCK = 'arrflix-auth-refresh'

let inFlight: Promise<boolean> | null = null

async function runRefresh(): Promise<boolean> {
  try {
    const res = await authRefresh<true>({ throwOnError: true })
    useAuthStore().setToken(res.data.token)
    return true
  } catch {
    // Missing/invalid/expired/reused refresh cookie, or offline. The caller
    // decides what a false means in its context (log out vs. best-effort).
    return false
  }
}

async function withCrossTabLock(fn: () => Promise<boolean>): Promise<boolean> {
  // Web Locks is absent on older Safari (< 15.4); there we fall back to in-tab
  // single-flight only and lean on the server's 60s rotation grace for the
  // rare cross-tab race.
  if (typeof navigator === 'undefined' || !('locks' in navigator)) {
    return fn()
  }
  const ok = await navigator.locks.request(REFRESH_LOCK, fn)
  return ok
}

/**
 * Rotate the session and store a fresh in-memory access token. Resolves true on
 * success; false when the refresh cookie can't be exchanged (treat as logged
 * out). Single-flighted within the tab and mutually exclusive across tabs.
 */
export function refreshAccessToken(): Promise<boolean> {
  if (inFlight) return inFlight
  inFlight = withCrossTabLock(runRefresh).finally(() => {
    inFlight = null
  })
  return inFlight
}
