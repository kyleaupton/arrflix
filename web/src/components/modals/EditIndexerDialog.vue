<script setup lang="ts">
import { ref, inject, computed, watch } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { Save, Check } from 'lucide-vue-next'
import { type IndexerOutput, type IndexerInput } from '@/client/types.gen'
import {
  indexersSaveMutation,
  indexersTestSavedMutation,
  indexersListConfiguredQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import ConfigurationStep from './steps/ConfigurationStep.vue'
import BaseDialog from './BaseDialog.vue'
import { problemMessage } from '@/lib/api'
import { useModal } from '@/composables/useModal'

interface Props {
  indexer: IndexerOutput
}

const props = defineProps<Props>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const modal = useModal()
const queryClient = useQueryClient()

// Form state
const saveData = ref<IndexerInput | undefined>(undefined)
const indexerError = ref<string | null>(null)

const updateIndexerMutation = useMutation({
  ...indexersSaveMutation(),
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: indexersListConfiguredQueryKey() })
    indexerError.value = null
    dialogRef.value.close({ indexerUpdated: true })
  },
  onError: (error) => {
    console.error('Failed to update indexer:', error)
    indexerError.value = problemMessage(error, 'Failed to update indexer')
  },
})

const testIndexerMutation = useMutation({
  ...indexersTestSavedMutation(),
  onSuccess: async (result) => {
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
  },
  onError: (err) => {
    indexerError.value = problemMessage(err, 'Test failed')
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

const handleTestIndexer = () => {
  if (!props.indexer?.id) {
    indexerError.value = 'Indexer ID is required'
    return
  }
  indexerError.value = null
  testIndexerMutation.mutate({ path: { id: props.indexer.id } })
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
        <Button
          variant="outline"
          :disabled="testIndexerMutation.isPending.value"
          @click="handleTestIndexer"
        >
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
