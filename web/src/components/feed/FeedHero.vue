<template>
  <section
    class="feed-hero relative overflow-hidden"
    :class="fullBleed ? 'h-[56vh] min-h-[400px] pt-14' : 'h-80 -mx-4 -mt-4'"
  >
    <img
      :src="`https://image.tmdb.org/t/p/original${hero.backdropPath}`"
      :alt="hero.title"
      class="absolute inset-0 w-full h-full object-cover"
    />
    <div class="absolute inset-0 bg-gradient-to-t from-black/80 via-black/40 to-transparent" />
    <div class="absolute bottom-0 left-0 right-0 p-6">
      <h1 class="text-3xl font-bold text-white mb-2">{{ hero.title }}</h1>
      <p class="text-white/80 line-clamp-2 max-w-2xl">{{ hero.overview }}</p>
      <div class="mt-4 flex gap-3">
        <Button @click="navigateToDetail">View Details</Button>
        <Button v-if="hero.trailerUrl" variant="outline" @click="playTrailer">
          Watch Trailer
        </Button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import type { ModelHeroItem } from '@/client/types.gen'

const props = defineProps<{ hero: ModelHeroItem; fullBleed?: boolean }>()
const router = useRouter()

const navigateToDetail = () => {
  const path = props.hero.mediaType === 'movie'
    ? `/movie/${props.hero.tmdbId}`
    : `/series/${props.hero.tmdbId}`
  router.push(path)
}

const playTrailer = () => {
  if (props.hero.trailerUrl) {
    window.open(props.hero.trailerUrl, '_blank')
  }
}
</script>
