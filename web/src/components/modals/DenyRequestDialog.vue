<script setup lang="ts">
import { ref, inject } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { requestsDenyMutation, requestsListQueryKey } from '@/client/@tanstack/vue-query.gen'
import BaseDialog from './BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { problemMessage } from '@/lib/api'

// The requester sees this reason on their denied request, so it's required here
// even though the API marks it optional — a bare "Denied" with no context is a
// dead end. type only shapes the copy; the deny endpoint keys off the id.
const props = defineProps<{
  requestId: string
  type: 'movie' | 'series'
}>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const queryClient = useQueryClient()

const reason = ref('')
const error = ref<string | null>(null)

const denyRequest = useMutation({
  ...requestsDenyMutation(),
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: requestsListQueryKey() })
    dialogRef.value.close({ saved: true })
  },
  onError: (err) => {
    error.value = problemMessage(err, 'Failed to deny request')
  },
})

function handleDeny() {
  const trimmed = reason.value.trim()
  if (!trimmed) {
    error.value = 'A reason is required.'
    return
  }
  error.value = null
  denyRequest.mutate({ path: { id: props.requestId }, body: { reason: trimmed } })
}
</script>

<template>
  <BaseDialog title="Deny request" description="Tell the requester why this was denied.">
    <div class="flex flex-col gap-4">
      <div
        v-if="error"
        class="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
      >
        {{ error }}
      </div>

      <div class="flex flex-col gap-2">
        <Label for="deny-reason">Reason</Label>
        <Textarea
          id="deny-reason"
          v-model="reason"
          placeholder="e.g. not available at this quality yet"
          rows="3"
        />
      </div>
    </div>

    <template #footer>
      <Button
        variant="outline"
        :disabled="denyRequest.isPending.value"
        @click="dialogRef.value.close()"
      >
        Cancel
      </Button>
      <Button variant="destructive" :disabled="denyRequest.isPending.value" @click="handleDeny">
        {{ denyRequest.isPending.value ? 'Denying…' : 'Deny request' }}
      </Button>
    </template>
  </BaseDialog>
</template>
