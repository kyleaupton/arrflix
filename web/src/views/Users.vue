<script setup lang="ts">
import { ref, computed } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Plus, X, Users as UsersIcon } from 'lucide-vue-next'
import {
  usersListOptions,
  usersListQueryKey,
  usersDeleteMutation,
  invitesListOptions,
  invitesListQueryKey,
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
const { data: users, isLoading } = useQuery(usersListOptions())
const { data: invites, isLoading: invitesLoading } = useQuery(invitesListOptions())
// Claimed invites are dropped here: once accepted, the person is a real user in
// the table below, so surfacing the spent invite too is pure duplication.
const pendingInvites = computed(() => invites.value?.filter((invite) => !invite.claimedAt) ?? [])
const queryClient = useQueryClient()
const modal = useModal()

function invalidateUsers() {
  queryClient.invalidateQueries({ queryKey: usersListQueryKey() })
}

function invalidateInvites() {
  queryClient.invalidateQueries({ queryKey: invitesListQueryKey() })
}

// State
const userError = ref<string | null>(null)

// Mutations
const deleteUserMutation = useMutation({
  ...usersDeleteMutation(),
  onSuccess: invalidateUsers,
  onError: (err) => {
    userError.value = problemMessage(err, 'Failed to delete user')
  },
})
const deleteInviteMutation = useMutation({
  ...invitesDeleteMutation(),
  onSuccess: invalidateInvites,
  onError: (err) => {
    userError.value = problemMessage(err, 'Failed to revoke invite')
  },
})

// Handlers
const handleInviteUser = () => {
  modal.open(InviteDialog)
}

const handleEditUser = (user: User) => {
  modal.open(UserDialog, {
    props: {
      user,
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
    <Card v-if="invitesLoading || pendingInvites.length > 0">
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Pending Invites</CardTitle>
            <p class="text-sm text-muted-foreground">
              People you've invited who haven't accepted yet.
            </p>
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
        <div v-else-if="pendingInvites.length > 0" class="space-y-2">
          <div
            v-for="invite in pendingInvites"
            :key="invite.id"
            class="flex items-center justify-between rounded-lg border p-3"
          >
            <span class="text-sm font-medium">{{ invite.email }}</span>
            <div class="flex items-center gap-3">
              <span class="text-xs text-muted-foreground">{{ formatDate(invite.createdAt) }}</span>
              <Button
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
          <Button v-if="pendingInvites.length === 0" @click="handleInviteUser">
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
