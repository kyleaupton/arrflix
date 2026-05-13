<template>
  <router-link
    :to="itemRoute"
    class="flex items-center gap-3 px-2 py-2 rounded-md hover:bg-accent cursor-pointer"
    @click="$emit('select')"
  >
    <!-- Poster thumbnail -->
    <div class="w-10 h-15 flex-shrink-0 rounded overflow-hidden bg-muted">
      <img
        v-if="result.posterPath"
        :src="posterUrl"
        :alt="result.title"
        class="w-full h-full object-cover"
      />
      <div v-else class="w-full h-full flex items-center justify-center">
        <component :is="placeholderIcon" class="h-4 w-4 text-muted-foreground" />
      </div>
    </div>

    <!-- Details -->
    <div class="flex-1 min-w-0">
      <div class="flex items-center gap-2">
        <span class="font-medium truncate">{{ result.title }}</span>
        <span v-if="result.year" class="text-sm text-muted-foreground"> ({{ result.year }}) </span>
      </div>
      <div class="flex items-center gap-2 mt-0.5">
        <Badge variant="outline" class="text-xs">
          {{ mediaTypeLabel }}
        </Badge>
        <div v-if="result.isInLibrary" class="flex items-center gap-1 text-xs text-emerald-500">
          <CheckCircle2 class="h-3 w-3" />
          <span>In Library</span>
        </div>
      </div>
    </div>
  </router-link>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Film, Tv, User, CheckCircle2 } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import type { SearchResult } from '@/client/types.gen'

const props = defineProps<{
  result: SearchResult
}>()

defineEmits<{
  select: []
}>()

const itemRoute = computed(() => {
  switch (props.result.mediaType) {
    case 'movie':
      return `/movie/${props.result.id}`
    case 'tv':
      return `/series/${props.result.id}`
    case 'person':
      return `/person/${props.result.id}`
    default:
      return '/'
  }
})

const posterUrl = computed(() => {
  if (!props.result.posterPath) return ''
  return `https://image.tmdb.org/t/p/w92${props.result.posterPath}`
})

const mediaTypeLabel = computed(() => {
  switch (props.result.mediaType) {
    case 'movie':
      return 'Movie'
    case 'tv':
      return 'Series'
    case 'person':
      return 'Person'
    default:
      return props.result.mediaType
  }
})

const placeholderIcon = computed(() => {
  switch (props.result.mediaType) {
    case 'movie':
      return Film
    case 'tv':
      return Tv
    case 'person':
      return User
    default:
      return Film
  }
})
</script>
