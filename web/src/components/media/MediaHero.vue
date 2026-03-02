<template>
  <section
    class="media-hero relative"
    :class="fullBleed ? 'pt-14' : '-mx-4 -my-4 overflow-hidden'"
  >
    <div class="backdrop" :class="{ 'has-image': !!backdropUrl }">
      <img v-if="backdropUrl" :src="backdropUrl" alt="" aria-hidden="true" />
      <div class="backdrop-overlay" />
    </div>

    <div class="content relative px-4 py-6 sm:px-6 sm:py-8 md:px-8 md:py-10">
      <div class="flex gap-4 md:gap-6 items-start">
        <div v-if="posterUrl || $slots.poster" class="poster shadow-lg">
          <slot name="poster">
            <img v-if="posterUrl" :src="posterUrl" :alt="title" loading="eager" decoding="async" />
          </slot>
        </div>
        <div class="flex-1 min-w-0">
          <div class="flex items-start justify-between gap-3">
            <h1 class="title text-2xl sm:text-3xl md:text-4xl font-semibold truncate">
              {{ title }}
            </h1>
            <div class="actions shrink-0">
              <slot name="actions" />
            </div>
          </div>
          <p v-if="tagline" class="text-sm italic opacity-70 mt-1">"{{ tagline }}"</p>
          <p v-if="subtitle" class="subtitle text-sm opacity-80 mt-1">{{ subtitle }}</p>
          <p v-if="credits" class="text-sm opacity-70 mt-0.5">{{ credits }}</p>

          <div v-if="chips && chips.length" class="chips mt-3 flex flex-wrap gap-2">
            <Badge v-for="(chip, i) in chips" :key="i">{{ chip }}</Badge>
          </div>

          <div v-if="trailerUrl" class="trailer mt-4">
            <Button @click="openTrailerModal">
              <ExternalLink class="size-4" />
              Watch Trailer
            </Button>
          </div>

          <p
            v-if="overview"
            class="overview mt-4 max-w-prose text-sm md:text-base leading-relaxed opacity-90"
          >
            {{ overview }}
          </p>

          <slot />
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ExternalLink } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'

const props = defineProps<{
  title: string
  subtitle?: string
  tagline?: string
  credits?: string
  overview?: string
  posterUrl?: string
  backdropUrl?: string
  chips?: string[]
  trailerUrl?: string
  fullBleed?: boolean
}>()

const openTrailerModal = () => {
  window.open(props.trailerUrl, '_blank')
}
</script>

<style scoped>
.backdrop {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: -6rem;
  pointer-events: none;
  mask-image: linear-gradient(to bottom, black 30%, transparent 90%);
  -webkit-mask-image: linear-gradient(to bottom, black 30%, transparent 90%);
}

.backdrop img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center top;
  filter: blur(6px);
  transform: scale(1.03);
}

.backdrop-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
}

.poster {
  flex: 0 0 auto;
  display: flex;
  align-items: flex-start;
}

.poster img {
  width: 8rem; /* 128px */
  aspect-ratio: 2 / 3;
  border-radius: 12px;
  object-fit: cover;
}

@media (min-width: 768px) {
  .poster img {
    width: 10rem;
  }
}

.title {
  color: var(--p-content-color);
}
.subtitle {
  color: var(--p-content-color);
}
.overview {
  color: var(--p-content-color);
}

.chip {
  display: inline-block;
  padding: 2px 8px;
  font-size: 12px;
  border-radius: 9999px;
  background: rgba(0, 0, 0, 0.35);
  color: var(--p-content-color);
  border: 1px solid rgba(255, 255, 255, 0.08);
}
</style>
