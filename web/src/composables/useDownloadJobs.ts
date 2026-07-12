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

export type DownloadJob = DownloadJobWithSummary

// Import-status groupings used by the Downloads filter chips.
const ATTENTION_STATUSES = ['download_failed', 'partial_failure', 'import_failed']
const ACTIVE_STATUSES = ['download_pending', 'awaiting_import', 'importing']
const COMPLETED_STATUSES = ['fully_imported', 'download_cancelled']

const listKey = downloadJobsListQueryKey()

// isJobActive reports whether a job is still in flight, for the "is this media
// downloading" badges on the Movie page.
export function isJobActive(job: DownloadJob): boolean {
  return job.importStatus === 'download_pending'
}

// useDownloadJobs reads the download-jobs list from TanStack Query. The list is
// kept live globally by the realtime cache bindings (realtime/bindings.ts) —
// per-job `download_job_updated` events upsert into this query key regardless of
// what's mounted — so this composable carries no SSE wiring and is safe to call
// from multiple components (the query dedupes by key).
export function useDownloadJobs() {
  const query = useQuery(downloadJobsListOptions())

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

  // The in-flight job for a movie, read from the shared live list. A movie
  // tracking is single-atom (one want, one job at a time), and `jobs` is
  // newest-first, so the first match is the one advancing the current want.
  // Callers get SSE-patched progress without a second per-movie fetch.
  function getMovieJob(tmdbId: number): DownloadJob | undefined {
    return jobs.value.find((j) => j.mediaType === 'movie' && j.tmdbId === tmdbId)
  }

  return {
    isLoading: query.isLoading,
    jobs,
    jobsById,
    getJobById,
    getMovieJob,
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
