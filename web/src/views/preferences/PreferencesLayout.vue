<script setup lang="ts">
import type { Component } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { Bell, MonitorSmartphone } from 'lucide-vue-next'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from '@/components/ui/sidebar'

interface NavItem {
  label: string
  icon: Component
  to: string
}

// Per-user preferences — available to every authenticated role, so (unlike the
// admin-only Settings layout) the nav has no capability gates.
const items: NavItem[] = [
  { label: 'Notifications', to: '/preferences/notifications', icon: Bell },
  { label: 'Devices', to: '/preferences/devices', icon: MonitorSmartphone },
]

const route = useRoute()
const isActive = (to: string) => route.path === to || route.path.startsWith(`${to}/`)
</script>

<template>
  <!--
    The app navbar is fixed at h-14 (3.5rem) and sits outside document flow, so
    the sidebar — itself position:fixed inset-y-0 — is offset down by the navbar
    height to avoid tucking underneath it. This route renders full-bleed
    (meta.layout: 'sidebar'), so it owns its own spacing rather than the App.vue
    pt-20 wrapper. Mirrors the Settings layout.
  -->
  <SidebarProvider class="mt-14 min-h-[calc(100svh-3.5rem)]">
    <Sidebar
      variant="sidebar"
      collapsible="icon"
      :style="{ top: '3.5rem', height: 'calc(100svh - 3.5rem)' }"
    >
      <SidebarHeader>
        <div class="flex items-center justify-between gap-2 px-2 py-1">
          <span class="text-sm font-semibold group-data-[collapsible=icon]:hidden">Preferences</span>
          <SidebarTrigger class="-mr-1" />
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem v-for="item in items" :key="item.label">
                <SidebarMenuButton
                  as-child
                  :is-active="isActive(item.to)"
                  :tooltip="item.label"
                >
                  <RouterLink :to="item.to">
                    <component :is="item.icon" />
                    <span>{{ item.label }}</span>
                  </RouterLink>
                </SidebarMenuButton>
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
