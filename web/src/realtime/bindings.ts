// The SSE → TanStack Query cache contract — the single place that decides what
// each realtime event does to server state. installRealtime() is called once at
// app start with the shared QueryClient; components stay SSE-unaware and just
// useQuery. The cache is kept live globally, regardless of what's mounted.

import type { QueryClient } from '@tanstack/vue-query'
import { downloadJobsListQueryKey } from '@/client/@tanstack/vue-query.gen'
import type { DownloadJobWithSummary, TrackingByTmdb, Want } from '@/client/types.gen'
import { on, onResync } from '@/realtime/connection'

// upsertJob merges a full download-job delta into the cached list.
function upsertJob(
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

// upsertWant merges a want delta into a tracking's wants array.
function upsertWant(list: Want[], want: Want): Want[] {
  const idx = list.findIndex((w) => w.id === want.id)
  if (idx === -1) return [...list, want]
  const next = list.slice()
  next[idx] = want
  return next
}

// coalesce collapses a burst of calls into a single trailing invocation. The
// invalidate-based bindings can fire many times in a short window (a season's
// worth of proposals, an import sweep); coalescing bounds them to one refetch
// per burst instead of one per event.
function coalesce(fn: () => void, ms = 300): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null
  return () => {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      timer = null
      fn()
    }, ms)
  }
}

export function installRealtime(qc: QueryClient) {
  const jobsKey = downloadJobsListQueryKey()

  // Full-state payload → write the cache directly. The event carries the whole
  // enriched job, so this is accurate without a refetch and snappy at per-second
  // progress cadence.
  on('download_job_updated', (data) => {
    qc.setQueryData<DownloadJobWithSummary[]>(jobsKey, (prev) =>
      upsertJob(prev, data as DownloadJobWithSummary),
    )
  })

  // Kick-only payload → invalidate every per-job import-task list (partial match
  // on the operation id). This is what live-updates the detail drawer during an
  // import; only the mounted/selected one refetches immediately.
  const refreshImportTasks = coalesce(() =>
    qc.invalidateQueries({ queryKey: [{ _id: 'downloadJobsListImportTasks' }] }),
  )

  // The media-detail payload (files[], per-episode availability, download-job
  // links) has no direct SSE delta, so it freezes at page load. Refetch it on the
  // events that change what's on disk — an import task moving, or a want reaching a
  // terminal-availability status — so files flip to "Available" without a reload.
  // Deliberately NOT fired on download_job_updated: those tick per second and the
  // jobs cache already carries live progress, so refetching the whole detail on
  // each tick would be pure waste. Coalesced like the other invalidations.
  const refreshMedia = coalesce(() => {
    qc.invalidateQueries({ queryKey: [{ _id: 'mediaGetMovie' }] })
    qc.invalidateQueries({ queryKey: [{ _id: 'mediaGetSeries' }] })
  })
  on('import_task_updated', () => {
    refreshImportTasks()
    refreshMedia()
  })

  // Want lifecycle delta — the full want, carrying trackingId. Patch it straight
  // into every cached trackingByTmdb entry whose tracking matches, exactly like
  // download jobs: no refetch, so a series fulfilling dozens of episode wants
  // costs zero round-trips. A brand-new tracking (just-spawned, no cached entry)
  // isn't patched here — the mounted page's own request mutation invalidates that
  // key, so it's already covered.
  on('want_updated', (data) => {
    const want = data as Want
    qc.setQueriesData<TrackingByTmdb>({ queryKey: [{ _id: 'trackingByTmdb' }] }, (prev) => {
      if (!prev?.tracking || prev.tracking.id !== want.trackingId) return prev
      return { ...prev, wants: upsertWant(prev.wants ?? [], want) }
    })
    // A want reaching the library flips file/episode availability the media-detail
    // query can't learn from this patch alone — refetch it (coalesced) so the page
    // lands on "Available" even if the import-task event is dropped.
    if (want.status === 'imported' || want.status === 'available') refreshMedia()
  })

  // Proposal delta (kick-only, keyed by trackingId). The want side is already
  // patched by want_updated above; this refreshes the separate proposal lists
  // (Approve/Decline rows) and, as a coalesced backstop, the by-tmdb wants.
  const refreshProposals = coalesce(() => {
    qc.invalidateQueries({ queryKey: [{ _id: 'trackingProposals' }] })
    qc.invalidateQueries({ queryKey: [{ _id: 'trackingByTmdb' }] })
  })
  on('proposal_updated', refreshProposals)

  // Recovery (reconnect-after-drop or replay-buffer gap): the cache may have
  // missed deltas, so refetch every active query.
  onResync(() => {
    qc.invalidateQueries()
  })
}
