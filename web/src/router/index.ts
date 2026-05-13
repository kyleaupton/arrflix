import { createRouter, createWebHistory } from 'vue-router'
import { watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'

// Prevent the browser's native scroll restoration from fighting Vue Router
if ('scrollRestoration' in history) {
  history.scrollRestoration = 'manual'
}

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  scrollBehavior(_to, _from, savedPosition) {
    // Delay scroll until after the page transition (150ms) completes
    return new Promise((resolve) => {
      setTimeout(() => {
        resolve(savedPosition || { top: 0 })
      }, 160)
    })
  },
  routes: [
    {
      path: '/',
      component: () => import('@/views/Home.vue'),
      meta: { layout: 'immersive' },
    },
    {
      path: '/library',
      component: () => import('@/views/Library.vue'),
    },
    {
      path: '/search',
      component: () => import('@/views/Search.vue'),
      meta: { layout: 'immersive' },
    },
    {
      path: '/downloads',
      component: () => import('@/views/Downloads.vue'),
    },
    {
      path: '/users',
      component: () => import('@/views/Users.vue'),
    },
    {
      path: '/settings',
      component: () => import('@/views/settings/SettingsLayout.vue'),
      children: [
        {
          path: '',
          redirect: '/settings/general',
        },
        {
          path: 'general',
          component: () => import('@/views/settings/GeneralSettings.vue'),
        },
        {
          path: 'libraries',
          component: () => import('@/views/settings/LibrarySettings.vue'),
        },
        {
          path: 'indexers',
          component: () => import('@/views/settings/IndexersSettings.vue'),
        },
        {
          path: 'name-templates',
          component: () => import('@/views/settings/NameTemplateSettings.vue'),
        },
        {
          path: 'downloaders',
          component: () => import('@/views/settings/downloader/DownloaderSettings.vue'),
        },
        {
          path: 'policies',
          component: () => import('@/views/settings/PolicySettings.vue'),
        },
      ],
    },
    {
      path: '/login',
      component: () => import('@/views/Login.vue'),
      meta: { public: true, layout: 'auth' },
    },
    {
      path: '/signup',
      component: () => import('@/views/Signup.vue'),
      meta: { public: true, layout: 'auth' },
    },
    {
      path: '/setup',
      component: () => import('@/views/Setup.vue'),
      meta: { public: true, layout: 'auth', setup: true },
    },
    {
      path: '/auth/callback',
      component: () => import('@/views/AuthCallback.vue'),
      meta: { public: true, layout: 'auth' },
    },

    // Dev playground (dev only)
    ...(import.meta.env.DEV
      ? [
          {
            path: '/dev',
            component: () => import('@/views/DevPlayground.vue'),
          },
        ]
      : []),

    // Media
    {
      path: '/movie/:id',
      component: () => import('@/views/Movie.vue'),
      meta: { layout: 'immersive' },
    },
    {
      path: '/series/:id',
      component: () => import('@/views/Series.vue'),
      meta: { layout: 'immersive' },
    },
    {
      path: '/person/:id',
      component: () => import('@/views/Person.vue'),
      meta: { layout: 'immersive' },
    },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  const appStore = useAppStore()

  // Wait for bootstrap to complete before enforcing any guards.
  // The initial navigation fires before main.ts finishes bootstrap,
  // so we block here until the app state is known.
  if (!appStore.isReady) {
    await new Promise<void>((resolve) => {
      const unwatch = watch(
        () => appStore.isReady,
        (ready) => {
          if (ready) {
            unwatch()
            resolve()
          }
        },
        { immediate: true },
      )
    })
  }

  // If app needs setup, force all non-setup routes to /setup
  if (appStore.needsSetup && !to.meta.setup) {
    return { path: '/setup' }
  }

  // If setup is complete, don't allow visiting /setup
  if (!appStore.needsSetup && to.meta.setup) {
    return { path: '/login' }
  }

  // Public routes (login, signup, auth callback, setup)
  if (to.meta.public) {
    return true
  }

  // Require auth for protected routes
  if (!auth.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }

  return true
})

export default router
