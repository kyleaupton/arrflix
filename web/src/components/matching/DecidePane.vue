<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { MousePointerClick } from 'lucide-vue-next'
import { unmatchedFilesGetOptions } from '@/client/@tanstack/vue-query.gen'
import { problemMessage } from '@/lib/api'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { outcomeLabel, outcomeVariant } from './outcome'

const props = defineProps<{ fileId: string | undefined }>()

// Decide-pane data (suggestions, evidence, match actions) lands in pass two via
// filesMatchDecision. For now the pane fetches the inbox item so the two-pane
// wiring is verifiable end to end.
const query = useQuery(
  computed(() => ({
    ...unmatchedFilesGetOptions({ path: { id: props.fileId! } }),
    enabled: !!props.fileId,
  })),
)

const item = computed(() => query.data.value)
const errorMessage = computed(() => problemMessage(query.error.value, 'Failed to load file'))
</script>

<template>
  <Empty v-if="!fileId" class="h-full">
    <EmptyHeader>
      <EmptyMedia variant="icon"><MousePointerClick /></EmptyMedia>
      <EmptyTitle>Select a file</EmptyTitle>
      <EmptyDescription>Pick an item from the inbox to review and match it.</EmptyDescription>
    </EmptyHeader>
  </Empty>

  <div v-else-if="query.isLoading.value" class="space-y-3 p-1">
    <Skeleton class="h-6 w-2/3" />
    <Skeleton class="h-4 w-full" />
    <Skeleton class="h-32 w-full" />
  </div>

  <div v-else-if="query.isError.value" class="p-4 text-sm text-destructive">{{ errorMessage }}</div>

  <div v-else-if="item" class="space-y-4 p-1">
    <div class="space-y-1">
      <div class="flex items-center gap-2">
        <h2 class="text-lg font-semibold">{{ item.title || 'Unidentified file' }}</h2>
        <span v-if="item.year" class="text-sm text-muted-foreground">{{ item.year }}</span>
      </div>
      <p class="break-all text-xs text-muted-foreground">{{ item.path }}</p>
      <Badge :variant="outcomeVariant(item.outcome)">{{ outcomeLabel(item.outcome) }}</Badge>
    </div>

    <div class="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
      Suggestions, evidence, and match actions arrive next.
    </div>
  </div>
</template>
