<script setup lang="ts">
import { useDebugStore } from '@/stores/debug'
import { useViewportDebug } from '@/composables/useViewportDebug'
import SafeAreaBars from './SafeAreaBars.vue'

// App-root developer overlay for dialing in iOS safe-area padding: the inset bars
// (SafeAreaBars, also mounted inside BottomSheet's top-layer <dialog>) plus a
// numeric readout. Mounted only while a debug flag is on (App.vue), so the
// viewport probes and listeners don't exist otherwise.
const debug = useDebugStore()
const { insets, metrics } = useViewportDebug()

// One decimal is enough; insets are often fractional (e.g. 47.3px).
const px = (n: number) => (Math.round(n * 10) / 10).toString()
</script>

<template>
  <SafeAreaBars />

  <!-- Readout, pinned below the top inset so it clears the notch/status bar. In
       the normal layer, so an open native <dialog> covers it — the in-sheet bars
       carry the guide there; this panel is for the un-obscured views. -->
  <div
    v-if="debug.metrics"
    class="pointer-events-none fixed left-1/2 z-[100] -translate-x-1/2 rounded-md bg-black/85 px-3 py-2 font-mono text-[11px] leading-snug text-white tabular-nums shadow-lg"
    :style="{ top: 'calc(' + insets.top + 'px + 0.5rem)' }"
  >
    <div>
      inset <span class="text-fuchsia-300">T {{ px(insets.top) }}</span>
      <span class="text-cyan-300"> R {{ px(insets.right) }}</span>
      <span class="text-fuchsia-300"> B {{ px(insets.bottom) }}</span>
      <span class="text-cyan-300"> L {{ px(insets.left) }}</span>
    </div>
    <div>innerH {{ metrics.innerHeight }} · vv {{ metrics.visualViewportHeight }}</div>
    <div>1dvh {{ px(metrics.dvhPx) }}px · dpr {{ metrics.devicePixelRatio }}</div>
    <div>standalone {{ metrics.standalone ? 'yes' : 'no' }}</div>
  </div>
</template>
