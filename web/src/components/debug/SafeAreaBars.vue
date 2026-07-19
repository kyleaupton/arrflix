<script setup lang="ts">
import { useDebugStore } from '@/stores/debug'
import { useViewportDebug } from '@/composables/useViewportDebug'

// The safe-area inset bars on their own, shared by two mount points:
//   - SafeAreaOverlay (app root) — normal stacking layer.
//   - BottomSheet's <dialog> — the browser top layer, so the guide paints over an
//     open sheet (a native modal <dialog> that would otherwise cover a normal-layer
//     overlay). The dialog fills the viewport at translate:0, so a fixed inset-0
//     child aligns to the real screen edges.
// Self-gates on the debug flag, so either caller can mount it unconditionally.
const debug = useDebugStore()
const { insets } = useViewportDebug()
</script>

<template>
  <!-- pointer-events-none so it never intercepts taps on the UI (or sheet) below.
       z-index sits above a BottomSheet's own content when mounted inside its
       <dialog>; harmless in the app-root overlay. -->
  <div v-if="debug.safeArea" class="pointer-events-none fixed inset-0 z-[60]">
    <!-- Top/bottom insets in fuchsia, left/right in cyan, so opposing edges stay
         visually distinct where they overlap in a corner. -->
    <div
      class="absolute inset-x-0 top-0 border-b border-fuchsia-500/70 bg-fuchsia-500/25"
      :style="{ height: insets.top + 'px' }"
    />
    <div
      class="absolute inset-x-0 bottom-0 border-t border-fuchsia-500/70 bg-fuchsia-500/25"
      :style="{ height: insets.bottom + 'px' }"
    />
    <div
      class="absolute inset-y-0 left-0 border-r border-cyan-500/70 bg-cyan-500/25"
      :style="{ width: insets.left + 'px' }"
    />
    <div
      class="absolute inset-y-0 right-0 border-l border-cyan-500/70 bg-cyan-500/25"
      :style="{ width: insets.right + 'px' }"
    />
  </div>
</template>
