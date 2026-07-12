<template>
  <div class="flex flex-wrap items-center gap-3">
    <!-- Tracked: a series has one want per in-scope episode, so there is no
         single pill to show — surface the tracking state and an available count.
         Everything you do *to* the tracking lives in the kebab. -->
    <template v-if="isTracked">
      <Badge variant="secondary" class="gap-1">
        <Check class="size-3" />
        Tracking
      </Badge>
      <span class="text-sm text-muted-foreground">
        {{ availableCount }} / {{ totalCount }} available
      </span>

      <TrackingActionsMenu
        v-if="trackingId && auth.canManageJobs"
        type="series"
        :tmdb-id="tmdbId"
        :tracking-id="trackingId"
        :autonomy-backfill="tracking?.tracking?.autonomyBackfill"
        :autonomy-ongoing="tracking?.tracking?.autonomyOngoing"
      />
    </template>

    <!-- A genuine (non-404) load failure: 404 is the untracked signal, anything
         else is a real error worth showing rather than offering a stale Add. -->
    <p v-else-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>

    <!-- Not tracked, but the caller has a pending request: show its status
         read-only. Withdrawal lives on the /requests page. -->
    <template v-else-if="!isLoading && myPending">
      <RequestStatusPill :status="myPending.status" />
    </template>

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
import { useQuery } from '@tanstack/vue-query'
import { Check, Plus } from 'lucide-vue-next'
import { trackingByTmdbOptions } from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { useModal } from '@/composables/useModal'
import { useAuthStore } from '@/stores/auth'
import TrackSeriesDialog from '@/components/modals/TrackSeriesDialog.vue'
import RequestStatusPill from './RequestStatusPill.vue'
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

// Acquisition status for this series: the tracking + its per-episode wants, plus
// the caller's own pending request. Untracked / un-requested is a normal 200 with
// null fields (not a 404), read directly by the branches below.
const {
  data: tracking,
  isLoading,
  error,
} = useQuery(
  computed(() =>
    trackingByTmdbOptions({ path: { tmdbId: props.tmdbId }, query: { type: 'series' } }),
  ),
)

const isTracked = computed(() => !!tracking.value?.tracking)

// Present only when tracked — gates the overflow menu.
const trackingId = computed(() => tracking.value?.tracking?.id ?? null)

// The caller's own pending request rides along on the status payload, so the
// pending badge needs no separate request-list fetch. The endpoint returns only a
// pending request (denied/canceled never surface here), so a re-request stays open.
const myPending = computed(() => tracking.value?.myRequest ?? null)

const availableCount = computed(() => props.availableCount ?? 0)
const totalCount = computed(() => props.totalCount ?? 0)

// Series picks its tier inside the track dialog, so the hero label keys off the
// default HD grant rather than a selected tier.
const primaryLabel = computed(() =>
  auth.canAutoApprove('series', 'HD') ? 'Add to Library' : 'Request',
)

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
      isOperator: auth.canAutoApprove('series', 'HD'),
    },
  })
}

// Untracked is a normal 200, so any error here is a genuine load failure worth
// showing rather than offering a stale Add.
const loadError = computed(() =>
  isProblem(error.value) ? problemMessage(error.value, 'Failed to load tracking state') : null,
)
</script>
