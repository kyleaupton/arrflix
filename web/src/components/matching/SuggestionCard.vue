<script setup lang="ts">
import { computed } from 'vue'
import { Check, Film, Tv } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import type { SuggestedMatch } from '@/client/types.gen'

const props = defineProps<{
  candidate: SuggestedMatch
  isCurrent: boolean
  disabled: boolean
}>()

defineEmits<{ match: [] }>()

const pct = computed(() => Math.round((props.candidate.confidence ?? 0) * 100))
const posterUrl = computed(() =>
  props.candidate.posterPath ? `https://image.tmdb.org/t/p/w154${props.candidate.posterPath}` : '',
)
const fallbackIcon = computed(() => (props.candidate.type === 'series' ? Tv : Film))
</script>

<template>
  <div
    :class="[
      'flex gap-3 rounded-lg border p-3',
      isCurrent ? 'border-primary/50 bg-primary/5' : 'border-border',
    ]"
  >
    <!-- Poster -->
    <div class="h-24 w-16 shrink-0 overflow-hidden rounded-md bg-muted">
      <img
        v-if="posterUrl"
        :src="posterUrl"
        :alt="candidate.title"
        class="h-full w-full object-cover"
      />
      <div v-else class="flex h-full w-full items-center justify-center">
        <component :is="fallbackIcon" class="size-5 text-muted-foreground" />
      </div>
    </div>

    <!-- Details -->
    <div class="flex min-w-0 flex-1 flex-col">
      <div class="flex flex-wrap items-center gap-2">
        <span class="truncate text-sm font-medium">{{ candidate.title || 'Untitled' }}</span>
        <span v-if="candidate.year" class="text-xs text-muted-foreground">{{
          candidate.year
        }}</span>
        <Badge v-if="candidate.type" variant="outline" class="text-[10px]">{{
          candidate.type
        }}</Badge>
        <Badge v-if="isCurrent" variant="secondary" class="text-[10px]">Current</Badge>
      </div>

      <div class="mt-1.5 flex items-center gap-2">
        <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
          <div class="h-full rounded-full bg-primary" :style="{ width: pct + '%' }" />
        </div>
        <span class="text-xs font-semibold tabular-nums">{{ pct }}%</span>
      </div>

      <div class="mt-1.5 flex flex-wrap items-center gap-1">
        <Badge
          v-for="r in candidate.contributingResolvers ?? []"
          :key="r"
          variant="outline"
          class="text-[10px] font-normal"
          >{{ r }}</Badge
        >
        <span class="text-[10px] text-muted-foreground">
          {{ candidate.externalRef.source }}:{{ candidate.externalRef.externalId }}
        </span>
      </div>

      <Button
        :variant="isCurrent ? 'default' : 'outline'"
        size="sm"
        class="mt-auto self-start"
        :disabled="disabled"
        @click="$emit('match')"
      >
        <Check v-if="isCurrent" class="mr-1.5 size-3.5" />
        {{ isCurrent ? 'Confirm match' : 'Match this' }}
      </Button>
    </div>
  </div>
</template>
