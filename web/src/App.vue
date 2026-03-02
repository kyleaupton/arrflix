<script setup lang="ts">
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import AdminLayout from '@/layouts/AdminLayout.vue'
import ImmersiveLayout from '@/layouts/ImmersiveLayout.vue'
import DialogContainer from '@/components/DialogContainer.vue'
import { TooltipProvider } from '@/components/ui/tooltip'

import 'vue-sonner/style.css'
import { Toaster } from '@/components/ui/sonner'

const authStore = useAuthStore()
const appStore = useAppStore()
const router = useRouter()
const route = useRoute()

// Bootstrap already ran in main.ts — just handle setup redirect
if (appStore.needsSetup && route.path !== '/setup') {
  router.push('/setup')
}

const layoutComponent = computed(() => {
  return route.meta.layout === 'immersive' ? ImmersiveLayout : AdminLayout
})
</script>

<template>
  <TooltipProvider>
    <Toaster position="top-center" />
    <DialogContainer />
    <div
      v-if="!appStore.isReady"
      class="flex min-h-svh items-center justify-center"
    >
      <div class="text-muted-foreground">Loading...</div>
    </div>
    <router-view v-else-if="route.meta.public" />
    <component v-else-if="authStore.isAuthenticated" :is="layoutComponent">
      <router-view />
    </component>
    <router-view v-else />
  </TooltipProvider>
</template>
