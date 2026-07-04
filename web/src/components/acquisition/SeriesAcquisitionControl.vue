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
            <SelectItem value="manual">I'll pick</SelectItem>
          </SelectContent>
        </Select>
      </label>
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
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Check, Plus, X } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  trackingByTmdbOptions,
  trackingByTmdbQueryKey,
  requestsCreateMutation,
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
import { isProblem, problemMessage } from '@/lib/api'

// availableCount/totalCount are computed by the parent from the already-loaded
// series detail (episodes with files / total), avoiding a second fetch here.
const props = defineProps<{
  tmdbId: number
  availableCount?: number
  totalCount?: number
}>()

const auth = useAuthStore()
const modal = useModal()
const queryClient = useQueryClient()

const tier = ref<'HD' | '4K'>('HD')

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

const availableCount = computed(() => props.availableCount ?? 0)
const totalCount = computed(() => props.totalCount ?? 0)

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
    queryClient.invalidateQueries({ queryKey: trackingKey.value })
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to add to library'))
  },
})

function handleAdd() {
  createRequest.mutate({ body: { tmdbId: props.tmdbId, type: 'series', tier: tier.value } })
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

function submitAutonomy(backfill: 'auto' | 'manual', ongoing: 'auto' | 'manual') {
  const id = tracking.value?.tracking?.id
  if (!id) return
  setAutonomy.mutate({ path: { id }, body: { backfill, ongoing } })
}

const autonomyBackfill = computed<'auto' | 'manual'>({
  get: () => (tracking.value?.tracking?.autonomyBackfill as 'auto' | 'manual') ?? 'auto',
  set: (val) => submitAutonomy(val, autonomyOngoing.value),
})

const autonomyOngoing = computed<'auto' | 'manual'>({
  get: () => (tracking.value?.tracking?.autonomyOngoing as 'auto' | 'manual') ?? 'auto',
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
