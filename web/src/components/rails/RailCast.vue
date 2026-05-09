<template>
  <Rail :title="title">
    <div v-for="member in cast" :key="member.tmdbId" class="flex-shrink-0">
      <CastCard :member="member" />
    </div>
  </Rail>
</template>

<script setup lang="ts">
import { type CastMember } from '@/client/types.gen'
import Rail from './Rail.vue'
import CastCard from '@/components/cast/CastCard.vue'

withDefaults(
  defineProps<{
    title?: string
    cast: Array<CastMember>
  }>(),
  {
    title: 'Cast',
  }
)
</script>

