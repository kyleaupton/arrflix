<script setup lang="ts">
import { computed } from 'vue'
import { Check } from 'lucide-vue-next'
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
</script>

<template>
  <div
    :class="[
      'rounded-lg border p-3',
      isCurrent ? 'border-primary/50 bg-primary/5' : 'border-border',
    ]"
  >
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
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
        <div class="mt-1 flex flex-wrap items-center gap-1">
          <Badge
            v-for="r in candidate.contributingResolvers ?? []"
            :key="r"
            variant="outline"
            class="text-[10px] font-normal"
            >{{ r }}</Badge
          >
        </div>
      </div>
      <div class="shrink-0 text-right">
        <div class="text-sm font-semibold tabular-nums">{{ pct }}%</div>
        <span class="text-[10px] text-muted-foreground">
          {{ candidate.externalRef.source }}:{{ candidate.externalRef.externalId }}
        </span>
      </div>
    </div>

    <div class="mt-2 h-1 w-full overflow-hidden rounded-full bg-muted">
      <div class="h-full rounded-full bg-primary" :style="{ width: pct + '%' }" />
    </div>

    <Button
      :variant="isCurrent ? 'default' : 'outline'"
      size="sm"
      class="mt-3 w-full"
      :disabled="disabled"
      @click="$emit('match')"
    >
      <Check v-if="isCurrent" class="mr-1.5 size-3.5" />
      {{ isCurrent ? 'Confirm match' : 'Match this' }}
    </Button>
  </div>
</template>
