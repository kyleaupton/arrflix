<script setup lang="ts">
import { useRouter } from 'vue-router'
import { LogOut, Settings2, Users } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
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

const initials = authStore.user?.name?.[0] ?? 'U'

const handleLogout = () => {
  authStore.logout()
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <button class="rounded-full focus:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <Avatar class="h-8 w-8">
          <AvatarFallback>{{ initials }}</AvatarFallback>
        </Avatar>
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
      <DropdownMenuItem @click="router.push('/users')">
        <Users class="mr-2 size-4" />
        Users
      </DropdownMenuItem>
      <DropdownMenuItem @click="router.push('/settings')">
        <Settings2 class="mr-2 size-4" />
        Settings
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem @click="handleLogout">
        <LogOut class="mr-2 size-4" />
        Log out
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
