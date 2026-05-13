<script setup lang="ts">
import { computed, inject } from 'vue'
import { Button } from '@/components/ui/button'
import BaseDialog from './BaseDialog.vue'

interface Props {
  title?: string
  message: string
  severity?: 'info' | 'warning' | 'error' | 'success'
  okLabel?: string
}

const props = withDefaults(defineProps<Props>(), {
  severity: 'info',
  okLabel: 'OK',
})

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }

const handleOk = () => {
  dialogRef.value.close()
}

// Map severity to button variant
const buttonVariant = computed(() => {
  switch (props.severity) {
    case 'error':
      return 'destructive'
    case 'success':
      return 'default'
    case 'warning':
      return 'default'
    case 'info':
    default:
      return 'default'
  }
})
</script>

<template>
  <BaseDialog :title="title">
    <p class="text-base">{{ message }}</p>
    <template #footer>
      <Button :variant="buttonVariant" @click="handleOk">
        {{ okLabel }}
      </Button>
    </template>
  </BaseDialog>
</template>
