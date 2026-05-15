<template>
  <div class="flex flex-col gap-6">
    <!-- Header -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-semibold">Library</h1>
        <p class="text-sm text-muted-foreground">Browse and manage your media</p>
      </div>
      <div class="relative">
        <Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          v-model="searchInput"
          placeholder="Search titles..."
          class="pl-9 w-64"
          @input="onSearchInput"
        />
      </div>
    </div>

    <!-- Type Filter Tabs -->
    <Tabs v-model="typeFilter" @update:model-value="onTypeFilterChange">
      <TabsList>
        <TabsTrigger value="">All</TabsTrigger>
        <TabsTrigger value="movie">Movies</TabsTrigger>
        <TabsTrigger value="series">Series</TabsTrigger>
      </TabsList>
    </Tabs>

    <Transition name="fade" mode="out-in">
      <!-- Loading State (initial) -->
      <div
        v-if="isLoading"
        key="loading"
        class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3 max-w-6xl"
      >
        <Skeleton v-for="i in pageSize" :key="i" class="aspect-[2/3] rounded-lg" />
      </div>

      <!-- Error State -->
      <div
        v-else-if="isError"
        key="error"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <p class="text-destructive">Failed to load library</p>
        <p class="text-sm text-muted-foreground mt-2">
          {{ errorMessage }}
        </p>
        <Button variant="outline" class="mt-4" @click="() => refetch()"> Try Again </Button>
      </div>

      <!-- Empty State -->
      <div
        v-else-if="items.length === 0"
        key="empty"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <Film class="h-12 w-12 text-muted-foreground mb-4" />
        <p class="text-lg font-medium">No media found</p>
        <p class="text-sm text-muted-foreground mt-1">
          {{
            searchQuery
              ? 'Try adjusting your search or filters'
              : 'Your library is empty. Add some media to get started!'
          }}
        </p>
      </div>

      <!-- Grid Content -->
      <div v-else key="content">
        <div
          class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3 max-w-6xl"
        >
          <Poster
            v-for="item in items"
            :key="item.id"
            :item="item"
            :to="getItemRoute(item)"
            size="medium"
            responsive
          />

          <!-- Loading skeletons for next page -->
          <template v-if="isFetchingNextPage">
            <Skeleton
              v-for="i in pageSize"
              :key="`skeleton-${i}`"
              class="aspect-[2/3] rounded-lg"
            />
          </template>
        </div>

        <!-- End of list indicator -->
        <p
          v-if="!hasNextPage && items.length > 0"
          class="text-center text-sm text-muted-foreground py-4"
        >
          End of library
        </p>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useInfiniteQuery } from '@tanstack/vue-query'
import { useDebounceFn, useInfiniteScroll } from '@vueuse/core'
import { Search, Film } from 'lucide-vue-next'
import { libraryListInfiniteOptions } from '@/client/@tanstack/vue-query.gen'
import type { LibraryItem } from '@/client/types.gen'
import { problemMessage } from '@/lib/api'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import Poster from '@/components/poster/Poster.vue'
import { useGridColumns } from '@/composables/useGridColumns'

// Grid columns composable
const { pageSize } = useGridColumns(3)

// State
const typeFilter = ref<'' | 'movie' | 'series'>('')
const searchQuery = ref('')
const searchInput = ref('')

// Debounced search
const debouncedSearch = useDebounceFn((value: string) => {
  searchQuery.value = value
}, 300)

const onSearchInput = () => {
  debouncedSearch(searchInput.value)
}

const onTypeFilterChange = () => {
  // Query will refetch from page 1 due to queryKey change
}

// Infinite Query
const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading, isError, error, refetch } =
  useInfiniteQuery(
    computed(() => ({
      ...libraryListInfiniteOptions({
        query: {
          type: typeFilter.value || undefined,
          search: searchQuery.value || undefined,
          pageSize: pageSize.value,
        },
      }),
      getNextPageParam: (lastPage: { pagination: { page: number; totalPages: number } }) => {
        const { page, totalPages } = lastPage.pagination
        return page < totalPages ? page + 1 : undefined
      },
      initialPageParam: 1,
    })),
  )

// Flatten pages into single array
const items = computed(() => data.value?.pages.flatMap((p) => p.data ?? []) ?? [])

const errorMessage = computed(() => problemMessage(error.value, 'Please try again later'))

// Infinite scroll - use window as scroll container
useInfiniteScroll(
  window,
  () => {
    if (hasNextPage.value && !isFetchingNextPage.value) {
      fetchNextPage()
    }
  },
  { distance: 200 },
)

// Navigation
const getItemRoute = (item: LibraryItem) => {
  return { path: `/${item.type}/${item.tmdbId}` }
}
</script>
