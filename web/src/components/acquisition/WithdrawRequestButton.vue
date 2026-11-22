<template>
  <!-- Offered only while the title-status projection offers the viewer's own
       withdrawal, so the list and the focus page apply one rule: an available
       title's request is no longer withdrawable (REQ-CANCEL-008). -->
  <Button
    v-if="cancelAction?.enabled"
    variant="outline"
    size="sm"
    :disabled="disabled"
    @click="emit('withdraw')"
  >
    Cancel
  </Button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import { useTitleStatus } from '@/composables/useTitleStatus'
import type { Request } from '@/client/types.gen'

const props = defineProps<{ request: Request; disabled?: boolean }>()
const emit = defineEmits<{ withdraw: [] }>()

const { cancelAction } = useTitleStatus(
  computed(() => props.request.type as 'movie' | 'series'),
  computed(() => props.request.tmdbId),
)
</script>
