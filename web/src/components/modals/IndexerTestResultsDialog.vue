<script setup lang="ts">
import { inject, computed, h } from 'vue'
import { Button } from '@/components/ui/button'
import BaseDialog from './BaseDialog.vue'
import DataTable, { type TableColumn } from '@/components/tables/DataTable.vue'
import { CheckCircle, XCircle, FlaskConical } from 'lucide-vue-next'
import type { IndexerBatchTestResult } from '@/client/types.gen'

interface Props {
  results: IndexerBatchTestResult[]
}

const props = withDefaults(defineProps<Props>(), {
  results: () => [],
})

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }

const handleClose = () => {
  dialogRef.value.close()
}

const successCount = computed(() => props.results.filter((r) => r.success).length)
const failureCount = computed(() => props.results.filter((r) => !r.success).length)

const columns: TableColumn<IndexerBatchTestResult>[] = [
  {
    key: 'success',
    label: 'Status',
    sortable: true,
    width: '80px',
    render: (value: boolean) => {
      return h(
        'div',
        { class: 'flex items-center justify-center' },
        value
          ? h(CheckCircle, { class: 'size-5 text-green-600' })
          : h(XCircle, { class: 'size-5 text-red-600' }),
      )
    },
  },
  {
    key: 'indexerName',
    label: 'Indexer',
    sortable: true,
    filterable: true,
  },
  {
    key: 'message',
    label: 'Result',
    sortable: false,
    render: (_value: string, row: IndexerBatchTestResult) => {
      return row.success ? row.message || 'Test passed' : row.error || 'Test failed'
    },
  },
]
</script>

<template>
  <BaseDialog title="Indexer Test Results">
    <div class="flex flex-col gap-4">
      <!-- Summary -->
      <div class="flex gap-4 p-4 bg-muted/50 rounded-lg">
        <div class="flex items-center gap-2">
          <CheckCircle class="size-5 text-green-600" />
          <span class="font-medium">{{ successCount }} Passed</span>
        </div>
        <div class="flex items-center gap-2">
          <XCircle class="size-5 text-red-600" />
          <span class="font-medium">{{ failureCount }} Failed</span>
        </div>
      </div>

      <!-- Results DataTable -->
      <DataTable
        :data="results"
        :columns="columns"
        :paginator="results.length > 10"
        :rows="10"
        :searchable="false"
        empty-message="No test results available"
      >
        <template #empty-icon>
          <FlaskConical class="size-5" />
        </template>
      </DataTable>
    </div>

    <template #footer>
      <div class="flex justify-end w-full">
        <Button @click="handleClose">Close</Button>
      </div>
    </template>
  </BaseDialog>
</template>
