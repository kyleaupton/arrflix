<script setup lang="ts">
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
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
    <router-view v-else-if="route.meta.public" v-slot="{ Component: publicComponent, route: publicRoute }">
      <Transition name="page" mode="out-in">
        <component :is="publicComponent" :key="publicRoute.path" />
      </Transition>
    </router-view>
    <ImmersiveLayout v-else-if="authStore.isAuthenticated">
      <router-view v-slot="{ Component, route: resolvedRoute }">
        <Transition name="page" mode="out-in">
          <component :is="Component" :key="resolvedRoute.path" />
        </Transition>
      </router-view>
    </ImmersiveLayout>
    <router-view v-else v-slot="{ Component: fallbackComponent, route: fallbackRoute }">
      <Transition name="page" mode="out-in">
        <component :is="fallbackComponent" :key="fallbackRoute.path" />
      </Transition>
    </router-view>
  </TooltipProvider>
</template>
