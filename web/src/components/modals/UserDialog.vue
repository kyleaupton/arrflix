<script setup lang="ts">
import { ref, inject, watch, computed } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  usersUpdateMutation,
  usersAssignRoleMutation,
  usersUpdateRequestSettingsMutation,
  usersListQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import type { User } from '@/components/tables/configs/userTableConfig'
import BaseDialog from './BaseDialog.vue'
import RequestSettingsFields from '@/components/users/RequestSettingsFields.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { problemMessage } from '@/lib/api'
import { ASSIGNABLE_ROLES, isAssignableRole, type AssignableRole } from '@/lib/roles'

interface Props {
  user: User
}

const props = defineProps<Props>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const queryClient = useQueryClient()

const updateUserMutation = useMutation(usersUpdateMutation())
const updateRoleMutation = useMutation(usersAssignRoleMutation())
const updateRequestSettingsMutation = useMutation(usersUpdateRequestSettingsMutation())

const userForm = ref<{
  email: string
  username: string
  role: AssignableRole
  isActive: boolean
  autoApprove: boolean
  allow4k: boolean
}>({
  email: '',
  username: '',
  role: 'requester',
  isActive: true,
  autoApprove: false,
  allow4k: false,
})

const userError = ref<string | null>(null)

// The name of the user's role, read from the roles JSONB; null when they hold none.
function currentRoleName(user: User): string | null {
  if (!user.roles) return null
  try {
    const rolesArray = typeof user.roles === 'string' ? JSON.parse(user.roles) : user.roles
    if (Array.isArray(rolesArray) && rolesArray.length > 0) {
      return rolesArray[0].name || null
    }
  } catch {
    // Unparseable roles read as no role.
  }
  return null
}

// Initialize form when user changes
watch(
  () => props.user,
  (user) => {
    const roleName = currentRoleName(user)
    userForm.value = {
      email: user.email || '',
      username: user.username || '',
      role: isAssignableRole(roleName) ? roleName : 'requester',
      isActive: user.isActive ?? true,
      autoApprove: user.autoApprove ?? false,
      allow4k: user.allow4k ?? false,
    }
    userError.value = null
  },
  { immediate: true },
)

const handleSave = async () => {
  if (!userForm.value.email || !userForm.value.username) {
    userError.value = 'Email and username are required'
    return
  }

  try {
    await updateUserMutation.mutateAsync({
      path: { id: props.user.id },
      body: {
        email: userForm.value.email,
        username: userForm.value.username,
        isActive: userForm.value.isActive,
      },
    })

    // If role changed, update role separately
    if (currentRoleName(props.user) !== userForm.value.role) {
      await updateRoleMutation.mutateAsync({
        path: { id: props.user.id },
        body: { role: userForm.value.role },
      })
    }

    // Hidden for admins, whose role grants everything: their stored settings are
    // left as they are, to apply again if they're demoted.
    const { autoApprove, allow4k } = userForm.value
    if (
      userForm.value.role !== 'admin' &&
      (autoApprove !== (props.user.autoApprove ?? false) ||
        allow4k !== (props.user.allow4k ?? false))
    ) {
      await updateRequestSettingsMutation.mutateAsync({
        path: { id: props.user.id },
        body: { autoApprove, allow4k },
      })
    }

    queryClient.invalidateQueries({ queryKey: usersListQueryKey() })
    userError.value = null
    dialogRef.value.close({ saved: true })
  } catch (err) {
    userError.value = problemMessage(err, 'Failed to save user')
  }
}

const handleCancel = () => {
  dialogRef.value.close()
}

const isLoading = computed(
  () =>
    updateUserMutation.isPending.value ||
    updateRoleMutation.isPending.value ||
    updateRequestSettingsMutation.isPending.value,
)
</script>

<template>
  <BaseDialog title="Edit User">
    <div class="flex flex-col gap-4">
      <div
        v-if="userError"
        class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm"
      >
        {{ userError }}
      </div>
      <div class="flex flex-col gap-2">
        <Label for="user-email">Email</Label>
        <Input id="user-email" v-model="userForm.email" type="email" />
      </div>
      <div class="flex flex-col gap-2">
        <Label for="user-username">Username</Label>
        <Input id="user-username" v-model="userForm.username" />
      </div>
      <div class="flex flex-col gap-2">
        <Label for="user-role">Role</Label>
        <Select v-model="userForm.role">
          <SelectTrigger id="user-role" class="w-full">
            <SelectValue placeholder="Select role" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="role in ASSIGNABLE_ROLES" :key="role" :value="role">
              {{ role }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <RequestSettingsFields
        v-model:auto-approve="userForm.autoApprove"
        v-model:allow4k="userForm.allow4k"
        :is-admin="userForm.role === 'admin'"
      />
      <div class="flex items-center justify-between">
        <Label for="user-active">Active</Label>
        <Switch id="user-active" v-model="userForm.isActive" />
      </div>
    </div>
    <template #footer>
      <Button variant="outline" @click="handleCancel">Cancel</Button>
      <Button :disabled="isLoading" @click="handleSave">Save</Button>
    </template>
  </BaseDialog>
</template>
