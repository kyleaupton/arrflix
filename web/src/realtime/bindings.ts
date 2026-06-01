// The SSE → TanStack Query cache contract — the single place that decides what
// each realtime event does to server state. installRealtime() is called once at
// app start with the shared QueryClient; components stay SSE-unaware and just
// useQuery. The cache is kept live globally, regardless of what's mounted.

import type { QueryClient } from '@tanstack/vue-query'
import { downloadJobsListQueryKey } from '@/client/@tanstack/vue-query.gen'
import type { DownloadJobWithSummary } from '@/client/types.gen'
import { on, onResync } from '@/realtime/connection'

// upsert merges a full download-job delta into the cached list.
function upsert(
  prev: DownloadJobWithSummary[] | undefined,
  job: DownloadJobWithSummary,
): DownloadJobWithSummary[] {
  const list = prev ?? []
  const idx = list.findIndex((j) => j.id === job.id)
  if (idx === -1) return [...list, job]
  const next = list.slice()
  next[idx] = job
  return next
}

export function installRealtime(qc: QueryClient) {
  const jobsKey = downloadJobsListQueryKey()

  // Full-state payload → write the cache directly. The event carries the whole
  // enriched job, so this is accurate without a refetch and snappy at per-second
  // progress cadence.
  on('download_job_updated', (data) => {
    qc.setQueryData<DownloadJobWithSummary[]>(jobsKey, (prev) =>
      upsert(prev, data as DownloadJobWithSummary),
    )
  })

  // Kick-only payload → invalidate every per-job import-task list (partial match
  // on the operation id). This is what live-updates the detail drawer during an
  // import; only the mounted/selected one refetches immediately.
  on('import_task_updated', () => {
    qc.invalidateQueries({ queryKey: [{ _id: 'downloadJobsListImportTasks' }] })
  })

  // Recovery (reconnect-after-drop or replay-buffer gap): the cache may have
  // missed deltas, so refetch every active query.
  onResync(() => {
    qc.invalidateQueries()
  })
}
