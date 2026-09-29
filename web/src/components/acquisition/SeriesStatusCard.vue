<script setup lang="ts">
import { computed } from 'vue'
import { Check, Search } from 'lucide-vue-next'
import type { Tracking } from '@/client/types.gen'
import { Progress } from '@/components/ui/progress'
import { useTitleStatus } from '@/composables/useTitleStatus'
import { present, headlineOf } from '@/lib/titleStatus'

// The requester-facing acquisition story for a series — the sibling of
// MovieStatusCard, sitting atop the left column so a requester always sees what's
// happening without decoding the per-episode pills in the accordion. Visible to
// everyone; AttentionCard is the operator counterpart directly below it.
//
// `tracking` is the automation configuration, not acquisition state — it answers
// "what will happen to future episodes", which no amount of current state can
// tell you. Everything else comes from the projection.
const props = defineProps<{
  tmdbId: number
  tracking: Tracking | null
  // Whether the series may still get new episodes, from its TMDB status.
  ongoing: boolean
}>()

const tmdbId = computed(() => props.tmdbId)
const { status } = useTitleStatus('series', tmdbId)

const state = computed(() => status.value?.state ?? null)
const counts = computed(() => status.value?.counts)
const intent = computed(() => status.value?.viewer.intent)

// A series that has everything it is trying to get is caught up rather than
// finished — an ongoing one is still watching for more. The distinction is the
// series' TMDB status, which the projection has no view of.
const caughtUp = computed(() => state.value === 'available')

// Nothing to say: an untracked series with no request, or a complete and ended
// one. The hero and the accordion already carry those.
const hidden = computed(() => {
  if (!state.value) return true
  if (state.value === 'not_requested') return true
  return caughtUp.value && !props.ongoing
})

const icon = computed(() => (caughtUp.value ? Check : present(state.value).icon))

const headline = computed(() => {
  if (caughtUp.value) return 'Up to date'
  if (state.value === 'searching') return 'Looking for your episodes'
  return headlineOf(state.value)
})

// Only the two scope presets are ever created; anything else falls through blank.
const scopeLabel = computed(() => {
  switch (intent.value?.scopeRule) {
    case 'future_only':
      return 'New episodes only'
    case 'all':
      return 'Full series'
    default:
      return ''
  }
})

const subline = computed(() => {
  if (caughtUp.value) {
    switch (props.tracking?.autonomyOngoing) {
      case 'auto':
        return 'New episodes are added automatically.'
      case 'propose':
        return 'New episodes will be suggested for approval.'
      default:
        return 'Watching for new episodes.'
    }
  }
  if (state.value === 'awaiting_approval') {
    return [scopeLabel.value, intent.value?.tier].filter(Boolean).join(' · ')
  }
  const c = counts.value
  if (!c) return ''
  return [
    c.working > 0 ? `${c.working} episode${c.working === 1 ? '' : 's'} in flight` : null,
    `${c.available} of ${c.total} available`,
  ]
    .filter(Boolean)
    .join(' · ')
})

// The bar means library completeness (available/total), not download speed — it
// answers "how much of my series do I have?". Caught-up hides it: it would sit
// below 100% for an ongoing series and read as contradictory.
const showProgress = computed(() => !caughtUp.value && (counts.value?.total ?? 0) > 0)
const progressPct = computed(() => {
  const c = counts.value
  if (!c?.total) return 0
  return Math.round((c.available / c.total) * 100)
})

const iconComponent = computed(() => icon.value ?? Search)
</script>

<template>
  <div v-if="!hidden" class="rounded-lg border bg-card p-4">
    <div class="flex items-start gap-3">
      <component :is="iconComponent" class="mt-0.5 size-5 shrink-0 text-muted-foreground" />
      <div class="min-w-0 flex-1">
        <p class="truncate font-medium">{{ headline }}</p>
        <p v-if="subline" class="mt-0.5 text-sm text-muted-foreground">{{ subline }}</p>

        <Progress v-if="showProgress" :model-value="progressPct" class="mt-3 h-1.5" />
      </div>
    </div>
  </div>
</template>
