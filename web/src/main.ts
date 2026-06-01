import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'

import App from '@/App.vue'
import router from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { client } from '@/client/client.gen'
import { bootstrapGet } from '@/client/sdk.gen'
import { installRealtime } from '@/realtime/bindings'
import '@/main.css'

const app = createApp(App)

// One QueryClient shared by the Vue Query plugin and the realtime bindings, so
// SSE frames write the same cache the components read.
const queryClient = new QueryClient()

app.use(createPinia())
app.use(router)
app.use(VueQueryPlugin, {
  queryClient,
  enableDevtoolsV6Plugin: import.meta.env.DEV,
})

// Wire SSE events → query cache once, globally. The stream itself is opened on
// auth in App.vue; this just registers the cache-update contract.
installRealtime(queryClient)

// Redirect to login on any 401 response
client.interceptors.response.use(async (response) => {
  if (response.status === 401) {
    const auth = useAuthStore()
    auth.logout()
    const current = router.currentRoute.value
    if (current.path !== '/login') {
      router.replace({ path: '/login', query: { redirect: current.fullPath } })
    }
  }
  return response
})

// Force dark mode globally
document.documentElement.classList.add('dark')

// Restore token from localStorage (no HTTP call)
const auth = useAuthStore()
auth.rehydrateToken()

// Single bootstrap request populates auth + app state
const appStore = useAppStore()
try {
  const res = await bootstrapGet<true>({ throwOnError: true })
  appStore.setFromBootstrap({
    initialized: res.data.initialized,
    config: res.data.config,
    setup: res.data.setup,
  })
  if (res.data.user) {
    auth.setUserFromBootstrap(res.data.user)
  } else if (auth.token) {
    // Token was present but server says invalid — clear it
    auth.logout()
  }
} catch {
  // Bootstrap failed — app will show in default state
}
appStore.isReady = true

app.mount('#app')
