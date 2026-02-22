<template>
  <div class="rail space-y-2">
    <div class="rail-header flex items-center justify-between">
      <slot name="title">
        <h2 class="text-xl font-semibold">{{ title }}</h2>
      </slot>
      <div class="flex items-center gap-2">
        <Button
          variant="outline"
          size="icon-sm"
          :disabled="!canScrollPrev"
          aria-label="Scroll left"
          @click="scrollByPage(-1)"
        >
          <ChevronLeft class="size-4" />
        </Button>
        <Button
          variant="outline"
          size="icon-sm"
          :disabled="!canScrollNext"
          aria-label="Scroll right"
          @click="scrollByPage(1)"
        >
          <ChevronRight class="size-4" />
        </Button>
      </div>
    </div>

    <div class="rail-body relative">
      <div
        ref="scroller"
        class="scroller flex gap-3 overflow-x-auto overflow-y-hidden pt-4 pb-8 px-4 -mx-4"
        @scroll="onScroll"
      >
        <slot />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { ref, onMounted, onBeforeUnmount } from 'vue'

defineProps<{
  title: string
}>()

const scroller = ref<HTMLDivElement | null>(null)
const canScrollPrev = ref(false)
const canScrollNext = ref(false)
let resizeObserver: ResizeObserver | null = null
let rafId: number | null = null

const updateScrollState = () => {
  const el = scroller.value
  if (!el) return
  const maxScrollLeft = el.scrollWidth - el.clientWidth - 1
  canScrollPrev.value = el.scrollLeft > 0
  canScrollNext.value = el.scrollLeft < maxScrollLeft
}

const onScroll = () => {
  if (rafId != null) cancelAnimationFrame(rafId)
  rafId = requestAnimationFrame(updateScrollState)
}

const scrollByPage = (direction: number) => {
  const el = scroller.value
  if (!el) return
  const page = Math.max(1, el.clientWidth - 64)
  el.scrollBy({ left: direction * page, behavior: 'smooth' })
}

onMounted(() => {
  updateScrollState()
  resizeObserver = new ResizeObserver(updateScrollState)
  if (scroller.value) {
    resizeObserver.observe(scroller.value)
  }
})

onBeforeUnmount(() => {
  if (resizeObserver && scroller.value) {
    resizeObserver.unobserve(scroller.value)
    resizeObserver.disconnect()
    resizeObserver = null
  }
})
</script>

<style scoped>
.scroller {
  scrollbar-width: none; /* Firefox */
}

.scroller::-webkit-scrollbar {
  display: none; /* Chrome/Safari */
}
</style>
