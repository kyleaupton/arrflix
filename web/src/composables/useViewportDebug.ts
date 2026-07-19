import { onBeforeUnmount, onMounted, ref } from 'vue'

// Live viewport measurements for the debug overlay. env(safe-area-inset-*) can't
// be read straight off a CSS custom property — getComputedStyle returns the
// unresolved `env(...)` token — so a hidden probe element applies the insets as
// padding and we read the resolved pixels back. 1dvh is measured the same way,
// off a 100dvh probe, since the dynamic viewport unit isn't otherwise readable in
// JS. Everything recomputes on resize / rotate / visual-viewport change.
//
// State is module-level and reference-counted: the overlay and the in-sheet guide
// can both mount at once, and we want a single probe pair + listener set behind
// one shared reactive value rather than one per consumer.

export interface ViewportInsets {
  top: number
  right: number
  bottom: number
  left: number
}

export interface ViewportMetrics {
  innerHeight: number
  visualViewportHeight: number
  /** Pixel size of 1dvh (100dvh probe / 100). */
  dvhPx: number
  devicePixelRatio: number
  standalone: boolean
}

const insets = ref<ViewportInsets>({ top: 0, right: 0, bottom: 0, left: 0 })
const metrics = ref<ViewportMetrics>({
  innerHeight: 0,
  visualViewportHeight: 0,
  dvhPx: 0,
  devicePixelRatio: 1,
  standalone: false,
})

let refCount = 0
let insetProbe: HTMLDivElement | null = null
let dvhProbe: HTMLDivElement | null = null

function measure() {
  if (insetProbe) {
    const cs = getComputedStyle(insetProbe)
    insets.value = {
      top: parseFloat(cs.paddingTop) || 0,
      right: parseFloat(cs.paddingRight) || 0,
      bottom: parseFloat(cs.paddingBottom) || 0,
      left: parseFloat(cs.paddingLeft) || 0,
    }
  }
  const dvh100 = dvhProbe?.getBoundingClientRect().height ?? 0
  metrics.value = {
    innerHeight: window.innerHeight,
    visualViewportHeight: Math.round(window.visualViewport?.height ?? 0),
    dvhPx: dvh100 / 100,
    devicePixelRatio: window.devicePixelRatio,
    standalone:
      window.matchMedia('(display-mode: standalone)').matches ||
      (navigator as unknown as { standalone?: boolean }).standalone === true,
  }
}

function setup() {
  insetProbe = document.createElement('div')
  insetProbe.style.cssText =
    'position:fixed;top:0;left:0;visibility:hidden;pointer-events:none;' +
    'padding-top:env(safe-area-inset-top);padding-right:env(safe-area-inset-right);' +
    'padding-bottom:env(safe-area-inset-bottom);padding-left:env(safe-area-inset-left);'
  dvhProbe = document.createElement('div')
  dvhProbe.style.cssText =
    'position:fixed;top:0;left:0;width:0;height:100dvh;visibility:hidden;pointer-events:none;'
  document.body.append(insetProbe, dvhProbe)

  measure()
  window.addEventListener('resize', measure)
  window.addEventListener('orientationchange', measure)
  window.visualViewport?.addEventListener('resize', measure)
  window.visualViewport?.addEventListener('scroll', measure)
}

function teardown() {
  window.removeEventListener('resize', measure)
  window.removeEventListener('orientationchange', measure)
  window.visualViewport?.removeEventListener('resize', measure)
  window.visualViewport?.removeEventListener('scroll', measure)
  insetProbe?.remove()
  dvhProbe?.remove()
  insetProbe = dvhProbe = null
}

export function useViewportDebug() {
  onMounted(() => {
    if (refCount++ === 0) setup()
  })
  onBeforeUnmount(() => {
    if (--refCount === 0) teardown()
  })
  return { insets, metrics }
}
