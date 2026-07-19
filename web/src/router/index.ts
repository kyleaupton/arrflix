import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import { watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'

// Prevent the browser's native scroll restoration from fighting Vue Router
if ('scrollRestoration' in history) {
  history.scrollRestoration = 'manual'
}

// Master–detail section trees (Settings, Preferences) show a full-screen list at
// their bare index on mobile, so the index must remain a real, renderable route
// there. Desktop has no list — forward the index to the first section. Doing this
// as a guard (not a router redirect) is what keeps mobile on the list; the
// breakpoint mirrors SidebarProvider's `(max-width: 768px)`.
const forwardIndexOnDesktop =
  (basePath: string, firstSection: string) => (to: RouteLocationNormalized) => {
    if (to.path === basePath && window.matchMedia('(min-width: 769px)').matches) {
      return firstSection
    }
    return true
  }

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  scrollBehavior(_to, _from, savedPosition) {
    return new Promise((resolve) => {
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          resolve(savedPosition || { top: 0 })
        })
      })
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
      path: '/library/matching',
      component: () => import('@/views/MatchingInbox.vue'),
    },
    {
      path: '/search',
      component: () => import('@/views/Search.vue'),
    },
    {
      path: '/downloads',
      component: () => import('@/views/Downloads.vue'),
      meta: { requires: 'jobs.read' },
    },
    {
      // requests.view.own is the floor — excludes viewer, who can't request.
      path: '/requests',
      component: () => import('@/views/Requests.vue'),
      meta: { requires: 'requests.view.own' },
    },
    {
      // Users moved under Settings; keep the old path working for bookmarks.
      path: '/users',
      redirect: '/settings/users',
    },
    {
      // Per-user preferences — every authenticated role, no capability gate
      // (unlike the admin-only /settings tree). Full-bleed sidebar layout like
      // Settings; sections are Notifications and Devices.
      path: '/preferences',
      component: () => import('@/views/preferences/PreferencesLayout.vue'),
      meta: { layout: 'sidebar' },
      beforeEnter: forwardIndexOnDesktop('/preferences', '/preferences/notifications'),
      children: [
        {
          path: 'notifications',
          component: () => import('@/views/preferences/NotificationsView.vue'),
        },
        {
          path: 'devices',
          component: () => import('@/views/preferences/DevicesView.vue'),
        },
        {
          path: 'developer',
          component: () => import('@/views/preferences/DeveloperView.vue'),
        },
      ],
    },
    {
      path: '/settings',
      component: () => import('@/views/settings/SettingsLayout.vue'),
      meta: { layout: 'sidebar', requires: 'admin.settings.read' },
      beforeEnter: forwardIndexOnDesktop('/settings', '/settings/general'),
      children: [
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
          path: 'routing',
          component: () => import('@/views/settings/RoutingSettings.vue'),
        },
        {
          path: 'quality-profiles',
          component: () => import('@/views/settings/QualityProfilesSettings.vue'),
        },
        {
          path: 'email',
          component: () => import('@/views/settings/EmailSettings.vue'),
        },
        {
          path: 'users',
          component: () => import('@/views/Users.vue'),
          meta: { requires: 'admin.users.manage' },
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

  // Permission gate: a route (or any matched ancestor) may declare a required
  // capability key. This is a UX guard, not a security boundary — the API is
  // fail-closed, so this only avoids routing a user to a page that would 403.
  // Permissions are loaded by now: beforeEach awaits appStore.isReady above,
  // which main.ts sets only after fetchMe. meta.requires has no RouteMeta
  // augmentation today, so it's read via a cast.
  for (const record of to.matched) {
    const requires = record.meta.requires as string | undefined
    if (requires && !auth.can(requires)) {
      return { path: '/' }
    }
  }

  return true
})

export default router
