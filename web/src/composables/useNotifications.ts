import { computed, type Ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  notificationsListOptions,
  notificationsListQueryKey,
  notificationsUnreadCountOptions,
  notificationsUnreadCountQueryKey,
  notificationsMarkReadMutation,
  notificationsMarkAllReadMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { InboxNotification } from '@/client/types.gen'
import { useAuthStore } from '@/stores/auth'

// The bell's data layer. Two queries with different jobs: the unread count is
// ambient nav chrome (always live, cheap), the list is on-demand (only fetched
// once the bell is opened). Delivery emits no SSE event today, so freshness comes
// from a slow poll + refetch-on-focus rather than a realtime binding — a
// stale-but-cheap badge, matching the app's other nav counts (useInboxCount).
// When the backend broadcasts a per-user notification event, swap the poll for a
// realtime binding that invalidates these two keys.

const LIST_LIMIT = 50
const listOpts = { query: { limit: LIST_LIMIT } }
const listKey = notificationsListQueryKey(listOpts)
const countKey = notificationsUnreadCountQueryKey()

type UnreadCount = { count: number }

export function useNotifications(listEnabled: Ref<boolean>) {
  const auth = useAuthStore()
  const qc = useQueryClient()

  // Unread count — polls on a slow cadence and on window focus. Soft (no
  // throwOnError): a failed count must never tear down the navbar.
  const countQuery = useQuery(
    computed(() => ({
      ...notificationsUnreadCountOptions(),
      enabled: auth.isAuthenticated,
      refetchInterval: 60_000,
      staleTime: 30_000,
      throwOnError: false,
    })),
  )
  const unreadCount = computed(() => countQuery.data.value?.count ?? 0)

  // The list is only fetched once the bell is opened — most sessions never open
  // it, and the badge already says whether it's worth opening.
  const listQuery = useQuery(
    computed(() => ({
      ...notificationsListOptions(listOpts),
      enabled: auth.isAuthenticated && listEnabled.value,
      staleTime: 15_000,
      throwOnError: false,
    })),
  )
  const notifications = computed<InboxNotification[]>(() => listQuery.data.value ?? [])

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: listKey })
    qc.invalidateQueries({ queryKey: countKey })
  }

  // markRead / markAllRead patch the cache optimistically so the badge and the
  // read-dots react instantly, then invalidate on settle as a correctness
  // backstop (the server call is idempotent).
  const markReadM = useMutation({
    ...notificationsMarkReadMutation(),
    onMutate: (vars) => {
      const id = vars.path?.id
      const stamp = new Date().toISOString()
      qc.setQueryData<InboxNotification[]>(listKey, (prev) =>
        prev?.map((n) => (n.id === id && !n.readAt ? { ...n, readAt: stamp } : n)),
      )
      qc.setQueryData<UnreadCount>(countKey, (prev) =>
        prev ? { count: Math.max(0, prev.count - 1) } : prev,
      )
    },
    onSettled: invalidate,
  })

  const markAllReadM = useMutation({
    ...notificationsMarkAllReadMutation(),
    onMutate: () => {
      const stamp = new Date().toISOString()
      qc.setQueryData<InboxNotification[]>(listKey, (prev) =>
        prev?.map((n) => (n.readAt ? n : { ...n, readAt: stamp })),
      )
      qc.setQueryData<UnreadCount>(countKey, () => ({ count: 0 }))
    },
    onSettled: invalidate,
  })

  return {
    unreadCount,
    notifications,
    isLoading: computed(() => listQuery.isLoading.value),
    isError: computed(() => listQuery.isError.value),
    markRead: (id: string) => {
      const n = notifications.value.find((x) => x.id === id)
      if (n && !n.readAt) markReadM.mutate({ path: { id } })
    },
    markAllRead: () => {
      if (unreadCount.value > 0) markAllReadM.mutate({})
    },
  }
}
