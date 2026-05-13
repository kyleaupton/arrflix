<template>
  <Rail :title="rail.title" :subtitle="rail.subtitle">
    <div v-for="movie in rail.movies" :key="movie.tmdbId" class="flex-shrink-0">
      <Poster :item="movie" :to="{ path: `/movie/${movie.tmdbId}` }" />
    </div>
  </Rail>
</template>

<script setup lang="ts">
import type { MovieRail } from '@/client/types.gen'
import Rail from '@/components/rails/Rail.vue'
import Poster from '@/components/poster/Poster.vue'

defineProps<{
  rail: {
    id: string
    title: string
    subtitle?: string
    type: string
    movies: MovieRail[]
    series: any[]
  }
}>()
</script>
