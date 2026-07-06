<template>
  <div v-if="dedupedProviders.length" class="rounded-lg border bg-card p-4">
    <div class="mb-3 flex items-baseline justify-between gap-2">
      <h3 class="text-sm font-semibold">Where to Watch</h3>
      <p class="text-xs text-muted-foreground/60">
        via
        <a
          v-if="providers?.link"
          :href="providers.link"
          target="_blank"
          rel="noopener noreferrer"
          class="underline transition-colors hover:text-muted-foreground"
        >
          JustWatch
        </a>
        <span v-else>JustWatch</span>
      </p>
    </div>

    <div class="flex flex-wrap gap-3">
      <a
        v-for="p in visibleProviders"
        :key="p.providerId"
        :href="providers?.link"
        target="_blank"
        rel="noopener noreferrer"
        class="group flex w-16 flex-col items-center gap-1"
      >
        <img
          :src="`https://image.tmdb.org/t/p/w92${p.logoPath}`"
          :alt="p.providerName"
          class="size-12 rounded-xl shadow-sm transition-transform group-hover:scale-105"
          loading="lazy"
        />
        <div class="w-full text-center">
          <p class="truncate text-[10px] font-medium">{{ p.providerName }}</p>
          <p class="text-[10px] text-muted-foreground">{{ p.tier }}</p>
        </div>
      </a>
    </div>

    <button
      v-if="dedupedProviders.length > CAP"
      type="button"
      class="mt-3 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
      @click="expanded = !expanded"
    >
      {{ expanded ? 'Show less' : `+ ${dedupedProviders.length - CAP} more` }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { WatchProviders, WatchProvider } from '@/client/types.gen'

type DedupedProvider = WatchProvider & { tier: string }

const props = defineProps<{
  providers?: WatchProviders
}>()

// Cap the list so a provider-heavy title doesn't blow out the rail height; the
// rest expand on demand.
const CAP = 6
const expanded = ref(false)

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

const visibleProviders = computed(() =>
  expanded.value ? dedupedProviders.value : dedupedProviders.value.slice(0, CAP),
)
</script>
