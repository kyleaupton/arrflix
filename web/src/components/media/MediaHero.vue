<template>
  <section
    class="media-hero relative min-h-[24rem] md:min-h-[28rem] lg:min-h-[32rem] flex flex-col justify-end"
    :class="
      fullBleed ? 'pt-[calc(env(safe-area-inset-top)_+_3.5rem)]' : '-mx-4 -my-4 overflow-hidden'
    "
  >
    <div class="backdrop" :class="{ 'has-image': !!backdropUrl }">
      <img v-if="backdropUrl" :src="backdropUrl" alt="" aria-hidden="true" />
      <div class="backdrop-overlay" />
    </div>

    <div class="content relative px-4 py-6 sm:px-6 sm:py-8 md:px-8 md:py-10">
      <div class="flex flex-col sm:flex-row gap-4 md:gap-6 sm:items-end">
        <div v-if="posterUrl || $slots.poster" class="poster shadow-xl w-28 sm:w-44 md:w-56">
          <slot name="poster">
            <img v-if="posterUrl" :src="posterUrl" :alt="title" loading="eager" decoding="async" />
          </slot>
        </div>
        <div class="flex-1 min-w-0">
          <h1 class="title text-3xl sm:text-4xl md:text-5xl font-bold">
            {{ title }}
          </h1>
          <p v-if="tagline" class="text-sm italic opacity-70 mt-1">"{{ tagline }}"</p>
          <p v-if="subtitle" class="subtitle text-sm md:text-base opacity-80 mt-1">
            {{ subtitle }}
          </p>
          <p v-if="credits" class="text-sm md:text-base opacity-70 mt-0.5">{{ credits }}</p>

          <div v-if="$slots.ratings" class="mt-2 flex flex-wrap items-center gap-2">
            <slot name="ratings" />
          </div>

          <div v-if="chips && chips.length" class="chips mt-3 flex flex-wrap gap-2">
            <Badge v-for="(chip, i) in chips" :key="i">{{ chip }}</Badge>
          </div>

          <div v-if="$slots.actions || trailerUrl" class="mt-4 flex flex-wrap items-center gap-3">
            <slot name="actions" />
            <Button v-if="trailerUrl" variant="outline" @click="openTrailerModal">
              <ExternalLink class="size-4" />
              Watch Trailer
            </Button>
          </div>

          <p
            v-if="overview"
            class="overview mt-4 max-w-3xl text-sm md:text-base leading-relaxed opacity-90"
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
  inset: 0 0 -1px 0;
  pointer-events: none;
  overflow: hidden;
}

.backdrop img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center top;
  filter: blur(2px);
  transform: scale(1.01);
}

.backdrop-overlay {
  position: absolute;
  inset: 0;
  /* Bottom fade — strong, ensures text readability */
  background:
    linear-gradient(to top, oklch(0.145 0 0) 0%, oklch(0.145 0 0 / 0.9) 18%, transparent 65%),
    /* Lateral gradient — subtle left-side darkening for text legibility */
      linear-gradient(to right, rgba(0, 0, 0, 0.45) 0%, transparent 70%),
    /* Top vignette — subtle top edge for navbar blending */
      linear-gradient(to bottom, rgba(0, 0, 0, 0.3) 0%, transparent 20%);
}

.poster {
  flex: 0 0 auto;
  display: flex;
  align-items: flex-start;
}

/* Wrapper owns the responsive width (w-28 sm:w-44 md:w-56); the poster — whether
   the slotted <Poster responsive> or the fallback <img> — fills it. */
.poster img {
  width: 100%;
  aspect-ratio: 2 / 3;
  border-radius: 0.75rem;
  object-fit: cover;
}

.title {
  color: var(--p-content-color);
  text-shadow: 0 2px 8px rgba(0, 0, 0, 0.5);
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
