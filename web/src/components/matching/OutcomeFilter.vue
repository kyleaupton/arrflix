<script setup lang="ts">
import { computed } from 'vue'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { INBOX_OUTCOMES, outcomeLabel, type InboxOutcome } from './outcome'

const props = defineProps<{
  modelValue: InboxOutcome | undefined
  counts: Record<string, number>
  total: number
}>()

const emit = defineEmits<{ 'update:modelValue': [InboxOutcome | undefined] }>()

const ALL = '__all__'

const tabValue = computed({
  get: () => props.modelValue ?? ALL,
  set: (v: string) => emit('update:modelValue', v === ALL ? undefined : (v as InboxOutcome)),
})

// Show a band's tab only when it has items — but always keep the active filter
// visible even if its count momentarily drops to zero after a decision.
const visibleOutcomes = computed(() =>
  INBOX_OUTCOMES.filter((o) => (props.counts[o] ?? 0) > 0 || props.modelValue === o),
)
</script>

<template>
  <Tabs v-model="tabValue">
    <TabsList class="flex-wrap h-auto">
      <TabsTrigger :value="ALL" class="gap-1.5">
        All
        <Badge variant="secondary" class="text-[10px]">{{ total }}</Badge>
      </TabsTrigger>
      <TabsTrigger v-for="o in visibleOutcomes" :key="o" :value="o" class="gap-1.5">
        {{ outcomeLabel(o) }}
        <Badge variant="secondary" class="text-[10px]">{{ counts[o] ?? 0 }}</Badge>
      </TabsTrigger>
    </TabsList>
  </Tabs>
</template>
