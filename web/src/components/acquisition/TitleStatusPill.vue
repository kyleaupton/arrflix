<script setup lang="ts">
import { computed } from 'vue'
import { Badge } from '@/components/ui/badge'
import { present } from '@/lib/titleStatus'

// The glanceable face of a title's acquisition state. Pure render: it takes the
// state the server derived and looks up how to show it, so every pill in the app
// says the same word for the same state.
//
// Progress rides along only when the caller has it — the state alone is a
// complete answer, and a percentage-less "Downloading" is correct rather than
// degraded.
const props = defineProps<{
  state: string | null | undefined
  progress?: number | null
}>()

const presentation = computed(() => present(props.state))

const label = computed(() => {
  if (props.state === 'downloading' && props.progress != null) {
    return `Downloading ${Math.round(props.progress * 100)}%`
  }
  return presentation.value.label
})
</script>

<template>
  <Badge :variant="presentation.variant">
    <component :is="presentation.icon" class="size-3" />
    {{ label }}
  </Badge>
</template>
