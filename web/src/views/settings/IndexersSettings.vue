<script setup lang="ts">
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Plus, Check, Search } from 'lucide-vue-next'
import {
  indexersListConfiguredOptions,
  indexersListConfiguredQueryKey,
  indexersDeleteMutation,
  indexersTestSavedMutation,
  indexersTestAllMutation,
  indexersToggleMutation,
} from '@/client/@tanstack/vue-query.gen'
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

const { data: indexers, isLoading, error } = useQuery(indexersListConfiguredOptions())
const queryClient = useQueryClient()
const modal = useModal()

function invalidateIndexers() {
  queryClient.invalidateQueries({ queryKey: indexersListConfiguredQueryKey() })
}

// Mutations
const deleteIndexerMutation = useMutation({
  ...indexersDeleteMutation(),
  onSuccess: invalidateIndexers,
  onError: async (err) => {
    await modal.alert({
      title: 'Delete Failed',
      message: problemMessage(err, 'Failed to delete indexer'),
      severity: 'error',
    })
  },
})

const toggleIndexerMutation = useMutation({
  ...indexersToggleMutation(),
  onSuccess: invalidateIndexers,
  onError: async (err) => {
    await modal.alert({
      title: 'Toggle Failed',
      message: problemMessage(err, 'Failed to toggle indexer'),
      severity: 'error',
    })
  },
})

const testIndexerMutation = useMutation({
  ...indexersTestSavedMutation(),
  onSuccess: async (result, variables) => {
    const indexer = indexers.value?.find((i) => i.id === variables.path.id)
    const name = indexer?.name ?? 'Indexer'
    if (result.success) {
      await modal.alert({
        title: 'Test Successful',
        message: `${name}: ${result.message || 'Connection test passed'}`,
        severity: 'success',
      })
    } else {
      await modal.alert({
        title: 'Test Failed',
        message: `${name}: ${result.error || 'Connection test failed'}`,
        severity: 'error',
      })
    }
  },
  onError: async (err) => {
    await modal.alert({
      title: 'Test Failed',
      message: problemMessage(err, 'Test failed'),
      severity: 'error',
    })
  },
})

const testAllIndexersMutation = useMutation({
  ...indexersTestAllMutation(),
  onSuccess: (results) => {
    modal.open(IndexerTestResultsDialog, {
      props: {
        results: results ?? [],
        class: 'max-w-[90vw] sm:max-w-2xl lg:max-w-4xl',
      },
    })
  },
  onError: async (err) => {
    await modal.alert({
      title: 'Test All Failed',
      message: problemMessage(err, 'Failed to test indexers'),
      severity: 'error',
    })
  },
})

const handleEdit = (indexer: IndexerOutput) => {
  modal.open(EditIndexerDialog, {
    props: {
      indexer,
      class: 'max-w-[90vw] sm:max-w-2xl lg:max-w-4xl',
    },
  })
}

const handleTest = (indexer: IndexerOutput) => {
  if (!indexer.id) return
  testIndexerMutation.mutate({ path: { id: indexer.id } })
}

const handleToggle = (indexer: IndexerOutput) => {
  if (!indexer.id) return
  toggleIndexerMutation.mutate({ path: { id: indexer.id } })
}

const handleDelete = async (indexer: IndexerOutput) => {
  if (!indexer.id) return
  const confirmed = await modal.confirm({
    title: 'Delete Indexer',
    message: `Are you sure you want to delete "${indexer.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  deleteIndexerMutation.mutate({ path: { id: indexer.id } })
}

const handleTestAll = () => {
  testAllIndexersMutation.mutate({})
}

const handleAddIndexer = () => {
  modal.open(AddIndexerDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-4xl lg:max-w-6xl',
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
              :disabled="testAllIndexersMutation.isPending.value || !indexers?.length"
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
