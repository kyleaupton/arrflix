import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { unmatchedFilesListOptions } from '@/client/@tanstack/vue-query.gen'

// Ambient inbox stats, shared by the nav badge and the matching page. Both read
// the same query key (page 1, pageSize 1) so TanStack dedupes to one request and
// a single invalidation refreshes every observer.
//
// We fetch the smallest possible page: `pagination.total` is the inbox size and
// `countsByOutcome` is the per-band breakdown — both are computed server-side
// over the whole inbox regardless of pageSize, so we never pull actual rows here.
export function useInboxCount() {
  const query = useQuery({
    ...unmatchedFilesListOptions({ query: { page: 1, pageSize: 1 } }),
    // Nav chrome — a stale-but-cheap count beats hammering, and a failed count
    // must never tear down the menu, so it stays soft (no throwOnError).
    throwOnError: false,
    staleTime: 30_000,
  })

  const count = computed(() => query.data.value?.pagination.total ?? 0)
  const counts = computed<Record<string, number>>(() => query.data.value?.countsByOutcome ?? {})

  return { count, counts }
}
