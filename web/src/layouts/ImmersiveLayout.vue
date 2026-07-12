<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import ImmersiveNavbar from './ImmersiveNavbar.vue'
import ImmersiveTabBar from './ImmersiveTabBar.vue'
import SearchDialog from '@/components/search/SearchDialog.vue'

defineProps<{
  navbarOpaque?: boolean
}>()

const searchOpen = ref(false)

const handleKeydown = (e: KeyboardEvent) => {
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault()
    searchOpen.value = true
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
})
</script>

<template>
  <!-- min-h-lvh, not svh/dvh: in an iOS standalone PWA the dynamic viewport (dvh)
       stops ~44px above the physical screen bottom, so a fixed bottom:0 bar drifts up
       on pages too short to scroll. lvh equals the full screen, so forcing the page to
       at least lvh lets even short pages reach the bottom, keeping the bar pinned. -->
  <div class="min-h-lvh">
    <ImmersiveNavbar :opaque="navbarOpaque" @open-search="searchOpen = true" />
    <SearchDialog v-model:open="searchOpen" />
    <!-- The window scrolls. The navbar overlays the top (fixed) so immersive heroes
         bleed up under it; the tab bar is fixed to the bottom of the screen. Bottom
         clearance keeps content clear of that bar (its height plus the home-indicator
         safe area); zeroed once the bar is gone at sm. -->
    <main class="min-w-0 pb-[calc(3.75rem_+_var(--tabbar-inset-bottom))] sm:pb-0">
      <slot />
    </main>
    <ImmersiveTabBar />
  </div>
</template>
