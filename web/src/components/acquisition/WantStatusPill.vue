<template>
  <Badge :variant="variant" :title="lastError ?? undefined">
    <component :is="icon" v-if="icon" class="size-3" />
    {{ label }}
  </Badge>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import { Check, CircleAlert, Download, Loader, Search } from 'lucide-vue-next'
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
}>()

const label = computed(() => {
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
