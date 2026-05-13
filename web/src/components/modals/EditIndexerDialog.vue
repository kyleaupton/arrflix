<script setup lang="ts">
import { ref, inject, computed, watch } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import { Save, Check } from 'lucide-vue-next'
import { type IndexerOutput, type IndexerInput } from '@/client/types.gen'
import { indexersSaveMutation } from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import ConfigurationStep from './steps/ConfigurationStep.vue'
import BaseDialog from './BaseDialog.vue'
import { client } from '@/client/client.gen'
import { useModal } from '@/composables/useModal'

interface Props {
  indexer: IndexerOutput
}

const props = defineProps<Props>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const modal = useModal()

// Form state
const saveData = ref<IndexerInput | undefined>(undefined)
const indexerError = ref<string | null>(null)
const isTestingConfig = ref(false)

const updateIndexerMutation = useMutation({
  ...indexersSaveMutation(),
  onSuccess: () => {
    indexerError.value = null
    dialogRef.value.close({ indexerUpdated: true })
  },
  onError: (error) => {
    console.error('Failed to update indexer:', error)
    const err = error as { message?: string }
    indexerError.value = err.message || 'Failed to update indexer'
  },
})

// Watch for changes to the indexer prop and reset error
watch(
  () => props.indexer,
  () => {
    indexerError.value = null
  },
  { immediate: true },
)

const handleTestIndexer = async () => {
  if (!props.indexer?.id) {
    indexerError.value = 'Indexer ID is required'
    return
  }

  isTestingConfig.value = true
  indexerError.value = null

  try {
    const response = await client.post({
      url: `/v1/indexer/${props.indexer.id}/test`,
    })

    const result = response.data as { success: boolean; message?: string; error?: string }

    if (result.success) {
      indexerError.value = null
      await modal.alert({
        title: 'Test Successful',
        message: result.message || 'Indexer connection test passed',
        severity: 'success',
      })
    } else {
      indexerError.value = result.error || 'Test failed'
    }
  } catch (err) {
    const error = err as { message?: string; data?: { error?: string } }
    indexerError.value = error.data?.error || error.message || 'Test failed'
  } finally {
    isTestingConfig.value = false
  }
}

const handleSave = () => {
  if (!saveData.value) {
    indexerError.value = 'Configuration data is required'
    return
  }

  // Include the ID to indicate this is an update operation
  const updatePayload: IndexerInput = {
    ...saveData.value,
    id: props.indexer.id,
  }

  updateIndexerMutation.mutate({
    body: updatePayload,
  })
}

const handleCancel = () => {
  dialogRef.value.close()
}

const canSave = computed(() => {
  return saveData.value !== undefined
})
</script>

<template>
  <BaseDialog :title="`Edit ${indexer.name}`">
    <div class="flex flex-col gap-4">
      <div
        v-if="indexerError"
        class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm"
      >
        {{ indexerError }}
      </div>

      <div class="max-h-[calc(100vh*0.6)] overflow-y-auto">
        <ConfigurationStep v-model="saveData" :selected-indexer="indexer" />
      </div>
    </div>

    <template #footer>
      <div class="flex items-center justify-between w-full">
        <Button variant="outline" :disabled="isTestingConfig" @click="handleTestIndexer">
          <Check class="mr-2 size-4" />
          Test Connection
        </Button>
        <div class="flex justify-end gap-2">
          <Button variant="outline" @click="handleCancel">Cancel</Button>
          <Button :disabled="!canSave || updateIndexerMutation.isPending.value" @click="handleSave">
            <Save class="mr-2 size-4" />
            Save Changes
          </Button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>
