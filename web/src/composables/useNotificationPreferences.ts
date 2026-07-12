import { computed } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  notificationPreferencesGetOptions,
  notificationPreferencesGetQueryKey,
  notificationPreferencesSetMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { BundlePreference } from '@/client/types.gen'

// The notification-preferences data layer for the self-service prefs page. One
// query (the resolved per-bundle channel view) and one toggle mutation. Writes
// are optimistic so a switch flips instantly, then reconcile against the server
// on settle (the write is idempotent and the server owns the resolved value).

const prefsKey = notificationPreferencesGetQueryKey()

type PrefsResponse = { bundles: BundlePreference[] | null }

export function useNotificationPreferences() {
  const qc = useQueryClient()

  const query = useQuery({
    ...notificationPreferencesGetOptions(),
    staleTime: 30_000,
  })
  const bundles = computed<BundlePreference[]>(() => query.data.value?.bundles ?? [])

  const setM = useMutation({
    ...notificationPreferencesSetMutation(),
    onMutate: (vars) => {
      const body = vars.body
      if (!body) return
      qc.setQueryData<PrefsResponse>(prefsKey, (prev) => {
        if (!prev?.bundles) return prev
        return {
          bundles: prev.bundles.map((b) =>
            b.bundle !== body.value
              ? b
              : {
                  ...b,
                  channels: (b.channels ?? []).map((c) =>
                    c.channel === body.channel ? { ...c, enabled: body.enabled } : c,
                  ),
                },
          ),
        }
      })
    },
    // Reconcile on both paths: a failed write reverts to the server truth, a
    // successful one confirms the resolved value the server computed.
    onSettled: () => qc.invalidateQueries({ queryKey: prefsKey }),
  })

  return {
    bundles,
    isLoading: computed(() => query.isLoading.value),
    isError: computed(() => query.isError.value),
    setPreference: (bundle: string, channel: string, enabled: boolean) =>
      setM.mutate({ body: { scope: 'bundle', value: bundle, channel, enabled } }),
  }
}
