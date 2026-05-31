<script setup lang="ts">
import type { ResolverConsulted } from './decision-types'

defineProps<{ resolvers: ResolverConsulted[] }>()

const pct = (n: number) => Math.round((n ?? 0) * 100)
</script>

<template>
  <div v-if="resolvers.length" class="space-y-1">
    <div v-for="r in resolvers" :key="r.name" class="flex items-center justify-between text-xs">
      <span class="flex items-center gap-1.5">
        <span class="font-medium">{{ r.name }}</span>
        <span class="text-muted-foreground">tier {{ r.tier }}</span>
      </span>
      <span class="tabular-nums text-muted-foreground">
        {{ pct(r.topConfidence) }}% · {{ r.candidateCount }}
        {{ r.candidateCount === 1 ? 'candidate' : 'candidates' }}
      </span>
    </div>
  </div>
</template>
