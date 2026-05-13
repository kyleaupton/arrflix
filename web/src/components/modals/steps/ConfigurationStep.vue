<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { type IndexerOutput, type IndexerInput, type FieldOutput } from '@/client/types.gen'
import { cloneDeep } from '@/utils'
import ConfigurationStepField from './ConfigurationStepField.vue'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'

const model = defineModel<IndexerInput | undefined>(undefined)

const props = defineProps<{
  selectedIndexer: IndexerOutput
}>()

onMounted(() => {
  const copy = cloneDeep(props.selectedIndexer)
  model.value = {
    enable: copy.enable,
    redirect: copy.redirect,
    priority: copy.priority,
    appProfileId: 1,
    configContract: copy.configContract,
    implementation: copy.implementation,
    name: copy.name,
    protocol: copy.protocol,
    // tags: copy.tags,
    fields: (copy.fields ?? []).map((f) => ({ name: f.name, value: f.value })),
  }
})

const handleValueChange = (fieldName: string, value: unknown) => {
  if (model.value && model.value.fields) {
    const index = model.value.fields.findIndex((field) => field.name === fieldName)
    if (index !== -1) {
      model.value.fields[index]!.value = value
    }
  }
}

// Create a computed that merges field definitions with their current values
const getFieldWithValue = (fieldDef: FieldOutput) => {
  const modelField = model.value?.fields?.find((f) => f.name === fieldDef.name)
  return {
    ...fieldDef,
    value: modelField?.value ?? fieldDef.value,
  }
}

const regularFields = computed(() => {
  return (props.selectedIndexer.fields ?? [])
    .filter((field) => !field.advanced)
    .map((field) => getFieldWithValue(field))
})

const advancedFields = computed(() => {
  return (props.selectedIndexer.fields ?? [])
    .filter((field) => field.advanced)
    .map((field) => getFieldWithValue(field))
})
</script>

<template>
  <div class="configuration-step h-full">
    <div class="space-y-1 mb-6">
      <h3 class="text-lg font-semibold">Configuration</h3>
      <p class="text-sm text-muted-foreground">
        Configure the specific settings for {{ selectedIndexer?.name }}.
      </p>
    </div>

    <!-- Configuration Fields -->
    <div class="space-y-6">
      <!-- Regular Fields -->
      <ConfigurationStepField
        v-for="field in regularFields"
        :key="field.name"
        :field="field as any"
        :selected-indexer="selectedIndexer"
        @value-change="handleValueChange"
      />

      <!-- Advanced Fields -->
      <Accordion v-if="advancedFields.length > 0" type="single" collapsible class="w-full">
        <AccordionItem value="advanced">
          <AccordionTrigger>Advanced Settings</AccordionTrigger>
          <AccordionContent>
            <div class="space-y-6 pt-2">
              <ConfigurationStepField
                v-for="field in advancedFields"
                :key="field.name"
                :field="field as any"
                :selected-indexer="selectedIndexer"
                @value-change="handleValueChange"
              />
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  </div>
</template>
