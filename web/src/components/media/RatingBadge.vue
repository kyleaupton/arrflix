<template>
  <TooltipProvider v-if="score">
    <Tooltip>
      <TooltipTrigger as-child>
        <span
          class="inline-flex items-center gap-1.5 rounded-full bg-white/10 backdrop-blur-sm border border-white/10 px-2.5 py-1 text-sm font-medium text-white"
        >
          <Star class="size-3.5 fill-yellow-400 text-yellow-400" />
          <span>{{ formattedScore }}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <span>{{ tooltipText }}</span>
      </TooltipContent>
    </Tooltip>
  </TooltipProvider>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Star } from 'lucide-vue-next'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

export type RatingSource = 'tmdb' | 'rt-critics' | 'rt-audience' | 'imdb'

const props = defineProps<{
  source: RatingSource
  score: number
  voteCount?: number
}>()

const formattedScore = computed(() => {
  switch (props.source) {
    case 'tmdb':
      return props.score.toFixed(1)
    case 'imdb':
      return props.score.toFixed(1)
    case 'rt-critics':
    case 'rt-audience':
      return `${Math.round(props.score)}%`
    default:
      return props.score.toFixed(1)
  }
})

const tooltipText = computed(() => {
  const sourceLabel =
    props.source === 'tmdb'
      ? 'TMDB'
      : props.source === 'imdb'
        ? 'IMDb'
        : props.source === 'rt-critics'
          ? 'Rotten Tomatoes (Critics)'
          : 'Rotten Tomatoes (Audience)'

  if (props.voteCount) {
    return `${sourceLabel} — ${props.voteCount.toLocaleString()} votes`
  }
  return sourceLabel
})
</script>
