<script setup lang="ts">
import { ref, computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { ClipboardList, Home, Search } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useInboxCount } from '@/composables/useInboxCount'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import MobileMoreSheet from './MobileMoreSheet.vue'

// The mobile primary nav. The four slots stay stable across roles (Requests
// hides only for users who can't request); operator/admin extras live in More.
const route = useRoute()
const auth = useAuthStore()
const { count: inboxCount } = useInboxCount()

const moreOpen = ref(false)
const initials = computed(() => auth.user?.name?.[0]?.toUpperCase() ?? 'U')

const isActive = (prefix: string) =>
  prefix === '/' ? route.path === '/' : route.path.startsWith(prefix)

const itemBase =
  'relative flex flex-1 flex-col items-center justify-center gap-1 py-2 text-[0.625rem] font-medium transition-colors'
const itemActive = 'text-foreground'
const itemIdle = 'text-foreground/55'
</script>

<template>
  <nav
    class="fixed inset-x-0 bottom-0 z-50 flex border-t border-border/60 bg-background/95 backdrop-blur-lg pb-[env(safe-area-inset-bottom)] sm:hidden"
    aria-label="Primary"
  >
    <RouterLink to="/" :class="[itemBase, isActive('/') ? itemActive : itemIdle]">
      <Home class="size-5" />
      <span>Home</span>
    </RouterLink>

    <RouterLink to="/search" :class="[itemBase, isActive('/search') ? itemActive : itemIdle]">
      <Search class="size-5" />
      <span>Search</span>
    </RouterLink>

    <RouterLink
      v-if="auth.canViewOwnRequests"
      to="/requests"
      :class="[itemBase, isActive('/requests') ? itemActive : itemIdle]"
    >
      <ClipboardList class="size-5" />
      <span>Requests</span>
    </RouterLink>

    <button
      type="button"
      :class="[itemBase, moreOpen ? itemActive : itemIdle]"
      @click="moreOpen = true"
    >
      <span class="relative flex items-center justify-center">
        <Avatar class="size-5 text-[0.5rem]">
          <AvatarFallback>{{ initials }}</AvatarFallback>
        </Avatar>
        <span
          v-if="inboxCount > 0"
          class="absolute -right-1 -top-1 size-2 rounded-full bg-primary ring-2 ring-background"
        />
      </span>
      <span>More</span>
    </button>

    <MobileMoreSheet v-model:open="moreOpen" />
  </nav>
</template>
