<script setup lang="ts">
import { computed } from 'vue'
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
import SidebarLayout, { type NavGroup, type NavItem } from '@/layouts/SidebarLayout.vue'
import { useInboxCount } from '@/composables/useInboxCount'
import { useAuthStore } from '@/stores/auth'

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
</script>

<template>
  <SidebarLayout title="Settings" base-path="/settings" :groups="groups" />
</template>
