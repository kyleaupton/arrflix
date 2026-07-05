<template>
  <Badge :variant="variant" :title="lastError ?? undefined">
    <component :is="icon" v-if="icon" class="size-3" />
    {{ label }}
  </Badge>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import { Check, CircleAlert, Download, Hand, Loader, Search, Sparkles } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'

// Pure render of a want's lifecycle state. The owning control correlates
// download progress from the linked job and passes it in; this component never
// fetches. last_error surfaces both inline (failed) and as the hover title.
const props = defineProps<{
  status: string
  attemptCount?: number
  lastError?: string | null
  progress?: number | null
  hold?: string | null
}>()

// A held want (segment on a manual autonomy dial) stays pending/searching but is
// never auto-searched — the user picks the release. That annotation overrides the
// lifecycle face while the want is still pre-grab; once it advances (grabbed and
// beyond) the hold is cleared, so the normal face resumes.
const isHeld = computed(
  () => props.hold === 'needs_pick' && (props.status === 'pending' || props.status === 'searching'),
)

// A proposed want (segment on the 'propose' autonomy dial) has a release picked
// and parked for one-tap approval. Like a hold it overrides the pre-grab
// lifecycle face; a grab clears the hold, so the normal face resumes afterward.
const isProposed = computed(
  () => props.hold === 'proposed' && (props.status === 'pending' || props.status === 'searching'),
)

const label = computed(() => {
  if (isProposed.value) return 'Suggested'
  if (isHeld.value) return 'Needs your pick'
  switch (props.status) {
    case 'pending':
      return 'Queued'
    case 'searching':
      return props.attemptCount && props.attemptCount > 1
        ? `Searching • ${props.attemptCount} attempts`
        : 'Searching…'
    case 'grabbed':
      return 'Grabbed'
    case 'downloading':
      return props.progress != null ? `Downloading ${Math.round(props.progress)}%` : 'Downloading…'
    case 'imported':
      return 'Importing…'
    case 'available':
      return 'Available'
    case 'failed':
      return props.lastError ? `Failed — ${props.lastError}` : 'Failed'
    case 'canceled':
      return 'Canceled'
    default:
      return props.status
  }
})

const variant = computed<BadgeVariants['variant']>(() => {
  if (isProposed.value) return 'default'
  if (isHeld.value) return 'outline'
  switch (props.status) {
    case 'available':
      return 'default'
    case 'downloading':
      return 'default'
    case 'failed':
      return 'destructive'
    case 'canceled':
      return 'outline'
    default:
      return 'secondary'
  }
})

const icon = computed<Component | null>(() => {
  if (isProposed.value) return Sparkles
  if (isHeld.value) return Hand
  switch (props.status) {
    case 'searching':
      return Search
    case 'downloading':
      return Download
    case 'pending':
    case 'grabbed':
    case 'imported':
      return Loader
    case 'available':
      return Check
    case 'failed':
      return CircleAlert
    default:
      return null
  }
})
</script>
