import { ref, onMounted, onUnmounted } from 'vue'

export function useScrollProgress(threshold = 300) {
  const scrollY = ref(0)
  const progress = ref(0)

  let ticking = false

  const update = () => {
    scrollY.value = window.scrollY
    progress.value = Math.min(scrollY.value / threshold, 1)
    ticking = false
  }

  const onScroll = () => {
    if (!ticking) {
      requestAnimationFrame(update)
      ticking = true
    }
  }

  onMounted(() => {
    window.addEventListener('scroll', onScroll, { passive: true })
    update() // capture initial position
  })

  onUnmounted(() => {
    window.removeEventListener('scroll', onScroll)
  })

  return { scrollY, progress }
}
