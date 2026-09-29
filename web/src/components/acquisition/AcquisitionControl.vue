<template>
  <div class="flex flex-wrap items-center gap-3">
    <!-- The ask. Present only when the viewer may make it: a tier they hold, on
         a title that still affords asking. The label reflects what pressing it
         actually does, which the server already decided per tier. -->
    <Button
      v-if="requestAction"
      class="w-full sm:w-auto"
      :disabled="createRequest.isPending.value"
      @click="handleRequest"
    >
      <Plus class="mr-2 size-4" />
      {{ createRequest.isPending.value ? pendingLabel : requestLabel }}
    </Button>

    <!-- Otherwise the state speaks for itself. -->
    <TitleStatusPill v-else-if="status" :state="status.state" :progress="progress?.progress" />

    <p v-else-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>

    <!-- Withdrawal of the viewer's own request. Disabled carries its reason —
         the action is only sent at all when it needs to explain itself. -->
    <Button
      v-if="cancelAction"
      variant="outline"
      :disabled="!cancelAction.enabled || cancelRequest.isPending.value"
      :title="cancelAction.disabledReason || undefined"
      @click="handleCancel"
    >
      {{ cancelRequest.isPending.value ? 'Withdrawing…' : 'Withdraw request' }}
    </Button>

    <!-- Overflow actions are all operator actions (manual search, retry, cancel),
         so the whole menu is operator-only — shown even when untracked so manual
         search stays reachable before anything is added. -->
    <TrackingActionsMenu
      v-if="auth.canManageJobs"
      type="movie"
      :tmdb-id="tmdbId"
      :tracking-id="trackingId"
      :want="want"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useQuery, useMutation } from '@tanstack/vue-query'
import { Plus } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  trackingByTmdbOptions,
  requestsCreateMutation,
  requestsCancelMutation,
} from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'
import { useModal } from '@/composables/useModal'
import { useTitleStatus } from '@/composables/useTitleStatus'
import { useTitleInvalidation } from '@/composables/useTitleInvalidation'
import { problemMessage } from '@/lib/api'
import AddMovieDialog from '@/components/modals/AddMovieDialog.vue'
import TitleStatusPill from './TitleStatusPill.vue'
import TrackingActionsMenu from './TrackingActionsMenu.vue'

// tmdbId doubles as the movie route id the download-jobs endpoint is keyed on;
// title feeds the AddMovieDialog header.
const props = defineProps<{ tmdbId: number; title: string }>()

const auth = useAuthStore()
const modal = useModal()
const tmdbId = computed(() => props.tmdbId)

// The projection answers all three questions this control used to join four
// caches to answer: what is happening, whether this viewer may ask, and what
// asking will do.
const { status, progress, requestAction, cancelAction, error } = useTitleStatus('movie', tmdbId)
const invalidateTitle = useTitleInvalidation('movie', tmdbId)

const tiers = computed(() => requestAction.value?.tiers ?? [])

// One tier leaves nothing to decide, so the button states the outcome directly.
// More than one is a genuine choice and the verb belongs in the dialog, where
// the tier is picked — approval is per tier, so the two cannot be settled apart.
const requestLabel = computed(() => {
  if (tiers.value.length > 1) return 'Add to Library'
  return requestAction.value?.requiresApproval ? 'Request' : 'Add to Library'
})
const pendingLabel = computed(() =>
  requestAction.value?.requiresApproval ? 'Requesting…' : 'Adding…',
)

// Still read for the operator kebab, which acts on the want and tracking rows
// themselves rather than on the title's state.
const { data: tracking } = useQuery(
  computed(() => trackingByTmdbOptions({ path: { tmdbId: props.tmdbId } })),
)
const want = computed(() => tracking.value?.wants?.[0] ?? null)
const trackingId = computed(() => tracking.value?.tracking?.id ?? null)

const createRequest = useMutation({
  ...requestsCreateMutation(),
  onSuccess: (req) => {
    // Two faces of one endpoint: a spawned tracking means the request
    // auto-approved and is already searching; otherwise it awaits approval.
    toast.success(req.spawnedTrackingId ? 'Added — searching now' : 'Requested — pending approval')
    invalidateTitle()
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to add to library'))
  },
})

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

function handleRequest() {
  const options = tiers.value
  if (options.length > 1) {
    modal.open(AddMovieDialog, {
      props: { tmdbId: props.tmdbId, title: props.title, tiers: options },
    })
    return
  }
  createRequest.mutate({
    body: { tmdbId: props.tmdbId, type: 'movie', tier: options[0]?.tier ?? 'HD' },
  })
}

function handleCancel() {
  const id = status.value?.viewer.requestId
  if (!id) return
  cancelRequest.mutate({ path: { id } })
}

const loadError = computed(() =>
  error.value ? problemMessage(error.value, 'Failed to load acquisition state') : null,
)
</script>
