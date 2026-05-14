import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { client } from '@/client/client.gen'
import { authLogin, authMe, authPlexExchange } from '@/client/sdk.gen'
import { problemMessage } from '@/lib/api'

type Nullable<T> = T | null

interface MeResponseMinimal {
  sub?: string
  email?: string | null
  name?: string | null
}

const AUTH_TOKEN_KEY = 'arrflix.auth.token'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<Nullable<string>>(null)
  const user = ref<Nullable<MeResponseMinimal>>(null)
  const isLoading = ref(false)
  const errorMessage = ref<Nullable<string>>(null)

  const isAuthenticated = computed(() => Boolean(token.value))

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

  function setToken(nextToken: string | null) {
    token.value = nextToken
    if (nextToken) {
      localStorage.setItem(AUTH_TOKEN_KEY, nextToken)
    } else {
      localStorage.removeItem(AUTH_TOKEN_KEY)
    }
    applyTokenToClient(nextToken)
  }

  async function fetchMe(): Promise<void> {
    if (!token.value) return
    try {
      const res = await authMe<true>({ throwOnError: true })
      user.value = {
        sub: res.data.sub,
        email: res.data.email ?? null,
        name: res.data.name ?? null,
      }
    } catch {
      // Token likely invalid
      user.value = null
    }
  }

  /** Restore token from localStorage without making any HTTP calls. */
  function rehydrateToken(): boolean {
    const stored = localStorage.getItem(AUTH_TOKEN_KEY)
    if (!stored) {
      setToken(null)
      return false
    }
    setToken(stored)
    return true
  }

  /** Set user state directly from bootstrap response data. */
  function setUserFromBootstrap(u: {
    id: string
    email?: string | null
    username?: string | null
  }) {
    user.value = {
      sub: u.id,
      email: u.email,
      name: u.username,
    }
  }

  async function rehydrate(): Promise<void> {
    const stored = localStorage.getItem(AUTH_TOKEN_KEY)
    if (!stored) {
      setToken(null)
      return
    }
    setToken(stored)
    await fetchMe()
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
    // Direct token (future use)
    const fromToken = params.get('token')
    if (fromToken) {
      setToken(fromToken)
      await fetchMe()
      return true
    }

    // Plex PIN exchange
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

  function logout(): void {
    setToken(null)
    user.value = null
  }

  return {
    // state
    token,
    user,
    isLoading,
    errorMessage,
    isAuthenticated,
    // actions
    rehydrateToken,
    setUserFromBootstrap,
    rehydrate,
    loginWithPassword,
    startPlexSso,
    completeSsoFromCallback,
    logout,
  }
})
