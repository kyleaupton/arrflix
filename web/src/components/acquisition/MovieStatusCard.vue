<script setup lang="ts">
import { computed, type Component } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import {
  CalendarClock,
  CircleCheck,
  Clock,
  Download,
  Film,
  Search,
  Sparkles,
} from 'lucide-vue-next'
import { trackingByTmdbOptions } from '@/client/@tanstack/vue-query.gen'
import type { MovieDetail, Request, Want } from '@/client/types.gen'
import { Progress } from '@/components/ui/progress'
import { useDownloadJobs, isJobActive } from '@/composables/useDownloadJobs'
import { useAuthStore } from '@/stores/auth'
import { formatBytes, formatSpeed, formatEta } from '@/lib/format'

// Tells a movie's acquisition story so the page's main column is never empty.
// State-adaptive and requester-first (visible to everyone): a download in flight,
// an active want, a suggestion awaiting review, a title not yet obtainable (still
// in its theatrical window), or simply absent from the library. Once the movie is
// on disk the card yields to the operator's Local Files table, but requesters have
// no such table, so they instead get a plain "in your library" confirmation.
// Acquisition state is fetched here — the same query keys the page's
// AcquisitionControl uses, so TanStack dedupes them.
const props = defineProps<{ movie: MovieDetail }>()

const auth = useAuthStore()
const tmdbId = computed(() => props.movie.tmdbId)

// On disk = at least one real (non-predicted) file. Predicted in-flight entries
// carry a downloading/importing status, so 'available' is the on-disk signal.
const availableLocally = computed(
  () => props.movie.files?.some((f) => f.status === 'available') ?? false,
)

// Untracked is a normal 200 with null tracking, read directly below.
const { data: tracking } = useQuery(
  computed(() => trackingByTmdbOptions({ path: { tmdbId: tmdbId.value } })),
)
const isTracked = computed(() => !!tracking.value?.tracking)
const want = computed<Want | null>(() => tracking.value?.wants?.[0] ?? null)

// The caller's own pending request rides along on the status payload — surfaces the
// requested state before anything is tracked, so a fresh request isn't rendered as
// "Not in your library".
const myRequest = computed<Request | null>(() => tracking.value?.myRequest ?? null)

// The movie's in-flight job, read from the shared live jobs cache (SSE-patched)
// rather than a second per-movie fetch, so the progress bar advances live.
const { getMovieJob } = useDownloadJobs()
const job = computed(() => getMovieJob(tmdbId.value) ?? null)

const jobActive = computed(() => !!job.value && isJobActive(job.value))

function isProposed(w: Want): boolean {
  return w.hold === 'proposed' && (w.status === 'pending' || w.status === 'searching')
}

type CardState =
  | 'available'
  | 'downloading'
  | 'wanted'
  | 'proposed'
  | 'requested'
  | 'unobtainable'
  | 'notInLibrary'

const state = computed<CardState | null>(() => {
  if (availableLocally.value) {
    // Operators read the Local Files table instead, so their card stays hidden;
    // requesters have no table, so give them a plain "it's here" confirmation.
    return auth.canViewJobs ? null : 'available'
  }
  if (isTracked.value) {
    if (jobActive.value) return 'downloading'
    const w = want.value
    if (w) return isProposed(w) ? 'proposed' : 'wanted'
    // Tracked with no want is unexpected — fall through to obtainability.
  }
  if (myRequest.value?.status === 'pending') return 'requested'
  return obtainable.value ? 'notInLibrary' : 'unobtainable'
})

// --- Obtainability (untracked branch) ---------------------------------------

function reached(dateStr?: string): boolean {
  if (!dateStr) return false
  const d = new Date(dateStr + 'T00:00:00')
  return !isNaN(d.getTime()) && d.getTime() <= Date.now()
}
const hasHomeDates = computed(
  () => !!(props.movie.releaseDates?.digital || props.movie.releaseDates?.physical),
)
// Obtainable = a home-release (digital/physical) date has passed, or — for older
// catalog titles TMDB has no per-type dates for — the movie is simply released.
const obtainable = computed(
  () =>
    reached(props.movie.releaseDates?.digital) ||
    reached(props.movie.releaseDates?.physical) ||
    (!hasHomeDates.value && props.movie.status === 'released'),
)

// --- Per-state presentation -------------------------------------------------

const icon = computed<Component>(() => {
  switch (state.value) {
    case 'available':
      return CircleCheck
    case 'downloading':
      return Download
    case 'wanted':
      return Search
    case 'proposed':
      return Sparkles
    case 'requested':
      return Clock
    case 'unobtainable':
      return CalendarClock
    default:
      return Film
  }
})

const headline = computed(() => {
  switch (state.value) {
    case 'available':
      return 'In your library'
    case 'downloading':
      // The release name is operator detail; requesters get a generic line so the
      // torrent title never surfaces in the requester lens.
      return auth.canViewJobs
        ? job.value?.candidateTitle || 'Downloading'
        : `Getting ${props.movie.title}`
    case 'proposed':
      return 'A download suggestion is waiting for review'
    case 'requested':
      return 'Your request is awaiting approval'
    case 'unobtainable':
      return 'Not available yet'
    case 'notInLibrary':
      return 'Not in your library'
    case 'wanted':
      return wantHeadline(want.value)
    default:
      return ''
  }
})

const subline = computed(() => {
  switch (state.value) {
    case 'available':
      return 'Available to watch'
    case 'downloading':
      return 'Downloading now'
    case 'proposed':
      return 'A release was found and is awaiting approval.'
    case 'requested':
      return myRequest.value?.tier ?? ''
    case 'unobtainable':
      return unobtainableLine.value
    case 'notInLibrary':
      return props.movie.releaseDate ? `Released ${formatDate(props.movie.releaseDate)}` : ''
    case 'wanted': {
      const n = want.value?.attemptCount ?? 0
      return n > 1 ? `Attempt ${n}` : ''
    }
    default:
      return ''
  }
})

function wantHeadline(w: Want | null): string {
  if (!w) return 'Working on it'
  if (w.hold === 'needs_pick' && (w.status === 'pending' || w.status === 'searching')) {
    return 'Waiting for a release to be chosen'
  }
  switch (w.status) {
    case 'pending':
      return 'Queued to search'
    case 'searching':
      return 'Searching for a release'
    case 'grabbed':
      return 'Release grabbed — starting download'
    case 'imported':
      return 'Adding to your library'
    case 'failed':
      return 'Last search failed'
    case 'canceled':
      return 'Acquisition canceled'
    default:
      return 'Working on it'
  }
}

const wantError = computed(() => (state.value === 'wanted' ? (want.value?.lastError ?? '') : ''))

const downloadPct = computed(() => Math.round((job.value?.progress ?? 0) * 100))
const downloadMeta = computed(() => {
  const j = job.value
  if (!j) return ''
  // Requesters get progress + ETA only — the agreed floor. Speed and total size
  // are operator detail, so they're dropped from the requester line.
  if (!auth.canViewJobs) {
    return [`${downloadPct.value}%`, formatEta(j.etaSeconds)].filter(Boolean).join(' · ')
  }
  return [
    `${downloadPct.value}%`,
    formatSpeed(j.downloadSpeed),
    formatEta(j.etaSeconds),
    formatBytes(j.totalSize),
  ]
    .filter(Boolean)
    .join(' · ')
})

// "In theaters <date> · digital release <date/'not yet announced'>", falling back
// to the primary release date when no per-type dates are known.
const unobtainableLine = computed(() => {
  const rd = props.movie.releaseDates
  const parts: string[] = []
  if (rd?.theatrical) parts.push(`In theaters ${formatDate(rd.theatrical)}`)
  if (rd?.digital) parts.push(`digital release ${formatDate(rd.digital)}`)
  else if (rd?.theatrical) parts.push('digital release not yet announced')
  const line = parts.join(' · ')
  if (line) return line.charAt(0).toUpperCase() + line.slice(1)
  return props.movie.releaseDate
    ? `Expected ${formatDate(props.movie.releaseDate)}`
    : 'Release date not yet announced'
})

function formatDate(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso.includes('T') ? iso : iso + 'T00:00:00')
  if (isNaN(d.getTime())) return iso
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}
</script>

<template>
  <div v-if="state" class="rounded-lg border bg-card p-4">
    <div class="flex items-start gap-3">
      <component :is="icon" class="mt-0.5 size-5 shrink-0 text-muted-foreground" />
      <div class="min-w-0 flex-1">
        <p class="truncate font-medium">{{ headline }}</p>
        <p v-if="subline" class="mt-0.5 text-sm text-muted-foreground">{{ subline }}</p>

        <div v-if="state === 'downloading'" class="mt-3 space-y-1.5">
          <Progress :model-value="downloadPct" class="h-1.5" />
          <p class="text-xs tabular-nums text-muted-foreground">{{ downloadMeta }}</p>
        </div>

        <p v-if="wantError" class="mt-1 text-xs text-destructive">{{ wantError }}</p>
      </div>
    </div>
  </div>
</template>
