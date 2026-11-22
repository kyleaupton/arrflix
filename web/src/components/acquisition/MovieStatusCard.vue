<script setup lang="ts">
import { computed } from 'vue'
import type { MovieDetail } from '@/client/types.gen'
import { Progress } from '@/components/ui/progress'
import { useTitleStatus } from '@/composables/useTitleStatus'
import { present, headlineOf } from '@/lib/titleStatus'
import { formatSpeed, formatEta } from '@/lib/format'
import { useAuthStore } from '@/stores/auth'

// Tells a movie's acquisition story so the page's main column is never empty.
//
// The state, the counts, and what the viewer may do about it all arrive derived
// from the server. This component chooses words and pixels; it does not decide
// what is happening. The only thing it reads off the movie itself is release
// dates, which are metadata rather than acquisition state — see releaseLine.
const props = defineProps<{ movie: MovieDetail }>()

const auth = useAuthStore()
const tmdbId = computed(() => props.movie.tmdbId)

const { status, progress } = useTitleStatus('movie', tmdbId)

const state = computed(() => status.value?.state ?? null)
const presentation = computed(() => present(state.value))
const headline = computed(() => headlineOf(state.value))

// Once the movie is on disk the operator reads the Local Files table instead, so
// their card would only repeat it. Requesters have no such table, so they keep a
// plain confirmation that it landed.
const hidden = computed(() => state.value === 'available' && auth.canViewJobs)

const subline = computed(() => {
  switch (state.value) {
    case 'available':
      return 'Available to watch'
    case 'unreleased':
      return waitLine.value
    case 'not_requested':
      return releaseLine.value
    default:
      return ''
  }
})

const theatrical = computed(() => props.movie.releaseDates?.theatrical || props.movie.releaseDate)

// A requested film that isn't out yet says when it's expected. The projection
// carries the home-release date only when one is announced; otherwise the
// cinema release is all there is to say, and no date is invented for home.
const waitLine = computed(() => {
  const home = status.value?.homeReleaseDate
  if (home) return `Expected ${dayMonth(home)}`
  const cinema = theatrical.value
  if (cinema && isoDate(cinema) > isoDaysAgo(0)) {
    return `In cinemas ${dayMonth(cinema)} · home release not announced yet`
  }
  return 'In cinemas · home release not announced yet'
})

// Release dates are TMDB metadata the page already holds, not acquisition state,
// so rendering them here is copy rather than a second derivation. A film whose
// cinema release is more than a year old is back catalogue: its home release is
// long settled (the same 365-day window the backend uses to drop reissues), so
// it reads as a plain dated release instead.
const releaseLine = computed(() => {
  const rd = props.movie.releaseDates
  const cinema = theatrical.value
  if (cinema && isoDate(cinema) < isoDaysAgo(365)) {
    return `Released ${formatDate(isoDate(cinema))}`
  }
  if (rd?.theatrical) {
    const home = rd.digital
      ? `digital release ${dayMonth(rd.digital)}`
      : 'home release not announced yet'
    return `In cinemas ${dayMonth(rd.theatrical)} · ${home}`
  }
  return props.movie.releaseDate ? `Released ${formatDate(props.movie.releaseDate)}` : ''
})

const downloadPct = computed(() => Math.round((progress.value?.progress ?? 0) * 100))

// Requesters get percentage and ETA — the agreed floor. Transfer speed is
// operator detail and is dropped from their line.
const downloadMeta = computed(() => {
  const p = progress.value
  if (!p) return ''
  const parts = [`${downloadPct.value}%`]
  if (auth.canViewJobs) parts.push(formatSpeed(p.bytesPerSecond))
  parts.push(formatEta(p.etaSeconds))
  return parts.filter(Boolean).join(' · ')
})

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// isoDate reduces a date or timestamp to its YYYY-MM-DD part. Release dates are
// calendar days, so they are read as written rather than shifted into the
// viewer's time zone.
function isoDate(iso: string): string {
  return iso.slice(0, 10)
}

// isoDaysAgo gives the viewer's local calendar day `days` before today.
function isoDaysAgo(days: number): string {
  const d = new Date()
  d.setDate(d.getDate() - days)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

// dayMonth renders a release date as "12 Sep".
function dayMonth(iso: string): string {
  const [, month, day] = isoDate(iso).split('-').map(Number)
  if (!month || !day || month > 12) return iso
  return `${day} ${MONTHS[month - 1]}`
}

function formatDate(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso.includes('T') ? iso : iso + 'T00:00:00')
  if (isNaN(d.getTime())) return iso
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}
</script>

<template>
  <div v-if="state && !hidden" class="rounded-lg border bg-card p-4">
    <div class="flex items-start gap-3">
      <component :is="presentation.icon" class="mt-0.5 size-5 shrink-0 text-muted-foreground" />
      <div class="min-w-0 flex-1">
        <p class="truncate font-medium">{{ headline }}</p>
        <p v-if="subline" class="mt-0.5 text-sm text-muted-foreground">{{ subline }}</p>

        <div v-if="progress" class="mt-3 space-y-1.5">
          <Progress :model-value="downloadPct" class="h-1.5" />
          <p class="text-xs tabular-nums text-muted-foreground">{{ downloadMeta }}</p>
        </div>
      </div>
    </div>
  </div>
</template>
