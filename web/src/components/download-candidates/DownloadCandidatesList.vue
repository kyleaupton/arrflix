<template>
  <div class="download-candidates">
    <div class="mb-4">
      <!-- <h3 class="text-lg font-semibold mb-2">Download Candidates</h3> -->
      <!-- <p class="text-sm text-muted-color">
        Search results for download candidates. Select a candidate to enqueue it for download.
      </p> -->
    </div>

    <DataTable
      :query-options="queryOptions"
      :columns="downloadCandidateColumns"
      :actions="candidateActions"
      searchable
      search-placeholder="Search candidates..."
      paginator
      :rows="20"
    >
      <template #empty-icon>
        <Search class="size-5" />
      </template>
    </DataTable>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Search } from 'lucide-vue-next'
import {
  getV1MovieByIdCandidatesOptions,
  getV1SeriesByIdCandidatesOptions,
} from '@/client/@tanstack/vue-query.gen'
import { type ModelDownloadCandidate } from '@/client/types.gen'
import DataTable from '@/components/tables/DataTable.vue'
import {
  downloadCandidateColumns,
  createDownloadCandidateActions,
} from '@/components/tables/configs/downloadCandidateTableConfig'

const emit = defineEmits<{
  (e: 'enqueue', candidate: ModelDownloadCandidate): void
}>()

const props = defineProps<{
  movieId?: number
  seriesId?: number
  season?: number
  episode?: number
}>()

// Query options for fetching download candidates
const queryOptions = computed(() => {
  if (props.movieId) {
    return getV1MovieByIdCandidatesOptions({
      path: { id: props.movieId },
    })
  } else if (props.seriesId) {
    return getV1SeriesByIdCandidatesOptions({
      path: { id: props.seriesId },
      query: {
        season: props.season,
        episode: props.episode,
      },
    })
  }
  return undefined
})

// Handle enqueue action
const handleEnqueue = (candidate: ModelDownloadCandidate) => {
  console.log('enqueue', candidate)
  emit('enqueue', candidate)
}

// Create actions
const candidateActions = createDownloadCandidateActions(handleEnqueue)
</script>

<style scoped>
.download-candidates {
  width: 100%;
  height: 100%;
}
</style>
