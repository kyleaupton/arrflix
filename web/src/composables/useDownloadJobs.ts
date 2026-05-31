import { computed } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  downloadJobsListOptions,
  downloadJobsListQueryKey,
  downloadJobsCancelMutation,
  downloadJobsReimportMutation,
  downloadJobsRetryMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { DownloadJobWithSummary } from '@/client/types.gen'
import { useSSEMutation } from '@/composables/useSSE'

export type DownloadJob = DownloadJobWithSummary

// Import-status groupings used by the Downloads filter chips.
const ATTENTION_STATUSES = ['download_failed', 'partial_failure', 'import_failed']
const ACTIVE_STATUSES = ['download_pending', 'awaiting_import', 'importing']
const COMPLETED_STATUSES = ['fully_imported', 'download_cancelled']

const listKey = downloadJobsListQueryKey()

function upsert(prev: DownloadJob[] | undefined, job: DownloadJob): DownloadJob[] {
  const list = prev ?? []
  const idx = list.findIndex((j) => j.id === job.id)
  if (idx === -1) return [...list, job]
  const next = list.slice()
  next[idx] = job
  return next
}

// isJobActive reports whether a job is still in flight, for the "is this media
// downloading" badges on the Movie page. Kept as the prior store semantics.
export function isJobActive(job: DownloadJob): boolean {
  return job.importStatus === 'download_pending'
}

// useDownloadJobsLive owns the download-jobs list as a TanStack query and keeps
// it live off the app-wide SSE stream: the connect-time snapshot replaces the
// cache, per-job `download_job_updated` events upsert into it. The stream
// itself is opened once in App.vue — this composable only listens. Calling it
// more than once (e.g. the Downloads view plus its detail drawer) is safe: the
// query dedupes by key and the SSE bridges are idempotent setQueryData writes.
export function useDownloadJobsLive() {
  const query = useQuery(downloadJobsListOptions())

  useSSEMutation<DownloadJob[], DownloadJob[] | null>(
    listKey,
    'download_jobs_snapshot',
    (_prev, snapshot) => snapshot ?? [],
  )
  useSSEMutation<DownloadJob[], DownloadJob>(listKey, 'download_job_updated', upsert)

  // Stable sort by createdAt (newest first) so rows never reorder mid-progress.
  const jobs = computed(() =>
    [...(query.data.value ?? [])].sort(
      (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime(),
    ),
  )

  const jobsById = computed<Record<string, DownloadJob>>(() => {
    const map: Record<string, DownloadJob> = {}
    for (const j of jobs.value) map[j.id] = j
    return map
  })

  const activeJobs = computed(() =>
    jobs.value.filter((j) => ACTIVE_STATUSES.includes(j.importStatus)),
  )
  const needsAttentionJobs = computed(() =>
    jobs.value.filter((j) => ATTENTION_STATUSES.includes(j.importStatus)),
  )
  const completedJobs = computed(() =>
    jobs.value.filter((j) => COMPLETED_STATUSES.includes(j.importStatus)),
  )

  function getJobById(id: string): DownloadJob | undefined {
    return jobsById.value[id]
  }

  return {
    isLoading: query.isLoading,
    jobs,
    jobsById,
    getJobById,
    activeJobs,
    needsAttentionJobs,
    completedJobs,
  }
}

// useDownloadJobMutations exposes the write actions as TanStack mutations. Each
// invalidates the list (and, where relevant, the import-tasks query) so every
// observer refetches; the live SSE deltas also reflect the change, so the
// invalidation is a correctness backstop, not the only path.
export function useDownloadJobMutations() {
  const queryClient = useQueryClient()
  const invalidateList = () => queryClient.invalidateQueries({ queryKey: listKey })
  const invalidateImportTasks = () =>
    queryClient.invalidateQueries({ queryKey: [{ _id: 'downloadJobsListImportTasks' }] })

  const cancel = useMutation({ ...downloadJobsCancelMutation(), onSuccess: invalidateList })
  const reimport = useMutation({
    ...downloadJobsReimportMutation(),
    onSuccess: () => {
      invalidateList()
      invalidateImportTasks()
    },
  })
  const retry = useMutation({ ...downloadJobsRetryMutation(), onSuccess: invalidateList })

  return {
    cancelJob: (id: string) => cancel.mutateAsync({ path: { id } }),
    reimportFailed: (id: string, all = false) =>
      reimport.mutateAsync({ path: { id }, query: { all } }),
    retryDownload: (id: string) => retry.mutateAsync({ path: { id } }),
  }
}
