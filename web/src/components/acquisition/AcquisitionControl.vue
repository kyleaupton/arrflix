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

    <!-- Not tracked: initiate the autonomous flow. The button face depends on
         whether this user auto-approves; the tier select offers only tiers the
         caller can request (a user with no movie-create grant sees nothing). -->
    <template v-else-if="!isLoading && availableTiers.length">
      <div class="flex w-full sm:w-auto items-center gap-2">
        <Button class="grow sm:grow-0" :disabled="createRequest.isPending.value" @click="handleAdd">
          <Plus class="mr-2 size-4" />
          {{ createRequest.isPending.value ? 'Adding…' : primaryLabel }}
        </Button>
        <Select v-model="tier">
          <SelectTrigger class="w-20" aria-label="Quality tier">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="t in availableTiers" :key="t" :value="t">{{ t }}</SelectItem>
          </SelectContent>
        </Select>
      </div>
    </template>

    <!-- Overflow actions. Shown to operators even when untracked, so manual
         search stays reachable before anything is added; otherwise only once
         tracked. -->
    <TrackingActionsMenu
      v-if="auth.canManageJobs || trackingId"
      type="movie"
      :tmdb-id="tmdbId"
      :tracking-id="trackingId"
      :want="want"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Plus } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  trackingByTmdbOptions,
  trackingByTmdbQueryKey,
  requestsCreateMutation,
  requestsListOptions,
  downloadJobsListForMovieOptions,
} from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useAuthStore } from '@/stores/auth'
import { isProblem, problemMessage } from '@/lib/api'
import WantStatusPill from './WantStatusPill.vue'
import RequestStatusPill from './RequestStatusPill.vue'
import TrackingActionsMenu from './TrackingActionsMenu.vue'

// tmdbId doubles as the movie route id the download-jobs endpoint is keyed on.
const props = defineProps<{ tmdbId: number }>()

const auth = useAuthStore()
const queryClient = useQueryClient()

// The caller sees only the tiers they hold a create grant for, so they can't
// pick one the API would 403. tier defaults to the first available (or HD when
// the caller has no grant — the add block is hidden in that case anyway).
const availableTiers = computed<('HD' | '4K')[]>(() =>
  (['HD', '4K'] as const).filter((t) => auth.can(`requests.create:movie:${t.toLowerCase()}`)),
)
const tier = ref<'HD' | '4K'>(availableTiers.value[0] ?? 'HD')

// An expected 404 ("not tracked") is the common case for most movies, so don't
// burn retries on it; the 404 is read as the untracked state below, not an error.
const {
  data: tracking,
  isLoading,
  error,
} = useQuery(
  computed(() => ({
    ...trackingByTmdbOptions({ path: { tmdbId: props.tmdbId } }),
    retry: false,
  })),
)

const want = computed(() => tracking.value?.wants?.[0] ?? null)

// Present only when the movie is tracked — gates the tracking-only kebab items.
const trackingId = computed(() => tracking.value?.tracking?.id ?? null)

// A pending request has no tracking yet, so it doesn't surface as a want; find
// the caller's own pending request for this movie to show its status instead of
// the Add button. denied/canceled fall through, so a re-request stays possible.
const { data: myRequests } = useQuery(requestsListOptions({}))
const myPending = computed(
  () =>
    (myRequests.value ?? []).find(
      (r) => r.tmdbId === props.tmdbId && r.type === 'movie' && r.status === 'pending',
    ) ?? null,
)

// Download jobs for this movie, to correlate download progress onto the want.
// A movie tracking is single-atom (one want, one in-flight job), and the list is
// scoped to this movie and ordered newest-first, so the most recent job is the
// one advancing the current want. The job↔want edge now lives in download_job_want
// (M:N), so the job no longer carries a wantId to match on directly.
const { data: jobs } = useQuery(
  computed(() => downloadJobsListForMovieOptions({ path: { id: props.tmdbId } })),
)

const wantProgress = computed(() => {
  if (!want.value) return null
  return jobs.value?.[0]?.progress ?? null
})

const primaryLabel = computed(() =>
  auth.canAutoApprove('movie', tier.value) ? 'Add to Library' : 'Request',
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
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to add to library'))
  },
})

function handleAdd() {
  createRequest.mutate({ body: { tmdbId: props.tmdbId, type: 'movie', tier: tier.value } })
}

// A 404 is the untracked signal, not an error; surface anything else.
const loadError = computed(() =>
  isProblem(error.value) && error.value.status !== 404
    ? problemMessage(error.value, 'Failed to load acquisition state')
    : null,
)
</script>
