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
    case 'not_requested':
      return releaseLine.value
    default:
      return ''
  }
})

// Release dates are TMDB metadata the page already holds, not acquisition state,
// so rendering them here is copy rather than a second derivation.
//
// They also stand in for a state the projection cannot reach yet: no obtainable
// date is persisted for a movie, so an unreleased film derives as not_requested
// rather than unreleased (REQ-UNREL-003). Until that date exists server-side,
// this line is what tells a user the film simply isn't out.
const releaseLine = computed(() => {
  const rd = props.movie.releaseDates
  const parts: string[] = []
  if (rd?.theatrical) parts.push(`In theaters ${formatDate(rd.theatrical)}`)
  if (rd?.digital) parts.push(`digital release ${formatDate(rd.digital)}`)
  else if (rd?.theatrical) parts.push('digital release not yet announced')
  const line = parts.join(' · ')
  if (line) return line.charAt(0).toUpperCase() + line.slice(1)
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
