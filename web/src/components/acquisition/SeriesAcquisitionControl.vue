<template>
  <div class="flex flex-wrap items-center gap-3">
    <!-- Tracked: a series has one want per in-scope episode, so there is no
         single pill to show — surface the tracking state, an available count,
         and a stop control instead. -->
    <template v-if="isTracked">
      <Badge variant="secondary" class="gap-1">
        <Check class="size-3" />
        Tracking
      </Badge>
      <span class="text-sm text-muted-foreground">
        {{ availableCount }} / {{ totalCount }} available
      </span>
      <Button variant="outline" :disabled="cancelTracking.isPending.value" @click="handleStop">
        <X class="mr-2 size-4" />
        {{ cancelTracking.isPending.value ? 'Stopping…' : 'Stop tracking' }}
      </Button>

      <!-- Acquisition autonomy, dialed per segment: back-catalog (episodes aired
           before tracking began) and new episodes (aired after). 'I'll pick'
           holds that segment's wants for manual download instead of auto-search. -->
      <label class="flex items-center gap-1.5 text-sm text-muted-foreground">
        Back-catalog
        <Select v-model="autonomyBackfill" :disabled="setAutonomy.isPending.value">
          <SelectTrigger class="w-32" aria-label="Back-catalog autonomy">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="auto">Automatic</SelectItem>
            <SelectItem value="propose">Suggest first</SelectItem>
            <SelectItem value="manual">I'll pick</SelectItem>
          </SelectContent>
        </Select>
      </label>
      <label class="flex items-center gap-1.5 text-sm text-muted-foreground">
        New episodes
        <Select v-model="autonomyOngoing" :disabled="setAutonomy.isPending.value">
          <SelectTrigger class="w-32" aria-label="New episodes autonomy">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="auto">Automatic</SelectItem>
            <SelectItem value="propose">Suggest first</SelectItem>
            <SelectItem value="manual">I'll pick</SelectItem>
          </SelectContent>
        </Select>
      </label>

      <!-- Overflow actions — retry re-drives this series' wants. -->
      <TrackingActionsMenu v-if="trackingId" :tracking-id="trackingId" />
    </template>

    <!-- A genuine (non-404) load failure: 404 is the untracked signal, anything
         else is a real error worth showing rather than offering a stale Add. -->
    <p v-else-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>

    <!-- Not tracked: open the track dialog, where quality, scope, and per-segment
         autonomy are chosen before anything is added. The button face depends on
         whether this user auto-approves. -->
    <template v-else-if="!isLoading">
      <Button @click="openTrackDialog">
        <Plus class="mr-2 size-4" />
        {{ primaryLabel }}
      </Button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Check, Plus, X } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  trackingByTmdbOptions,
  trackingByTmdbQueryKey,
  trackingCancelMutation,
  trackingSetAutonomyMutation,
} from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useModal } from '@/composables/useModal'
import { useAuthStore } from '@/stores/auth'
import TrackSeriesDialog from '@/components/modals/TrackSeriesDialog.vue'
import TrackingActionsMenu from './TrackingActionsMenu.vue'
import { isProblem, problemMessage } from '@/lib/api'

// availableCount/totalCount are computed by the parent from the already-loaded
// series detail. airedEpisodeCount / seasonCount / hasOngoing feed the track
// dialog's scope cards and decide which questions can apply (no back-catalog,
// or an ended series).
const props = defineProps<{
  tmdbId: number
  title: string
  availableCount?: number
  totalCount?: number
  airedEpisodeCount?: number
  seasonCount?: number
  hasOngoing?: boolean
}>()

const auth = useAuthStore()
const modal = useModal()
const queryClient = useQueryClient()

const trackingKey = computed(() =>
  trackingByTmdbQueryKey({ path: { tmdbId: props.tmdbId }, query: { type: 'series' } }),
)

// An expected 404 ("not tracked") is the common case for most series, so don't
// burn retries on it; the 404 is read as the untracked state below, not an error.
const {
  data: tracking,
  isLoading,
  error,
} = useQuery(
  computed(() => ({
    ...trackingByTmdbOptions({ path: { tmdbId: props.tmdbId }, query: { type: 'series' } }),
    retry: false,
  })),
)

const isTracked = computed(() => !!tracking.value?.tracking)

// Present only when tracked — gates the overflow menu.
const trackingId = computed(() => tracking.value?.tracking?.id ?? null)

const availableCount = computed(() => props.availableCount ?? 0)
const totalCount = computed(() => props.totalCount ?? 0)

const primaryLabel = computed(() => (auth.canAutoApproveMovie ? 'Add to Library' : 'Request'))

// The dialog owns quality/scope/autonomy selection and fires the create request
// atomically, so the chosen config lands before any search runs. It invalidates
// the tracking query itself on success.
function openTrackDialog() {
  modal.open(TrackSeriesDialog, {
    props: {
      tmdbId: props.tmdbId,
      title: props.title,
      airedEpisodeCount: props.airedEpisodeCount ?? 0,
      seasonCount: props.seasonCount ?? 0,
      hasOngoing: props.hasOngoing ?? false,
      isOperator: auth.canAutoApproveMovie,
    },
  })
}

const cancelTracking = useMutation({
  ...trackingCancelMutation(),
  onSuccess: () => {
    toast.success('Stopped tracking')
    queryClient.invalidateQueries({ queryKey: trackingKey.value })
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to stop tracking'))
  },
})

// Autonomy dials are writable computeds over the server value: reading returns
// the tracking's current setting; setting one fires the mutation with BOTH
// fields (the endpoint takes both segments), so the other keeps its current
// value. The server holds/releases the segment's wants; we invalidate to refetch.
const setAutonomy = useMutation({
  ...trackingSetAutonomyMutation(),
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: trackingKey.value })
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to update autonomy'))
  },
})

type Autonomy = 'auto' | 'propose' | 'manual'

function submitAutonomy(backfill: Autonomy, ongoing: Autonomy) {
  const id = tracking.value?.tracking?.id
  if (!id) return
  setAutonomy.mutate({ path: { id }, body: { backfill, ongoing } })
}

const autonomyBackfill = computed<Autonomy>({
  get: () => (tracking.value?.tracking?.autonomyBackfill as Autonomy) ?? 'auto',
  set: (val) => submitAutonomy(val, autonomyOngoing.value),
})

const autonomyOngoing = computed<Autonomy>({
  get: () => (tracking.value?.tracking?.autonomyOngoing as Autonomy) ?? 'auto',
  set: (val) => submitAutonomy(autonomyBackfill.value, val),
})

async function handleStop() {
  const id = tracking.value?.tracking?.id
  if (!id) return
  const confirmed = await modal.confirm({
    title: 'Stop tracking',
    message:
      'Stop acquiring new episodes and cancel any in-flight downloads? Already-downloaded episodes are kept.',
    severity: 'danger',
  })
  if (!confirmed) return
  cancelTracking.mutate({ path: { id } })
}

// A 404 is the untracked signal, not an error; surface anything else.
const loadError = computed(() =>
  isProblem(error.value) && error.value.status !== 404
    ? problemMessage(error.value, 'Failed to load tracking state')
    : null,
)
</script>
