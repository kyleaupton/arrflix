<script setup lang="ts">
import type { Component } from 'vue'
import { computed } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import {
  SlidersHorizontal,
  Library,
  Gauge,
  Route,
  Tags,
  Search,
  Download,
  Sparkles,
  FolderTree,
  Inbox,
  MonitorPlay,
  Clapperboard,
  Captions,
  Bell,
  Mail,
  Users,
} from 'lucide-vue-next'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from '@/components/ui/sidebar'
import { useInboxCount } from '@/composables/useInboxCount'
import { useAuthStore } from '@/stores/auth'

interface NavItem {
  label: string
  icon: Component
  // Present for areas that exist today; absent for planned, disabled placeholders.
  to?: string
  disabled?: boolean
  // A reactive badge count; only rendered when > 0. Read lazily so the count
  // stays live without making the whole nav array reactive.
  badge?: () => number
}

interface NavGroup {
  label?: string
  items: NavItem[]
}

const route = useRoute()
const auth = useAuthStore()
const { count: unmatchedCount } = useInboxCount()

// Sidebar groups mirror the V1 information architecture
// (specs/patterns/navigation). Items without a `to` are planned areas rendered
// as disabled placeholders so the full shape is previewable before they exist.
// Users needs admin.users.manage, which a co_admin (who holds admin.settings.read
// and so reaches this layout) lacks — hide the tab from them.
const groups = computed<NavGroup[]>(() => [
  { items: [{ label: 'General', to: '/settings/general', icon: SlidersHorizontal }] },
  {
    label: 'Users & Requests',
    items: [
      ...(auth.canManageUsers
        ? [{ label: 'Users', to: '/settings/users', icon: Users } as NavItem]
        : []),
      { label: 'Requests', icon: Inbox, disabled: true },
    ],
  },
  // Sourcing = what release to grab; Delivery = where it lands and how (Routing
  // dispatches to a downloader, name template, and library).
  {
    label: 'Sourcing',
    items: [
      { label: 'Indexers', to: '/settings/indexers', icon: Search },
      { label: 'Quality Profiles', to: '/settings/quality-profiles', icon: Gauge },
    ],
  },
  {
    label: 'Delivery',
    items: [
      { label: 'Routing', to: '/settings/routing', icon: Route },
      { label: 'Downloaders', to: '/settings/downloaders', icon: Download },
      { label: 'Name Templates', to: '/settings/name-templates', icon: Tags },
      {
        label: 'Libraries',
        to: '/settings/libraries',
        icon: Library,
        badge: () => unmatchedCount.value,
      },
      { label: 'Path Mapping', icon: FolderTree, disabled: true },
    ],
  },
  {
    label: 'Maintenance',
    items: [{ label: 'Hygiene', icon: Sparkles, disabled: true }],
  },
  {
    label: 'Integrations',
    items: [
      { label: 'Media Servers', icon: MonitorPlay, disabled: true },
      { label: 'Metadata', icon: Clapperboard, disabled: true },
      { label: 'Subtitles', icon: Captions, disabled: true },
      { label: 'Email', to: '/settings/email', icon: Mail },
      { label: 'Notifications', icon: Bell, disabled: true },
    ],
  },
])

const isActive = (to: string) => route.path === to || route.path.startsWith(`${to}/`)
</script>

<template>
  <!--
    The app navbar is fixed at h-14 (3.5rem) and sits outside document flow, so
    the sidebar — which is itself position:fixed inset-y-0 — must be offset down
    by the navbar height to avoid tucking underneath it. mt-14 clears the navbar
    for the inset content; the inline top/height on <Sidebar> does the same for
    its fixed rail. This route renders full-bleed (meta.layout: 'sidebar'), so it
    owns its own spacing rather than the App.vue pt-20 wrapper.
  -->
  <SidebarProvider class="mt-14 min-h-[calc(100svh-3.5rem)]">
    <Sidebar
      variant="sidebar"
      collapsible="icon"
      :style="{ top: '3.5rem', height: 'calc(100svh - 3.5rem)' }"
    >
      <SidebarHeader>
        <div class="flex items-center justify-between gap-2 px-2 py-1">
          <span class="text-sm font-semibold group-data-[collapsible=icon]:hidden">Settings</span>
          <SidebarTrigger class="-mr-1" />
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup v-for="(group, i) in groups" :key="group.label ?? `group-${i}`">
          <SidebarGroupLabel v-if="group.label">{{ group.label }}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem v-for="item in group.items" :key="item.label">
                <SidebarMenuButton v-if="item.disabled" disabled :tooltip="`${item.label} — soon`">
                  <component :is="item.icon" />
                  <span>{{ item.label }}</span>
                </SidebarMenuButton>
                <SidebarMenuButton
                  v-else
                  as-child
                  :is-active="isActive(item.to!)"
                  :tooltip="item.label"
                >
                  <RouterLink :to="item.to!">
                    <component :is="item.icon" />
                    <span>{{ item.label }}</span>
                  </RouterLink>
                </SidebarMenuButton>
                <SidebarMenuBadge v-if="item.badge && item.badge() > 0">
                  {{ item.badge() }}
                </SidebarMenuBadge>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>

    <SidebarInset>
      <div class="p-4 md:p-6">
        <RouterView />
      </div>
    </SidebarInset>
  </SidebarProvider>
</template>
