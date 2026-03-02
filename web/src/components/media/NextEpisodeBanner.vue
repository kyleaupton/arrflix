<template>
  <div class="flex items-center gap-3 rounded-lg border bg-muted/30 px-4 py-3">
    <CalendarDays class="size-5 shrink-0 text-muted-foreground" />
    <div class="min-w-0">
      <p class="text-xs font-medium text-muted-foreground">Next Episode</p>
      <p class="text-sm font-medium truncate">
        S{{ String(episode.seasonNumber).padStart(2, '0') }}E{{ String(episode.episodeNumber).padStart(2, '0') }}
        <template v-if="episode.title"> &ldquo;{{ episode.title }}&rdquo;</template>
        <template v-if="formattedDate">
          <span class="text-muted-foreground font-normal"> · Airs {{ formattedDate }}</span>
        </template>
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CalendarDays } from 'lucide-vue-next'
import type { ModelNextEpisode } from '@/client/types.gen'

const props = defineProps<{
  episode: ModelNextEpisode
}>()

const formattedDate = computed(() => {
  if (!props.episode.airDate) return null
  const date = new Date(props.episode.airDate + 'T00:00:00')
  if (isNaN(date.getTime())) return props.episode.airDate
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
})
</script>
