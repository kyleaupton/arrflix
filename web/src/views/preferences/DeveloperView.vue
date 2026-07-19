<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { useDebugStore } from '@/stores/debug'

// Developer overlays for tuning layout against real device chrome. Intentionally
// not gated on import.meta.env.DEV: these need to be reachable in the prod-like
// PWA build, which is the only place the iOS safe-area insets are non-zero.
const { safeArea, metrics } = storeToRefs(useDebugStore())
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col gap-6">
    <header class="flex flex-col gap-1">
      <h1 class="text-2xl font-semibold">Developer</h1>
      <p class="text-muted-foreground">
        Diagnostic overlays for tuning layout against device safe areas. Toggles persist on this
        device.
      </p>
    </header>

    <Card>
      <CardHeader>
        <CardTitle>Safe-area overlay</CardTitle>
        <CardDescription>Visualize the iOS notch and home-indicator insets.</CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col divide-y">
        <div class="flex items-center gap-4 py-3 first:pt-0 last:pb-0">
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium">Inset bars</p>
            <p class="text-xs text-muted-foreground">
              Draws a translucent band on each edge, sized to env(safe-area-inset-*).
            </p>
          </div>
          <Switch v-model="safeArea" aria-label="Show safe-area inset bars" />
        </div>
        <div class="flex items-center gap-4 py-3 first:pt-0 last:pb-0">
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium">Viewport metrics</p>
            <p class="text-xs text-muted-foreground">
              Live readout of the insets, innerHeight, visualViewport, 1dvh, DPR, and standalone
              state.
            </p>
          </div>
          <Switch v-model="metrics" aria-label="Show viewport metrics readout" />
        </div>
      </CardContent>
    </Card>
  </div>
</template>
