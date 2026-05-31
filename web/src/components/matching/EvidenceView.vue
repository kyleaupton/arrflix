<script setup lang="ts">
import { computed } from 'vue'
import { ChevronRight } from 'lucide-vue-next'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'

const props = defineProps<{ evidence: unknown; truncated?: boolean }>()

// Evidence is resolver-specific and open-ended; the structured summary lives in
// ResolverSummary, so here we just offer the raw blob for power users.
const pretty = computed(() => {
  try {
    return JSON.stringify(props.evidence, null, 2)
  } catch {
    return String(props.evidence)
  }
})
const hasEvidence = computed(
  () => props.evidence != null && pretty.value !== '{}' && pretty.value !== 'null',
)
</script>

<template>
  <Collapsible v-if="hasEvidence" class="group">
    <CollapsibleTrigger
      class="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
    >
      <ChevronRight class="size-3 transition-transform group-data-[state=open]:rotate-90" />
      Raw evidence<span v-if="truncated"> (truncated)</span>
    </CollapsibleTrigger>
    <CollapsibleContent>
      <pre
        class="mt-2 max-h-64 overflow-auto rounded-md bg-muted p-2 text-[11px] leading-relaxed"
        >{{ pretty }}</pre
      >
    </CollapsibleContent>
  </Collapsible>
</template>
