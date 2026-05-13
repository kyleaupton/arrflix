<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Popcorn, Search } from 'lucide-vue-next'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { KbdGroup, Kbd } from '@/components/ui/kbd'
import { useScrollProgress } from '@/composables/useScrollProgress'
import { isMac } from '@/lib/platform'
import ImmersiveNavUser from './ImmersiveNavUser.vue'

const props = defineProps<{
  opaque?: boolean
}>()

const emit = defineEmits<{
  openSearch: []
}>()

const route = useRoute()
const { progress } = useScrollProgress(300)

const navbarStyle = computed(() => {
  if (props.opaque) {
    return {
      backgroundColor: 'oklch(0.145 0 0 / 0.85)',
      backdropFilter: 'blur(20px)',
      WebkitBackdropFilter: 'blur(20px)',
    }
  }
  return {
    backgroundColor: `oklch(0.145 0 0 / ${progress.value * 0.85})`,
    backdropFilter: `blur(${progress.value * 20}px)`,
    WebkitBackdropFilter: `blur(${progress.value * 20}px)`,
  }
})

const navLinks = [
  { label: 'Home', to: '/' },
  { label: 'Library', to: '/library' },
  { label: 'Downloads', to: '/downloads' },
]

const isLinkActive = (to: string) => {
  if (to === '/') return route.path === '/'
  return route.path.startsWith(to)
}
</script>

<template>
  <header
    class="fixed top-0 left-0 right-0 z-50 h-14 flex items-center px-6 transition-colors"
    :class="{ 'border-b border-border/50': opaque }"
    :style="navbarStyle"
  >
    <!-- Logo -->
    <RouterLink to="/" class="flex items-center gap-2 mr-8">
      <div
        class="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground"
      >
        <Popcorn class="size-5" />
      </div>
      <span class="font-medium text-sm text-foreground">Arrflix</span>
    </RouterLink>

    <!-- Nav links -->
    <nav class="flex items-center gap-1">
      <RouterLink
        v-for="link in navLinks"
        :key="link.to"
        :to="link.to"
        :class="[
          'px-3 py-1.5 rounded-md text-sm font-medium transition-colors',
          isLinkActive(link.to)
            ? 'text-foreground bg-white/10'
            : 'text-foreground/70 hover:text-foreground hover:bg-white/5',
        ]"
      >
        {{ link.label }}
      </RouterLink>
    </nav>

    <!-- Spacer -->
    <div class="flex-1" />

    <!-- Search button -->
    <Tooltip>
      <TooltipTrigger as-child>
        <button
          class="flex items-center gap-2 px-3 py-1.5 rounded-md text-sm text-foreground/70 hover:text-foreground hover:bg-white/5 transition-colors mr-2"
          @click="emit('openSearch')"
        >
          <Search class="size-4" />
        </button>
      </TooltipTrigger>
      <TooltipContent>
        <div class="flex items-center gap-1.5">
          <span>Search</span>
          <KbdGroup>
            <Kbd>{{ isMac ? '⌘' : 'Ctrl' }}</Kbd>
            <Kbd>K</Kbd>
          </KbdGroup>
        </div>
      </TooltipContent>
    </Tooltip>

    <!-- User avatar -->
    <ImmersiveNavUser />
  </header>
</template>
