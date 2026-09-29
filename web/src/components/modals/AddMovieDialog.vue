<script setup lang="ts">
import { ref, computed, inject } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import { requestsCreateMutation } from '@/client/@tanstack/vue-query.gen'
import { toast } from 'vue-sonner'
import BaseDialog from './BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import TierSegmentedControl from '@/components/acquisition/TierSegmentedControl.vue'
import { useTitleInvalidation } from '@/composables/useTitleInvalidation'
import { problemMessage } from '@/lib/api'
import type { TitleActionTier } from '@/client/types.gen'

type Tier = TitleActionTier['tier']

// The movie counterpart to TrackSeriesDialog. Only opened when the request action
// offers more than one tier, so quality is a genuine choice.
//
// The tiers arrive already filtered to what this viewer may pick, each labeled
// with whether choosing it needs approval — approval is granted per (type, tier),
// so a user who auto-approves HD but not 4K sees the verb flip when they pick 4K.
const props = defineProps<{
  tmdbId: number
  title: string
  tiers: TitleActionTier[]
  defaultTier?: Tier
}>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const invalidateTitle = useTitleInvalidation('movie', () => props.tmdbId)

const tier = ref<Tier>(props.defaultTier ?? props.tiers[0]?.tier ?? 'HD')
const error = ref<string | null>(null)

const tierNames = computed(() => props.tiers.map((t) => t.tier))
const autoApproves = computed(
  () => props.tiers.find((t) => t.tier === tier.value)?.requiresApproval === false,
)
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
    invalidateTitle()
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
        <TierSegmentedControl v-model="tier" :options="tierNames" label="Quality tier" />
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
