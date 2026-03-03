<template>
  <div class="flex flex-col gap-6">
    <Transition name="fade" mode="out-in">
      <div v-if="isLoading" key="loading" class="space-y-6">
        <!-- Hero skeleton -->
        <div class="relative h-[56vh] min-h-[400px] w-full rounded-lg overflow-hidden">
          <Skeleton class="absolute inset-0 !rounded-none" />
          <div class="absolute inset-x-0 bottom-0 h-1/3 bg-gradient-to-t from-background to-transparent" />
        </div>
        <!-- Rail skeletons -->
        <div v-for="i in 3" :key="i" class="space-y-2" :class="isImmersive ? 'px-6' : ''">
          <Skeleton class="h-8 w-64" />
          <div class="flex gap-3 overflow-x-auto pb-4">
            <Skeleton v-for="j in 5" :key="j" class="aspect-[2/3] w-44 flex-shrink-0" />
          </div>
        </div>
      </div>

      <div v-else-if="isError" key="error" class="flex flex-col items-center justify-center py-12 text-center">
        <p class="text-destructive">Failed to load content</p>
        <p class="text-sm text-muted-foreground mt-2">Please try again later</p>
      </div>

      <div
        v-else-if="!data || !data.rows || data.rows.length === 0"
        key="empty"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <p class="text-muted-foreground">No content available</p>
      </div>

      <div v-else key="content" class="flex flex-col gap-6">
        <!-- Hero Section -->
        <FeedHero
          v-if="data.hero"
          :hero="data.hero"
          :full-bleed="isImmersive"
          class="animate-stagger-in"
          style="--stagger-delay: 0ms"
        />

        <!-- Feed Rows -->
        <div :class="isImmersive ? 'flex flex-col gap-6 px-6' : 'flex flex-col gap-6'">
          <FeedRow
            v-for="(row, index) in data.rows"
            :key="row.id"
            :row="row"
            class="animate-stagger-in"
            :style="{ '--stagger-delay': (index + 1) * 80 + 'ms' }"
          />
        </div>
      </div>
    </Transition>
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
