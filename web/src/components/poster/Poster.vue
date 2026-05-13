<template>
  <component
    :is="to ? 'router-link' : 'div'"
    :to="to"
    class="poster-outer"
    :class="[sizeClass, { 'is-clickable': clickable }]"
  >
    <!-- Inner container: scales on hover, clips image to rounded corners -->
    <div class="poster-inner">
      <img
        class="poster"
        :class="{ 'is-loaded': !isLoading, 'cursor-pointer': clickable }"
        :src="posterPath"
        :alt="item.title"
        loading="lazy"
        decoding="async"
        @load="onLoad"
        @error="onError"
      />
      <Skeleton v-if="isLoading" class="poster-skeleton" />
      <!-- Status badges stay with the image -->
      <div v-if="isDownloading" class="status-badge download-badge">
        <Loader2 class="size-4 animate-spin" aria-hidden="true" />
        <span>Downloading</span>
      </div>
      <div v-else-if="showLibraryBadge" class="status-badge library-badge">
        <CheckCircle2 class="size-4" aria-hidden="true" />
        <span>In library</span>
      </div>
    </div>
    <!-- Overlay: sibling to inner, not clipped by it -->
    <div class="poster-overlay">
      <div class="poster-info">
        <span class="poster-title">{{ item.title }}</span>
        <span class="poster-meta">{{ mediaTypeLabel }} · {{ item.year }}</span>
      </div>
    </div>
  </component>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { CheckCircle2, Loader2 } from 'lucide-vue-next'
import { Skeleton } from '@/components/ui/skeleton'
import {
  type HydratedTitle,
  type LibraryItem,
  type MovieDetail,
  type MovieRail,
  type SeriesDetail,
} from '@/client/types.gen'

/**
 * "poster_sizes": [
      "w92",
      "w154",
      "w185",
      "w342",
      "w500",
      "w780",
      "original"
    ],
 */

// Poster size configuration
const POSTER_SIZES = {
  small: {
    width: '8rem', // ~128px
    class: 'poster--sm',
    tmdbSize: 'w185',
  },
  medium: {
    width: '11rem', // ~176px
    class: 'poster--md',
    tmdbSize: 'w500',
  },
  large: {
    width: '16rem', // ~256px
    class: 'poster--lg',
    tmdbSize: 'w500',
  },
} as const

type PosterSize = keyof typeof POSTER_SIZES

const props = withDefaults(
  defineProps<{
    item: MovieDetail | SeriesDetail | HydratedTitle | MovieRail | LibraryItem
    size?: PosterSize
    to?: { path: string } | string
    clickable?: boolean
    isDownloading?: boolean
    responsive?: boolean
  }>(),
  {
    size: 'medium',
    clickable: true,
    isDownloading: false,
    responsive: false,
  },
)

const isInLibrary = computed(() => {
  if ('files' in props.item && props.item.files) {
    return props.item.files.some((file: { status: string }) => file.status === 'available')
  } else if ('isInLibrary' in props.item) {
    return props.item.isInLibrary
  }

  return false
})

const showLibraryBadge = computed(() => !props.isDownloading && isInLibrary.value)

const mediaTypeLabel = computed(() => {
  if ('mediaType' in props.item && props.item.mediaType) {
    return props.item.mediaType === 'series' ? 'Series' : 'Movie'
  }
  // Fallback: check for series-specific fields
  if ('numberOfSeasons' in props.item || 'seasons' in props.item) {
    return 'Series'
  }
  return 'Movie'
})

const posterPath = computed(() => {
  const sizeConfig = POSTER_SIZES[props.size]
  return `https://image.tmdb.org/t/p/${sizeConfig.tmdbSize}/${props.item.posterPath}`
})

const sizeClass = computed(() => {
  if (props.responsive) return ''
  return POSTER_SIZES[props.size].class
})

const isLoading = ref(true)
const onLoad = () => {
  isLoading.value = false
}
const onError = () => {
  isLoading.value = false
}
</script>

<style scoped>
/* Outer container: handles sizing, is the clickable link, no clipping */
.poster-outer {
  display: block;
  width: 100%;
  aspect-ratio: 2 / 3;
  position: relative;
  text-decoration: none;
  color: inherit;
}

/* Inner container: scales on hover, clips image to rounded corners */
.poster-inner {
  position: absolute;
  inset: 0;
  border-radius: 8px;
  overflow: hidden;
  background-color: #111827;
  transition:
    transform 0.2s ease,
    box-shadow 0.2s ease;
}

.poster-outer.is-clickable:hover .poster-inner {
  transform: scale(1.05);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
  z-index: 10;
}

.poster {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  opacity: 0;
  transition: opacity 150ms ease;
}

.poster.is-loaded {
  opacity: 1;
}

.status-badge {
  position: absolute;
  top: 0.5rem;
  left: 0.5rem;
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.3rem 0.55rem;
  border-radius: 9999px;
  background: rgba(0, 0, 0, 0.7);
  color: #e5e7eb;
  font-weight: 600;
  font-size: 0.75rem;
  letter-spacing: 0.01em;
  border: 1px solid rgba(255, 255, 255, 0.15);
  z-index: 5;
}

.status-badge svg {
  flex-shrink: 0;
}

.library-badge svg {
  color: #22c55e;
}

.download-badge svg {
  color: #3b82f6;
}

.poster-skeleton {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

.poster--sm {
  --poster-width: 8rem;
  width: var(--poster-width);
  max-width: var(--poster-width);
}

.poster--md {
  --poster-width: 11rem;
  width: var(--poster-width);
  max-width: var(--poster-width);
}

.poster--lg {
  --poster-width: 16rem;
  width: var(--poster-width);
  max-width: var(--poster-width);
}

/* Overlay: sibling to inner, not clipped by it */
.poster-overlay {
  position: absolute;
  inset: 0;
  border-radius: 8px;
  background: linear-gradient(
    to top,
    rgba(0, 0, 0, 0.8) 0%,
    rgba(0, 0, 0, 0.4) 40%,
    transparent 100%
  );
  opacity: 0;
  transition:
    opacity 0.2s ease,
    transform 0.2s ease;
  pointer-events: none;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  padding: 0.75rem;
}

.poster-outer.is-clickable:hover .poster-overlay {
  opacity: 1;
  transform: scale(1.05);
  z-index: 20;
}

/* Info text at bottom */
.poster-info {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
}

.poster-title {
  font-weight: 600;
  font-size: 0.875rem;
  line-height: 1.2;
  color: white;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.poster-meta {
  font-size: 0.75rem;
  color: rgba(255, 255, 255, 0.7);
}
</style>
