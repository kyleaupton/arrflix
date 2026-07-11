<template>
  <Badge :variant="variant">
    <component :is="icon" v-if="icon" class="size-3" />
    {{ label }}
  </Badge>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import { Check, CircleX, Clock, Ban } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'

// Pure render of a request's lifecycle state — the requester-facing counterpart
// to WantStatusPill. approved and spawned collapse to one "Approved" face: from
// the requester's side the distinction (queued for spawn vs already tracking) is
// noise; the tracking/want pills carry the acquisition detail from there.
// Request.status is a plain string on the wire, narrowed in the switch here.
const props = defineProps<{
  status: string
}>()

const label = computed(() => {
  switch (props.status) {
    case 'pending':
      return 'Awaiting approval'
    case 'approved':
    case 'spawned':
      return 'Approved'
    case 'denied':
      return 'Denied'
    case 'canceled':
      return 'Canceled'
    default:
      return props.status
  }
})

const variant = computed<BadgeVariants['variant']>(() => {
  switch (props.status) {
    case 'approved':
    case 'spawned':
      return 'default'
    case 'denied':
      return 'destructive'
    case 'canceled':
      return 'outline'
    default:
      return 'secondary'
  }
})

const icon = computed<Component | null>(() => {
  switch (props.status) {
    case 'pending':
      return Clock
    case 'approved':
    case 'spawned':
      return Check
    case 'denied':
      return CircleX
    case 'canceled':
      return Ban
    default:
      return null
  }
})
</script>
