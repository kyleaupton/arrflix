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
  <div class="min-h-svh">
    <ImmersiveNavbar :opaque="navbarOpaque" @open-search="searchOpen = true" />
    <SearchDialog v-model:open="searchOpen" />
    <!-- Bottom clearance keeps content clear of the fixed mobile tab bar (its own
         height plus the home-indicator safe area); zeroed once the bar is gone at sm. -->
    <main class="min-w-0 pb-[calc(3.75rem+env(safe-area-inset-bottom))] sm:pb-0">
      <slot />
    </main>
    <ImmersiveTabBar />
  </div>
</template>
