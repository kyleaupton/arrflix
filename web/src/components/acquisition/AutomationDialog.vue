<script setup lang="ts">
import { ref, inject } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { toast } from 'vue-sonner'
import { trackingSetAutonomyMutation } from '@/client/@tanstack/vue-query.gen'
import BaseDialog from '@/components/modals/BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import AutonomySegmentedControl from '@/components/acquisition/AutonomySegmentedControl.vue'
import { problemMessage } from '@/lib/api'

type Autonomy = 'auto' | 'propose' | 'manual'

// Per-segment acquisition autonomy for a tracked series: back-catalog (episodes
// aired before tracking began) and new episodes (aired after). Edited locally and
// committed together on Save — the endpoint takes both segments at once, and the
// server holds/releases each segment's wants to match.
const props = defineProps<{
  trackingId: string
  backfill: Autonomy
  ongoing: Autonomy
}>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const queryClient = useQueryClient()

const backfill = ref<Autonomy>(props.backfill)
const ongoing = ref<Autonomy>(props.ongoing)
const error = ref<string | null>(null)

const setAutonomy = useMutation({
  ...trackingSetAutonomyMutation(),
  onSuccess: () => {
    toast.success('Automation updated')
    // Refresh every trackingByTmdb reader (the control's badge, the episode grid)
    // so re-armed / held statuses show at once.
    queryClient.invalidateQueries({ queryKey: [{ _id: 'trackingByTmdb' }] })
    dialogRef.value.close({ saved: true })
  },
  onError: (err) => {
    error.value = problemMessage(err, 'Failed to update automation')
  },
})

function handleSave() {
  setAutonomy.mutate({
    path: { id: props.trackingId },
    body: { backfill: backfill.value, ongoing: ongoing.value },
  })
}
</script>

<template>
  <BaseDialog title="Automation">
    <div class="flex flex-col gap-5">
      <div
        v-if="error"
        class="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
      >
        {{ error }}
      </div>

      <p class="text-sm text-muted-foreground">
        Choose how much this series is acquired for you. "I'll pick" holds a segment's episodes for
        manual download; "Suggest first" parks a found release for your approval.
      </p>

      <div class="flex flex-col gap-3">
        <div class="flex items-center justify-between gap-4">
          <Label class="font-normal text-muted-foreground">Back-catalog</Label>
          <AutonomySegmentedControl
            v-model="backfill"
            label="Back-catalog autonomy"
            :disabled="setAutonomy.isPending.value"
          />
        </div>
        <div class="flex items-center justify-between gap-4">
          <Label class="font-normal text-muted-foreground">New episodes</Label>
          <AutonomySegmentedControl
            v-model="ongoing"
            label="New episodes autonomy"
            :disabled="setAutonomy.isPending.value"
          />
        </div>
      </div>
    </div>

    <template #footer>
      <Button
        variant="outline"
        :disabled="setAutonomy.isPending.value"
        @click="dialogRef.value.close()"
      >
        Cancel
      </Button>
      <Button :disabled="setAutonomy.isPending.value" @click="handleSave">
        {{ setAutonomy.isPending.value ? 'Saving…' : 'Save' }}
      </Button>
    </template>
  </BaseDialog>
</template>
