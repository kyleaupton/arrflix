import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { DbgenListDownloadJobsWithImportSummaryRow, DbgenImportTask } from '@/client/types.gen'
import {
  getV1DownloadJobs,
  deleteV1DownloadJobsById,
  postV1DownloadJobsByIdReimport,
  postV1DownloadJobsByIdRetry,
  getV1DownloadJobsByIdImportTasks,
} from '@/client/sdk.gen'
import { useEventsStore } from '@/stores/events'
import { createMockJobs, simulateProgress } from './downloadJobs.mock'

export type DownloadJob = DbgenListDownloadJobsWithImportSummaryRow

export type DownloadFilter = 'all' | 'active' | 'attention' | 'completed'

const ATTENTION_STATUSES = ['download_failed', 'partial_failure', 'import_failed']
const ACTIVE_STATUSES = ['download_pending', 'awaiting_import', 'importing']
const COMPLETED_STATUSES = ['fully_imported', 'download_cancelled']

export const useDownloadJobsStore = defineStore('downloadJobs', () => {
  const mockMode = ref(false)
  const mockCleanup = ref<(() => void) | null>(null)

  const jobsById = ref<Record<string, DownloadJob>>({})
  const isLoading = ref(false)
  const filter = ref<DownloadFilter>('all')
  const visibleCount = ref(20)

  // Drawer state
  const selectedJobId = ref<string | null>(null)
  const isDetailDrawerOpen = ref(false)
  const drawerImportTasks = ref<DbgenImportTask[]>([])
  const isLoadingImportTasks = ref(false)

  // Stable sort by created_at (newest first) — never jumps during progress updates
  const jobsSorted = computed(() => {
    return Object.values(jobsById.value).sort((a, b) => {
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    })
  })

  const activeJobs = computed(() =>
    jobsSorted.value.filter((j) => ACTIVE_STATUSES.includes(j.import_status)),
  )

  const needsAttentionJobs = computed(() =>
    jobsSorted.value.filter((j) => ATTENTION_STATUSES.includes(j.import_status)),
  )

  const completedJobs = computed(() =>
    jobsSorted.value.filter((j) => COMPLETED_STATUSES.includes(j.import_status)),
  )

  const filteredJobs = computed(() => {
    switch (filter.value) {
      case 'active':
        return activeJobs.value
      case 'attention':
        return needsAttentionJobs.value
      case 'completed':
        return completedJobs.value
      default:
        return jobsSorted.value
    }
  })

  const visibleJobs = computed(() => filteredJobs.value.slice(0, visibleCount.value))
  const hasMore = computed(() => filteredJobs.value.length > visibleCount.value)

  function showMore() {
    visibleCount.value += 20
  }

  function setFilter(f: DownloadFilter) {
    filter.value = f
    visibleCount.value = 20
  }

  const selectedJob = computed(() => {
    if (!selectedJobId.value) return null
    return jobsById.value[selectedJobId.value] ?? null
  })

  function upsert(job: DownloadJob) {
    jobsById.value = { ...jobsById.value, [job.id]: job }
  }

  function replaceAll(list: DownloadJob[]) {
    if (!list) return
    const next: Record<string, DownloadJob> = {}
    for (const j of list) next[j.id] = j
    jobsById.value = next
  }

  async function refresh() {
    if (mockMode.value) return
    isLoading.value = true
    try {
      const res = await getV1DownloadJobs({ throwOnError: true })
      replaceAll(res.data as unknown as DownloadJob[])
    } finally {
      isLoading.value = false
    }
  }

  function connectLive() {
    if (mockMode.value) {
      if (mockCleanup.value) mockCleanup.value()
      replaceAll(createMockJobs())
      mockCleanup.value = simulateProgress(upsert)
      return
    }

    const events = useEventsStore()
    events.connect(['download_jobs_snapshot', 'download_job_updated', 'ping', 'ready'])

    events.on('download_jobs_snapshot', (data) => {
      replaceAll((data as unknown as DownloadJob[]) ?? [])
    })
    events.on('download_job_updated', (data) => {
      if (!data) return
      upsert(data as unknown as DownloadJob)
    })

    // When the tab comes back from being backgrounded, do a full refresh
    // to catch any events we may have missed while inactive.
    events.on('_visibility_restored', () => {
      refresh()
    })
  }

  async function cancelJob(id: string) {
    const res = await deleteV1DownloadJobsById({
      throwOnError: true,
      path: { id },
    })
    // Optimistically update local state; SSE may also deliver an update later.
    upsert(res.data as unknown as DownloadJob)
  }

  function getJobById(jobId: string): DownloadJob | undefined {
    return jobsById.value[jobId]
  }

  function isJobActive(job: DownloadJob): boolean {
    const activeStatuses = ['download_pending']
    return activeStatuses.includes(job.import_status)
  }

  // Drawer methods
  function openDetailDrawer(jobId: string) {
    selectedJobId.value = jobId
    isDetailDrawerOpen.value = true
    loadImportTasks(jobId)
  }

  function closeDetailDrawer() {
    isDetailDrawerOpen.value = false
    selectedJobId.value = null
    drawerImportTasks.value = []
  }

  async function loadImportTasks(jobId: string) {
    isLoadingImportTasks.value = true
    try {
      const res = await getV1DownloadJobsByIdImportTasks({
        throwOnError: true,
        path: { id: jobId },
      })
      drawerImportTasks.value = res.data ?? []
    } catch {
      drawerImportTasks.value = []
    } finally {
      isLoadingImportTasks.value = false
    }
  }

  async function reimportFailed(jobId: string, all: boolean = false) {
    const res = await postV1DownloadJobsByIdReimport({
      throwOnError: true,
      path: { id: jobId },
      query: { all },
    })
    // Reload import tasks if drawer is open
    if (isDetailDrawerOpen.value && selectedJobId.value === jobId) {
      await loadImportTasks(jobId)
    }
    // Refresh the job list to get updated import_status
    await refresh()
    return res.data
  }

  async function retryDownload(jobId: string) {
    await postV1DownloadJobsByIdRetry({
      throwOnError: true,
      path: { id: jobId },
    })
    closeDetailDrawer()
    await refresh()
  }

  return {
    mockMode,
    jobsById,
    jobsSorted,
    activeJobs,
    needsAttentionJobs,
    completedJobs,
    filteredJobs,
    visibleJobs,
    hasMore,
    filter,
    setFilter,
    showMore,
    isLoading,
    refresh,
    connectLive,
    cancelJob,
    getJobById,
    isJobActive,
    retryDownload,
    // Drawer
    selectedJobId,
    selectedJob,
    isDetailDrawerOpen,
    drawerImportTasks,
    isLoadingImportTasks,
    openDetailDrawer,
    closeDetailDrawer,
    loadImportTasks,
    reimportFailed,
  }
})
