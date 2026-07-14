import { computed, onMounted, ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { pushPublicKey, pushSubscribe, notificationPreferencesSet } from '@/client/sdk.gen'
import {
  pushListOptions,
  pushListQueryKey,
  pushRemoveMutation,
  pushTestMutation,
  notificationPreferencesGetQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import type { PushSubscriptionView } from '@/client/types.gen'
import { useAuthStore } from '@/stores/auth'

// Web Push for THIS browser + the caller's registered-device list. Browser facts
// (support, permission, this device's subscription endpoint) are local refs; the
// device list, removal, and test-send are server operations through the API.
//
// Enabling is deliberately one action = subscribe this browser AND turn the push
// channel on for the requests bundle, so a user who opts in actually receives
// something. The onboarding prompt calls enableOnThisDevice from a click handler
// because iOS/Safari reject Notification.requestPermission() outside a gesture.

const PUSH_BUNDLE = 'my_requests'

// VAPID application server keys arrive base64url-encoded; the Push API wants raw
// bytes. Build over an explicit ArrayBuffer so the result is Uint8Array<ArrayBuffer>
// (not <ArrayBufferLike>), which applicationServerKey's BufferSource type requires.
function urlBase64ToUint8Array(base64String: string): Uint8Array<ArrayBuffer> {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(base64)
  const out = new Uint8Array(new ArrayBuffer(raw.length))
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

export function usePushSubscription() {
  const auth = useAuthStore()
  const qc = useQueryClient()

  // --- Browser capability (static for this session) ---
  const supported =
    typeof navigator !== 'undefined' &&
    'serviceWorker' in navigator &&
    typeof window !== 'undefined' &&
    'PushManager' in window &&
    'Notification' in window
  const secureContext = typeof window !== 'undefined' && window.isSecureContext
  // iOS only exposes PushManager inside an installed PWA. On iOS in a plain
  // browser tab, push can't be enabled until "Add to Home Screen" — surface that
  // distinctly from a truly unsupported browser.
  const isStandalone =
    typeof window !== 'undefined' &&
    (window.matchMedia?.('(display-mode: standalone)').matches ||
      (navigator as Navigator & { standalone?: boolean }).standalone === true)
  const isIos = typeof navigator !== 'undefined' && /iphone|ipad|ipod/i.test(navigator.userAgent)
  const needsInstall = isIos && !isStandalone && !supported

  // --- Reactive browser state ---
  const permission = ref<NotificationPermission>(
    'Notification' in globalThis ? Notification.permission : 'default',
  )
  // The endpoint of THIS browser's current subscription, if any.
  const currentEndpoint = ref<string | null>(null)
  const isEnabling = ref(false)
  const enableError = ref<string | null>(null)

  // --- Device list (server state) ---
  const listQuery = useQuery(
    computed(() => ({
      ...pushListOptions(),
      enabled: auth.isAuthenticated,
      staleTime: 30_000,
    })),
  )
  const devices = computed<PushSubscriptionView[]>(() => listQuery.data.value ?? [])

  const isThisDeviceSubscribed = computed(() => currentEndpoint.value !== null)
  function isCurrentDevice(d: PushSubscriptionView) {
    return currentEndpoint.value !== null && d.endpoint === currentEndpoint.value
  }

  async function refreshCurrentEndpoint() {
    if (!supported) return
    try {
      const reg = await navigator.serviceWorker.ready
      const sub = await reg.pushManager.getSubscription()
      currentEndpoint.value = sub?.endpoint ?? null
    } catch {
      currentEndpoint.value = null
    }
  }

  onMounted(refreshCurrentEndpoint)

  // Must be called from a user gesture (a click handler).
  async function enableOnThisDevice(): Promise<boolean> {
    enableError.value = null
    if (!supported || !secureContext) {
      enableError.value = 'Push notifications need a secure (HTTPS or localhost) context.'
      return false
    }
    isEnabling.value = true
    try {
      const result = await Notification.requestPermission()
      permission.value = result
      if (result !== 'granted') {
        enableError.value =
          result === 'denied'
            ? 'Notifications are blocked. Enable them for this site in your browser settings.'
            : 'Notification permission was dismissed.'
        return false
      }

      const keyRes = await pushPublicKey()
      if (keyRes.error || !keyRes.data?.publicKey) {
        throw new Error('Could not fetch the server push key.')
      }

      const reg = await navigator.serviceWorker.ready
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(keyRes.data.publicKey),
      })

      const json = sub.toJSON()
      const subRes = await pushSubscribe({
        body: {
          endpoint: sub.endpoint,
          p256dh: json.keys?.p256dh ?? '',
          auth: json.keys?.auth ?? '',
        },
      })
      if (subRes.error) throw new Error('Could not register this device.')

      // Opt the push channel in so enabling actually delivers something.
      await notificationPreferencesSet({
        body: { scope: 'bundle', value: PUSH_BUNDLE, channel: 'push', enabled: true },
      })

      currentEndpoint.value = sub.endpoint
      qc.invalidateQueries({ queryKey: pushListQueryKey() })
      qc.invalidateQueries({ queryKey: notificationPreferencesGetQueryKey() })
      return true
    } catch (err) {
      enableError.value = err instanceof Error ? err.message : 'Could not enable notifications.'
      return false
    } finally {
      isEnabling.value = false
    }
  }

  // --- Remove / test (server mutations) ---
  const removeM = useMutation({
    ...pushRemoveMutation(),
    onSuccess: async (_data, vars) => {
      // If this was the current browser's device, drop its local subscription
      // too so it stops receiving pushes and can be cleanly re-enabled.
      const removed = devices.value.find((d) => d.id === vars.path?.id)
      if (removed && removed.endpoint === currentEndpoint.value) {
        try {
          const reg = await navigator.serviceWorker.ready
          const sub = await reg.pushManager.getSubscription()
          await sub?.unsubscribe()
        } catch {
          // best-effort — the server row is already gone
        }
        currentEndpoint.value = null
      }
      qc.invalidateQueries({ queryKey: pushListQueryKey() })
    },
  })

  const testM = useMutation({ ...pushTestMutation() })

  return {
    supported,
    secureContext,
    needsInstall,
    permission,
    devices,
    isLoading: computed(() => listQuery.isLoading.value),
    isError: computed(() => listQuery.isError.value),
    isThisDeviceSubscribed,
    isCurrentDevice,
    isEnabling,
    enableError,
    enableOnThisDevice,
    removeDevice: (id: string) => removeM.mutateAsync({ path: { id } }),
    removingId: computed(() =>
      removeM.isPending.value ? (removeM.variables.value?.path?.id ?? null) : null,
    ),
    testDevice: (id: string) => testM.mutateAsync({ path: { id } }),
    testingId: computed(() =>
      testM.isPending.value ? (testM.variables.value?.path?.id ?? null) : null,
    ),
  }
}
