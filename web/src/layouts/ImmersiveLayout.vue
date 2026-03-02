<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import ImmersiveNavbar from './ImmersiveNavbar.vue'
import SearchDialog from '@/components/search/SearchDialog.vue'

const route = useRoute()
const searchOpen = ref(false)
const isHeroPage = computed(() => route.meta.layout === 'immersive')

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
    <ImmersiveNavbar :opaque="!isHeroPage" @open-search="searchOpen = true" />
    <SearchDialog v-model:open="searchOpen" />
    <main class="min-w-0" :class="!isHeroPage && 'pt-20 px-4 pb-4'">
      <slot />
    </main>
  </div>
</template>
