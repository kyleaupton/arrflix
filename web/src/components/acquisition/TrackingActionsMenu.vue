<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button variant="outline" size="icon" aria-label="More actions">
        <Ellipsis class="size-4" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" :side-offset="8" class="w-56">
      <!-- Series-only: per-segment acquisition autonomy lives in a dialog. -->
      <DropdownMenuItem v-if="showAutomation" @click="openAutomation">
        <SlidersHorizontal class="mr-2 size-4" />
        Automation…
      </DropdownMenuItem>

      <!-- Movie-only: pick a specific release yourself. -->
      <DropdownMenuItem v-if="showManualSearch" @click="openManualSearch">
        <Search class="mr-2 size-4" />
        Search manually
      </DropdownMenuItem>

      <DropdownMenuItem v-if="trackingId" :disabled="retry.isPending.value" @click="handleRetry">
        <RotateCw class="mr-2 size-4" />
        {{ retry.isPending.value ? 'Retrying…' : 'Retry search' }}
      </DropdownMenuItem>

      <!-- Metadata refresh has no endpoint yet; surfaced disabled so the menu
           reads as the eventual home for it. -->
      <DropdownMenuItem disabled>
        <RefreshCw class="mr-2 size-4" />
        Refresh metadata
        <DropdownMenuShortcut>Soon</DropdownMenuShortcut>
      </DropdownMenuItem>

      <template v-if="showCancel || showStop">
        <DropdownMenuSeparator />
        <DropdownMenuItem
          v-if="showCancel"
          variant="destructive"
          :disabled="cancelWant.isPending.value"
          @click="handleCancel"
        >
          <X class="mr-2 size-4" />
          {{ cancelWant.isPending.value ? 'Canceling…' : 'Cancel acquisition' }}
        </DropdownMenuItem>
        <DropdownMenuItem
          v-if="showStop"
          variant="destructive"
          :disabled="cancelTracking.isPending.value"
          @click="handleStop"
        >
          <X class="mr-2 size-4" />
          {{ cancelTracking.isPending.value ? 'Stopping…' : 'Stop tracking' }}
        </DropdownMenuItem>
      </template>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { Ellipsis, RotateCw, RefreshCw, Search, SlidersHorizontal, X } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  trackingRetryMutation,
  trackingCancelMutation,
  wantsCancelMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { Want } from '@/client/types.gen'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useModal } from '@/composables/useModal'
import { useAuthStore } from '@/stores/auth'
import { problemMessage } from '@/lib/api'
import DownloadCandidatesDialog from '@/components/download-candidates/DownloadCandidatesDialog.vue'
import AutomationDialog from './AutomationDialog.vue'

type Autonomy = 'auto' | 'propose' | 'manual'

// The single kebab behind both hero controls. It hosts everything you do *to* a
// tracking; which items apply is decided by media type and whether the thing is
// tracked yet. `want` (movie) drives the Cancel affordance; the autonomy fields
// (series) seed the Automation dialog. `trackingId` may be absent — an untracked
// movie still offers admin manual search.
const props = defineProps<{
  type: 'movie' | 'series'
  tmdbId: number
  trackingId?: string | null
  autonomyBackfill?: string
  autonomyOngoing?: string
  want?: Want | null
}>()

const auth = useAuthStore()
const modal = useModal()
const queryClient = useQueryClient()

const showAutomation = computed(() => props.type === 'series' && !!props.trackingId)
const showManualSearch = computed(() => props.type === 'movie' && auth.isAdmin)
const showStop = computed(() => props.type === 'series' && !!props.trackingId)

// Cancel is offered only while the movie's want is still in flight; terminal
// states ('available', 'failed', 'canceled') have nothing to stop.
const CANCELABLE_STATUSES = new Set(['pending', 'searching', 'grabbed', 'downloading', 'imported'])
const showCancel = computed(
  () => props.type === 'movie' && !!props.want && CANCELABLE_STATUSES.has(props.want.status),
)

function openAutomation() {
  if (!props.trackingId) return
  modal.open(AutomationDialog, {
    props: {
      trackingId: props.trackingId,
      backfill: (props.autonomyBackfill as Autonomy) ?? 'auto',
      ongoing: (props.autonomyOngoing as Autonomy) ?? 'auto',
    },
  })
}

function openManualSearch() {
  modal.open(DownloadCandidatesDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-4xl lg:max-w-6xl',
      movieId: props.tmdbId,
    },
  })
}

// Retry re-arms this tracking's terminal wants and nudges its pending ones to
// search now. The count comes back so the toast can distinguish a real re-drive
// from a no-op (everything already in flight or available).
const retry = useMutation({
  ...trackingRetryMutation(),
  onSuccess: (res) => {
    toast.success(
      res.retried > 0
        ? `Searching now — re-armed ${res.retried} ${res.retried === 1 ? 'want' : 'wants'}`
        : 'Nothing to retry',
    )
    // Partial-match every trackingByTmdb reader (the acquisition control's want
    // state and the series episode grid) so the re-armed statuses show at once.
    queryClient.invalidateQueries({ queryKey: [{ _id: 'trackingByTmdb' }] })
  },
  onError: (err) => toast.error(problemMessage(err, 'Failed to retry')),
})

function handleRetry() {
  if (!props.trackingId) return
  retry.mutate({ path: { id: props.trackingId } })
}

const cancelTracking = useMutation({
  ...trackingCancelMutation(),
  onSuccess: () => {
    toast.success('Stopped tracking')
    queryClient.invalidateQueries({ queryKey: [{ _id: 'trackingByTmdb' }] })
  },
  onError: (err) => toast.error(problemMessage(err, 'Failed to stop tracking')),
})

async function handleStop() {
  if (!props.trackingId) return
  const confirmed = await modal.confirm({
    title: 'Stop tracking',
    message:
      'Stop acquiring new episodes and cancel any in-flight downloads? Already-downloaded episodes are kept.',
    severity: 'danger',
  })
  if (!confirmed) return
  cancelTracking.mutate({ path: { id: props.trackingId } })
}

const cancelWant = useMutation({
  ...wantsCancelMutation(),
  onSuccess: () => {
    toast.success('Canceled')
    queryClient.invalidateQueries({ queryKey: [{ _id: 'trackingByTmdb' }] })
  },
  onError: (err) => toast.error(problemMessage(err, 'Failed to cancel')),
})

async function handleCancel() {
  if (!props.want) return
  const confirmed = await modal.confirm({
    title: 'Cancel acquisition',
    message: 'Stop searching/downloading and mark this movie canceled?',
    severity: 'danger',
  })
  if (!confirmed) return
  cancelWant.mutate({ path: { id: props.want.id } })
}
</script>
