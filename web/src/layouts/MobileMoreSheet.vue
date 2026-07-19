<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { Calendar, Download, Inbox, Library, LogOut, Settings2, UserCog } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useInboxCount } from '@/composables/useInboxCount'
import { ResponsiveDialog } from '@/components/responsive-dialog'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'

// The mobile overflow menu: everything that didn't earn a primary tab slot,
// plus the user identity + logout that the desktop avatar dropdown owns. Built on
// ResponsiveDialog in forced-sheet mode — this surface is only ever reachable from
// the mobile tab bar, and the shared BottomSheet frame is where sheet styling and
// safe-area handling get iterated.
const open = defineModel<boolean>('open', { required: true })

const router = useRouter()
const auth = useAuthStore()
const { count: inboxCount } = useInboxCount()

const initials = computed(() => auth.user?.name?.[0]?.toUpperCase() ?? 'U')

interface MoreLink {
  label: string
  icon: unknown
  to?: string
  badge?: number
  disabled?: boolean
}

// Permission-gated the same way the desktop nav is: hide doors the router would
// bounce the user from. Calendar stays a disabled placeholder.
const links = computed<MoreLink[]>(() => [
  { label: 'Library', icon: Library, to: '/library' },
  ...(auth.canViewJobs ? [{ label: 'Downloads', icon: Download, to: '/downloads' }] : []),
  { label: 'Matching', icon: Inbox, to: '/library/matching', badge: inboxCount.value || undefined },
  { label: 'Calendar', icon: Calendar, disabled: true },
  { label: 'Preferences', icon: UserCog, to: '/preferences' },
  ...(auth.canManageSettings ? [{ label: 'Settings', icon: Settings2, to: '/settings' }] : []),
])

function go(to: string) {
  open.value = false
  router.push(to)
}

function handleLogout() {
  open.value = false
  auth.logout()
}
</script>

<template>
  <ResponsiveDialog v-model:open="open" mode="sheet" title="Account and navigation">
    <template #header>
      <div class="flex items-center gap-3 text-left">
        <Avatar class="size-10">
          <AvatarFallback>{{ initials }}</AvatarFallback>
        </Avatar>
        <div class="min-w-0">
          <p class="truncate text-base font-semibold leading-tight">
            {{ auth.user?.name ?? 'Account' }}
          </p>
          <p v-if="auth.user?.email" class="truncate text-xs text-muted-foreground">
            {{ auth.user.email }}
          </p>
        </div>
      </div>
    </template>

    <!-- Full-bleed rows: negate the sheet's content padding, re-indent inside each
         row so hover/press backgrounds reach the sheet edges like a native menu.
         The sheet owns the home-indicator inset (footerless content wrapper). -->
    <nav class="-mx-4 flex flex-col">
      <template v-for="link in links" :key="link.label">
        <span
          v-if="link.disabled"
          class="flex items-center gap-3 px-4 py-3 text-base text-foreground/35 select-none"
        >
          <component :is="link.icon" class="size-5" />
          {{ link.label }}
          <span class="ml-auto text-xs text-muted-foreground">Soon</span>
        </span>
        <button
          v-else
          type="button"
          class="flex items-center gap-3 px-4 py-3 text-base text-foreground transition-colors hover:bg-accent"
          @click="go(link.to!)"
        >
          <component :is="link.icon" class="size-5" />
          {{ link.label }}
          <Badge v-if="link.badge" variant="secondary" class="ml-auto">{{ link.badge }}</Badge>
        </button>
      </template>

      <div class="my-1 h-px bg-border" />

      <button
        type="button"
        class="flex items-center gap-3 px-4 py-3 text-base text-foreground transition-colors hover:bg-accent"
        @click="handleLogout"
      >
        <LogOut class="size-5" />
        Log out
      </button>
    </nav>
  </ResponsiveDialog>
</template>
