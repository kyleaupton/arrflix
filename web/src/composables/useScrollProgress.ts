import { ref, onMounted, onUnmounted, watch, type Ref } from 'vue'

// Tracks scroll progress toward a threshold, driving the immersive navbar's
// transparent→opaque fade. The scroll source is an inner container when the app
// runs its full-height shell (the tab bar is layout-pinned, so the app scrolls
// inside `<main>` rather than the window); it falls back to the window when no
// target is provided.
export function useScrollProgress(threshold = 300, target?: Ref<HTMLElement | null>) {
  const scrollY = ref(0)
  const progress = ref(0)

  let ticking = false

  const update = () => {
    scrollY.value = target?.value ? target.value.scrollTop : window.scrollY
    progress.value = Math.min(scrollY.value / threshold, 1)
    ticking = false
  }

  const onScroll = () => {
    if (!ticking) {
      requestAnimationFrame(update)
      ticking = true
    }
  }

  // Bind to whichever source is current, moving the listener if the target
  // element mounts after this composable runs (parent and navbar mount together,
  // so the ref can still be null on first bind).
  let bound: HTMLElement | Window | null = null
  const bind = () => {
    const el: HTMLElement | Window = target?.value ?? window
    if (bound === el) return
    bound?.removeEventListener('scroll', onScroll)
    bound = el
    bound.addEventListener('scroll', onScroll, { passive: true })
    update()
  }

  onMounted(bind)
  if (target) watch(target, bind)

  onUnmounted(() => {
    bound?.removeEventListener('scroll', onScroll)
  })

  return { scrollY, progress }
}
