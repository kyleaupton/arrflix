<template>
  <button
    @click="openVideo"
    class="video-card flex flex-col gap-2 w-64 sm:w-72 group"
  >
    <div class="video-thumbnail-wrapper relative w-full aspect-video rounded-lg overflow-hidden bg-muted">
      <img
        v-if="thumbnailUrl"
        :src="thumbnailUrl"
        :alt="video.name"
        class="w-full h-full object-cover"
        loading="lazy"
        @error="onImageError"
      />
      <div
        class="absolute inset-0 flex items-center justify-center transition-colors"
        :class="thumbnailUrl ? 'bg-black/40 group-hover:bg-black/60' : 'bg-black/60'"
      >
        <Play class="size-12 text-white" />
      </div>
      <div v-if="video.isOfficialTrailer" class="absolute top-2 left-2 z-10">
        <Badge variant="default" class="text-xs">Official</Badge>
      </div>
      <div class="absolute bottom-2 right-2 z-10">
        <Badge variant="secondary" class="text-xs">{{ video.type }}</Badge>
      </div>
    </div>
    <div class="text-left w-full">
      <p class="font-medium text-sm line-clamp-2 group-hover:text-primary transition-colors">
        {{ video.name }}
      </p>
      <div class="flex items-center gap-2 mt-1">
        <span class="text-xs text-muted-foreground">{{ video.site }}</span>
        <span v-if="video.size" class="text-xs text-muted-foreground">•</span>
        <span v-if="video.size" class="text-xs text-muted-foreground">{{ video.size }}p</span>
      </div>
    </div>
  </button>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { Play } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { type Video } from '@/client/types.gen'

const props = defineProps<{
  video: Video
}>()

const imageError = ref(false)

const thumbnailUrl = computed(() => {
  if (imageError.value) return undefined
  
  switch (props.video.site) {
    case 'YouTube':
      return `https://img.youtube.com/vi/${props.video.key}/mqdefault.jpg`
    case 'Vimeo':
      // Vimeo thumbnails require API call, so we'll use a placeholder for now
      return undefined
    default:
      return undefined
  }
})

const onImageError = () => {
  imageError.value = true
}

const getVideoUrl = (video: Video): string => {
  switch (video.site) {
    case 'YouTube':
      return `https://www.youtube.com/watch?v=${video.key}`
    case 'Vimeo':
      return `https://vimeo.com/${video.key}`
    default:
      console.warn(`Unknown video site: ${video.site}`)
      return '#'
  }
}

const openVideo = () => {
  const url = getVideoUrl(props.video)
  if (url !== '#') {
    window.open(url, '_blank')
  }
}
</script>

<style scoped>
.video-card {
  text-align: left;
  transition: transform 0.2s;
}

.video-card:hover {
  transform: translateY(-2px);
}
</style>

