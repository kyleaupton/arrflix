<script setup lang="ts">
import { ref } from 'vue'
import { useQuery, useMutation } from '@tanstack/vue-query'
import { Plus, Check, Search } from 'lucide-vue-next'
import {
  indexersListConfiguredOptions,
  indexersDeleteMutation,
} from '@/client/@tanstack/vue-query.gen'
import { indexersTestSaved, indexersTestAll, indexersToggle } from '@/client/sdk.gen'
import { type IndexerOutput } from '@/client/types.gen'
import { problemMessage } from '@/lib/api'
import {
  indexerColumns,
  createIndexerActions,
} from '@/components/tables/configs/indexerTableConfig'
import DataTable from '@/components/tables/DataTable.vue'
import AddIndexerDialog from '@/components/modals/AddIndexerDialog.vue'
import EditIndexerDialog from '@/components/modals/EditIndexerDialog.vue'
import IndexerTestResultsDialog from '@/components/modals/IndexerTestResultsDialog.vue'
import { useModal } from '@/composables/useModal'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

const { data: indexers, isLoading, error, refetch } = useQuery(indexersListConfiguredOptions())
const modal = useModal()
const isTestingAll = ref(false)

// Mutations
const deleteIndexerMutation = useMutation(indexersDeleteMutation())

const handleEdit = (indexer: IndexerOutput) => {
  modal.open(EditIndexerDialog, {
    props: {
      indexer,
      class: 'max-w-[90vw] sm:max-w-2xl lg:max-w-4xl',
    },
    onClose: (result) => {
      if ((result?.data as { indexerUpdated?: boolean })?.indexerUpdated) {
        refetch()
      }
    },
  })
}

const handleTest = async (indexer: IndexerOutput) => {
  if (!indexer.id) return
  try {
    const { data: result } = await indexersTestSaved<true>({
      throwOnError: true,
      path: { id: indexer.id },
    })

    if (result.success) {
      await modal.alert({
        title: 'Test Successful',
        message: `${indexer.name}: ${result.message || 'Connection test passed'}`,
        severity: 'success',
      })
    } else {
      await modal.alert({
        title: 'Test Failed',
        message: `${indexer.name}: ${result.error || 'Connection test failed'}`,
        severity: 'error',
      })
    }
  } catch (err) {
    await modal.alert({
      title: 'Test Failed',
      message: problemMessage(err, 'Test failed'),
      severity: 'error',
    })
  }
}

const handleToggle = async (indexer: IndexerOutput) => {
  if (!indexer.id) return
  try {
    await indexersToggle<true>({
      throwOnError: true,
      path: { id: indexer.id },
    })
    refetch()
  } catch (err) {
    await modal.alert({
      title: 'Toggle Failed',
      message: problemMessage(err, 'Failed to toggle indexer'),
      severity: 'error',
    })
  }
}

const handleDelete = async (indexer: IndexerOutput) => {
  if (!indexer.id) return
  const confirmed = await modal.confirm({
    title: 'Delete Indexer',
    message: `Are you sure you want to delete "${indexer.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  try {
    await deleteIndexerMutation.mutateAsync({ path: { id: indexer.id } })
    refetch()
  } catch (err) {
    const error = err as { message?: string }
    await modal.alert({
      title: 'Delete Failed',
      message: error.message || 'Failed to delete indexer',
      severity: 'error',
    })
  }
}

const handleTestAll = async () => {
  isTestingAll.value = true
  try {
    const { data: results } = await indexersTestAll<true>({ throwOnError: true })

    modal.open(IndexerTestResultsDialog, {
      props: {
        results: results ?? [],
        class: 'max-w-[90vw] sm:max-w-2xl lg:max-w-4xl',
      },
    })
  } catch (err) {
    await modal.alert({
      title: 'Test All Failed',
      message: problemMessage(err, 'Failed to test indexers'),
      severity: 'error',
    })
  } finally {
    isTestingAll.value = false
  }
}

const handleAddIndexer = () => {
  modal.open(AddIndexerDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-4xl lg:max-w-6xl',
    },
    onClose: (result) => {
      if ((result?.data as { indexerAdded?: boolean })?.indexerAdded) {
        refetch()
      }
    },
  })
}

const indexerActions = createIndexerActions(handleEdit, handleTest, handleToggle, handleDelete)
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Indexers</CardTitle>
            <p class="text-sm text-muted-foreground">
              Configure your media indexers and search providers.
            </p>
          </div>
          <div class="flex gap-2">
            <Button
              variant="outline"
              :disabled="isTestingAll || !indexers?.length"
              @click="handleTestAll"
            >
              <Check class="mr-2 size-4" />
              Test All
            </Button>
            <Button @click="handleAddIndexer">
              <Plus class="mr-2 size-4" />
              Add Indexer
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div v-if="isLoading" class="space-y-3">
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
        </div>
        <DataTable
          v-else
          :data="(indexers || []) as unknown as IndexerOutput[]"
          :columns="indexerColumns"
          :actions="indexerActions"
          :loading="isLoading"
          :empty-message="error ? `Error: ${error.message}` : 'No indexers configured'"
          searchable
          search-placeholder="Search indexers..."
          paginator
          :rows="10"
        >
          <template #empty-icon>
            <Search class="size-5" />
          </template>
        </DataTable>
      </CardContent>
    </Card>
  </div>
</template>
