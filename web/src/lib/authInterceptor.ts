// The global 401 handler: queue-and-replay silent refresh, installed once on the
// shared client. A 401 from a protected endpoint runs a single-flight refresh
// and replays the original request with the rotated access token. POST bodies
// replay too — the original body survives on `options` (rebuilt via
// getValidRequestBody), so we never depend on the already-consumed Request
// stream. When refresh fails, this is the one place a 401 becomes a logout
// (frontend-api rule 7): clear the session and redirect to /login.

import type { Router } from 'vue-router'
import { client } from '@/client/client.gen'
import { getValidRequestBody } from '@/client/core/utils.gen'
import { refreshAccessToken } from '@/lib/authRefresh'
import { useAuthStore } from '@/stores/auth'

// Auth-lifecycle endpoints are cookie- or credential-authed; a 401 from them is
// terminal (bad credentials, expired/invalid/reused refresh cookie) and can't be
// fixed by refreshing an access token — retrying there would recurse. /auth/me is
// deliberately NOT here: it's a normal bearer-protected read and participates in
// refresh + replay like any other endpoint.
const NO_REFRESH_PATHS = new Set([
  '/api/v1/auth/login',
  '/api/v1/auth/signup',
  '/api/v1/auth/plex/exchange',
  '/api/v1/auth/refresh',
  '/api/v1/auth/logout',
])

export function installAuthInterceptor(router: Router): void {
  client.interceptors.response.use(async (response, request, options) => {
    if (response.status !== 401) return response

    const { pathname } = new URL(request.url, window.location.origin)
    if (NO_REFRESH_PATHS.has(pathname)) return response

    const refreshed = await refreshAccessToken()
    const auth = useAuthStore()

    if (!refreshed) {
      auth.clearLocal()
      const current = router.currentRoute.value
      if (current.path !== '/login') {
        void router.replace({ path: '/login', query: { redirect: current.fullPath } })
      }
      return response
    }

    // Replay with the rotated bearer. Rebuild from `options` (body intact) and
    // issue via the raw fetch so the replay doesn't re-enter this interceptor —
    // no retry loop, no guard flag needed.
    const headers = new Headers(request.headers)
    headers.set('Authorization', `Bearer ${auth.token}`)
    const retried = new Request(request.url, {
      method: request.method,
      headers,
      body: getValidRequestBody(options) as BodyInit | null | undefined,
      credentials: request.credentials,
      redirect: request.redirect,
    })
    return options.fetch!(retried)
  })
}
