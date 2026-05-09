<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { storeToRefs } from 'pinia'
import type { SidebarProps } from '@/components/ui/sidebar'
import { useAuthStore } from '@/stores/auth'
import {
  Popcorn,
  GalleryVerticalEnd,
  Settings2,
  Home,
  Download,
  ChevronRight,
  Users,
} from 'lucide-vue-next'
import NavUser from '@/components/NavUser.vue'

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarRail,
  SidebarGroup,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuSub,
  SidebarMenuSubItem,
  SidebarMenuSubButton,
} from '@/components/ui/sidebar'

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'

const props = withDefaults(defineProps<SidebarProps>(), {
  collapsible: 'icon',
})

const router = useRouter()
const authStore = useAuthStore()
const { user } = storeToRefs(authStore)

const currentPath = computed(() => router.currentRoute.value.path)

type NavItem = {
  title: string
  url: string
  icon?: typeof Home
  items?: { title: string; url: string }[]
}

const data: { user: { name: string; email: string; initials: string }; navMain: NavItem[] } = {
  user: {
    name: user.value?.name ?? 'Unknown User',
    email: user.value?.email ?? 'Unknown Email',
    initials: user.value?.name?.[0] ?? 'U',
  },
  navMain: [
    {
      title: 'Home',
      url: '/',
      icon: Home,
    },
    {
      title: 'Library',
      url: '/library',
      icon: GalleryVerticalEnd,
    },
    {
      title: 'Downloads',
      url: '/downloads',
      icon: Download,
    },
    {
      title: 'Users',
      url: '/users',
      icon: Users,
    },
    {
      title: 'Settings',
      icon: Settings2,
      url: '/settings',
      // items: [
      //   {
      //     title: 'General',
      //     url: '/settings/general',
      //   },
      //   {
      //     title: 'Policies',
      //     url: '/settings/policies',
      //   },
      //   {
      //     title: 'Libraries',
      //     url: '/settings/libraries',
      //   },
      //   {
      //     title: 'Indexers',
      //     url: '/settings/indexers',
      //   },
      //   {
      //     title: 'Downloaders',
      //     url: '/settings/downloaders',
      //   },
      //   {
      //     title: 'Name Templates',
      //     url: '/settings/name-templates',
      //   },
      // ],
    },
  ],
}

const isItemActive = (url: string, items?: Array<{ url: string }>) => {
  // If item has children (like Settings), check if current path matches parent or any child
  if (items && items.length > 0) {
    // Parent is active if current path matches parent URL or any child URL
    return (
      currentPath.value === url ||
      items.some(
        (item) => currentPath.value === item.url || currentPath.value.startsWith(item.url + '/'),
      )
    )
  }
  // For items without children, exact match only
  // Special handling for root path to avoid matching everything
  if (url === '/') {
    return currentPath.value === '/'
  }
  return currentPath.value === url
}

const isSubItemActive = (url: string) => {
  // Sub-items match exactly or if path starts with sub-item URL (for nested routes)
  return currentPath.value === url || currentPath.value.startsWith(url + '/')
}

// Reactive object to store open state for each collapsible item
const collapsibleOpenStates = reactive<Record<string, boolean>>({})

// Function to get open state for a collapsible
const getCollapsibleOpen = (title: string) => {
  const item = data.navMain.find((i) => i.title === title)
  if (!item?.items) return false

  // Check if any child is active
  const isActive = isItemActive(item.url, item.items)

  // Initialize if not set
  if (!(title in collapsibleOpenStates)) {
    collapsibleOpenStates[title] = isActive
  }

  return collapsibleOpenStates[title]
}

// Watch current path and update collapsible states reactively
watch(
  currentPath,
  () => {
    data.navMain.forEach((item) => {
      if (item.items) {
        const isActive = isItemActive(item.url, item.items)
        // Update the reactive state
        collapsibleOpenStates[item.title] = isActive
      }
    })
  },
  { immediate: true },
)
</script>

<template>
  <Sidebar v-bind="props">
    <SidebarHeader>
      <SidebarMenu>
        <SidebarMenuItem>
          <SidebarMenuButton size="lg" as-child>
            <a href="#">
              <div
                class="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground"
              >
                <Popcorn class="size-6" />
              </div>
              <div class="grid flex-1 text-left text-sm leading-tight">
                <span class="truncate font-medium">Arrflix</span>
              </div>
            </a>
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarHeader>
    <SidebarContent>
      <SidebarGroup>
        <SidebarMenu>
          <template v-for="item in data.navMain" :key="item.title">
            <Collapsible
              v-if="item.items"
              :key="item.title"
              as-child
              :open="getCollapsibleOpen(item.title)"
              @update:open="collapsibleOpenStates[item.title] = $event"
              class="group/collapsible"
            >
              <SidebarMenuItem>
                <CollapsibleTrigger as-child>
                  <SidebarMenuButton
                    :is-active="isItemActive(item.url, item.items)"
                    :tooltip="item.title"
                    as-child
                  >
                    <RouterLink :to="item.url">
                      <component :is="item.icon" v-if="item.icon" />
                      <span>{{ item.title }}</span>
                      <ChevronRight
                        class="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90"
                      />
                    </RouterLink>
                  </SidebarMenuButton>
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <SidebarMenuSub>
                    <SidebarMenuSubItem v-for="subItem in item.items" :key="subItem.title">
                      <SidebarMenuSubButton :is-active="isSubItemActive(subItem.url)">
                        <RouterLink class="w-full" :to="subItem.url">
                          <span>{{ subItem.title }}</span>
                        </RouterLink>
                      </SidebarMenuSubButton>
                    </SidebarMenuSubItem>
                  </SidebarMenuSub>
                </CollapsibleContent>
              </SidebarMenuItem>
            </Collapsible>
            <SidebarMenuItem v-else>
              <SidebarMenuButton
                :is-active="isItemActive(item.url) || isSubItemActive(item.url)"
                as-child
              >
                <RouterLink :to="item.url">
                  <component :is="item.icon" v-if="item.icon" />
                  <span>{{ item.title }}</span>
                </RouterLink>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </template>
        </SidebarMenu>
      </SidebarGroup>
    </SidebarContent>
    <SidebarFooter>
      <NavUser :user="data.user" />
    </SidebarFooter>
    <SidebarRail />
  </Sidebar>
</template>
