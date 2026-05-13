<script setup lang="ts">
import { ref, inject, watch, computed } from 'vue'
import { useMutation, useQuery } from '@tanstack/vue-query'
import {
  usersUpdateMutation,
  usersAssignRoleMutation,
  rolesListOptions,
} from '@/client/@tanstack/vue-query.gen'
import type { User } from '@/components/tables/configs/userTableConfig'
import BaseDialog from './BaseDialog.vue'
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

interface Props {
  user: User
}

const props = defineProps<Props>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }

const updateUserMutation = useMutation(usersUpdateMutation())
const updateRoleMutation = useMutation(usersAssignRoleMutation())

// Fetch roles
const { data: roles } = useQuery(rolesListOptions())

const userForm = ref({
  email: '',
  username: '',
  role: 'user',
  isActive: true,
})

const userError = ref<string | null>(null)

// Initialize form when user changes
watch(
  () => props.user,
  (user) => {
    // Extract role name from roles JSONB
    let roleName = 'user'
    if (user.roles) {
      try {
        const rolesArray = typeof user.roles === 'string' ? JSON.parse(user.roles) : user.roles
        if (Array.isArray(rolesArray) && rolesArray.length > 0) {
          roleName = rolesArray[0].name || 'user'
        }
      } catch {
        roleName = 'user'
      }
    }

    userForm.value = {
      email: user.email || '',
      username: user.username || '',
      role: roleName,
      isActive: user.isActive ?? true,
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

    // Extract current role
    let currentRole = 'user'
    if (props.user.roles) {
      try {
        const rolesArray =
          typeof props.user.roles === 'string' ? JSON.parse(props.user.roles) : props.user.roles
        if (Array.isArray(rolesArray) && rolesArray.length > 0) {
          currentRole = rolesArray[0].name || 'user'
        }
      } catch {
        currentRole = 'user'
      }
    }

    // If role changed, update role separately
    if (currentRole !== userForm.value.role) {
      await updateRoleMutation.mutateAsync({
        path: { id: props.user.id },
        body: { role: userForm.value.role },
      })
    }

    userError.value = null
    dialogRef.value.close({ saved: true })
  } catch (err: any) {
    userError.value = err.message || 'Failed to save user'
  }
}

const handleCancel = () => {
  dialogRef.value.close()
}

const isLoading = computed(
  () => updateUserMutation.isPending.value || updateRoleMutation.isPending.value,
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
            <SelectItem v-for="role in roles" :key="role.id" :value="role.name">
              {{ role.name }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
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
