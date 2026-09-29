// Refetch one title's projection after the viewer changes it themselves.
//
// Most movement arrives as a `title_status` kick from the server. A request that
// awaits approval creates no want and starts no work, so nothing announces it —
// the viewer's own mutation is the only signal that their view of the title just
// changed. This closes that one gap; it is not a substitute for the kick.

import { toValue, type MaybeRefOrGetter } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import { titleStatusGetQueryKey } from '@/client/@tanstack/vue-query.gen'
import type { TitleMediaType } from '@/composables/useTitleStatus'

export function useTitleInvalidation(
  mediaType: MaybeRefOrGetter<TitleMediaType>,
  tmdbId: MaybeRefOrGetter<number>,
): () => void {
  const queryClient = useQueryClient()
  return () => {
    queryClient.invalidateQueries({
      queryKey: titleStatusGetQueryKey({
        path: { mediaType: toValue(mediaType), tmdbId: toValue(tmdbId) },
      }),
    })
  }
}
