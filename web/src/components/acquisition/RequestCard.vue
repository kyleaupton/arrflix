<template>
  <div class="flex items-center gap-3 rounded-lg border bg-card p-3">
    <!-- Poster thumbnail, self-resolved from the media detail. -->
    <RouterLink :to="itemRoute" class="shrink-0">
      <div class="h-18 w-12 overflow-hidden rounded bg-muted">
        <img
          v-if="posterUrl"
          :src="posterUrl"
          :alt="title"
          class="h-full w-full object-cover"
          loading="lazy"
        />
        <div v-else class="flex h-full w-full items-center justify-center">
          <component :is="placeholderIcon" class="size-4 text-muted-foreground" />
        </div>
      </div>
    </RouterLink>

    <!-- Title + metadata -->
    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-2">
        <RouterLink :to="itemRoute" class="truncate font-medium hover:underline">
          {{ title }}
        </RouterLink>
        <span v-if="year" class="shrink-0 text-sm text-muted-foreground">({{ year }})</span>
      </div>
      <div class="mt-1 flex flex-wrap items-center gap-2">
        <RequestStatusPill :status="request.status" />
        <Badge variant="outline" class="text-xs">{{ request.tier }}</Badge>
        <span v-if="showRequester && request.requesterName" class="text-xs text-muted-foreground">
          {{ request.requesterName }}
        </span>
      </div>
      <!-- A denial's reason is the one piece of decision context the requester
           needs; surface it inline rather than hiding it behind a drill-in. -->
      <p v-if="request.deniedReason" class="mt-1 text-xs text-muted-foreground">
        {{ request.deniedReason }}
      </p>
    </div>

    <!-- Per-row actions (Cancel / Approve / Deny), supplied by the page. -->
    <div class="flex shrink-0 items-center gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { Film, Tv } from 'lucide-vue-next'
import { mediaGetMovieOptions, mediaGetSeriesOptions } from '@/client/@tanstack/vue-query.gen'
import { Badge } from '@/components/ui/badge'
import RequestStatusPill from './RequestStatusPill.vue'
import type { Request } from '@/client/types.gen'

const props = defineProps<{
  request: Request
  showRequester?: boolean
}>()

const isMovie = computed(() => props.request.type === 'movie')

// Title/poster come from the full media-detail endpoint — heavy (credits, videos,
// etc.) per row, but TanStack-cached and the v1 queue is small. Scale path: a
// lightweight title/poster projection on the request list itself. The movie and
// series endpoints have distinct response types, so they're two enabled-gated
// queries (one runs per card) rather than one union-typed query.
const movieQuery = useQuery(
  computed(() => ({
    ...mediaGetMovieOptions({ path: { id: props.request.tmdbId } }),
    enabled: isMovie.value,
  })),
)
const seriesQuery = useQuery(
  computed(() => ({
    ...mediaGetSeriesOptions({ path: { id: props.request.tmdbId } }),
    enabled: !isMovie.value,
  })),
)

const detail = computed(() => (isMovie.value ? movieQuery.data.value : seriesQuery.data.value))
const title = computed(() => detail.value?.title ?? 'Loading…')
const year = computed(() => detail.value?.year)

const posterUrl = computed(() => {
  const path = detail.value?.posterPath
  return path ? `https://image.tmdb.org/t/p/w92${path}` : ''
})

const placeholderIcon = computed(() => (isMovie.value ? Film : Tv))
const itemRoute = computed(() => `/${isMovie.value ? 'movie' : 'series'}/${props.request.tmdbId}`)
</script>
