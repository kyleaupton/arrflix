import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'

import App from '@/App.vue'
import router from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { bootstrapGet } from '@/client/sdk.gen'
import { installAuthInterceptor } from '@/lib/authInterceptor'
import { refreshAccessToken } from '@/lib/authRefresh'
import { installRealtime } from '@/realtime/bindings'
import '@/main.css'

const app = createApp(App)

// One QueryClient shared by the Vue Query plugin and the realtime bindings, so
// SSE frames write the same cache the components read.
//
// staleTime: the volatile server state (jobs, wants) is kept live by the realtime
// bindings regardless, so a default staleTime here only affects the non-realtime
// reads (media detail, lists) — it stops every navigation and window-focus from
// refetching data seconds old, which was the bulk of the request volume. Focus
// refetch stays on for genuinely-stale data so cross-actor list changes still land.
const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000 } },
})

app.use(createPinia())
app.use(router)
app.use(VueQueryPlugin, {
  queryClient,
  enableDevtoolsV6Plugin: import.meta.env.DEV,
})

// Wire SSE events → query cache once, globally. The stream itself is opened on
// auth in App.vue; this just registers the cache-update contract.
installRealtime(queryClient)

// Queue-and-replay 401 handling: a 401 on a protected endpoint triggers a
// single-flight refresh and replays the original request with the rotated
// token; an unrecoverable refresh clears the session and redirects to /login.
installAuthInterceptor(router)

// Force dark mode globally
document.documentElement.classList.add('dark')

const auth = useAuthStore()
const appStore = useAppStore()

// Boot the session from the HttpOnly refresh cookie — no token is read from
// localStorage. A valid cookie mints an in-memory access token; otherwise the
// app boots unauthenticated and the router guard sends the user to /login.
try {
  const refreshed = await refreshAccessToken()
  const res = await bootstrapGet<true>({ throwOnError: true })
  appStore.setFromBootstrap({
    initialized: res.data.initialized,
    config: res.data.config,
    setup: res.data.setup,
  })
  if (refreshed) {
    if (res.data.user) auth.setUserFromBootstrap(res.data.user)
    // Enrich roles + auto-approve policy. A token rejected here means the
    // session is unusable, so clear it and boot unauthenticated.
    const ok = await auth.fetchMe()
    if (!ok) auth.clearLocal()
  }
} catch {
  // Bootstrap failed — app will show in default state
}
appStore.isReady = true

app.mount('#app')
