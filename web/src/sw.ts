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

// The wire contract the backend marshals (push.Message): the notification text
// plus the metadata this worker needs to group and route it. Kept as a local
// type so the worker stays decoupled from the app bundle.
interface PushMessage {
  title: string
  body: string
  tag?: string
  media?: { tmdbId: number; type: string }
}

// The app path a notification opens. The backend sends the title's identity
// rather than a URL, so route shapes live here alongside the router that
// defines them. Anything without media — a test send — opens the root.
function pathFor(msg: PushMessage): string {
  if (!msg.media?.tmdbId) return '/'
  return msg.media.type === 'series' ? `/series/${msg.media.tmdbId}` : `/movie/${msg.media.tmdbId}`
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
      // The backend scopes the tag to one logical notification, so a re-delivery
      // replaces its earlier self while unrelated titles stack. An absent tag
      // never collapses, which is the safe default for an unparsed payload.
      tag: msg.tag,
      data: { url: pathFor(msg) },
    }),
  )
})

self.addEventListener('notificationclick', (event: NotificationEvent) => {
  event.notification.close()

  const url = (event.notification.data as { url?: string } | undefined)?.url ?? '/'
  const target = new URL(url, self.location.origin)

  // Reuse an open app window rather than piling up duplicates: focus it, and
  // navigate only when it isn't already on the target — navigating reloads the
  // SPA, so skipping the no-op keeps a tap on the page you're already reading
  // from throwing away its state.
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      for (const client of clientList) {
        if (!('focus' in client)) continue
        if (new URL(client.url).pathname === target.pathname) return client.focus()
        // navigate() rejects on a client this worker doesn't control, which
        // matchAll includes; surfacing the app still beats a tap that does
        // nothing, so fall back to a plain focus.
        return client
          .navigate(target.href)
          .then((navigated) => (navigated ?? client).focus())
          .catch(() => client.focus())
      }
      return self.clients.openWindow(target.href)
    }),
  )
})
