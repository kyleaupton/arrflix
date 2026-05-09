<template>
  <div class="w-80">
    <!-- Loading state -->
    <div v-if="isLoading && query.length >= 2" class="p-2 space-y-2">
      <div v-for="i in 3" :key="i" class="flex items-center gap-3 px-2 py-2">
        <Skeleton class="w-10 h-15 rounded" />
        <div class="flex-1 space-y-2">
          <Skeleton class="h-4 w-3/4" />
          <Skeleton class="h-3 w-1/2" />
        </div>
      </div>
    </div>

    <!-- Results -->
    <div v-else-if="results.length > 0" class="p-1">
      <SearchResultItem
        v-for="result in results"
        :key="`${result.mediaType}-${result.id}`"
        :result="result"
        @select="$emit('select')"
      />

      <!-- See all results link -->
      <router-link
        v-if="totalResults > results.length"
        :to="{ path: '/search', query: { q: query } }"
        class="flex items-center justify-center gap-2 px-2 py-2 mt-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent rounded-md"
        @click="$emit('select')"
      >
        <span>See all {{ totalResults }} results</span>
        <ArrowRight class="h-4 w-4" />
      </router-link>
    </div>

    <!-- Empty state -->
    <div v-else-if="query.length >= 2 && !isLoading" class="p-4 text-center text-muted-foreground">
      <p>No results found for "{{ query }}"</p>
    </div>

    <!-- Hint -->
    <div v-else class="p-4 text-center text-muted-foreground text-sm">
      <p>Search for movies, series, and people</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ArrowRight } from 'lucide-vue-next'
import { Skeleton } from '@/components/ui/skeleton'
import SearchResultItem from './SearchResultItem.vue'
import type { SearchResult } from '@/client/types.gen'

defineProps<{
  query: string
  results: SearchResult[]
  totalResults: number
  isLoading: boolean
}>()

defineEmits<{
  select: []
}>()
</script>
