<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { connect as connectRealtime, reset as resetRealtime } from '@/realtime/connection'
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

// One app-wide SSE stream for the whole session: open it once the user is
// authenticated, tear it down on logout. The stream carries all of the user's
// events; the cache bindings (installed in main.ts) keep the query cache live,
// and ephemeral consumers use useRealtimeListener — nothing else opens a stream.
watch(
  () => authStore.isAuthenticated,
  (authed) => {
    if (authed) connectRealtime()
    else resetRealtime()
  },
  { immediate: true },
)

// Track navbar opaque state separately so it updates after the leave transition,
// preventing the navbar from snapping before the old page has faded out.
const navbarOpaque = ref(route.meta.layout !== 'immersive')

const onPageAfterLeave = () => {
  navbarOpaque.value = route.meta.layout !== 'immersive'
}
</script>

<template>
  <TooltipProvider>
    <Toaster position="top-center" />
    <DialogContainer />
    <div v-if="!appStore.isReady" class="flex min-h-svh items-center justify-center">
      <div class="text-muted-foreground">Loading...</div>
    </div>
    <router-view
      v-else-if="route.meta.public"
      v-slot="{ Component: publicComponent, route: publicRoute }"
    >
      <Transition name="page" mode="out-in">
        <component :is="publicComponent" :key="publicRoute.path" />
      </Transition>
    </router-view>
    <ImmersiveLayout v-else-if="authStore.isAuthenticated" :navbar-opaque="navbarOpaque">
      <router-view v-slot="{ Component, route: resolvedRoute }">
        <Transition name="page" mode="out-in" @after-leave="onPageAfterLeave">
          <!--
            Key by the top-level matched segment, not the full path, so the
            transition only fires on genuine page swaps. Nested layouts (settings)
            and param routes (movie→movie) keep the same depth-0 component, so
            they no longer remount/flash — their own inner views handle the swap.
          -->
          <div
            :key="resolvedRoute.matched[0]?.path ?? resolvedRoute.path"
            :class="
              !['immersive', 'sidebar'].includes(resolvedRoute.meta.layout as string) &&
              'pt-20 px-4 pb-4'
            "
          >
            <component :is="Component" />
          </div>
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
