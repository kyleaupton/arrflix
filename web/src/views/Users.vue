<script setup lang="ts">
import { ref } from 'vue'
import { useQuery, useMutation } from '@tanstack/vue-query'
import { Plus, X, Users as UsersIcon } from 'lucide-vue-next'
import {
  usersListOptions,
  usersDeleteMutation,
  invitesListOptions,
  invitesDeleteMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { Invite } from '@/client/types.gen'
import type { User } from '@/components/tables/configs/userTableConfig'
import DataTable from '@/components/tables/DataTable.vue'
import { userColumns, createUserActions } from '@/components/tables/configs/userTableConfig'
import { useModal } from '@/composables/useModal'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import UserDialog from '@/components/modals/UserDialog.vue'
import InviteDialog from '@/components/modals/InviteDialog.vue'
import { problemMessage } from '@/lib/api'

// Data queries
const { data: users, isLoading, refetch } = useQuery(usersListOptions())
const {
  data: invites,
  isLoading: invitesLoading,
  refetch: refetchInvites,
} = useQuery(invitesListOptions())
const modal = useModal()

// State
const userError = ref<string | null>(null)

// Mutations
const deleteUserMutation = useMutation({
  ...usersDeleteMutation(),
  onSuccess: () => refetch(),
  onError: (err) => {
    userError.value = problemMessage(err, 'Failed to delete user')
  },
})
const deleteInviteMutation = useMutation({
  ...invitesDeleteMutation(),
  onSuccess: () => refetchInvites(),
  onError: (err) => {
    userError.value = problemMessage(err, 'Failed to revoke invite')
  },
})

// Handlers
const handleInviteUser = () => {
  modal.open(InviteDialog, {
    onClose: () => {
      refetchInvites()
    },
  })
}

const handleEditUser = (user: User) => {
  modal.open(UserDialog, {
    props: {
      user,
    },
    onClose: () => {
      refetch()
    },
  })
}

const handleDeleteUser = async (user: User) => {
  if (!user.id) return
  const confirmed = await modal.confirm({
    title: 'Delete User',
    message: `Are you sure you want to delete "${user.email}"? This action cannot be undone.`,
    severity: 'danger',
  })
  if (!confirmed) return
  deleteUserMutation.mutate({ path: { id: user.id } })
}

const handleDeleteInvite = async (invite: Invite) => {
  const confirmed = await modal.confirm({
    title: 'Revoke Invite',
    message: `Are you sure you want to revoke the invite for "${invite.email}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  deleteInviteMutation.mutate({ path: { id: invite.id } })
}

const userActions = createUserActions(handleEditUser, handleDeleteUser)

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleDateString()
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <!-- Pending Invites -->
    <Card v-if="invitesLoading || (invites && invites.length > 0)">
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Invites</CardTitle>
            <p class="text-sm text-muted-foreground">Pending and claimed invitations.</p>
          </div>
          <Button @click="handleInviteUser">
            <Plus class="mr-2 size-4" />
            Invite User
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div v-if="invitesLoading" class="space-y-3">
          <Skeleton class="h-10 w-full" />
          <Skeleton class="h-10 w-full" />
        </div>
        <div v-else-if="invites && invites.length > 0" class="space-y-2">
          <div
            v-for="invite in invites"
            :key="invite.id"
            class="flex items-center justify-between rounded-lg border p-3"
          >
            <div class="flex items-center gap-3">
              <span class="text-sm font-medium">{{ invite.email }}</span>
              <span
                v-if="invite.claimedAt"
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200"
              >
                Claimed
              </span>
              <span
                v-else
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200"
              >
                Pending
              </span>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-xs text-muted-foreground">{{ formatDate(invite.createdAt) }}</span>
              <Button
                v-if="!invite.claimedAt"
                variant="ghost"
                size="icon"
                class="size-8"
                @click="handleDeleteInvite(invite)"
              >
                <X class="size-4" />
              </Button>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Users -->
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">User Management</CardTitle>
            <p class="text-sm text-muted-foreground">Manage application users and their roles.</p>
          </div>
          <Button v-if="!invites || invites.length === 0" @click="handleInviteUser">
            <Plus class="mr-2 size-4" />
            Invite User
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div
          v-if="userError"
          class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive mb-4"
        >
          {{ userError }}
        </div>
        <div v-if="isLoading" class="space-y-3">
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
        </div>
        <DataTable
          v-else
          :data="users || []"
          :columns="userColumns"
          :actions="userActions"
          :loading="isLoading"
          empty-message="No users found"
          searchable
          search-placeholder="Search users..."
          paginator
          :rows="10"
        >
          <template #empty-icon>
            <UsersIcon class="size-5" />
          </template>
        </DataTable>
      </CardContent>
    </Card>
  </div>
</template>
