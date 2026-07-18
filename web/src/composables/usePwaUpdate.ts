import { useRegisterSW } from 'virtual:pwa-register/vue'

// How often to poll for a newer build while the app stays open.
const UPDATE_INTERVAL_MS = 60 * 60 * 1000

// Registers the service worker and drives the "an update is waiting" signal.
//
// `needRefresh` flips true once a new build has finished precaching and its
// worker is parked in "waiting" (see src/sw.ts — it holds off skipWaiting until
// we ask). Calling `applyUpdate` sends SKIP_WAITING; the new worker activates,
// claims the page, and vite-plugin-pwa reloads it onto the fresh bundle.
//
// The browser only re-checks the worker on navigation, so a long-lived PWA can
// sit on a stale build indefinitely. We poll on an interval and — the load-
// bearing part on iOS standalone, which never re-checks on relaunch — force a
// check whenever the app regains focus.
export function usePwaUpdate() {
  const { needRefresh, updateServiceWorker } = useRegisterSW({
    onRegisteredSW(_swUrl, registration) {
      if (!registration) return

      const check = () => {
        // Nothing to do mid-install, and update() would fail offline anyway.
        if (registration.installing || !navigator.onLine) return
        void registration.update()
      }

      setInterval(check, UPDATE_INTERVAL_MS)
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') check()
      })
      window.addEventListener('focus', check)
    },
  })

  return {
    needRefresh,
    applyUpdate: () => updateServiceWorker(),
  }
}
