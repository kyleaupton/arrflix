<script setup lang="ts">
import { Bell, MonitorSmartphone } from 'lucide-vue-next'
import SidebarLayout, { type NavGroup } from '@/layouts/SidebarLayout.vue'

// Per-user preferences — available to every authenticated role, so (unlike the
// admin-only Settings layout) the nav has no capability gates. One unlabeled
// group renders as a flat section list.
const groups: NavGroup[] = [
  {
    items: [
      { label: 'Notifications', to: '/preferences/notifications', icon: Bell },
      { label: 'Devices', to: '/preferences/devices', icon: MonitorSmartphone },
    ],
  },
]
</script>

<template>
  <SidebarLayout title="Preferences" base-path="/preferences" :groups="groups" />
</template>
