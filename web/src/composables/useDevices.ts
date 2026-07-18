import { computed, ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { pushPublicKey, pushSubscribe, notificationPreferencesSet } from '@/client/sdk.gen'
import {
  sessionsListOptions,
  sessionsListQueryKey,
  sessionsRevokeMutation,
  sessionsRevokeAllMutation,
  pushRemoveMutation,
  pushTestMutation,
  notificationPreferencesGetQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import type { SessionView } from '@/client/types.gen'
import { useAuthStore } from '@/stores/auth'

// The unified device surface for the preferences page. A "device" is a session
// (a login), and push is a capability of that session — so one list carries both
// the session concern (log out a device / log out others) and the push concern
// (enable on this device, test, disable). All server state is the sessionsList
// query; browser facts (push support, permission) are local to this tab.
//
// Push can only be *enabled* on the current device: it needs this browser's
// Notification permission + PushManager, and the subscription is attached to the
// caller's current session server-side. Test and disable work on any device that
// already has push, addressed by its pushSubscriptionId.

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

export function useDevices() {
  const auth = useAuthStore()
  const qc = useQueryClient()

  // --- Browser push capability (static for this session) ---
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

  const permission = ref<NotificationPermission>(
    'Notification' in globalThis ? Notification.permission : 'default',
  )
  const isEnabling = ref(false)
  const enableError = ref<string | null>(null)

  // --- Device list (server state) ---
  const listQuery = useQuery(
    computed(() => ({
      ...sessionsListOptions(),
      enabled: auth.isAuthenticated,
      staleTime: 30_000,
    })),
  )
  const devices = computed<SessionView[]>(() => listQuery.data.value ?? [])
  const current = computed<SessionView | undefined>(() => devices.value.find((d) => d.isCurrent))

  function invalidateDevices() {
    qc.invalidateQueries({ queryKey: sessionsListQueryKey() })
  }

  // --- Enable push on THIS device (multi-step: permission → key → subscribe →
  // opt the push channel in). Must be called from a user gesture (a click
  // handler): iOS/Safari reject Notification.requestPermission() outside one. ---
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

      // Opt the push channel in so enabling actually delivers something. If this
      // fails the browser is subscribed but the channel stays muted, so treat it
      // as a failed enable rather than reporting success (a retry re-runs both).
      const prefRes = await notificationPreferencesSet({
        body: { bundle: PUSH_BUNDLE, target: 'push', enabled: true },
      })
      if (prefRes.error) throw new Error('Registered this device, but could not switch push on.')

      invalidateDevices()
      qc.invalidateQueries({ queryKey: notificationPreferencesGetQueryKey() })
      return true
    } catch (err) {
      enableError.value = err instanceof Error ? err.message : 'Could not enable notifications.'
      return false
    } finally {
      isEnabling.value = false
    }
  }

  // --- Session mutations ---
  const revokeM = useMutation({
    ...sessionsRevokeMutation(),
    onSuccess: invalidateDevices,
  })
  const revokeOthersM = useMutation({
    ...sessionsRevokeAllMutation(),
    onSuccess: invalidateDevices,
  })

  // --- Push mutations (addressed by pushSubscriptionId) ---
  const removePushM = useMutation({
    ...pushRemoveMutation(),
    onSuccess: invalidateDevices,
  })
  const testPushM = useMutation({ ...pushTestMutation() })

  // Turning push off for a device removes its subscription. When it's this
  // browser, also drop the local PushManager subscription so it stops receiving
  // pushes and can be cleanly re-enabled later.
  async function disablePush(session: SessionView): Promise<void> {
    if (!session.pushSubscriptionId) return
    await removePushM.mutateAsync({ path: { id: session.pushSubscriptionId } })
    if (session.isCurrent && supported) {
      try {
        const reg = await navigator.serviceWorker.ready
        const sub = await reg.pushManager.getSubscription()
        await sub?.unsubscribe()
      } catch {
        // best-effort — the server row is already gone
      }
    }
  }

  return {
    devices,
    current,
    isLoading: computed(() => listQuery.isLoading.value),
    isError: computed(() => listQuery.isError.value),

    // Push capability + enable-on-this-device
    supported,
    secureContext,
    needsInstall,
    permission,
    isEnabling,
    enableError,
    enableOnThisDevice,

    // Session revoke ("log out")
    revokeDevice: (sessionId: string) => revokeM.mutateAsync({ path: { id: sessionId } }),
    revokingId: computed(() =>
      revokeM.isPending.value ? (revokeM.variables.value?.path?.id ?? null) : null,
    ),
    revokeOthers: () => revokeOthersM.mutateAsync({}),
    isRevokingOthers: computed(() => revokeOthersM.isPending.value),

    // Push per-device
    testDevice: (pushSubscriptionId: string) =>
      testPushM.mutateAsync({ path: { id: pushSubscriptionId } }),
    testingId: computed(() =>
      testPushM.isPending.value ? (testPushM.variables.value?.path?.id ?? null) : null,
    ),
    disablePush,
    disablingId: computed(() =>
      removePushM.isPending.value ? (removePushM.variables.value?.path?.id ?? null) : null,
    ),
  }
}
