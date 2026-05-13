<template>
  <div class="space-y-3">
    <h2 class="text-xl font-semibold">Trailer</h2>
    <button
      @click="openVideo"
      class="featured-trailer relative w-full max-w-3xl aspect-video rounded-xl overflow-hidden bg-muted group"
    >
      <img
        v-if="thumbnailUrl"
        :src="thumbnailUrl"
        :alt="video.name"
        class="w-full h-full object-cover"
        loading="lazy"
        @error="onImageError"
      />
      <div
        class="absolute inset-0 flex items-center justify-center bg-black/40 group-hover:bg-black/60 transition-colors"
      >
        <div class="bg-white/20 backdrop-blur-sm rounded-full p-4">
          <Play class="size-10 text-white" />
        </div>
      </div>
      <div
        class="absolute bottom-0 inset-x-0 bg-gradient-to-t from-black/80 to-transparent p-4 pt-10"
      >
        <div class="flex items-center gap-2">
          <p class="text-white text-sm font-medium truncate">{{ video.name }}</p>
          <Badge variant="default" class="text-xs shrink-0">Official Trailer</Badge>
          <span v-if="video.size" class="text-xs text-white/70 shrink-0">{{ video.size }}p</span>
        </div>
      </div>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { Play } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import type { Video } from '@/client/types.gen'

const props = defineProps<{
  video: Video
}>()

const imageError = ref(false)

const thumbnailUrl = computed(() => {
  if (props.video.site !== 'YouTube') return undefined
  if (imageError.value) return `https://img.youtube.com/vi/${props.video.key}/hqdefault.jpg`
  return `https://img.youtube.com/vi/${props.video.key}/maxresdefault.jpg`
})

const onImageError = () => {
  imageError.value = true
}

const openVideo = () => {
  let url: string | undefined
  switch (props.video.site) {
    case 'YouTube':
      url = `https://www.youtube.com/watch?v=${props.video.key}`
      break
    case 'Vimeo':
      url = `https://vimeo.com/${props.video.key}`
      break
    default:
      console.warn(`Unknown video site: ${props.video.site}`)
      return
  }
  window.open(url, '_blank')
}
</script>

<style scoped>
.featured-trailer {
  transition: transform 0.2s;
}
.featured-trailer:hover {
  transform: translateY(-2px);
}
</style>
