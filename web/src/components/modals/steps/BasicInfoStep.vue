<script setup lang="ts">
import { type IndexerOutput } from '@/client/types.gen'

interface Props {
  selectedIndexer: IndexerOutput | null
  formData: {
    name: string
    description: string
    enabled: boolean
    type: string
  }
}

defineProps<Props>()

const emit = defineEmits<{
  'update:formData': [data: Partial<Props['formData']>]
}>()

const updateField = <K extends keyof Props['formData']>(
  field: K,
  value: Props['formData'][K],
) => {
  emit('update:formData', { [field]: value })
}
</script>

<template>
  <div class="basic-info-step">
    <h3 class="text-lg font-semibold mb-4">Basic Information</h3>
    <p class="text-muted-color mb-6">Configure the basic settings for your indexer.</p>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium mb-2">Name</label>
          <input
            :value="formData.name"
            type="text"
            class="w-full p-inputtext p-component"
            placeholder="Enter indexer name"
            @input="updateField('name', ($event.target as HTMLInputElement).value)"
          />
        </div>

        <div>
          <label class="block text-sm font-medium mb-2">Description</label>
          <textarea
            :value="formData.description"
            class="w-full p-inputtextarea p-component"
            rows="3"
            placeholder="Enter indexer description"
            @input="updateField('description', ($event.target as HTMLTextAreaElement).value)"
          />
        </div>
      </div>

      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium mb-2">Type</label>
          <input
            :value="formData.type"
            type="text"
            class="w-full p-inputtext p-component"
            disabled
          />
        </div>

        <div class="flex items-center space-x-2">
          <input
            :checked="formData.enabled"
            type="checkbox"
            class="p-checkbox"
            id="enabled"
            @change="updateField('enabled', ($event.target as HTMLInputElement).checked)"
          />
          <label for="enabled" class="text-sm font-medium">Enable this indexer</label>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.basic-info-step {
  min-height: 400px;
}
</style>
