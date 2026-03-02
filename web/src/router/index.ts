import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
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

router.beforeEach((to) => {
  const auth = useAuthStore()

  // Public routes (login, setup, auth callback)
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
