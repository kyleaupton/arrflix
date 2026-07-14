// Custom service worker (injectManifest strategy). It does two jobs:
//   1. Workbox app-shell precaching + SPA navigation fallback — the same
//      behavior the previous generateSW config gave us.
//   2. Web Push: render an incoming push as a notification, and focus/open the
//      app when the user taps it.
//
// It runs in a WebWorker global, typed via tsconfig.worker.json (WebWorker lib),
// and is compiled separately by vite-plugin-pwa — it is not part of the DOM app
// bundle.
import {
  cleanupOutdatedCaches,
  createHandlerBoundToURL,
  precacheAndRoute,
  type PrecacheEntry,
} from 'workbox-precaching'
import { NavigationRoute, registerRoute } from 'workbox-routing'
import { clientsClaim } from 'workbox-core'

declare let self: ServiceWorkerGlobalScope & {
  // Injected by vite-plugin-pwa at build time (the precache manifest).
  __WB_MANIFEST: Array<PrecacheEntry | string>
}

// ----- Precaching + navigation -----

cleanupOutdatedCaches()
precacheAndRoute(self.__WB_MANIFEST)

// Serve the cached app shell for SPA navigations, except anything under /api —
// those must hit the network so the backend answers them as normal.
registerRoute(
  new NavigationRoute(createHandlerBoundToURL('index.html'), {
    denylist: [/^\/api/],
  }),
)

// autoUpdate: a new SW takes over immediately and claims open pages.
self.skipWaiting()
clientsClaim()

// ----- Web Push -----

// The wire contract the backend marshals (push.Message): a title + body the
// notification renders. Kept as a local type so the worker stays decoupled from
// the app bundle.
interface PushMessage {
  title: string
  body: string
}

self.addEventListener('push', (event: PushEvent) => {
  if (!event.data) return

  let msg: PushMessage
  try {
    msg = event.data.json() as PushMessage
  } catch {
    // Non-JSON payload — show the raw text rather than dropping it.
    msg = { title: 'Arrflix', body: event.data.text() }
  }

  event.waitUntil(
    self.registration.showNotification(msg.title, {
      body: msg.body,
      icon: '/pwa-192x192.png',
      badge: '/pwa-64x64.png',
      // A later notification replaces an earlier one rather than stacking.
      tag: 'arrflix',
    }),
  )
})

self.addEventListener('notificationclick', (event: NotificationEvent) => {
  event.notification.close()
  // Focus an already-open app window if there is one, else open a new one.
  // Deep-linking to the specific title is deferred until the push payload
  // carries a tmdbId (same gap as the in-app bell).
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      for (const client of clientList) {
        if ('focus' in client) return client.focus()
      }
      return self.clients.openWindow('/')
    }),
  )
})
