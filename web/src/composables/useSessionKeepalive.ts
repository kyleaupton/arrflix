import { onMounted, onUnmounted } from 'vue'
import { refreshAccessToken } from '@/lib/authRefresh'
import { useAuthStore } from '@/stores/auth'

// Minimum spacing between resume-triggered refreshes. A 15-min access token
// stays comfortably fresh with an at-most-once-per-minute refresh, and this
// keeps rapid focus/blur churn (iOS notification pulldown, tab flicking) from
// rotating the session on every event.
const KEEPALIVE_MIN_INTERVAL_MS = 60_000

/**
 * Refreshes the session when the app returns to the foreground. This is the
 * load-bearing path for iOS standalone PWAs, which never re-check on relaunch
 * and can resume with a long-expired access token; refreshing here keeps
 * `auth.token` valid for the next request and for the SSE stream's per-attempt
 * token read (realtime/connection.ts).
 *
 * Best-effort by design: a failed refresh (e.g. the brief no-network window
 * right after a mobile resume) is swallowed, never treated as logout — the 401
 * interceptor is the authority on hard auth failures, and punishing a transient
 * network blip with a logout would be worse than doing nothing.
 */
export function useSessionKeepalive(): void {
  const auth = useAuthStore()
  let lastRefreshAt = 0

  const maybeRefresh = () => {
    if (!auth.isAuthenticated) return
    const now = Date.now()
    if (now - lastRefreshAt < KEEPALIVE_MIN_INTERVAL_MS) return
    lastRefreshAt = now
    void refreshAccessToken()
  }

  const onVisible = () => {
    if (document.visibilityState === 'visible') maybeRefresh()
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('focus', maybeRefresh)
  })
  onUnmounted(() => {
    document.removeEventListener('visibilitychange', onVisible)
    window.removeEventListener('focus', maybeRefresh)
  })
}
