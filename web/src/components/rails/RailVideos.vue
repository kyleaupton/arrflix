<template>
  <Rail :title="title">
    <div v-for="video in videos" :key="video.tmdbId" class="flex-shrink-0">
      <VideoCard :video="video" />
    </div>
  </Rail>
</template>

<script setup lang="ts">
import { type Video } from '@/client/types.gen'
import Rail from './Rail.vue'
import VideoCard from '@/components/videos/VideoCard.vue'

withDefaults(
  defineProps<{
    title?: string
    videos: Array<Video>
  }>(),
  {
    title: 'Videos',
  },
)
</script>
