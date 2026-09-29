<template>
  <div class="flex flex-wrap items-center gap-3">
    <!-- The ask. A series always affords asking for more, so this survives a
         fully-acquired one — later seasons are always askable. -->
    <Button v-if="requestAction" @click="openTrackDialog">
      <Plus class="mr-2 size-4" />
      {{ requestLabel }}
    </Button>

    <!-- Otherwise the state speaks for itself, with the count beside it: a series
         has one want per in-scope episode, so "how far along" is the useful
         second fact that a single state cannot carry. -->
    <template v-else-if="status">
      <TitleStatusPill :state="status.state" />
      <span v-if="status.counts.total" class="text-sm text-muted-foreground">
        {{ status.counts.available }} / {{ status.counts.total }} available
      </span>
    </template>

    <p v-else-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>

    <Button
      v-if="cancelAction"
      variant="outline"
      :disabled="!cancelAction.enabled || cancelRequest.isPending.value"
      :title="cancelAction.disabledReason || undefined"
      @click="handleCancel"
    >
      {{ cancelRequest.isPending.value ? 'Withdrawing…' : 'Withdraw request' }}
    </Button>

    <TrackingActionsMenu
      v-if="trackingId && auth.canManageJobs"
      type="series"
      :tmdb-id="tmdbId"
      :tracking-id="trackingId"
      :autonomy-backfill="tracking?.tracking?.autonomyBackfill"
      :autonomy-ongoing="tracking?.tracking?.autonomyOngoing"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useQuery, useMutation } from '@tanstack/vue-query'
import { Plus } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { trackingByTmdbOptions, requestsCancelMutation } from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import { useModal } from '@/composables/useModal'
import { useAuthStore } from '@/stores/auth'
import { useTitleStatus } from '@/composables/useTitleStatus'
import { useTitleInvalidation } from '@/composables/useTitleInvalidation'
import TrackSeriesDialog from '@/components/modals/TrackSeriesDialog.vue'
import TitleStatusPill from './TitleStatusPill.vue'
import TrackingActionsMenu from './TrackingActionsMenu.vue'
import { problemMessage } from '@/lib/api'

// airedEpisodeCount / seasonCount / hasOngoing feed the track dialog's scope
// cards and decide which questions can apply (no back-catalog, or an ended
// series). They are series metadata the page already holds.
const props = defineProps<{
  tmdbId: number
  title: string
  airedEpisodeCount?: number
  seasonCount?: number
  hasOngoing?: boolean
}>()

const auth = useAuthStore()
const modal = useModal()
const tmdbId = computed(() => props.tmdbId)

const { status, requestAction, cancelAction, error } = useTitleStatus('series', tmdbId)
const invalidateTitle = useTitleInvalidation('series', tmdbId)

// The tiers this viewer may actually pick, each labeled with whether choosing it
// needs a decision. Previously this control assumed the HD grant on both counts
// and the dialog offered 4K unconditionally, so a viewer without the 4K series
// grant could pick it and collect a 403.
const tiers = computed(() => requestAction.value?.tiers ?? [])

// With one tier the outcome is settled and the button can state it. With more
// than one it depends on which tier is picked, and the dialog is where that
// happens — approval is per tier, so the verb belongs next to the choice.
const requestLabel = computed(() => {
  if (tiers.value.length > 1) return 'Add to Library'
  return requestAction.value?.requiresApproval ? 'Request' : 'Add to Library'
})

// The automation configuration behind the kebab — a setting, not a state.
const { data: tracking } = useQuery(
  computed(() =>
    trackingByTmdbOptions({ path: { tmdbId: props.tmdbId }, query: { type: 'series' } }),
  ),
)
const trackingId = computed(() => tracking.value?.tracking?.id ?? null)

const cancelRequest = useMutation({
  ...requestsCancelMutation(),
  onSuccess: () => {
    toast.success('Request withdrawn')
    invalidateTitle()
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to withdraw request'))
  },
})

function handleCancel() {
  const id = status.value?.viewer.requestId
  if (!id) return
  cancelRequest.mutate({ path: { id } })
}

// The dialog owns quality/scope/autonomy selection and fires the create request
// atomically, so the chosen config lands before any search runs.
function openTrackDialog() {
  modal.open(TrackSeriesDialog, {
    props: {
      tmdbId: props.tmdbId,
      title: props.title,
      tiers: tiers.value,
      airedEpisodeCount: props.airedEpisodeCount ?? 0,
      seasonCount: props.seasonCount ?? 0,
      hasOngoing: props.hasOngoing ?? false,
    },
  })
}

const loadError = computed(() =>
  error.value ? problemMessage(error.value, 'Failed to load acquisition state') : null,
)
</script>
