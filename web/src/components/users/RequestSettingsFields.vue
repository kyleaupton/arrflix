<script setup lang="ts">
import { useId } from 'vue'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

// An admin's role already grants every tier and skips approval, so the
// per-user settings would do nothing; they're hidden rather than shown inert.
defineProps<{ isAdmin: boolean }>()

const autoApprove = defineModel<boolean>('autoApprove', { required: true })
const allow4k = defineModel<boolean>('allow4k', { required: true })

const id = useId()
</script>

<template>
  <p v-if="isAdmin" class="text-sm text-muted-foreground">
    Admins can request anything and skip approval.
  </p>
  <template v-else>
    <div class="flex items-center justify-between gap-4">
      <div class="flex flex-col gap-1">
        <Label :for="`${id}-auto-approve`">Auto-approve requests</Label>
        <p class="text-xs text-muted-foreground">Their requests skip the approval queue.</p>
      </div>
      <Switch :id="`${id}-auto-approve`" v-model="autoApprove" />
    </div>
    <div class="flex items-center justify-between gap-4">
      <div class="flex flex-col gap-1">
        <Label :for="`${id}-allow-4k`">Allow 4K</Label>
        <p class="text-xs text-muted-foreground">They can request 4K versions.</p>
      </div>
      <Switch :id="`${id}-allow-4k`" v-model="allow4k" />
    </div>
  </template>
</template>
