<template>
  <div class="flex flex-col gap-6">
    <div v-if="isLoading" class="space-y-6">
      <!-- Hero skeleton -->
      <Skeleton class="h-80 w-full rounded-lg" />
      <!-- Rail skeletons -->
      <div v-for="i in 3" :key="i" class="space-y-2">
        <Skeleton class="h-8 w-64" />
        <div class="flex gap-3 overflow-x-auto pb-4">
          <Skeleton v-for="j in 5" :key="j" class="h-64 w-44 flex-shrink-0" />
        </div>
      </div>
    </div>

    <div v-else-if="isError" class="flex flex-col items-center justify-center py-12 text-center">
      <p class="text-destructive">Failed to load content</p>
      <p class="text-sm text-muted-foreground mt-2">Please try again later</p>
    </div>

    <div
      v-else-if="!data || !data.rows || data.rows.length === 0"
      class="flex flex-col items-center justify-center py-12 text-center"
    >
      <p class="text-muted-foreground">No content available</p>
    </div>

    <div v-else class="flex flex-col gap-6">
      <!-- Hero Section -->
      <FeedHero v-if="data.hero" :hero="data.hero" :full-bleed="isImmersive" />

      <!-- Feed Rows -->
      <div :class="isImmersive ? 'flex flex-col gap-6 px-6' : 'flex flex-col gap-6'">
        <FeedRow
          v-for="row in data.rows"
          :key="row.id"
          :row="row"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { getV1HomeOptions } from '@/client/@tanstack/vue-query.gen'
import { Skeleton } from '@/components/ui/skeleton'
import FeedHero from '@/components/feed/FeedHero.vue'
import FeedRow from '@/components/feed/FeedRow.vue'

const route = useRoute()
const isImmersive = computed(() => route.meta.layout === 'immersive')

const { isLoading, isError, data } = useQuery(getV1HomeOptions())
</script>
