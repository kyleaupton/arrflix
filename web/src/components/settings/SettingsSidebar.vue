<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import { PrimeIcons } from '@/icons'

const route = useRoute()
const router = useRouter()

type SettingsTab =
  | 'general'
  | 'libraries'
  | 'indexers'
  | 'name-templates'
  | 'downloaders'
  | 'routing'
  | 'quality-profiles'

const currentTab = computed<SettingsTab>(() => {
  const path = route.path
  if (path.endsWith('/libraries')) return 'libraries'
  if (path.endsWith('/indexers')) return 'indexers'
  if (path.endsWith('/name-templates')) return 'name-templates'
  if (path.endsWith('/downloaders')) return 'downloaders'
  if (path.endsWith('/routing')) return 'routing'
  if (path.endsWith('/quality-profiles')) return 'quality-profiles'
  return 'general'
})

const navigateToTab = (tab: SettingsTab) => {
  router.push(`/settings/${tab}`)
}

const settingsItems = [
  {
    key: 'general',
    label: 'General',
    icon: PrimeIcons.COG,
    to: '/settings/general',
  },
  {
    key: 'routing',
    label: 'Routing',
    icon: PrimeIcons.SLIDERS_H,
    to: '/settings/routing',
  },
  {
    key: 'quality-profiles',
    label: 'Quality Profiles',
    icon: PrimeIcons.STAR,
    to: '/settings/quality-profiles',
  },
  {
    key: 'libraries',
    label: 'Libraries',
    icon: PrimeIcons.FOLDER,
    to: '/settings/libraries',
  },
  {
    key: 'indexers',
    label: 'Indexers',
    icon: PrimeIcons.SEARCH,
    to: '/settings/indexers',
  },
  {
    key: 'downloaders',
    label: 'Downloaders',
    icon: PrimeIcons.DOWNLOAD,
    to: '/settings/downloaders',
  },
  {
    key: 'name-templates',
    label: 'Name Templates',
    icon: PrimeIcons.FILE_EDIT,
    to: '/settings/name-templates',
  },
]
</script>

<template>
  <div class="settings-sidebar">
    <nav class="settings-nav">
      <Button
        v-for="item in settingsItems"
        :key="item.key"
        :label="item.label"
        :icon="item.icon"
        :severity="currentTab === item.key ? 'primary' : 'secondary'"
        :text="currentTab !== item.key"
        class="settings-nav-item"
        @click="navigateToTab(item.key as SettingsTab)"
      />
    </nav>
  </div>
</template>

<style scoped>
.settings-sidebar {
  width: 280px;
  flex-shrink: 0;
  background: var(--bg);
  border-radius: 12px;
  border: 1px solid var(--p-border-color);
  box-shadow: var(--p-shadow-1);
}

.settings-nav {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding: 1rem 0.5rem;
}

.settings-nav-item {
  justify-content: flex-start;
  text-align: left;
  width: 100%;
  border-radius: 8px;
  transition: all 0.2s ease;
  font-weight: 500;
}

.settings-nav-item:hover {
  background: var(--p-emphasis-background);
}

.settings-nav-item:deep(.p-button-label) {
  font-weight: 500;
}

/* Active state styling */
.settings-nav-item:deep(.p-button.p-button-primary) {
  background: var(--p-primary-color);
  color: var(--p-primary-contrast-color);
  box-shadow: var(--p-shadow-2);
}

.settings-nav-item:deep(.p-button.p-button-primary:hover) {
  background: var(--p-primary-color-hover);
}

/* Mobile: Hide sidebar, will be replaced by horizontal tabs */
@media (max-width: 1023px) {
  .settings-sidebar {
    display: none;
  }
}
</style>
