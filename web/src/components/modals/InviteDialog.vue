<script setup lang="ts">
import { ref, inject } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import { invitesCreateMutation } from '@/client/@tanstack/vue-query.gen'
import BaseDialog from './BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const createInviteMutation = useMutation(invitesCreateMutation())

const email = ref('')
const error = ref<string | null>(null)

const handleSave = async () => {
  if (!email.value) {
    error.value = 'Email is required'
    return
  }
  try {
    await createInviteMutation.mutateAsync({
      body: { email: email.value },
    })
    dialogRef.value.close({ saved: true })
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to create invite'
  }
}

const handleCancel = () => {
  dialogRef.value.close()
}
</script>

<template>
  <BaseDialog title="Invite User">
    <div class="flex flex-col gap-4">
      <div
        v-if="error"
        class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm"
      >
        {{ error }}
      </div>
      <div class="flex flex-col gap-2">
        <Label for="invite-email">Email</Label>
        <Input id="invite-email" v-model="email" type="email" placeholder="user@example.com" />
      </div>
    </div>
    <template #footer>
      <Button variant="outline" @click="handleCancel">Cancel</Button>
      <Button :disabled="createInviteMutation.isPending.value" @click="handleSave">
        {{ createInviteMutation.isPending.value ? 'Sending...' : 'Send Invite' }}
      </Button>
    </template>
  </BaseDialog>
</template>
