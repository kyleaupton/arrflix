<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import ImmersiveNavbar from './ImmersiveNavbar.vue'
import SearchDialog from '@/components/search/SearchDialog.vue'

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
    <ImmersiveNavbar @open-search="searchOpen = true" />
    <SearchDialog v-model:open="searchOpen" />
    <main class="min-w-0">
      <slot />
    </main>
  </div>
</template>
