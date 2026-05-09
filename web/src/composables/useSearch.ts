import { ref, computed } from 'vue'
import { refDebounced } from '@vueuse/core'
import { useQuery } from '@tanstack/vue-query'
import { mediaSearch } from '@/client/sdk.gen'

export function useSearch(debounceMs = 300) {
  const query = ref('')
  const debouncedQuery = refDebounced(query, debounceMs)

  const { data, isLoading, isError, error } = useQuery({
    queryKey: computed(() => ['search', debouncedQuery.value]),
    queryFn: async () => {
      const { data } = await mediaSearch({
        query: {
          q: debouncedQuery.value,
          limit: 6,
        },
      })
      return data
    },
    enabled: computed(() => debouncedQuery.value.length >= 2),
  })

  const results = computed(() => data.value?.results ?? [])
  const totalResults = computed(() => data.value?.totalResults ?? 0)

  const clear = () => {
    query.value = ''
  }

  return {
    query,
    debouncedQuery,
    results,
    totalResults,
    isLoading,
    isError,
    error,
    clear,
  }
}
