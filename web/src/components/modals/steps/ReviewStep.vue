<script setup lang="ts">
import { computed } from 'vue'
import { type IndexerOutput, type IndexerInput } from '@/client/types.gen'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

const props = defineProps<{
  selectedIndexer: IndexerOutput | null
  saveData: IndexerInput | undefined
}>()

const hasValue = (value: unknown): boolean => {
  if (value === null || value === undefined) return false
  if (typeof value === 'string' && value.trim() === '') return false
  if (Array.isArray(value) && value.length === 0) return false
  return true
}

const configuredFields = computed(() => {
  if (!props.saveData?.fields || !props.selectedIndexer?.fields) {
    return []
  }

  return props.saveData.fields
    .filter(
      // @ts-expect-error field.type && hidden are not typed correctly
      (field) => hasValue(field.value) && field.type !== 'info' && field.hidden !== 'hidden',
    )
    .map((field) => {
      const fieldDefinition = props.selectedIndexer?.fields?.find((def) => def.name === field.name)
      return {
        name: field.name,
        label: fieldDefinition?.label || field.name,
        value: field.value,
      }
    })
})

const formatValue = (value: unknown): string => {
  if (Array.isArray(value)) {
    return value.join(', ')
  }
  if (typeof value === 'boolean') {
    return value ? 'Yes' : 'No'
  }
  return String(value)
}
</script>

<template>
  <div class="review-step h-full overflow-y-auto">
    <div class="space-y-1 mb-6">
      <h3 class="text-lg font-semibold">Review & Create</h3>
      <p class="text-sm text-muted-foreground">
        Review your indexer configuration before creating.
      </p>
    </div>

    <Card>
      <CardContent>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div class="space-y-4">
            <CardHeader class="px-0 pt-0">
              <CardTitle class="text-base">Basic Information</CardTitle>
            </CardHeader>
            <dl class="space-y-3">
              <div class="space-y-1">
                <dt class="text-sm font-medium text-muted-foreground">Name</dt>
                <dd class="text-sm">{{ selectedIndexer?.name || '—' }}</dd>
              </div>
              <div class="space-y-1">
                <dt class="text-sm font-medium text-muted-foreground">Description</dt>
                <dd class="text-sm">{{ selectedIndexer?.description || '—' }}</dd>
              </div>
              <div class="space-y-1">
                <dt class="text-sm font-medium text-muted-foreground">Language</dt>
                <dd class="text-sm">{{ selectedIndexer?.language || '—' }}</dd>
              </div>
            </dl>
          </div>

          <div class="space-y-4">
            <CardHeader class="px-0 pt-0">
              <CardTitle class="text-base">Configuration</CardTitle>
            </CardHeader>
            <dl v-if="configuredFields.length > 0" class="space-y-3">
              <div v-for="field in configuredFields" :key="field.name" class="space-y-1">
                <dt class="text-sm font-medium text-muted-foreground">
                  {{ field.label }}
                </dt>
                <dd class="text-sm">{{ formatValue(field.value) }}</dd>
              </div>
            </dl>
            <p v-else class="text-sm text-muted-foreground">No additional configuration set.</p>
          </div>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
