<template>
  <div class="flex flex-wrap items-center gap-3">
    <!-- Tracked: the want's live state replaces the primary action. -->
    <template v-if="want">
      <WantStatusPill
        :status="want.status"
        :attempt-count="want.attemptCount"
        :last-error="want.lastError"
        :progress="wantProgress"
      />
      <Button
        v-if="canCancel"
        variant="outline"
        :disabled="cancelWant.isPending.value"
        @click="handleCancel"
      >
        <X class="mr-2 size-4" />
        {{ cancelWant.isPending.value ? 'Canceling…' : 'Cancel' }}
      </Button>
    </template>

    <!-- A genuine (non-404) load failure: 404 is the untracked signal, anything
         else is a real error worth showing rather than offering a stale Add. -->
    <p v-else-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>

    <!-- Not tracked: initiate the autonomous flow. The button face depends on
         whether this user auto-approves. -->
    <template v-else-if="!isLoading">
      <div class="flex items-center gap-2">
        <Button :disabled="createRequest.isPending.value" @click="handleAdd">
          <Plus class="mr-2 size-4" />
          {{ createRequest.isPending.value ? 'Adding…' : primaryLabel }}
        </Button>
        <Select v-model="tier">
          <SelectTrigger class="w-20" aria-label="Quality tier">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="HD">HD</SelectItem>
            <SelectItem value="4K">4K</SelectItem>
          </SelectContent>
        </Select>
      </div>
    </template>

    <!-- Manual override — admin only. Available in both tracked and untracked
         states. -->
    <Button v-if="auth.isAdmin" variant="outline" @click="openManualSearch">
      <Search class="mr-2 size-4" />
      Search manually
    </Button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Plus, Search, X } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  trackingByTmdbOptions,
  trackingByTmdbQueryKey,
  requestsCreateMutation,
  wantsCancelMutation,
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
import { useModal } from '@/composables/useModal'
import { useAuthStore } from '@/stores/auth'
import { isProblem, problemMessage } from '@/lib/api'
import DownloadCandidatesDialog from '@/components/download-candidates/DownloadCandidatesDialog.vue'
import WantStatusPill from './WantStatusPill.vue'

// tmdbId doubles as the movie route id the download-jobs endpoint is keyed on.
const props = defineProps<{ tmdbId: number }>()

const auth = useAuthStore()
const modal = useModal()
const queryClient = useQueryClient()

const tier = ref<'HD' | '4K'>('HD')

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

const primaryLabel = computed(() => (auth.canAutoApproveMovie ? 'Add to Library' : 'Request'))

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

// Cancel is offered only while the want is still in flight; terminal states
// ('available', 'failed', 'canceled') have nothing to stop.
const CANCELABLE_STATUSES = new Set(['pending', 'searching', 'grabbed', 'downloading', 'imported'])

const canCancel = computed(() => !!want.value && CANCELABLE_STATUSES.has(want.value.status))

const cancelWant = useMutation({
  ...wantsCancelMutation(),
  onSuccess: () => {
    toast.success('Canceled')
    queryClient.invalidateQueries({
      queryKey: trackingByTmdbQueryKey({ path: { tmdbId: props.tmdbId } }),
    })
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to cancel'))
  },
})

async function handleCancel() {
  if (!want.value) return
  const confirmed = await modal.confirm({
    title: 'Cancel acquisition',
    message: 'Stop searching/downloading and mark this movie canceled?',
    severity: 'danger',
  })
  if (!confirmed) return
  cancelWant.mutate({ path: { id: want.value.id } })
}

function openManualSearch() {
  modal.open(DownloadCandidatesDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-4xl lg:max-w-6xl',
      movieId: props.tmdbId,
    },
  })
}

// A 404 is the untracked signal, not an error; surface anything else.
const loadError = computed(() =>
  isProblem(error.value) && error.value.status !== 404
    ? problemMessage(error.value, 'Failed to load acquisition state')
    : null,
)
</script>
