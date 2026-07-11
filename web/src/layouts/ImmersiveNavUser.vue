<script setup lang="ts">
import { useRouter } from 'vue-router'
import { Inbox, LogOut, Settings2, UserCog } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useInboxCount } from '@/composables/useInboxCount'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

const router = useRouter()
const authStore = useAuthStore()
const { count: inboxCount } = useInboxCount()

const initials = authStore.user?.name?.[0] ?? 'U'

const handleLogout = () => {
  authStore.logout()
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <button
        class="relative rounded-full focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Avatar class="h-8 w-8">
          <AvatarFallback>{{ initials }}</AvatarFallback>
        </Avatar>
        <span
          v-if="inboxCount > 0"
          class="absolute -right-0.5 -top-0.5 size-2.5 rounded-full bg-primary ring-2 ring-background"
        />
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent class="w-48" align="end" :side-offset="8">
      <DropdownMenuLabel v-if="authStore.user?.name" class="font-normal">
        <span class="block text-sm font-medium">{{ authStore.user.name }}</span>
        <span v-if="authStore.user.email" class="block text-xs text-muted-foreground truncate">
          {{ authStore.user.email }}
        </span>
      </DropdownMenuLabel>
      <DropdownMenuSeparator v-if="authStore.user?.name" />
      <DropdownMenuItem disabled>
        <UserCog class="mr-2 size-4" />
        Preferences
        <span class="ml-auto text-[10px] text-muted-foreground">Soon</span>
      </DropdownMenuItem>
      <DropdownMenuItem v-if="authStore.canManageSettings" @click="router.push('/settings')">
        <Settings2 class="mr-2 size-4" />
        Settings
      </DropdownMenuItem>
      <DropdownMenuItem @click="router.push('/library/matching')">
        <Inbox class="mr-2 size-4" />
        Matching
        <Badge v-if="inboxCount > 0" variant="secondary" class="ml-auto text-[10px]">
          {{ inboxCount }}
        </Badge>
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem @click="handleLogout">
        <LogOut class="mr-2 size-4" />
        Log out
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
