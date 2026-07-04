<template>
  <div class="flex flex-col gap-6">
    <!-- Hero: mirrors MediaHero in its full-bleed (immersive) form -->
    <section
      class="relative flex min-h-[24rem] flex-col justify-end overflow-hidden pt-14 md:min-h-[28rem] lg:min-h-[32rem]"
    >
      <Skeleton class="absolute inset-0 !rounded-none" />
      <div
        class="absolute inset-x-0 bottom-0 h-2/3 bg-gradient-to-t from-background via-background/70 to-transparent"
      />

      <div class="relative px-4 py-6 sm:px-6 sm:py-8 md:px-8 md:py-10">
        <div class="flex items-end gap-4 md:gap-6">
          <Skeleton class="aspect-[2/3] w-40 shrink-0 rounded-xl md:w-56" />
          <div class="min-w-0 flex-1 space-y-3">
            <Skeleton class="h-10 w-2/3 max-w-md md:h-12" />
            <Skeleton class="h-4 w-40" />
            <div class="flex gap-2 pt-1">
              <Skeleton class="h-6 w-16 rounded-full" />
              <Skeleton class="h-6 w-16 rounded-full" />
              <Skeleton class="h-6 w-16 rounded-full" />
            </div>
            <Skeleton class="h-10 w-40 rounded-md" />
            <div class="max-w-3xl space-y-2 pt-2">
              <Skeleton class="h-4 w-full" />
              <Skeleton class="h-4 w-11/12" />
              <Skeleton class="h-4 w-4/5" />
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- A couple of generic content rows so the page below the hero isn't a void -->
    <div class="space-y-10 px-6">
      <div v-for="i in 2" :key="i" class="space-y-4">
        <Skeleton class="h-7 w-48" />
        <div class="flex gap-3 overflow-hidden">
          <Skeleton
            v-for="j in 6"
            :key="j"
            class="aspect-[2/3] w-36 shrink-0 rounded-lg md:w-44"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton'
</script>
