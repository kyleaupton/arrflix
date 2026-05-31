import { defineStore } from 'pinia'
import { ref } from 'vue'

// Non-API UI state for the Downloads view: the active filter chip, how many
// rows are revealed, and which job the detail drawer is showing. The job data
// itself is server state and lives in TanStack Query — see
// `@/composables/useDownloadJobs`. This store is global because the drawer is a
// single app-wide surface and the filter persists across navigation.

export type DownloadFilter = 'all' | 'active' | 'attention' | 'completed'

export const useDownloadJobsStore = defineStore('downloadJobs', () => {
  const filter = ref<DownloadFilter>('all')
  const visibleCount = ref(20)

  // Detail drawer
  const selectedJobId = ref<string | null>(null)
  const isDetailDrawerOpen = ref(false)

  function setFilter(f: DownloadFilter) {
    filter.value = f
    visibleCount.value = 20
  }

  function showMore() {
    visibleCount.value += 20
  }

  function openDetailDrawer(jobId: string) {
    selectedJobId.value = jobId
    isDetailDrawerOpen.value = true
  }

  function closeDetailDrawer() {
    isDetailDrawerOpen.value = false
    selectedJobId.value = null
  }

  return {
    filter,
    visibleCount,
    setFilter,
    showMore,
    selectedJobId,
    isDetailDrawerOpen,
    openDetailDrawer,
    closeDetailDrawer,
  }
})
