<template>
  <div class="flex flex-wrap items-center gap-3">
    <!-- Tracked: the want's live state replaces the primary action. Cancel and
         manual search live in the kebab. -->
    <template v-if="want">
      <WantStatusPill
        :status="want.status"
        :attempt-count="want.attemptCount"
        :last-error="want.lastError"
        :progress="wantProgress"
        :hold="want.hold"
      />
    </template>

    <!-- A genuine (non-404) load failure: 404 is the untracked signal, anything
         else is a real error worth showing rather than offering a stale Add. -->
    <p v-else-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>

    <!-- Not tracked, but the caller has a pending request: show its status
         read-only. Withdrawal lives on the /requests page, keeping this control
         bounded to the add flow. -->
    <template v-else-if="!isLoading && myPending">
      <RequestStatusPill :status="myPending.status" />
    </template>

    <!-- Not tracked: initiate the autonomous flow. One held tier is a one-click
         add; more than one opens AddMovieDialog to pick quality. A user with no
         movie-create grant has no available tier and sees nothing here. -->
    <template v-else-if="!isLoading && availableTiers.length">
      <Button
        class="w-full sm:w-auto"
        :disabled="createRequest.isPending.value"
        @click="handlePrimary"
      >
        <Plus class="mr-2 size-4" />
        {{ createRequest.isPending.value ? 'Adding…' : primaryLabel }}
      </Button>
    </template>

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
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Plus } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  trackingByTmdbOptions,
  trackingByTmdbQueryKey,
  requestsCreateMutation,
  requestsListQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'
import { useModal } from '@/composables/useModal'
import { useDownloadJobs } from '@/composables/useDownloadJobs'
import { isProblem, problemMessage } from '@/lib/api'
import AddMovieDialog from '@/components/modals/AddMovieDialog.vue'
import WantStatusPill from './WantStatusPill.vue'
import RequestStatusPill from './RequestStatusPill.vue'
import TrackingActionsMenu from './TrackingActionsMenu.vue'

// tmdbId doubles as the movie route id the download-jobs endpoint is keyed on;
// title feeds the AddMovieDialog header.
const props = defineProps<{ tmdbId: number; title: string }>()

const auth = useAuthStore()
const queryClient = useQueryClient()
const modal = useModal()

// The caller sees only the tiers they hold a create grant for, so they can't pick
// one the API would 403. Empty for a user with no create grant — the add block is
// hidden in that case anyway.
const availableTiers = computed<('HD' | '4K')[]>(() =>
  (['HD', '4K'] as const).filter((t) => auth.can(`requests.create:movie:${t.toLowerCase()}`)),
)
// The one-click tier and the label's basis; the dialog resolves the precise tier
// when the caller holds more than one.
const defaultTier = computed<'HD' | '4K'>(() => availableTiers.value[0] ?? 'HD')

// Acquisition status for this movie: tracking + its want, plus the caller's own
// pending request — one call. Untracked / un-requested is a normal 200 with null
// fields (not a 404), so the branches below just read them.
const {
  data: tracking,
  isLoading,
  error,
} = useQuery(computed(() => trackingByTmdbOptions({ path: { tmdbId: props.tmdbId } })))

const want = computed(() => tracking.value?.wants?.[0] ?? null)

// Present only when the movie is tracked — gates the tracking-only kebab items.
const trackingId = computed(() => tracking.value?.tracking?.id ?? null)

// The caller's own pending request rides along on the status payload, so the
// pending badge needs no separate request-list fetch. The endpoint returns only a
// pending request (denied/canceled never surface here), so a re-request stays open.
const myPending = computed(() => tracking.value?.myRequest ?? null)

// Correlate download progress onto the want from the shared live jobs cache
// (SSE-patched) instead of a second per-movie fetch. A movie tracking is
// single-atom — one want, one in-flight job — so the newest matching job advances
// the current want. The job↔want edge lives in download_job_want (M:N), so the
// job carries no wantId to match on directly.
const { getMovieJob } = useDownloadJobs()

const wantProgress = computed(() => {
  if (!want.value) return null
  return getMovieJob(props.tmdbId)?.progress ?? null
})

const primaryLabel = computed(() =>
  auth.canAutoApprove('movie', defaultTier.value) ? 'Add to Library' : 'Request',
)

const createRequest = useMutation({
  ...requestsCreateMutation(),
  onSuccess: (req) => {
    // Two faces of one endpoint: a spawned tracking means the request
    // auto-approved and is already searching; otherwise it awaits approval.
    if (req.spawnedTrackingId) {
      toast.success('Added — searching now')
    } else {
      toast.success('Requested — pending approval')
    }
    queryClient.invalidateQueries({
      queryKey: trackingByTmdbQueryKey({ path: { tmdbId: props.tmdbId } }),
    })
    // Also refresh the request list this control reads myPending from, so an
    // await-approval submit flips the button to its Pending face without a reload.
    queryClient.invalidateQueries({ queryKey: requestsListQueryKey({}) })
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to add to library'))
  },
})

function handlePrimary() {
  // More than one held tier is a real choice → open the dialog. Exactly one leaves
  // nothing to decide, so fire the request straight away.
  if (availableTiers.value.length > 1) {
    modal.open(AddMovieDialog, {
      props: { tmdbId: props.tmdbId, title: props.title, availableTiers: availableTiers.value },
    })
    return
  }
  createRequest.mutate({ body: { tmdbId: props.tmdbId, type: 'movie', tier: defaultTier.value } })
}

// Untracked is a normal 200, so any error here is a genuine load failure worth
// showing rather than offering a stale Add.
const loadError = computed(() =>
  isProblem(error.value) ? problemMessage(error.value, 'Failed to load acquisition state') : null,
)
</script>
