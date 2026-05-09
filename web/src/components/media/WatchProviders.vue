<template>
  <Rail v-if="dedupedProviders.length" title="Where to Watch">
    <template #title>
      <div class="flex items-baseline gap-2">
        <h2 class="text-xl font-semibold">Where to Watch</h2>
        <p class="text-xs text-muted-foreground/60">
          via
          <a
            v-if="providers?.link"
            :href="providers.link"
            target="_blank"
            rel="noopener noreferrer"
            class="underline hover:text-muted-foreground transition-colors"
          >
            JustWatch
          </a>
          <span v-else>JustWatch</span>
        </p>
      </div>
    </template>

    <a
      v-for="p in dedupedProviders"
      :key="p.providerId"
      :href="providers?.link"
      target="_blank"
      rel="noopener noreferrer"
      class="flex-shrink-0 flex flex-col items-center gap-2 w-20 group"
    >
      <img
        :src="`https://image.tmdb.org/t/p/w92${p.logoPath}`"
        :alt="p.providerName"
        class="size-14 rounded-xl shadow-sm group-hover:scale-105 transition-transform"
        loading="lazy"
      />
      <div class="text-center w-full">
        <p class="text-xs font-medium truncate">{{ p.providerName }}</p>
        <p class="text-[10px] text-muted-foreground">{{ p.tier }}</p>
      </div>
    </a>
  </Rail>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { WatchProviders, WatchProvider } from '@/client/types.gen'
import Rail from '@/components/rails/Rail.vue'

type DedupedProvider = WatchProvider & { tier: string }

const props = defineProps<{
  providers?: WatchProviders
}>()

// Priority: Stream > Rent > Buy. Each provider shown once with best tier.
const dedupedProviders = computed<DedupedProvider[]>(() => {
  if (!props.providers) return []

  const map = new Map<number, DedupedProvider>()

  const tiers: { list: WatchProvider[] | null | undefined; label: string; rank: number }[] = [
    { list: props.providers.flatrate, label: 'Stream', rank: 0 },
    { list: props.providers.rent, label: 'Rent', rank: 1 },
    { list: props.providers.buy, label: 'Buy', rank: 2 },
  ]

  for (const { list, label, rank } of tiers) {
    for (const provider of list ?? []) {
      const existing = map.get(provider.providerId)
      if (!existing || rank < (tiers.find((t) => t.label === existing.tier)?.rank ?? 3)) {
        map.set(provider.providerId, { ...provider, tier: label })
      }
    }
  }

  return [...map.values()].sort((a, b) => a.displayPriority - b.displayPriority)
})
</script>
