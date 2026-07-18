<script lang="ts">
import type { Component } from 'vue'

export interface NavItem {
  label: string
  icon: Component
  // Present for areas that exist today; absent for planned, disabled placeholders.
  to?: string
  disabled?: boolean
  // A reactive badge count; only rendered when > 0. Read lazily so the count
  // stays live without making the whole nav array reactive.
  badge?: () => number
}

export interface NavGroup {
  label?: string
  items: NavItem[]
}
</script>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'
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
import { Badge } from '@/components/ui/badge'

// The shared shell for pages that navigate between sibling sections (Settings,
// Preferences). Desktop renders a two-pane sidebar + content; mobile collapses
// to master–detail — a full-screen section list at `basePath`, a back-bar +
// content at each section. `groups` is the single nav config both modes read.
const props = defineProps<{
  title: string
  // The layout's index route (e.g. '/settings'). Identifies the mobile list
  // state and is where the mobile back-bar returns to.
  basePath: string
  groups: NavGroup[]
}>()

const route = useRoute()
const router = useRouter()

// Matches SidebarProvider's own breakpoint so the desktop/mobile split and the
// sidebar's internal mobile handling never disagree at the boundary.
const isMobile = useMediaQuery('(max-width: 768px)')
const isIndex = computed(() => route.path === props.basePath)

const firstSection = computed(() => {
  for (const group of props.groups) {
    for (const item of group.items) {
      if (item.to) return item.to
    }
  }
  return undefined
})

// Desktop has no section list, so the bare index has nothing to show. The router
// forwards to the first section on direct navigation; this backstop covers the
// case where the viewport is resized across the breakpoint while sitting on the
// mobile list.
watch(
  [isMobile, isIndex],
  ([mobile, index]) => {
    if (!mobile && index && firstSection.value) router.replace(firstSection.value)
  },
  { immediate: true },
)

const isActive = (to: string) => route.path === to || route.path.startsWith(`${to}/`)
</script>

<template>
  <!--
    Desktop: the app navbar is fixed at h-14 (3.5rem) and sits outside document
    flow, so the sidebar — itself position:fixed inset-y-0 — is offset down by the
    navbar height to avoid tucking underneath it. This route renders full-bleed
    (meta.layout: 'sidebar'), so it owns its own spacing rather than the App.vue
    pt wrapper.
  -->
  <SidebarProvider v-if="!isMobile" class="mt-14 min-h-[calc(100svh-3.5rem)]">
    <Sidebar
      variant="sidebar"
      collapsible="icon"
      :style="{ top: '3.5rem', height: 'calc(100svh - 3.5rem)' }"
    >
      <SidebarHeader>
        <div class="flex items-center justify-between gap-2 px-2 py-1">
          <span class="text-sm font-semibold group-data-[collapsible=icon]:hidden">
            {{ title }}
          </span>
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

  <!--
    Mobile master view: a full-screen list of sections. Top padding clears the
    fixed mobile navbar (safe-area + 2.75rem); bottom padding clears the fixed tab
    bar (its height + home-indicator inset). Rows mirror MobileMoreSheet.
  -->
  <div
    v-else-if="isIndex"
    class="min-h-svh pt-[calc(env(safe-area-inset-top)_+_2.75rem)] pb-[calc(3.75rem_+_var(--tabbar-inset-bottom))]"
  >
    <header class="px-4 pb-2 pt-4">
      <h1 class="text-2xl font-semibold">{{ title }}</h1>
    </header>

    <nav class="flex flex-col px-2">
      <div v-for="(group, i) in groups" :key="group.label ?? `group-${i}`" class="flex flex-col">
        <p
          v-if="group.label"
          class="px-3 pb-1 pt-4 text-xs font-medium uppercase tracking-wide text-muted-foreground"
        >
          {{ group.label }}
        </p>
        <template v-for="item in group.items" :key="item.label">
          <span
            v-if="item.disabled"
            class="flex items-center gap-3 rounded-lg px-3 py-3 text-base text-foreground/35 select-none"
          >
            <component :is="item.icon" class="size-5" />
            {{ item.label }}
            <span class="ml-auto text-xs text-muted-foreground">Soon</span>
          </span>
          <RouterLink
            v-else
            :to="item.to!"
            class="flex items-center gap-3 rounded-lg px-3 py-3 text-base text-foreground transition-colors active:bg-accent"
          >
            <component :is="item.icon" class="size-5" />
            {{ item.label }}
            <Badge v-if="item.badge && item.badge() > 0" variant="secondary" class="ml-auto">
              {{ item.badge() }}
            </Badge>
            <ChevronRight
              class="size-4 text-muted-foreground"
              :class="item.badge && item.badge() > 0 ? 'ml-2' : 'ml-auto'"
            />
          </RouterLink>
        </template>
      </div>
    </nav>
  </div>

  <!--
    Mobile detail view: the section content with a slim back-bar. The bar sticks
    just below the fixed navbar; the section's own <h1> is the screen title, so
    the bar carries only the iOS-style back affordance to the section list.
  -->
  <div
    v-else
    class="min-h-svh pt-[calc(env(safe-area-inset-top)_+_2.75rem)] pb-[calc(3.75rem_+_var(--tabbar-inset-bottom))]"
  >
    <div
      class="sticky top-[calc(env(safe-area-inset-top)_+_2.75rem)] z-30 flex items-center border-b border-border/60 bg-background/95 px-2 py-2 backdrop-blur-lg"
    >
      <RouterLink
        :to="basePath"
        class="flex items-center gap-1 rounded-md px-2 py-1 text-sm font-medium text-foreground/80 transition-colors active:text-foreground"
      >
        <ChevronLeft class="size-5" />
        {{ title }}
      </RouterLink>
    </div>

    <div class="p-4">
      <RouterView />
    </div>
  </div>
</template>
