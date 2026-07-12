<script setup lang="ts">
import { ref, computed, inject } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  requestsCreateMutation,
  requestsListQueryKey,
  trackingByTmdbQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import { toast } from 'vue-sonner'
import BaseDialog from './BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import TierSegmentedControl from '@/components/acquisition/TierSegmentedControl.vue'
import { useAuthStore } from '@/stores/auth'
import { problemMessage } from '@/lib/api'

type Tier = 'HD' | '4K'

// The movie counterpart to TrackSeriesDialog. Only opened when the caller holds
// more than one tier, so quality is a genuine choice. Auto-approve is granted per
// (type, tier), so the selected tier decides the Add-vs-Request face — a user who
// auto-approves HD but not 4K sees the verb flip when they pick 4K.
const props = defineProps<{
  tmdbId: number
  title: string
  availableTiers: Tier[]
  defaultTier?: Tier
}>()

const auth = useAuthStore()
const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const queryClient = useQueryClient()

const tier = ref<Tier>(props.defaultTier ?? props.availableTiers[0] ?? 'HD')
const error = ref<string | null>(null)

const autoApproves = computed(() => auth.canAutoApprove('movie', tier.value))
const expectation = computed(() =>
  autoApproves.value
    ? `We'll find the best ${tier.value} release and download it now.`
    : 'A request will be sent for an admin to approve.',
)

const createRequest = useMutation({
  ...requestsCreateMutation(),
  onSuccess: (req) => {
    // Two faces of one endpoint: a spawned tracking auto-approved and is already
    // searching; otherwise it awaits approval.
    if (req.spawnedTrackingId) {
      toast.success('Added — searching now')
    } else {
      toast.success('Requested — pending approval')
    }
    queryClient.invalidateQueries({
      queryKey: trackingByTmdbQueryKey({ path: { tmdbId: props.tmdbId } }),
    })
    // Refresh the request list the control reads myPending from, so an await-approval
    // submit flips the hero to its Pending face without a reload.
    queryClient.invalidateQueries({ queryKey: requestsListQueryKey({}) })
    dialogRef.value.close({ saved: true })
  },
  onError: (err) => {
    error.value = problemMessage(err, 'Failed to add to library')
  },
})

function handleSubmit() {
  createRequest.mutate({ body: { tmdbId: props.tmdbId, type: 'movie', tier: tier.value } })
}
</script>

<template>
  <BaseDialog :title="`Add ${title}`">
    <div class="flex flex-col gap-5">
      <div
        v-if="error"
        class="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
      >
        {{ error }}
      </div>

      <div class="flex items-center justify-between gap-4">
        <Label>Quality</Label>
        <TierSegmentedControl v-model="tier" :options="availableTiers" label="Quality tier" />
      </div>

      <p class="text-sm text-muted-foreground">{{ expectation }}</p>
    </div>

    <template #footer>
      <Button
        variant="outline"
        :disabled="createRequest.isPending.value"
        @click="dialogRef.value.close()"
      >
        Cancel
      </Button>
      <Button :disabled="createRequest.isPending.value" @click="handleSubmit">
        {{
          createRequest.isPending.value
            ? autoApproves
              ? 'Adding…'
              : 'Requesting…'
            : autoApproves
              ? 'Add to Library'
              : 'Request'
        }}
      </Button>
    </template>
  </BaseDialog>
</template>
