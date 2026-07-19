import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'

// Developer overlays, toggled from Preferences ▸ Developer. Persisted in
// localStorage rather than gated on import.meta.env.DEV so the toggles survive
// in a prod-like/PWA build — where DEV is false — which is exactly where iOS
// safe-area padding has to be dialed in.
const STORAGE_KEY = 'arrflix:debug'

interface DebugFlags {
  /** Translucent bars sized to each env(safe-area-inset-*). */
  safeArea: boolean
  /** Readout panel: insets, innerHeight, visualViewport, 1dvh, dpr, standalone. */
  metrics: boolean
}

const DEFAULTS: DebugFlags = { safeArea: false, metrics: false }

function load(): DebugFlags {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) return { ...DEFAULTS, ...(JSON.parse(raw) as Partial<DebugFlags>) }
  } catch {
    // Malformed/blocked storage falls back to defaults — a debug toggle is never
    // worth throwing over.
  }
  return { ...DEFAULTS }
}

export const useDebugStore = defineStore('debug', () => {
  const initial = load()
  const safeArea = ref(initial.safeArea)
  const metrics = ref(initial.metrics)

  watch([safeArea, metrics], ([sa, m]) => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ safeArea: sa, metrics: m }))
    } catch {
      // Ignore — persistence is a convenience, not a requirement.
    }
  })

  // Whether anything is on — used to mount the overlay (and its viewport probes)
  // only while a developer is actually looking.
  const anyActive = computed(() => safeArea.value || metrics.value)

  return { safeArea, metrics, anyActive }
})
