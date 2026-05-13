<template>
  <div class="flex flex-col gap-6">
    <!-- Header -->
    <div>
      <h1 class="text-2xl font-semibold">Search Results</h1>
      <p v-if="searchQuery" class="text-sm text-muted-foreground">
        {{ totalResults }} results for "{{ searchQuery }}"
      </p>
    </div>

    <!-- Type Filter Tabs -->
    <Tabs v-model="typeFilter">
      <TabsList>
        <TabsTrigger value="">All</TabsTrigger>
        <TabsTrigger value="movie">Movies</TabsTrigger>
        <TabsTrigger value="tv">Series</TabsTrigger>
        <TabsTrigger value="person">People</TabsTrigger>
      </TabsList>
    </Tabs>

    <Transition name="fade" mode="out-in">
      <!-- Loading State -->
      <div
        v-if="isLoading"
        key="loading"
        class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3 max-w-6xl"
      >
        <Skeleton v-for="i in 12" :key="i" class="aspect-[2/3] rounded-lg" />
      </div>

      <!-- Error State -->
      <div
        v-else-if="isError"
        key="error"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <p class="text-destructive">Search failed</p>
        <p class="text-sm text-muted-foreground mt-2">{{ error?.message || 'Please try again' }}</p>
      </div>

      <!-- Empty State -->
      <div
        v-else-if="!searchQuery"
        key="no-query"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <Search class="h-12 w-12 text-muted-foreground mb-4" />
        <p class="text-lg font-medium">Enter a search query</p>
        <p class="text-sm text-muted-foreground mt-1">
          Use the search bar above to find movies, series, and people
        </p>
      </div>

      <!-- No Results -->
      <div
        v-else-if="filteredResults.length === 0"
        key="no-results"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <Search class="h-12 w-12 text-muted-foreground mb-4" />
        <p class="text-lg font-medium">No results found</p>
        <p class="text-sm text-muted-foreground mt-1">Try adjusting your search or filters</p>
      </div>

      <!-- Results Grid -->
      <div
        v-else
        key="results"
        class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3 max-w-6xl"
      >
        <router-link
          v-for="result in filteredResults"
          :key="`${result.mediaType}-${result.id}`"
          :to="getItemRoute(result)"
          class="group"
        >
          <div class="relative aspect-[2/3] rounded-lg overflow-hidden bg-muted">
            <img
              v-if="result.posterPath"
              :src="getPosterUrl(result)"
              :alt="result.title"
              class="w-full h-full object-cover"
            />
            <div v-else class="w-full h-full flex items-center justify-center">
              <component :is="getPlaceholderIcon(result)" class="h-8 w-8 text-muted-foreground" />
            </div>

            <!-- Library badge -->
            <div
              v-if="result.isInLibrary"
              class="absolute top-2 left-2 flex items-center gap-1 px-2 py-1 rounded-full bg-black/70 text-xs text-emerald-400"
            >
              <CheckCircle2 class="h-3 w-3" />
              <span>In Library</span>
            </div>

            <!-- Type badge -->
            <div class="absolute bottom-2 left-2">
              <Badge variant="secondary" class="text-xs">
                {{ getMediaTypeLabel(result) }}
              </Badge>
            </div>
          </div>

          <div class="mt-2">
            <p class="font-medium text-sm truncate group-hover:text-primary">
              {{ result.title }}
            </p>
            <p v-if="result.year" class="text-xs text-muted-foreground">
              {{ result.year }}
            </p>
          </div>
        </router-link>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { Search, Film, Tv, User, CheckCircle2 } from 'lucide-vue-next'
import { mediaSearch } from '@/client/sdk.gen'
import type { SearchResult } from '@/client/types.gen'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

const route = useRoute()

const searchQuery = computed(() => (route.query.q as string) || '')
const typeFilter = ref('')

// Reset type filter when search query changes
watch(searchQuery, () => {
  typeFilter.value = ''
})

const { data, isLoading, isError, error } = useQuery({
  queryKey: computed(() => ['search-page', searchQuery.value]),
  queryFn: async () => {
    const { data } = await mediaSearch({
      query: {
        q: searchQuery.value,
        limit: 50,
      },
    })
    return data
  },
  enabled: computed(() => searchQuery.value.length >= 2),
})

const results = computed(() => data.value?.results ?? [])
const totalResults = computed(() => data.value?.totalResults ?? 0)

const filteredResults = computed(() => {
  if (!typeFilter.value) return results.value
  return results.value.filter((r) => r.mediaType === typeFilter.value)
})

const getItemRoute = (result: SearchResult) => {
  switch (result.mediaType) {
    case 'movie':
      return `/movie/${result.id}`
    case 'tv':
      return `/series/${result.id}`
    case 'person':
      return `/person/${result.id}`
    default:
      return '/'
  }
}

const getPosterUrl = (result: SearchResult) => {
  if (!result.posterPath) return ''
  return `https://image.tmdb.org/t/p/w342${result.posterPath}`
}

const getMediaTypeLabel = (result: SearchResult) => {
  switch (result.mediaType) {
    case 'movie':
      return 'Movie'
    case 'tv':
      return 'Series'
    case 'person':
      return 'Person'
    default:
      return result.mediaType
  }
}

const getPlaceholderIcon = (result: SearchResult) => {
  switch (result.mediaType) {
    case 'movie':
      return Film
    case 'tv':
      return Tv
    case 'person':
      return User
    default:
      return Film
  }
}
</script>
