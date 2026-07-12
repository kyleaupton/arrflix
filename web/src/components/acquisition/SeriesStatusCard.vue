<script setup lang="ts">
import { computed, type Component } from 'vue'
import { Check, Clock, Download, Search } from 'lucide-vue-next'
import type { Request, Tracking } from '@/client/types.gen'
import { Progress } from '@/components/ui/progress'

// The requester-facing acquisition story for a series — the sibling of
// MovieStatusCard, sitting atop the left column so a requester always sees what's
// happening without decoding the per-episode pills in the accordion. Visible to
// everyone; AttentionCard is the operator counterpart directly below it.
//
// Pure presentation: Series.vue owns the tracking query and the episode/job
// aggregation and passes the derived counts in. The card renders nothing when
// there is no story to tell — untracked with no pending request, or a complete +
// ended series — since the hero and accordion already carry those.
const props = defineProps<{
  tracking: Tracking | null
  myRequest: Request | null
  ongoing: boolean
  availableCount: number
  totalCount: number
  airedTotalCount: number
  // Jobs in flight (season packs + singles) — drives the download-vs-search icon.
  activeDownloadCount: number
  // Episodes in flight, counted from the wants — what the subline reports, since a
  // requester thinks in episodes rather than the packs behind activeDownloadCount.
  episodesDownloading: number
}>()

type CardState = 'requested' | 'inProgress' | 'caughtUp'

// Compared against aired (not total) episodes: an ongoing series with episodes
// still to air should read as caught up once everything aired is on disk, not
// perpetually in-progress against a denominator it can't yet reach.
const allAiredOnDisk = computed(() => props.availableCount >= props.airedTotalCount)

const state = computed<CardState | null>(() => {
  if (!props.tracking) {
    // The endpoint only surfaces a *pending* request, but guard anyway.
    return props.myRequest?.status === 'pending' ? 'requested' : null
  }
  if (allAiredOnDisk.value) {
    // Complete + ended has no story worth a persistent card; ongoing is "watching".
    return props.ongoing ? 'caughtUp' : null
  }
  return 'inProgress'
})

const downloading = computed(() => props.activeDownloadCount > 0)

const icon = computed<Component>(() => {
  switch (state.value) {
    case 'requested':
      return Clock
    case 'caughtUp':
      return Check
    default:
      return downloading.value ? Download : Search
  }
})

const headline = computed(() => {
  switch (state.value) {
    case 'requested':
      return 'Your request is awaiting approval'
    case 'caughtUp':
      return 'Up to date'
    case 'inProgress':
      return downloading.value ? 'Getting your series' : 'Looking for your episodes'
    default:
      return ''
  }
})

// Only the two scope presets are ever created; anything else falls through blank.
const scopeLabel = computed(() => {
  switch (props.myRequest?.scopeRule) {
    case 'future_only':
      return 'New episodes only'
    case 'all':
      return 'Full series'
    default:
      return ''
  }
})

const subline = computed(() => {
  switch (state.value) {
    case 'requested':
      return [scopeLabel.value, props.myRequest?.tier].filter(Boolean).join(' · ')
    case 'caughtUp':
      switch (props.tracking?.autonomyOngoing) {
        case 'auto':
          return 'New episodes are added automatically.'
        case 'propose':
          return 'New episodes will be suggested for approval.'
        default:
          return 'Watching for new episodes.'
      }
    case 'inProgress':
      return [
        props.episodesDownloading > 0
          ? `${props.episodesDownloading} episode${props.episodesDownloading === 1 ? '' : 's'} downloading`
          : null,
        `${props.availableCount} of ${props.totalCount} available`,
      ]
        .filter(Boolean)
        .join(' · ')
    default:
      return ''
  }
})

// The bar means library completeness (available/total), not download speed — it
// answers "how much of my series do I have?". Only the in-progress state shows it;
// caught-up would sit below 100% for an ongoing series and read as contradictory.
const showProgress = computed(() => state.value === 'inProgress')
const progressPct = computed(() =>
  props.totalCount > 0 ? Math.round((props.availableCount / props.totalCount) * 100) : 0,
)
</script>

<template>
  <div v-if="state" class="rounded-lg border bg-card p-4">
    <div class="flex items-start gap-3">
      <component :is="icon" class="mt-0.5 size-5 shrink-0 text-muted-foreground" />
      <div class="min-w-0 flex-1">
        <p class="truncate font-medium">{{ headline }}</p>
        <p v-if="subline" class="mt-0.5 text-sm text-muted-foreground">{{ subline }}</p>

        <Progress v-if="showProgress" :model-value="progressPct" class="mt-3 h-1.5" />
      </div>
    </div>
  </div>
</template>
