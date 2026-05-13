<template>
  <Rail :title="row.title" :subtitle="row.subtitle">
    <div v-for="item in row.items" :key="`${item.mediaType}-${item.tmdbId}`" class="flex-shrink-0">
      <Poster :item="item" :to="{ path: getItemPath(item) }" :is-downloading="item.isDownloading" />
    </div>
  </Rail>
</template>

<script setup lang="ts">
import type { FeedRow, HydratedTitle } from '@/client/types.gen'
import Rail from '@/components/rails/Rail.vue'
import Poster from '@/components/poster/Poster.vue'

defineProps<{ row: FeedRow }>()

const getItemPath = (item: HydratedTitle) => {
  return item.mediaType === 'movie' ? `/movie/${item.tmdbId}` : `/series/${item.tmdbId}`
}
</script>
