<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button variant="outline" size="icon" aria-label="More actions">
        <Ellipsis class="size-4" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" :side-offset="8" class="w-56">
      <DropdownMenuItem :disabled="retry.isPending.value" @click="handleRetry">
        <RotateCw class="mr-2 size-4" />
        {{ retry.isPending.value ? 'Retrying…' : 'Retry search' }}
      </DropdownMenuItem>
      <!-- Metadata refresh has no endpoint yet; surfaced disabled so the menu
           reads as the eventual home for it. -->
      <DropdownMenuItem disabled>
        <RefreshCw class="mr-2 size-4" />
        Refresh metadata
        <DropdownMenuShortcut>Soon</DropdownMenuShortcut>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { Ellipsis, RotateCw, RefreshCw } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { trackingRetryMutation } from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { problemMessage } from '@/lib/api'

const props = defineProps<{ trackingId: string }>()

const queryClient = useQueryClient()

// Retry re-arms this tracking's terminal wants and nudges its pending ones to
// search now. The count comes back so the toast can distinguish a real re-drive
// from a no-op (everything already in flight or available).
const retry = useMutation({
  ...trackingRetryMutation(),
  onSuccess: (res) => {
    toast.success(
      res.retried > 0
        ? `Searching now — re-armed ${res.retried} ${res.retried === 1 ? 'want' : 'wants'}`
        : 'Nothing to retry',
    )
    // Partial-match every trackingByTmdb reader (the acquisition control's want
    // state and the series episode grid) so the re-armed statuses show at once.
    queryClient.invalidateQueries({ queryKey: [{ _id: 'trackingByTmdb' }] })
  },
  onError: (err) => {
    toast.error(problemMessage(err, 'Failed to retry'))
  },
})

function handleRetry() {
  retry.mutate({ path: { id: props.trackingId } })
}
</script>
