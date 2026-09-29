// The acquisition read for one title — the single query every status surface
// reads from.
//
// Before this, each surface joined four caches (tracking, jobs, media detail,
// requests) and reached its own conclusion, which is why they disagreed. There
// is nothing to join here: the server did it, and this returns the answer.
//
// Live progress is merged in from the SSE stream rather than the payload,
// because the payload is a snapshot taken on a kick and progress moves far
// faster than kicks do.

import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { titleStatusGetOptions } from '@/client/@tanstack/vue-query.gen'
import type { TitleAction, TitleStatus } from '@/client/types.gen'
import { readTitleProgress } from '@/realtime/titleProgress'

export type TitleMediaType = 'movie' | 'series'

export function useTitleStatus(
  mediaType: MaybeRefOrGetter<TitleMediaType>,
  tmdbId: MaybeRefOrGetter<number>,
) {
  const query = useQuery(
    computed(() =>
      titleStatusGetOptions({
        path: { mediaType: toValue(mediaType), tmdbId: toValue(tmdbId) },
      }),
    ),
  )

  const status = computed<TitleStatus | undefined>(() => query.data.value)

  // Live progress is only meaningful while the projection agrees work is in
  // flight. A reading that outlives its download is ignored rather than left on
  // screen — the projection is the authority on whether anything is happening.
  const progress = computed(() => {
    if (!status.value?.active) return null
    return readTitleProgress(toValue(mediaType), toValue(tmdbId))
  })

  const actions = computed<TitleAction[]>(() => status.value?.actions ?? [])
  const action = (kind: string) => computed(() => actions.value.find((a) => a.kind === kind))

  return {
    // Named rather than spread: a rest-spread of the query result observes every
    // field on it, so a consumer reading only `status` would still re-render on
    // fetch-status churn.
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    refetch: query.refetch,
    status,
    progress,
    actions,
    /** requestAction is absent when the viewer may not ask for this title. */
    requestAction: action('request'),
    /** cancelAction is present only for the viewer's own live request. */
    cancelAction: action('cancel'),
  }
}
