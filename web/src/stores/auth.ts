import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { client } from '@/client/client.gen'
import { authLogin, authLogout, authMe, authPlexExchange } from '@/client/sdk.gen'
import { problemMessage } from '@/lib/api'

type Nullable<T> = T | null

interface AuthUser {
  sub?: string
  email?: string | null
  name?: string | null
  roles: string[]
  permissions: string[]
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<Nullable<string>>(null)
  const user = ref<Nullable<AuthUser>>(null)
  const isLoading = ref(false)
  const errorMessage = ref<Nullable<string>>(null)

  const isAuthenticated = computed(() => Boolean(token.value))

  // The store is the one place that knows the permission-key grammar: /auth/me
  // hands us the effective global allow-key list and everything else reads a
  // named capability off it, never a role literal.
  const permissionSet = computed(() => new Set(user.value?.permissions ?? []))
  function can(key: string): boolean {
    return permissionSet.value.has(key)
  }
  // Add-vs-Request: exact per-(type,tier) auto-approve grant. Tier is lowercased
  // to match the key grammar ("HD" -> requests.auto_approve:movie:hd).
  function canAutoApprove(type: 'movie' | 'series', tier: string): boolean {
    return can(`requests.auto_approve:${type}:${tier.toLowerCase()}`)
  }
  const canManageJobs = computed(() => can('jobs.manage')) // operator actions
  const canViewJobs = computed(() => can('jobs.read')) // downloads view
  const canManageUsers = computed(() => can('admin.users.manage'))
  const canManageSettings = computed(() => can('admin.settings.read'))

  // Requests. view.own gates the /requests page + nav; view.any is the approver
  // queue. approve/deny are per-media-type, so they're functions, not getters.
  const canViewOwnRequests = computed(() => can('requests.view.own'))
  const canReviewRequests = computed(() => can('requests.view.any'))
  const canCancelRequests = computed(() => can('requests.cancel.own') || can('requests.cancel.any'))
  function canApprove(type: 'movie' | 'series'): boolean {
    return can(`requests.approve:${type}`)
  }
  function canDeny(type: 'movie' | 'series'): boolean {
    return can(`requests.deny:${type}`)
  }

  function applyTokenToClient(nextToken: string | null) {
    // Put Authorization on all requests
    if (nextToken) {
      client.setConfig({
        headers: {
          Authorization: `Bearer ${nextToken}`,
        },
      })
    } else {
      client.setConfig({
        headers: {
          Authorization: null,
        },
      })
    }
  }

  // The access token is memory-only — never persisted. It's reconstituted on
  // boot and on resume from the HttpOnly refresh cookie (see lib/authRefresh).
  function setToken(nextToken: string | null) {
    token.value = nextToken
    applyTokenToClient(nextToken)
  }

  async function fetchMe(): Promise<boolean> {
    if (!token.value) return false
    try {
      const res = await authMe<true>({ throwOnError: true })
      user.value = {
        sub: res.data.sub,
        email: res.data.email ?? null,
        name: res.data.name ?? null,
        roles: res.data.roles ?? [],
        permissions: res.data.permissions ?? [],
      }
      return true
    } catch {
      // Token likely invalid
      user.value = null
      return false
    }
  }

  /**
   * Set user state directly from bootstrap response data. Bootstrap carries
   * only identity, so roles/permissions default empty here; callers that need
   * them (capability gating, Add-vs-Request) follow with fetchMe to enrich.
   */
  function setUserFromBootstrap(u: {
    id: string
    email?: string | null
    username?: string | null
  }) {
    user.value = {
      sub: u.id,
      email: u.email,
      name: u.username,
      roles: [],
      permissions: [],
    }
  }

  async function loginWithPassword(login: string, password: string): Promise<boolean> {
    isLoading.value = true
    errorMessage.value = null
    try {
      const res = await authLogin<true>({
        throwOnError: true,
        body: { login, password },
      })
      const nextToken = res.data.token
      setToken(nextToken)
      await fetchMe()
      return true
    } catch {
      errorMessage.value = 'Invalid credentials'
      return false
    } finally {
      isLoading.value = false
    }
  }

  function startPlexSso(): void {
    // Backend endpoint expected to initiate Plex OAuth and redirect back
    const redirectUri = `${window.location.origin}/auth/callback`
    const url = `/api/v1/auth/plex/start?redirect_uri=${encodeURIComponent(redirectUri)}`
    window.location.href = url
  }

  async function completeSsoFromCallback(params: URLSearchParams): Promise<boolean> {
    // Plex PIN exchange. The exchange endpoint sets the refresh cookie
    // server-side and returns the access token in the body.
    const pinId = params.get('pinId')
    if (pinId) {
      try {
        const res = await authPlexExchange<true>({
          throwOnError: true,
          body: { pin_id: Number(pinId) },
        })
        const nextToken = res.data.token
        if (nextToken) {
          setToken(nextToken)
          await fetchMe()
          return true
        }
      } catch (err) {
        errorMessage.value = problemMessage(err, 'Plex login failed')
      }
      return false
    }

    return false
  }

  // Drop in-memory session state without touching the server. The 401
  // interceptor and boot use this when a refresh is unrecoverable.
  function clearLocal(): void {
    setToken(null)
    user.value = null
  }

  // Clear local state immediately for a snappy UI, then revoke the session
  // server-side (best-effort). Logout is cookie-authed, so dropping the bearer
  // first is fine. Without the server call a reload would silently re-auth from
  // the still-valid refresh cookie, so this must run for a real logout.
  async function logout(): Promise<void> {
    clearLocal()
    try {
      await authLogout<true>({ throwOnError: true })
    } catch {
      // Offline or already revoked — local session is gone regardless.
    }
  }

  return {
    // state
    token,
    user,
    isLoading,
    errorMessage,
    isAuthenticated,
    // capabilities
    can,
    canAutoApprove,
    canManageJobs,
    canViewJobs,
    canManageUsers,
    canManageSettings,
    canViewOwnRequests,
    canReviewRequests,
    canCancelRequests,
    canApprove,
    canDeny,
    // actions
    setToken,
    fetchMe,
    setUserFromBootstrap,
    loginWithPassword,
    startPlexSso,
    completeSsoFromCallback,
    clearLocal,
    logout,
  }
})
