<script setup lang="ts">
import { Search } from 'lucide-vue-next'
import { type ModelIndexerDefinition } from '@/client/types.gen'
import DataTable from '@/components/tables/DataTable.vue'
import {
  availableIndexerColumns,
  createAvailableIndexerActions,
} from '@/components/tables/configs/availableIndexerTableConfig'
import { getV1IndexersSchemaOptions } from '@/client/@tanstack/vue-query.gen'

defineProps<{
  selectedIndexer: ModelIndexerDefinition | null
}>()

const emit = defineEmits<{
  'indexer-selected': [indexer: ModelIndexerDefinition]
}>()

const queryOptions = getV1IndexersSchemaOptions()

// Create actions for the available indexers table
const availableIndexerActions = createAvailableIndexerActions((indexer: ModelIndexerDefinition) => {
  emit('indexer-selected', indexer)
})
</script>

<template>
  <div class="select-indexer-step">
    <DataTable
      class="h-full"
      ref="dataTableRef"
      :query-options="queryOptions"
      :columns="availableIndexerColumns"
      :actions="availableIndexerActions"
      :auto-load="false"
      empty-message="No unconfigured indexers available"
      searchable
      search-placeholder="Search available indexers..."
      :scrollable="true"
      :scroll-height="'calc(100vh*0.5 - 100px)'"
      selectable
      :paginator="true"
      selection-mode="single"
      @selection-change="
        (selection) => {
          if (selection && !Array.isArray(selection)) {
            emit('indexer-selected', selection)
          }
        }
      "
      @data-loaded="(data) => console.log('Loaded indexers:', data.length)"
      @load-error="(error) => console.error('Failed to load indexers:', error)"
    >
      <template #empty-icon>
        <Search class="size-5" />
      </template>
    </DataTable>
  </div>
</template>
