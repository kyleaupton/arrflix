<script setup lang="ts">
import { computed } from 'vue'
import { Film, Tv, FileQuestion } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { formatBytes } from '@/lib/format'
import { outcomeLabel, outcomeVariant } from './outcome'
import type { InboxItem } from '@/client/types.gen'

const props = defineProps<{
  item: InboxItem
  active: boolean
}>()

defineEmits<{ select: [] }>()

// The backend COALESCEs an identity title onto items that have one
// (confident_review / partial_series); otherwise this is the parsed guess.
const primary = computed(() => props.item.title || basename(props.item.path) || props.item.path)
const confidencePct = computed(() => Math.round((props.item.confidence ?? 0) * 100))

function basename(path: string): string {
  return path.split('/').pop() ?? path
}

const typeIcon = computed(() => {
  if (props.item.type === 'movie') return Film
  if (props.item.type === 'series') return Tv
  return FileQuestion
})
</script>

<template>
  <button
    type="button"
    :class="[
      'w-full text-left px-3 py-2.5 rounded-md border transition-colors',
      active
        ? 'border-primary/50 bg-primary/10'
        : 'border-transparent hover:bg-muted/60 hover:border-border',
    ]"
    @click="$emit('select')"
  >
    <div class="flex items-start gap-2">
      <component :is="typeIcon" class="size-4 mt-0.5 shrink-0 text-muted-foreground" />
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <span class="truncate text-sm font-medium">{{ primary }}</span>
          <span v-if="item.year" class="text-xs text-muted-foreground shrink-0">{{
            item.year
          }}</span>
        </div>
        <p class="truncate text-xs text-muted-foreground" :title="item.path">{{ item.path }}</p>
        <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
          <Badge :variant="outcomeVariant(item.outcome)" class="text-[10px]">
            {{ outcomeLabel(item.outcome) }}
          </Badge>
          <Badge v-if="item.partialSeries" variant="outline" class="text-[10px]">Partial</Badge>
          <span class="text-[11px] text-muted-foreground">{{ confidencePct }}%</span>
          <span v-if="item.fileSize" class="text-[11px] text-muted-foreground">
            · {{ formatBytes(item.fileSize) }}
          </span>
        </div>
      </div>
    </div>
  </button>
</template>
