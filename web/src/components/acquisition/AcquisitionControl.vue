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

      <!-- Untracked but a manual grab is in flight (orphan job, split-brain). -->
      <Badge v-if="manualJobActive" variant="secondary">
        <Download class="size-3" />
        Downloading (manual)
      </Badge>
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
import { Download, Plus, Search } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  trackingByTmdbOptions,
  trackingByTmdbQueryKey,
  requestsCreateMutation,
  downloadJobsListForMovieOptions,
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

// Download jobs for this movie, to correlate progress onto the want and to spot
// an in-flight manual grab when untracked. wantId links a job to its want.
const { data: jobs } = useQuery(
  computed(() => downloadJobsListForMovieOptions({ path: { id: props.tmdbId } })),
)

const ACTIVE_JOB_STATUSES = new Set(['created', 'enqueued', 'downloading', 'importing'])

const wantProgress = computed(() => {
  if (!want.value) return null
  const job = jobs.value?.find((j) => j.wantId === want.value!.id)
  return job?.progress ?? null
})

const manualJobActive = computed(
  () => !want.value && (jobs.value?.some((j) => ACTIVE_JOB_STATUSES.has(j.status)) ?? false),
)

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
