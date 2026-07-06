<script setup lang="ts">
import type { WatchProviders as WatchProvidersType } from '@/client/types.gen'
import WatchProviders from '@/components/media/WatchProviders.vue'

export type Fact = { label: string; value: string }

// The right-hand facts column shared by the movie and series pages: a static
// facts list, an optional read-only automation summary (tracked series only —
// the kebab's Automation dialog is where it's changed), and watch providers.
// Purely presentational; each page derives the facts from its own detail payload.
defineProps<{
  facts: Fact[]
  watchProviders?: WatchProvidersType
  automation?: { backfill: string; ongoing: string }
}>()
</script>

<template>
  <aside class="flex flex-col gap-6">
    <div v-if="facts.length" class="rounded-lg border bg-card p-4">
      <dl class="divide-y divide-border/60">
        <div
          v-for="fact in facts"
          :key="fact.label"
          class="flex items-start justify-between gap-4 py-2 text-sm first:pt-0 last:pb-0"
        >
          <dt class="shrink-0 text-muted-foreground">{{ fact.label }}</dt>
          <dd class="text-right font-medium">{{ fact.value }}</dd>
        </div>
      </dl>
    </div>

    <div v-if="automation" class="rounded-lg border bg-card p-4">
      <h3 class="mb-3 text-sm font-semibold">Automation</h3>
      <dl class="divide-y divide-border/60">
        <div class="flex items-center justify-between gap-4 py-2 text-sm first:pt-0 last:pb-0">
          <dt class="text-muted-foreground">Back-catalog</dt>
          <dd class="font-medium">{{ automation.backfill }}</dd>
        </div>
        <div class="flex items-center justify-between gap-4 py-2 text-sm first:pt-0 last:pb-0">
          <dt class="text-muted-foreground">New episodes</dt>
          <dd class="font-medium">{{ automation.ongoing }}</dd>
        </div>
      </dl>
    </div>

    <WatchProviders v-if="watchProviders" :providers="watchProviders" />
  </aside>
</template>
