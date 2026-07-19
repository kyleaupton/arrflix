<script setup lang="ts">
import { ref, inject } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { Check, Copy } from 'lucide-vue-next'
import {
  invitesCreateMutation,
  invitesListQueryKey,
  rolesListOptions,
} from '@/client/@tanstack/vue-query.gen'
import BaseDialog from './BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { InvitesCreateBody } from '@/client/types.gen'
import { problemMessage } from '@/lib/api'

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const queryClient = useQueryClient()

// Role is the invite body's enum, not a free string — the create endpoint only
// accepts the frozen role set.
type InviteRole = NonNullable<InvitesCreateBody['role']>
const email = ref('')
const role = ref<InviteRole>('requester')
const error = ref<string | null>(null)

// After creation we show the accept link instead of closing: email delivery is a
// convenience layered on later, so the copyable link is the source of truth — the
// admin sends it however they like (and it works with SMTP unconfigured).
const acceptLink = ref<string | null>(null)
const copied = ref(false)

const { data: roles } = useQuery(rolesListOptions())

const createInviteMutation = useMutation({
  ...invitesCreateMutation(),
  onSuccess: (data) => {
    queryClient.invalidateQueries({ queryKey: invitesListQueryKey() })
    acceptLink.value = `${window.location.origin}/accept?token=${encodeURIComponent(data.token)}`
  },
  onError: (err) => {
    error.value = problemMessage(err, 'Failed to create invite')
  },
})

const handleSave = () => {
  error.value = null
  if (!email.value) {
    error.value = 'Email is required'
    return
  }
  createInviteMutation.mutate({ body: { email: email.value, role: role.value } })
}

const handleCopy = async () => {
  if (!acceptLink.value) return
  try {
    await navigator.clipboard.writeText(acceptLink.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
  } catch {
    // Clipboard blocked (insecure context / permissions) — the field is
    // selectable, so the admin can still copy by hand.
  }
}

const handleClose = () => {
  dialogRef.value.close({ saved: Boolean(acceptLink.value) })
}
</script>

<template>
  <BaseDialog title="Invite User">
    <!-- Step 1: compose the invite -->
    <div v-if="!acceptLink" class="flex flex-col gap-4">
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
      <div class="flex flex-col gap-2">
        <Label for="invite-role">Role</Label>
        <Select v-model="role">
          <SelectTrigger id="invite-role" class="w-full">
            <SelectValue placeholder="Select role" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="r in roles" :key="r.id" :value="r.name">
              {{ r.name }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
    </div>

    <!-- Step 2: hand off the link -->
    <div v-else class="flex flex-col gap-3">
      <p class="text-sm text-muted-foreground">
        Invite created for <span class="font-medium text-foreground">{{ email }}</span
        >. Send them this link to finish setting up their account:
      </p>
      <div class="flex items-center gap-2">
        <Input
          :model-value="acceptLink"
          readonly
          class="font-mono text-xs"
          @focus="(e: FocusEvent) => (e.target as HTMLInputElement).select()"
        />
        <Button variant="outline" size="icon" class="shrink-0" @click="handleCopy">
          <Check v-if="copied" class="size-4 text-green-600" />
          <Copy v-else class="size-4" />
        </Button>
      </div>
      <p class="text-xs text-muted-foreground">The link expires in 7 days.</p>
    </div>

    <template #footer>
      <template v-if="!acceptLink">
        <Button variant="outline" @click="handleClose">Cancel</Button>
        <Button :disabled="createInviteMutation.isPending.value" @click="handleSave">
          {{ createInviteMutation.isPending.value ? 'Creating...' : 'Create Invite' }}
        </Button>
      </template>
      <Button v-else @click="handleClose">Done</Button>
    </template>
  </BaseDialog>
</template>
