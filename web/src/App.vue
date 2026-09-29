<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { START_LOCATION, useRouter, useRoute } from 'vue-router'
import { isPopNavigation, keepAliveViews } from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { connect as connectRealtime, reset as resetRealtime } from '@/realtime/connection'
import ImmersiveLayout from '@/layouts/ImmersiveLayout.vue'
import DialogContainer from '@/components/DialogContainer.vue'
import SafeAreaOverlay from '@/components/debug/SafeAreaOverlay.vue'
import { TooltipProvider } from '@/components/ui/tooltip'
import { useDebugStore } from '@/stores/debug'

import 'vue-sonner/style.css'
import { toast } from 'vue-sonner'
import { Toaster } from '@/components/ui/sonner'
import { usePwaUpdate } from '@/composables/usePwaUpdate'
import { useSessionKeepalive } from '@/composables/useSessionKeepalive'

const authStore = useAuthStore()
const debug = useDebugStore()

// Refresh the session on foreground/focus so a resumed PWA (notably iOS
// standalone) never sits on an expired access token.
useSessionKeepalive()
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

// Layout-derived chrome — navbar opacity and content padding — is committed once
// the outgoing page has been removed (on after-leave), so it flips in lockstep
// with the incoming page rather than snapping to the new route's layout a frame
// early. Two navigations never reach that hook and commit here instead: the
// initial one, which has no outgoing page to leave (main.ts mounts before the
// first navigation finalizes, so `route` is still START_LOCATION during setup
// and the seed below reads empty meta), and pops, which have no transition to
// stay in lockstep with.
const committedLayout = ref(route.meta.layout as string | undefined)
const commitLayout = (layout: unknown) => {
  committedLayout.value = layout as string | undefined
}
router.afterEach((to, from) => {
  if (from === START_LOCATION || isPopNavigation.value) commitLayout(to.meta.layout)
})

const navbarOpaque = computed(() => committedLayout.value !== 'immersive')
const contentPadded = computed(
  () => !['immersive', 'sidebar'].includes(committedLayout.value ?? ''),
)

// Safari's swipe-back animates the traversal itself, against a snapshot it drops
// the moment the traversal commits. Running our own reveal on top lands the page
// after that handoff, which reads as a flash of the page just swiped away — so a
// pop swaps with no transition at all. `css: false` (rather than a transition
// whose classes happen to be unstyled) is what makes that swap free: it leaves
// Vue with no leave hook, so the old page is removed synchronously rather than a
// frame later, and the new one is mounted in the same patch — one paint, no gap.
//
// `out-in` MUST be dropped alongside it. That mode resolves the pending update
// from `afterLeave`, which a synchronous leave fires mid-patch, re-entering the
// parent's own update while its subtree element is still null (`Cannot read
// properties of null (reading 'nextSibling')`). It also isn't wanted here: the
// gap it exists to create is the black frame this whole change removes.
const pageTransitionCss = computed(() => !isPopNavigation.value)
const pageTransitionMode = computed(() => (isPopNavigation.value ? undefined : 'out-in'))

// A new build has precached and is waiting to take over. Offer the reload rather
// than forcing it — a stable toast id keeps a single prompt if the check refires.
const { needRefresh, applyUpdate } = usePwaUpdate()
watch(
  needRefresh,
  (ready) => {
    if (!ready) return
    toast('A new version is available', {
      id: 'pwa-update',
      duration: Infinity,
      action: {
        label: 'Reload',
        onClick: () => {
          void applyUpdate()
        },
      },
    })
  },
  { immediate: true },
)
</script>

<template>
  <TooltipProvider>
    <Toaster position="top-center" />
    <DialogContainer />
    <SafeAreaOverlay v-if="debug.anyActive" />
    <div v-if="!appStore.isReady" class="flex min-h-svh items-center justify-center">
      <div class="text-muted-foreground">Loading...</div>
    </div>
    <router-view
      v-else-if="route.meta.public"
      v-slot="{ Component: publicComponent, route: publicRoute }"
    >
      <Transition name="page" :mode="pageTransitionMode" :css="pageTransitionCss">
        <component :is="publicComponent" :key="publicRoute.path" />
      </Transition>
    </router-view>
    <ImmersiveLayout v-else-if="authStore.isAuthenticated" :navbar-opaque="navbarOpaque">
      <!-- The padding wrapper sits outside the <Transition> on purpose: it must
           never be re-created, or the <KeepAlive> within it would lose its cache
           on every page swap. -->
      <div :class="contentPadded && 'px-4 pb-4 pt-[calc(env(safe-area-inset-top)_+_5rem)]'">
        <router-view v-slot="{ Component, route: resolvedRoute }">
          <Transition
            name="page"
            :mode="pageTransitionMode"
            :css="pageTransitionCss"
            @after-leave="commitLayout(route.meta.layout)"
          >
            <!--
              Listed views keep their instance and DOM, so returning to one is a
              re-activation rather than a remount — no refetch, no image decode,
              no replayed reveal animation, nothing to paint before it's on screen.

              The component is keyed by the top-level matched segment, not the full
              path, so the transition only fires on genuine page swaps. Nested
              layouts (settings) and param routes (movie→movie) keep the same
              depth-0 component, so they don't remount/flash — their own inner
              views handle the swap. Keep this comment outside <KeepAlive>: Vue
              preserves template comments in dev, and a second child vnode trips
              its single-child assertion.
            -->
            <KeepAlive :include="keepAliveViews">
              <component
                :is="Component"
                :key="resolvedRoute.matched[0]?.path ?? resolvedRoute.path"
              />
            </KeepAlive>
          </Transition>
        </router-view>
      </div>
    </ImmersiveLayout>
    <router-view v-else v-slot="{ Component: fallbackComponent, route: fallbackRoute }">
      <Transition name="page" :mode="pageTransitionMode" :css="pageTransitionCss">
        <component :is="fallbackComponent" :key="fallbackRoute.path" />
      </Transition>
    </router-view>
  </TooltipProvider>
</template>
